package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pandapages/api/internal/model"
)

func TestReaderEndpointReturnsOneSafeCoherentPayload(t *testing.T) {
	manager := testSessionManager(t, false, func() time.Time { return testSessionTime })
	author := "Panda Pages Test Fixture"
	level := 2
	chapterKey := strings.Repeat("b", 64)
	chapterOccurrence := 1
	store := &authTestStore{
		accountExists: true,
		readerResponse: model.ReaderStory{
			Slug:     "moonlit-cafe",
			Title:    "Moonlit Café",
			Author:   &author,
			Language: "en-GB",
			Version:  3,
			Segments: []model.ReaderSegment{{
				Ordinal:           4,
				Kind:              "heading",
				HeadingLevel:      &level,
				ContentKey:        chapterKey,
				ContentOccurrence: 1,
				ChapterKey:        &chapterKey,
				ChapterOccurrence: &chapterOccurrence,
				RenderedHTML:      "<h2>世界</h2>",
				WordCount:         1,
			}},
		},
	}
	response := httptest.NewRecorder()
	testHandler(t, store, manager).ServeHTTP(
		response,
		sessionRequest(t, manager, http.MethodGet, "/api/v1/reader/moonlit-cafe"),
	)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if store.readerCalls != 1 || store.readerAccount != testAccountID || store.readerSlug != "moonlit-cafe" {
		t.Fatalf("ReaderStory calls/scope = %d %q %q", store.readerCalls, store.readerAccount, store.readerSlug)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("Reader response is cacheable")
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["version"] != float64(3) || payload["language"] != "en-GB" {
		t.Fatalf("Reader metadata = %#v", payload)
	}
	segments := payload["segments"].([]any)
	segment := segments[0].(map[string]any)
	for _, forbidden := range []string{"id", "storyVersionId", "markdown", "locator"} {
		if _, exists := segment[forbidden]; exists {
			t.Fatalf("Reader segment exposed %q: %#v", forbidden, segment)
		}
	}
	if segment["renderedHtml"] != "<h2>世界</h2>" || segment["contentKey"] != chapterKey {
		t.Fatalf("Reader segment = %#v", segment)
	}
}

func TestReaderEndpointMethodAndFailureContracts(t *testing.T) {
	manager := testSessionManager(t, false, func() time.Time { return testSessionTime })
	tests := []struct {
		name       string
		method     string
		store      *authTestStore
		wantStatus int
		wantAllow  string
	}{
		{name: "missing story", method: http.MethodGet, store: &authTestStore{accountExists: true, readerErr: sql.ErrNoRows}, wantStatus: http.StatusNotFound},
		{name: "store failure", method: http.MethodGet, store: &authTestStore{accountExists: true, readerErr: errors.New("private SQL detail")}, wantStatus: http.StatusInternalServerError},
		{name: "method mismatch", method: http.MethodPost, store: &authTestStore{accountExists: true}, wantStatus: http.StatusMethodNotAllowed, wantAllow: http.MethodGet},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			testHandler(t, test.store, manager).ServeHTTP(
				response,
				sessionRequest(t, manager, test.method, "/api/v1/reader/test-story"),
			)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.wantStatus, response.Body.String())
			}
			if test.wantAllow != "" && response.Header().Get("Allow") != test.wantAllow {
				t.Fatalf("Allow = %q, want %q", response.Header().Get("Allow"), test.wantAllow)
			}
			if strings.Contains(response.Body.String(), "private SQL detail") {
				t.Fatal("raw database error leaked")
			}
		})
	}
}

func TestReaderEndpointAuthenticationAndSessionInfrastructureRemainDistinct(t *testing.T) {
	manager := testSessionManager(t, false, func() time.Time { return testSessionTime })

	unsigned := httptest.NewRecorder()
	testHandler(t, &authTestStore{accountExists: true}, manager).ServeHTTP(
		unsigned,
		httptest.NewRequest(http.MethodGet, "/api/v1/reader/test-story", nil),
	)
	if unsigned.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned status = %d, want 401", unsigned.Code)
	}

	unavailable := httptest.NewRecorder()
	testHandler(t, &authTestStore{accountExistsErr: errors.New("database unavailable")}, manager).ServeHTTP(
		unavailable,
		sessionRequest(t, manager, http.MethodGet, "/api/v1/reader/test-story"),
	)
	if unavailable.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable status = %d, want 503", unavailable.Code)
	}
}

func TestReaderOnePathsAreRemoved(t *testing.T) {
	manager := testSessionManager(t, false, func() time.Time { return testSessionTime })
	store := &authTestStore{accountExists: true}
	for _, path := range []string{
		"/api/v1/story/test-story",
		"/api/v1/story/test-story/segments",
	} {
		response := httptest.NewRecorder()
		testHandler(t, store, manager).ServeHTTP(
			response,
			sessionRequest(t, manager, http.MethodGet, path),
		)
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", path, response.Code)
		}
	}
	if store.readerCalls != 0 {
		t.Fatal("removed Reader 1 path reached Reader Store")
	}
}
