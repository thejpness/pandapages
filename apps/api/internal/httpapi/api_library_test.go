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

func TestLibraryEndpointReturnsProfileScopedResolutionReadModel(t *testing.T) {
	author := "Traditional"
	updatedAt := time.Date(2026, time.July, 19, 12, 0, 0, 0, time.UTC)
	selected := model.ReaderEditionGrowingReaders
	store := &authTestStore{
		libraryResponse: model.ReaderLibraryReadModel{
			UnavailableItemCount: 1,
			Items: []model.ReaderLibraryItem{
				{
					Slug:     "the-three-little-pigs",
					Title:    "The Three Little Pigs",
					Author:   &author,
					Language: "en",
					State:    model.ReaderResolutionSelected,
					EligibleEditions: []model.ReaderLibraryEditionSummary{
						{EditionKey: model.ReaderEditionGrowingReaders, Version: 2, WordCount: 1260, ChapterCount: 4},
						{EditionKey: model.ReaderEditionLittleListeners, Version: 4, WordCount: 620, ChapterCount: 2},
					},
					SelectedEdition: &selected,
					Progress: &model.ReaderLibraryProgressSummary{
						Version:           2,
						Percent:           0.42,
						UpdatedAt:         updatedAt,
						IsResolvedVersion: true,
					},
				},
				{
					Slug:     "the-snow-queen",
					Title:    "The Snow Queen",
					Language: "en-GB",
					State:    model.ReaderResolutionChooser,
					EligibleEditions: []model.ReaderLibraryEditionSummary{
						{EditionKey: model.ReaderEditionGrowingReaders, Version: 3, WordCount: 2450, ChapterCount: 7},
						{EditionKey: model.ReaderEditionStoryExplorers, Version: 5, WordCount: 1700, ChapterCount: 5},
					},
					SelectedEdition: nil,
					Progress: &model.ReaderLibraryProgressSummary{
						Version:           1,
						Percent:           0.6,
						UpdatedAt:         updatedAt.Add(-time.Hour),
						IsResolvedVersion: false,
					},
				},
			},
		},
	}
	response := httptest.NewRecorder()

	testHandler(t, store).ServeHTTP(
		response,
		profileBearerRequest(http.MethodGet, "/api/v1/library"),
	)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("Library response is cacheable")
	}
	if store.libraryCalls != 1 || store.libraryAccount != testAccountID || store.libraryProfile != testProfileID {
		t.Fatalf("Library calls/context = %d/%q/%q", store.libraryCalls, store.libraryAccount, store.libraryProfile)
	}

	var payload struct {
		Items                []map[string]any `json:"items"`
		UnavailableItemCount int64            `json:"unavailableItemCount"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Items) != 2 || payload.UnavailableItemCount != 1 {
		t.Fatalf("Library payload = %#v", payload)
	}

	current := payload.Items[0]
	if current["slug"] != "the-three-little-pigs" ||
		current["state"] != string(model.ReaderResolutionSelected) ||
		current["selectedEdition"] != string(model.ReaderEditionGrowingReaders) {
		t.Fatalf("selected item = %#v", current)
	}
	eligible, ok := current["eligibleEditions"].([]any)
	if !ok || len(eligible) != 2 {
		t.Fatalf("selected eligible editions = %#v", current["eligibleEditions"])
	}
	firstEdition, ok := eligible[0].(map[string]any)
	if !ok || firstEdition["editionKey"] != string(model.ReaderEditionGrowingReaders) ||
		firstEdition["version"] != float64(2) ||
		firstEdition["wordCount"] != float64(1260) ||
		firstEdition["chapterCount"] != float64(4) {
		t.Fatalf("selected edition summary = %#v", eligible[0])
	}
	progress, ok := current["progress"].(map[string]any)
	if !ok || progress["version"] != float64(2) || progress["percent"] != 0.42 ||
		progress["updatedAt"] != "2026-07-19T12:00:00Z" || progress["isResolvedVersion"] != true {
		t.Fatalf("selected progress = %#v", current["progress"])
	}

	chooser := payload.Items[1]
	if chooser["state"] != string(model.ReaderResolutionChooser) || chooser["selectedEdition"] != nil {
		t.Fatalf("chooser item = %#v", chooser)
	}
	chooserProgress, ok := chooser["progress"].(map[string]any)
	if !ok || chooserProgress["isResolvedVersion"] != false {
		t.Fatalf("chooser progress = %#v", chooser["progress"])
	}

	body := response.Body.String()
	for _, forbidden := range []string{
		"storyVersionId",
		"publishedVersion",
		"publishedVersionId",
		"isCurrentVersion",
		"locator",
		"markdown",
		"renderedHtml",
		testAccountID,
		testProfileID,
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("Library response exposed %q: %s", forbidden, body)
		}
	}
}

func TestLibraryEndpointMethodAndSafeFailureContracts(t *testing.T) {
	t.Run("method mismatch", func(t *testing.T) {
		store := &authTestStore{}
		response := httptest.NewRecorder()
		testHandler(t, store).ServeHTTP(
			response,
			profileBearerRequest(http.MethodPost, "/api/v1/library"),
		)

		if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodGet {
			t.Fatalf("response = %d Allow %q; body = %s", response.Code, response.Header().Get("Allow"), response.Body.String())
		}
		if store.libraryCalls != 0 {
			t.Fatal("method mismatch reached ReaderLibrary storage")
		}
	})

	t.Run("profile vanished after middleware", func(t *testing.T) {
		store := &authTestStore{libraryErr: sql.ErrNoRows}
		response := httptest.NewRecorder()
		testHandler(t, store).ServeHTTP(
			response,
			profileBearerRequest(http.MethodGet, "/api/v1/library"),
		)
		if response.Code != http.StatusForbidden ||
			!strings.Contains(response.Body.String(), `"code":"profile_forbidden"`) ||
			response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("response = %d; body = %s", response.Code, response.Body.String())
		}
	})

	t.Run("database failure", func(t *testing.T) {
		store := &authTestStore{
			libraryErr: errors.New("private relation and database detail"),
		}
		response := httptest.NewRecorder()
		testHandler(t, store).ServeHTTP(
			response,
			profileBearerRequest(http.MethodGet, "/api/v1/library"),
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
