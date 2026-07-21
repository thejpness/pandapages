package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pandapages/api/internal/readiness"
)

func TestHealthzIsDependencyFreeLiveness(t *testing.T) {
	store := &authTestStore{readinessErr: errors.New("database must not be consulted")}
	manager := testSessionManager(t, false, func() time.Time { return testSessionTime })

	response := httptest.NewRecorder()
	testHandler(t, store, manager).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if response.Code != http.StatusOK || response.Body.String() != "ok" {
		t.Fatalf("health response = %d %q, want 200 ok", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
	}
	if store.readinessCalls != 0 || store.existsCalls != 0 {
		t.Fatalf("health consulted dependencies: readiness=%d account=%d", store.readinessCalls, store.existsCalls)
	}
}

func TestHealthzRejectsOtherMethods(t *testing.T) {
	store := &authTestStore{}
	manager := testSessionManager(t, false, func() time.Time { return testSessionTime })
	response := httptest.NewRecorder()
	testHandler(t, store, manager).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/healthz", nil))

	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("health method response = %d Allow %q; body = %s", response.Code, response.Header().Get("Allow"), response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("health method Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
	}
	if store.readinessCalls != 0 {
		t.Fatalf("health method rejection consulted readiness %d times", store.readinessCalls)
	}
}

func TestReadyzStableSafeContracts(t *testing.T) {
	const sensitiveDetail = "private-database-host-and-credential-detail"
	tests := []struct {
		name       string
		checkErr   error
		wantStatus int
		wantBody   string
	}{
		{
			name:       "ready",
			wantStatus: http.StatusOK,
			wantBody:   `{"status":"ready"}`,
		},
		{
			name:       "database unavailable",
			checkErr:   fmtError(readiness.ErrDatabaseUnavailable, sensitiveDetail),
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   `{"reason":"database_unavailable","status":"not_ready"}`,
		},
		{
			name:       "schema not ready",
			checkErr:   fmtError(readiness.ErrSchemaNotReady, sensitiveDetail),
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   `{"reason":"schema_not_ready","status":"not_ready"}`,
		},
		{
			name:       "unknown probe failure is database unavailable",
			checkErr:   errors.New(sensitiveDetail),
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   `{"reason":"database_unavailable","status":"not_ready"}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &authTestStore{readinessErr: test.checkErr}
			manager := testSessionManager(t, false, func() time.Time { return testSessionTime })
			response := httptest.NewRecorder()
			testHandler(t, store, manager).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))

			if response.Code != test.wantStatus {
				t.Fatalf("ready response status = %d, want %d; body = %s", response.Code, test.wantStatus, response.Body.String())
			}
			if strings.TrimSpace(response.Body.String()) != test.wantBody {
				t.Fatalf("ready response body = %q, want %q", strings.TrimSpace(response.Body.String()), test.wantBody)
			}
			if strings.Contains(response.Body.String(), sensitiveDetail) {
				t.Fatalf("ready response exposed internal detail: %s", response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("ready response headers = %#v", response.Header())
			}
			if store.readinessCalls != 1 || store.existsCalls != 0 {
				t.Fatalf("ready calls readiness/account = %d/%d, want 1/0", store.readinessCalls, store.existsCalls)
			}
		})
	}
}

func TestReadyzUsesOneTwoSecondDeadline(t *testing.T) {
	var deadline time.Time
	store := &authTestStore{readinessCheck: func(ctx context.Context) error {
		var ok bool
		deadline, ok = ctx.Deadline()
		if !ok {
			return errors.New("missing deadline")
		}
		<-ctx.Done()
		return ctx.Err()
	}}
	manager := testSessionManager(t, false, func() time.Time { return testSessionTime })
	response := httptest.NewRecorder()
	started := time.Now()
	testHandler(t, store, manager).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	elapsed := time.Since(started)

	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"reason":"database_unavailable"`) {
		t.Fatalf("timeout response = %d %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("timeout Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
	}
	if untilDeadline := deadline.Sub(started); untilDeadline < 1900*time.Millisecond || untilDeadline > readinessTimeout+100*time.Millisecond {
		t.Fatalf("readiness deadline = %v after start, want approximately %v", untilDeadline, readinessTimeout)
	}
	if elapsed < 1900*time.Millisecond || elapsed > readinessTimeout+500*time.Millisecond {
		t.Fatalf("readiness timeout elapsed = %v, want bounded by %v", elapsed, readinessTimeout)
	}
}

func TestReadyzRejectsOtherMethodsWithoutProbe(t *testing.T) {
	store := &authTestStore{}
	manager := testSessionManager(t, false, func() time.Time { return testSessionTime })
	response := httptest.NewRecorder()
	testHandler(t, store, manager).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/readyz", nil))

	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("ready method response = %d Allow %q; body = %s", response.Code, response.Header().Get("Allow"), response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("ready method Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
	}
	if store.readinessCalls != 0 {
		t.Fatalf("method rejection consulted readiness %d times", store.readinessCalls)
	}
}

func fmtError(kind error, detail string) error {
	return &readinessTestError{kind: kind, detail: detail}
}

type readinessTestError struct {
	kind   error
	detail string
}

func (err *readinessTestError) Error() string { return err.detail }
func (err *readinessTestError) Unwrap() error { return err.kind }
