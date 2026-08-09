package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pandapages/api/internal/model"
)

func TestReaderEndpointReturnsOneSafeCoherentPayload(t *testing.T) {
	author := "Panda Pages Test Fixture"
	level := 2
	chapterKey := strings.Repeat("b", 64)
	chapterOccurrence := 1
	store := &authTestStore{
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
	testHandler(t, store).ServeHTTP(
		response,
		bearerRequest(http.MethodGet, "/api/v1/reader/moonlit-cafe"),
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
	tests := []struct {
		name       string
		method     string
		store      *authTestStore
		wantStatus int
		wantAllow  string
	}{
		{name: "missing story", method: http.MethodGet, store: &authTestStore{readerErr: sql.ErrNoRows}, wantStatus: http.StatusNotFound},
		{name: "store failure", method: http.MethodGet, store: &authTestStore{readerErr: errors.New("private SQL detail")}, wantStatus: http.StatusInternalServerError},
		{name: "method mismatch", method: http.MethodPost, store: &authTestStore{}, wantStatus: http.StatusMethodNotAllowed, wantAllow: http.MethodGet},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			testHandler(t, test.store).ServeHTTP(
				response,
				bearerRequest(test.method, "/api/v1/reader/test-story"),
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

func TestReaderEndpointRequiresBearerIdentity(t *testing.T) {

	unsigned := httptest.NewRecorder()
	testHandler(t, &authTestStore{}).ServeHTTP(
		unsigned,
		httptest.NewRequest(http.MethodGet, "/api/v1/reader/test-story", nil),
	)
	if unsigned.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned status = %d, want 401", unsigned.Code)
	}

	missingAccount := httptest.NewRecorder()
	req := bearerRequest(http.MethodGet, "/api/v1/reader/test-story")
	req.Header.Del("X-PP-Account-ID")
	testHandler(t, &authTestStore{}).ServeHTTP(missingAccount, req)
	if missingAccount.Code != http.StatusBadRequest {
		t.Fatalf("missing account status = %d, want 400", missingAccount.Code)
	}
}

func TestReaderOnePathsAreRemoved(t *testing.T) {
	store := &authTestStore{}
	for _, path := range []string{
		"/api/v1/story/test-story",
		"/api/v1/story/test-story/segments",
	} {
		response := httptest.NewRecorder()
		testHandler(t, store).ServeHTTP(
			response,
			bearerRequest(http.MethodGet, path),
		)
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", path, response.Code)
		}
	}
	if store.readerCalls != 0 {
		t.Fatal("removed Reader 1 path reached Reader Store")
	}
}

func profileReaderRequest(method, path, body string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+testBearer)
	request.Header.Set("X-PP-Account-ID", testAccountID)
	request.Header.Set("X-PP-Profile-ID", testProfileID)
	return request
}

func TestReaderResolutionEndpointReturnsSelectedAndChooserStates(t *testing.T) {
	author := "Panda Pages Test Fixture"
	level := 2
	chapterKey := strings.Repeat("c", 64)
	chapterOccurrence := 1

	selectedStore := &authTestStore{
		readerResolutionResponse: model.ReaderResolution{
			State: model.ReaderResolutionSelected,
			EligibleEditions: []model.ReaderEditionKey{
				model.ReaderEditionGrowingReaders,
				model.ReaderEditionStoryExplorers,
				model.ReaderEditionLittleListeners,
			},
			Story: &model.ReaderResolvedStory{
				ReaderStory: model.ReaderStory{
					Slug:     "moonlit-cafe",
					Title:    "Moonlit Café",
					Author:   &author,
					Language: "en-GB",
					Version:  4,
					Segments: []model.ReaderSegment{{
						Ordinal:           1,
						Kind:              "heading",
						HeadingLevel:      &level,
						ContentKey:        chapterKey,
						ContentOccurrence: 1,
						ChapterKey:        &chapterKey,
						ChapterOccurrence: &chapterOccurrence,
						RenderedHTML:      "<h2>Moonlit Café</h2>",
						WordCount:         2,
					}},
				},
				EditionKey: model.ReaderEditionStoryExplorers,
			},
		},
	}
	selected := httptest.NewRecorder()
	testHandler(t, selectedStore).ServeHTTP(
		selected,
		profileReaderRequest(http.MethodGet, "/api/v1/reader-resolution/moonlit-cafe", ""),
	)
	if selected.Code != http.StatusOK {
		t.Fatalf("selected status = %d; body = %s", selected.Code, selected.Body.String())
	}
	if selectedStore.readerResolutionCalls != 1 ||
		selectedStore.readerResolutionAccount != testAccountID ||
		selectedStore.readerResolutionProfile != testProfileID ||
		selectedStore.readerResolutionSlug != "moonlit-cafe" {
		t.Fatalf(
			"ReaderResolve calls/scope = %d %q/%q %q",
			selectedStore.readerResolutionCalls,
			selectedStore.readerResolutionAccount,
			selectedStore.readerResolutionProfile,
			selectedStore.readerResolutionSlug,
		)
	}
	var selectedPayload map[string]any
	if err := json.Unmarshal(selected.Body.Bytes(), &selectedPayload); err != nil {
		t.Fatalf("decode selected response: %v", err)
	}
	if selectedPayload["state"] != string(model.ReaderResolutionSelected) {
		t.Fatalf("selected state = %#v", selectedPayload)
	}
	story, ok := selectedPayload["story"].(map[string]any)
	if !ok ||
		story["editionKey"] != string(model.ReaderEditionStoryExplorers) ||
		story["version"] != float64(4) ||
		story["slug"] != "moonlit-cafe" {
		t.Fatalf("selected story = %#v", selectedPayload["story"])
	}
	if selected.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("selected Reader resolution is cacheable")
	}

	chooserStore := &authTestStore{
		readerResolutionResponse: model.ReaderResolution{
			State: model.ReaderResolutionChooser,
			EligibleEditions: []model.ReaderEditionKey{
				model.ReaderEditionGrowingReaders,
				model.ReaderEditionStoryExplorers,
			},
			Story: nil,
		},
	}
	chooser := httptest.NewRecorder()
	testHandler(t, chooserStore).ServeHTTP(
		chooser,
		profileReaderRequest(http.MethodGet, "/api/v1/reader-resolution/moonlit-cafe", ""),
	)
	if chooser.Code != http.StatusOK {
		t.Fatalf("chooser status = %d; body = %s", chooser.Code, chooser.Body.String())
	}
	var chooserPayload map[string]any
	if err := json.Unmarshal(chooser.Body.Bytes(), &chooserPayload); err != nil {
		t.Fatalf("decode chooser response: %v", err)
	}
	if chooserPayload["state"] != string(model.ReaderResolutionChooser) ||
		chooserPayload["story"] != nil {
		t.Fatalf("chooser payload = %#v", chooserPayload)
	}
	eligible, ok := chooserPayload["eligibleEditions"].([]any)
	if !ok ||
		len(eligible) != 2 ||
		eligible[0] != string(model.ReaderEditionGrowingReaders) ||
		eligible[1] != string(model.ReaderEditionStoryExplorers) {
		t.Fatalf("chooser eligible editions = %#v", chooserPayload["eligibleEditions"])
	}
}

func TestReaderResolutionEndpointFailureContracts(t *testing.T) {
	for _, test := range []struct {
		name       string
		method     string
		path       string
		store      *authTestStore
		wantStatus int
		wantAllow  string
	}{
		{
			name:       "missing story",
			method:     http.MethodGet,
			path:       "/api/v1/reader-resolution/missing-story",
			store:      &authTestStore{readerResolutionErr: sql.ErrNoRows},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "store failure",
			method:     http.MethodGet,
			path:       "/api/v1/reader-resolution/test-story",
			store:      &authTestStore{readerResolutionErr: errors.New("private SQL detail")},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "method mismatch",
			method:     http.MethodPost,
			path:       "/api/v1/reader-resolution/test-story",
			store:      &authTestStore{},
			wantStatus: http.StatusMethodNotAllowed,
			wantAllow:  http.MethodGet,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			testHandler(t, test.store).ServeHTTP(
				response,
				profileReaderRequest(test.method, test.path, ""),
			)
			if response.Code != test.wantStatus {
				t.Fatalf(
					"status = %d, want %d; body = %s",
					response.Code,
					test.wantStatus,
					response.Body.String(),
				)
			}
			if test.wantAllow != "" && response.Header().Get("Allow") != test.wantAllow {
				t.Fatalf("Allow = %q, want %q", response.Header().Get("Allow"), test.wantAllow)
			}
			if strings.Contains(response.Body.String(), "private SQL detail") {
				t.Fatal("raw Reader resolution database error leaked")
			}
		})
	}
}

