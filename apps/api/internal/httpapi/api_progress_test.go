package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pandapages/api/internal/model"
	"pandapages/api/internal/readercontract"
)

const progressTestKey = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func validProgressBody(percent float64) string {
	return fmt.Sprintf(
		`{"version":2,"locator":{"schema":2,"segment":{"key":"%s","occurrence":1,"ordinal":4,"offset":0.35},"chapter":{"key":"%s","occurrence":1}},"percent":%v}`,
		progressTestKey,
		progressTestKey,
		percent,
	)
}

func TestProgressPutStrictlyValidatesLocatorV2BeforeStore(t *testing.T) {
	manager := testSessionManager(t, false, func() time.Time { return testSessionTime })
	tests := []struct {
		name     string
		body     string
		wantCode string
	}{
		{name: "missing locator", body: `{"version":1,"percent":0.2}`, wantCode: "locator_invalid"},
		{name: "null locator", body: `{"version":1,"locator":null,"percent":0.2}`, wantCode: "locator_invalid"},
		{name: "Reader 1 locator", body: `{"version":1,"locator":{"mode":"scroll","scrollY":2},"percent":0.2}`, wantCode: "bad_json"},
		{name: "wrong schema", body: strings.Replace(validProgressBody(0.2), `"schema":2`, `"schema":1`, 1), wantCode: "locator_invalid"},
		{name: "uppercase key", body: strings.Replace(validProgressBody(0.2), progressTestKey, strings.ToUpper(progressTestKey), 1), wantCode: "locator_invalid"},
		{name: "zero occurrence", body: strings.Replace(validProgressBody(0.2), `"occurrence":1`, `"occurrence":0`, 1), wantCode: "locator_invalid"},
		{name: "zero ordinal", body: strings.Replace(validProgressBody(0.2), `"ordinal":4`, `"ordinal":0`, 1), wantCode: "locator_invalid"},
		{name: "offset above one", body: strings.Replace(validProgressBody(0.2), `"offset":0.35`, `"offset":1.01`, 1), wantCode: "locator_invalid"},
		{name: "partial chapter", body: strings.Replace(validProgressBody(0.2), `,"occurrence":1}}`, `}}`, 1), wantCode: "locator_invalid"},
		{name: "unknown top-level locator field", body: strings.Replace(validProgressBody(0.2), `"schema":2`, `"schema":2,"mode":"scroll"`, 1), wantCode: "bad_json"},
		{name: "unknown segment field", body: strings.Replace(validProgressBody(0.2), `"offset":0.35`, `"offset":0.35,"page":2`, 1), wantCode: "bad_json"},
		{name: "missing percent", body: strings.Replace(validProgressBody(0.2), `,"percent":0.2`, "", 1), wantCode: "percent"},
		{name: "percent below zero", body: validProgressBody(-0.1), wantCode: "percent"},
		{name: "percent above one", body: validProgressBody(1.1), wantCode: "percent"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &authTestStore{accountExists: true}
			request := sessionRequest(t, manager, http.MethodPut, "/api/v1/progress/test-story")
			request.Body = io.NopCloser(strings.NewReader(test.body))
			request.ContentLength = int64(len(test.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()

			testHandler(t, store, manager).ServeHTTP(response, request)

			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusBadRequest, response.Body.String())
			}
			if store.progressPutCalls != 0 {
				t.Fatal("invalid locator reached progress storage")
			}
			var payload struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if payload.Error.Code != test.wantCode {
				t.Fatalf("error code = %q, want %q; body = %s", payload.Error.Code, test.wantCode, response.Body.String())
			}
		})
	}
}

func TestProgressPutResponseFollowsTypedStoreResult(t *testing.T) {
	manager := testSessionManager(t, false, func() time.Time { return testSessionTime })

	for _, test := range []struct {
		name       string
		storeErr   error
		wantStatus int
		wantCode   string
		wantOK     bool
	}{
		{name: "success", wantStatus: http.StatusOK, wantOK: true},
		{name: "not found", storeErr: sql.ErrNoRows, wantStatus: http.StatusNotFound, wantCode: "not_found"},
		{name: "locator mismatch", storeErr: readercontract.ErrLocatorMismatch, wantStatus: http.StatusBadRequest, wantCode: "locator_mismatch"},
		{name: "database failure", storeErr: errors.New("private database detail"), wantStatus: http.StatusInternalServerError, wantCode: "db"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &authTestStore{accountExists: true, progressPutErr: test.storeErr}
			request := sessionRequest(t, manager, http.MethodPut, "/api/v1/progress/test-story")
			body := validProgressBody(0.5)
			request.Body = io.NopCloser(strings.NewReader(body))
			request.ContentLength = int64(len(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()

			testHandler(t, store, manager).ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.wantStatus, response.Body.String())
			}
			if store.progressPutCalls != 1 {
				t.Fatalf("ProgressPut calls = %d, want 1", store.progressPutCalls)
			}
			if store.progressAccount != testAccountID || store.progressSlug != "test-story" || store.progressVersion != 2 {
				t.Fatalf("progress scope = %q/%q/%d", store.progressAccount, store.progressSlug, store.progressVersion)
			}
			if store.progressPercent != 0.5 || store.progressLocator.Schema != 2 || store.progressLocator.Segment.Ordinal != 4 {
				t.Fatalf("typed progress = %#v at %v", store.progressLocator, store.progressPercent)
			}

			var payload map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if got, _ := payload["ok"].(bool); got != test.wantOK {
				t.Fatalf("ok = %v, want %t; body = %s", got, test.wantOK, response.Body.String())
			}
			if test.wantCode != "" {
				errorBody := payload["error"].(map[string]any)
				if errorBody["code"] != test.wantCode {
					t.Fatalf("error code = %#v, want %q", errorBody["code"], test.wantCode)
				}
			}
			if strings.Contains(response.Body.String(), "private database detail") {
				t.Fatal("raw database error leaked into response")
			}
		})
	}
}

func TestProgressGetDistinguishesMissingStoryFromKnownEmptyProgress(t *testing.T) {
	manager := testSessionManager(t, false, func() time.Time { return testSessionTime })

	t.Run("known empty", func(t *testing.T) {
		store := &authTestStore{
			accountExists:    true,
			progressGetState: model.ProgressResponse{Progress: nil},
		}
		response := httptest.NewRecorder()
		testHandler(t, store, manager).ServeHTTP(
			response,
			sessionRequest(t, manager, http.MethodGet, "/api/v1/progress/test-story"),
		)
		if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != `{"progress":null}` {
			t.Fatalf("response = %d %s", response.Code, response.Body.String())
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("progress response is cacheable")
		}
	})

	t.Run("missing story", func(t *testing.T) {
		store := &authTestStore{accountExists: true, progressGetErr: sql.ErrNoRows}
		response := httptest.NewRecorder()
		testHandler(t, store, manager).ServeHTTP(
			response,
			sessionRequest(t, manager, http.MethodGet, "/api/v1/progress/missing"),
		)
		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body = %s", response.Code, response.Body.String())
		}
	})
}

func TestProgressPutRequiresVerifiedSession(t *testing.T) {
	manager := testSessionManager(t, false, func() time.Time { return testSessionTime })
	store := &authTestStore{accountExists: true}
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/progress/test-story",
		strings.NewReader(validProgressBody(0.2)),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	testHandler(t, store, manager).ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusUnauthorized, response.Body.String())
	}
	if store.progressPutCalls != 0 {
		t.Fatal("unauthenticated progress request reached storage")
	}
}
