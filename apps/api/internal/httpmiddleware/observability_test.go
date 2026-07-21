package httpmiddleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

var generatedRequestIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &output
}

func TestObserveAcceptsSafeIncomingRequestIDAndLogsCompletion(t *testing.T) {
	logs := captureLogs(t)
	const requestID = "browser.Request_ID-123"
	const secret = "must-not-appear-in-logs"

	var contextRequestID string
	handler := Observe(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contextRequestID = RequestIDFromContext(r)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("created"))
	}))

	request := httptest.NewRequest(http.MethodPost, "/safe/path?oauth_code="+secret, nil)
	request.Header.Set(RequestIDHeader, requestID)
	request.Header.Set("Authorization", "Bearer "+secret)
	request.Header.Set("X-PP-Admin-Key", secret)
	request.AddCookie(&http.Cookie{Name: "pp_session", Value: secret})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if got := response.Header().Get(RequestIDHeader); got != requestID {
		t.Fatalf("response request ID = %q, want %q", got, requestID)
	}
	if contextRequestID != requestID {
		t.Fatalf("context request ID = %q, want %q", contextRequestID, requestID)
	}

	records := decodeLogRecords(t, logs.String())
	if len(records) != 1 {
		t.Fatalf("log record count = %d, want 1; logs = %s", len(records), logs.String())
	}
	record := records[0]
	for key, want := range map[string]any{
		"msg":            "http request completed",
		"request_id":     requestID,
		"method":         http.MethodPost,
		"path":           "/safe/path",
		"status":         float64(http.StatusCreated),
		"response_bytes": float64(len("created")),
	} {
		if got := record[key]; got != want {
			t.Errorf("log %s = %#v, want %#v", key, got, want)
		}
	}
	if _, ok := record["duration"]; !ok {
		t.Error("completion log omitted duration")
	}
	if strings.Contains(logs.String(), secret) || strings.Contains(logs.String(), "oauth_code") || strings.Contains(logs.String(), "Authorization") || strings.Contains(logs.String(), "Cookie") {
		t.Fatalf("completion log contains query or credential data: %s", logs.String())
	}
}

func TestObserveGeneratesOrReplacesRequestID(t *testing.T) {
	tests := []struct {
		name   string
		values []string
	}{
		{name: "missing"},
		{name: "contains whitespace", values: []string{"unsafe request id"}},
		{name: "contains control", values: []string{"unsafe\nvalue"}},
		{name: "oversized", values: []string{strings.Repeat("a", maxRequestIDLen+1)}},
		{name: "multiple", values: []string{"first", "second"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_ = captureLogs(t)
			handler := Observe(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			for _, value := range test.values {
				request.Header.Add(RequestIDHeader, value)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			got := response.Header().Get(RequestIDHeader)
			if !generatedRequestIDPattern.MatchString(got) {
				t.Fatalf("generated request ID = %q, want 32 lowercase hex characters", got)
			}
			for _, rejected := range test.values {
				if got == rejected {
					t.Fatalf("unsafe request ID %q was echoed", rejected)
				}
			}
		})
	}
}

func TestObserveRecoversPanicWithRequestIDStackAndOneCompletion(t *testing.T) {
	logs := captureLogs(t)
	const requestID = "panic-correlation-123"
	const panicSecret = "private-panic-value"

	handler := Observe(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(panicSecret)
	}))
	request := httptest.NewRequest(http.MethodGet, "/panic?secret=hidden", nil)
	request.Header.Set(RequestIDHeader, requestID)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body = %s", response.Code, response.Body.String())
	}
	if response.Header().Get(RequestIDHeader) != requestID {
		t.Fatalf("response request ID = %q, want %q", response.Header().Get(RequestIDHeader), requestID)
	}
	if !strings.Contains(response.Body.String(), `"code":"panic"`) || strings.Contains(response.Body.String(), panicSecret) || strings.Contains(response.Body.String(), "goroutine") {
		t.Fatalf("panic response is not the safe contract: %s", response.Body.String())
	}

	records := decodeLogRecords(t, logs.String())
	if len(records) != 2 {
		t.Fatalf("log record count = %d, want panic and completion; logs = %s", len(records), logs.String())
	}
	var panicCount, completionCount int
	for _, record := range records {
		if record["request_id"] != requestID {
			t.Errorf("log request ID = %#v, want %q", record["request_id"], requestID)
		}
		switch record["msg"] {
		case "http request panic recovered":
			panicCount++
			stack, _ := record["stack"].(string)
			if !strings.Contains(stack, "TestObserveRecoversPanicWithRequestIDStackAndOneCompletion") {
				t.Errorf("panic log omitted server-side stack: %#v", record)
			}
		case "http request completed":
			completionCount++
			if record["status"] != float64(http.StatusInternalServerError) {
				t.Errorf("completion status = %#v, want 500", record["status"])
			}
		}
	}
	if panicCount != 1 || completionCount != 1 {
		t.Fatalf("panic/completion counts = %d/%d, want 1/1; logs = %s", panicCount, completionCount, logs.String())
	}
	if strings.Contains(logs.String(), panicSecret) || strings.Contains(logs.String(), "?secret=") {
		t.Fatalf("panic log exposed panic value or query: %s", logs.String())
	}
}

func TestResponseMetricsSupportsResponseControllerUnwrap(t *testing.T) {
	_ = captureLogs(t)
	handler := Observe(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Fatalf("flush through response metrics: %v", err)
		}
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/flush", nil))
	if !response.Flushed {
		t.Fatal("underlying response writer was not flushed")
	}
}

func decodeLogRecords(t *testing.T, raw string) []map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	records := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode log record %q: %v", line, err)
		}
		records = append(records, record)
	}
	return records
}