func TestReaderEditionOverrideEndpointPersistsAndClearsExplicitChoice(t *testing.T) {
	store := &authTestStore{readerEditionClearRemoved: true}

	put := httptest.NewRecorder()
	testHandler(t, store).ServeHTTP(
		put,
		profileReaderRequest(
			http.MethodPut,
			"/api/v1/reader-edition/moonlit-cafe",
			`{"editionKey":"story-explorers"}`,
		),
	)
	if put.Code != http.StatusOK || !strings.Contains(put.Body.String(), `"ok":true`) {
		t.Fatalf("override PUT = %d %s", put.Code, put.Body.String())
	}
	if store.readerEditionPutCalls != 1 ||
		store.readerEditionPutAccount != testAccountID ||
		store.readerEditionPutProfile != testProfileID ||
		store.readerEditionPutSlug != "moonlit-cafe" ||
		store.readerEditionPutKey != model.ReaderEditionStoryExplorers {
		t.Fatalf(
			"override PUT scope = %d %q/%q %q %q",
			store.readerEditionPutCalls,
			store.readerEditionPutAccount,
			store.readerEditionPutProfile,
			store.readerEditionPutSlug,
			store.readerEditionPutKey,
		)
	}

	clear := httptest.NewRecorder()
	testHandler(t, store).ServeHTTP(
		clear,
		profileReaderRequest(http.MethodDelete, "/api/v1/reader-edition/moonlit-cafe", ""),
	)
	if clear.Code != http.StatusOK || !strings.Contains(clear.Body.String(), `"ok":true`) {
		t.Fatalf("override DELETE = %d %s", clear.Code, clear.Body.String())
	}
	if store.readerEditionClearCalls != 1 ||
		store.readerEditionClearAccount != testAccountID ||
		store.readerEditionClearProfile != testProfileID ||
		store.readerEditionClearSlug != "moonlit-cafe" {
		t.Fatalf(
			"override DELETE scope = %d %q/%q %q",
			store.readerEditionClearCalls,
			store.readerEditionClearAccount,
			store.readerEditionClearProfile,
			store.readerEditionClearSlug,
		)
	}
}

