package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pandapages/api/internal/model"
)

func TestLibraryEndpointReturnsEnrichedSafeReadModel(t *testing.T) {
	manager := testSessionManager(t, false, func() time.Time { return testSessionTime })
	author := "Traditional"
	updatedAt := time.Date(2026, time.July, 19, 12, 0, 0, 0, time.UTC)
	store := &authTestStore{
		accountExists: true,
		libraryResponse: []model.StoryItem{
			{
				Slug:             "the-three-little-pigs",
				Title:            "The Three Little Pigs",
				Author:           &author,
				Language:         "en",
				PublishedVersion: 2,
				WordCount:        1260,
				ChapterCount:     4,
				Progress: &model.LibraryProgressSummary{
					Version:          2,
					Percent:          0.42,
					UpdatedAt:        updatedAt,
					IsCurrentVersion: true,
				},
			},
			{
				Slug:             "the-snow-queen",
				Title:            "The Snow Queen",
				Language:         "en-GB",
				PublishedVersion: 3,
				WordCount:        2450,
				ChapterCount:     7,
				Progress:         nil,
			},
		},
	}
	response := httptest.NewRecorder()

	testHandler(t, store, manager).ServeHTTP(
		response,
		sessionRequest(t, manager, http.MethodGet, "/api/v1/library"),
	)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("Library response is cacheable")
	}
	if store.libraryCalls != 1 || store.libraryAccount != testAccountID {
		t.Fatalf("Library calls/account = %d/%q", store.libraryCalls, store.libraryAccount)
	}

	var payload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("items = %#v", payload.Items)
	}
	current := payload.Items[0]
	if current["slug"] != "the-three-little-pigs" || current["publishedVersion"] != float64(2) ||
		current["wordCount"] != float64(1260) || current["chapterCount"] != float64(4) {
		t.Fatalf("current item = %#v", current)
	}
	progress, ok := current["progress"].(map[string]any)
	if !ok || progress["version"] != float64(2) || progress["percent"] != 0.42 ||
		progress["updatedAt"] != "2026-07-19T12:00:00Z" || progress["isCurrentVersion"] != true {
		t.Fatalf("current progress = %#v", current["progress"])
	}
	if payload.Items[1]["progress"] != nil {
		t.Fatalf("empty progress = %#v, want null", payload.Items[1]["progress"])
	}

	body := response.Body.String()
	for _, forbidden := range []string{"storyVersionId", "publishedVersionId", "locator", "markdown", "renderedHtml", testAccountID} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("Library response exposed %q: %s", forbidden, body)
		}
	}
}

func TestLibraryEndpointMethodAndSafeFailureContracts(t *testing.T) {
	manager := testSessionManager(t, false, func() time.Time { return testSessionTime })

	t.Run("method mismatch", func(t *testing.T) {
		store := &authTestStore{accountExists: true}
		response := httptest.NewRecorder()
		testHandler(t, store, manager).ServeHTTP(
			response,
			sessionRequest(t, manager, http.MethodPost, "/api/v1/library"),
		)

		if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodGet {
			t.Fatalf("response = %d Allow %q; body = %s", response.Code, response.Header().Get("Allow"), response.Body.String())
		}
		if store.libraryCalls != 0 {
			t.Fatal("method mismatch reached Library storage")
		}
	})

	t.Run("database failure", func(t *testing.T) {
		store := &authTestStore{
			accountExists: true,
			libraryErr:    errors.New("private relation and database detail"),
		}
		response := httptest.NewRecorder()
		testHandler(t, store, manager).ServeHTTP(
			response,
			sessionRequest(t, manager, http.MethodGet, "/api/v1/library"),
		)

		if response.Code != http.StatusInternalServerError || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("response = %d; body = %s", response.Code, response.Body.String())
		}
		if !strings.Contains(response.Body.String(), `"code":"db"`) ||
			!strings.Contains(response.Body.String(), `"message":"library query failed"`) {
			t.Fatalf("safe error body = %s", response.Body.String())
		}
		if strings.Contains(response.Body.String(), "private relation") {
			t.Fatal("raw database error leaked")
		}
	})
}
