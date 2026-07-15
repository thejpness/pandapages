package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProgressPutValidatesLocatorBeforeStore(t *testing.T) {
	manager := testSessionManager(t, false, func() time.Time { return testSessionTime })

	for _, test := range []struct {
		name string
		body string
	}{
		{name: "missing", body: `{"version":1,"percent":0.2}`},
		{name: "null", body: `{"version":1,"locator":null,"percent":0.2}`},
		{name: "string", body: `{"version":1,"locator":"scroll","percent":0.2}`},
		{name: "array", body: `{"version":1,"locator":[],"percent":0.2}`},
		{name: "number", body: `{"version":1,"locator":4,"percent":0.2}`},
	} {
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
			if payload.Error.Code != "locator" {
				t.Fatalf("error code = %q, want locator", payload.Error.Code)
			}
		})
	}
}

func TestProgressPutResponseFollowsStoreResult(t *testing.T) {
	manager := testSessionManager(t, false, func() time.Time { return testSessionTime })

	for _, test := range []struct {
		name       string
		storeErr   error
		wantStatus int
		wantOK     bool
	}{
		{name: "success", wantStatus: http.StatusOK, wantOK: true},
		{name: "not found", storeErr: sql.ErrNoRows, wantStatus: http.StatusNotFound},
		{name: "database failure", storeErr: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &authTestStore{accountExists: true, progressPutErr: test.storeErr}
			body := `{"version":2,"locator":{"mode":"scroll","scrollY":128},"percent":1.5}`
			request := sessionRequest(t, manager, http.MethodPut, "/api/v1/progress/test-story")
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
			if store.progressPercent != 1 {
				t.Fatalf("clamped percent = %v, want 1", store.progressPercent)
			}
			var payload map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if got, _ := payload["ok"].(bool); got != test.wantOK {
				t.Fatalf("ok = %v, want %t; body = %s", got, test.wantOK, response.Body.String())
			}
		})
	}
}

func TestProgressPutRequiresVerifiedSession(t *testing.T) {
	manager := testSessionManager(t, false, func() time.Time { return testSessionTime })
	store := &authTestStore{accountExists: true}
	body := `{"version":1,"locator":{"mode":"scroll","scrollY":128},"percent":0.2}`
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/progress/test-story",
		strings.NewReader(body),
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