func TestReaderEditionOverrideEndpointRejectsInvalidOrUnavailableChoices(t *testing.T) {
	invalidStore := &authTestStore{}
	invalid := httptest.NewRecorder()
	testHandler(t, invalidStore).ServeHTTP(
		invalid,
		profileReaderRequest(
			http.MethodPut,
			"/api/v1/reader-edition/moonlit-cafe",
			`{"editionKey":"nearest-easier"}`,
		),
	)
	if invalid.Code != http.StatusBadRequest ||
		!strings.Contains(invalid.Body.String(), `"edition_invalid"`) ||
		invalidStore.readerEditionPutCalls != 0 {
		t.Fatalf(
			"invalid override = %d calls=%d body=%s",
			invalid.Code,
			invalidStore.readerEditionPutCalls,
			invalid.Body.String(),
		)
	}

	unavailableStore := &authTestStore{readerEditionPutErr: sql.ErrNoRows}
	unavailable := httptest.NewRecorder()
	testHandler(t, unavailableStore).ServeHTTP(
		unavailable,
		profileReaderRequest(
			http.MethodPut,
			"/api/v1/reader-edition/moonlit-cafe",
			`{"editionKey":"classic"}`,
		),
	)
	if unavailable.Code != http.StatusNotFound ||
		!strings.Contains(unavailable.Body.String(), `"not_found"`) {
		t.Fatalf("unavailable override = %d %s", unavailable.Code, unavailable.Body.String())
	}

	failedStore := &authTestStore{readerEditionPutErr: errors.New("private SQL detail")}
	failed := httptest.NewRecorder()
	testHandler(t, failedStore).ServeHTTP(
		failed,
		profileReaderRequest(
			http.MethodPut,
			"/api/v1/reader-edition/moonlit-cafe",
			`{"editionKey":"classic"}`,
		),
	)
	if failed.Code != http.StatusInternalServerError ||
		strings.Contains(failed.Body.String(), "private SQL detail") {
		t.Fatalf("failed override = %d %s", failed.Code, failed.Body.String())
	}
}
