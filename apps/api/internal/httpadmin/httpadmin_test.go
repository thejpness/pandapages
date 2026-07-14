package httpadmin

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pandapages/api/internal/model"
	"pandapages/api/internal/session"
)

const (
	testAdminKey = "server-only-admin-key"
	testAccount  = "11111111-1111-4111-8111-111111111111"
	testSecret   = "test-session-secret-with-at-least-32-bytes"
)

var testNow = time.Date(2026, time.July, 14, 17, 10, 41, 0, time.UTC)

type fakeAdminStore struct {
	accountMissing bool
	accountErr     error
	existsCalls    int
	listResponse   model.AdminStoriesListResponse
	listCalls      int
	listAccount    string
	draftRequest   model.AdminDraftUpsertRequest
	draftCalls     int
	draftAccount   string
}

func (s *fakeAdminStore) AccountExists(accountID string) (bool, error) {
	s.existsCalls++
	if s.accountErr != nil {
		return false, s.accountErr
	}
	return !s.accountMissing && accountID == testAccount, nil
}

func (s *fakeAdminStore) AdminDraftUpsert(accountID string, req model.AdminDraftUpsertRequest) (model.AdminDraftUpsertResponse, error) {
	s.draftCalls++
	s.draftRequest = req
	s.draftAccount = accountID
	return model.AdminDraftUpsertResponse{
		StoryID:        "story-id",
		StoryVersionID: "version-id",
		Slug:           req.Slug,
		Version:        1,
		SegmentsCount:  2,
		RenderedHTML:   "<h1>" + req.Title + "</h1>",
	}, nil
}

func (*fakeAdminStore) AdminPublish(_, _, _ string) error {
	return nil
}

func (*fakeAdminStore) AdminPreview(req model.AdminPreviewRequest) (model.AdminPreviewResponse, error) {
	return model.AdminPreviewResponse{RenderedHTML: "<p>" + req.Markdown + "</p>"}, nil
}

func (s *fakeAdminStore) AdminListStories(accountID string) (model.AdminStoriesListResponse, error) {
	s.listCalls++
	s.listAccount = accountID
	return s.listResponse, nil
}

func newAdminSessionManager(t *testing.T) *session.Manager {
	t.Helper()
	manager, err := session.New(testSecret, false, session.WithClock(func() time.Time { return testNow }))
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	return manager
}

func addAdminSession(t *testing.T, req *http.Request, manager *session.Manager, mode string) {
	t.Helper()
	switch mode {
	case "valid", "tampered":
		token, err := manager.Issue(testAccount)
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		if mode == "tampered" {
			if strings.HasSuffix(token, "A") {
				token = token[:len(token)-1] + "B"
			} else {
				token = token[:len(token)-1] + "A"
			}
		}
		req.AddCookie(&http.Cookie{Name: session.CookieName, Value: token})
	case "legacy":
		req.AddCookie(&http.Cookie{Name: session.LegacyUnlockCookieName, Value: "1"})
		req.AddCookie(&http.Cookie{Name: session.LegacyAccountCookieName, Value: testAccount})
	case "none":
	default:
		t.Fatalf("unknown session mode %q", mode)
	}
}

func serveAdmin(t *testing.T, store *fakeAdminStore, method, path string, body []byte, sessionMode, adminKey string) *httptest.ResponseRecorder {
	t.Helper()

	manager := newAdminSessionManager(t)
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	addAdminSession(t, req, manager, sessionMode)
	if adminKey != "" {
		req.Header.Set("X-PP-Admin-Key", adminKey)
	}

	rec := httptest.NewRecorder()
	New(Config{AdminKey: testAdminKey, Sessions: manager}, store).ServeHTTP(rec, req)
	return rec
}

func TestAdminListStoriesAuthorised(t *testing.T) {
	author := "A. Author"
	store := &fakeAdminStore{
		listResponse: model.AdminStoriesListResponse{
			Items: []model.AdminStoryListItem{{
				Slug:        "safe-story",
				Title:       "Safe Story",
				Author:      &author,
				Language:    "en-GB",
				IsPublished: true,
				UpdatedAt:   "2026-07-12T12:00:00Z",
				CreatedAt:   "2026-07-11T12:00:00Z",
			}},
		},
	}

	rec := serveAdmin(t, store, http.MethodGet, "/api/v1/admin/stories", nil, "valid", testAdminKey)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if store.listCalls != 1 {
		t.Fatalf("AdminListStories calls = %d, want 1", store.listCalls)
	}
	if store.listAccount != testAccount || store.existsCalls != 1 {
		t.Fatalf("verified account/calls = %q/%d", store.listAccount, store.existsCalls)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	items, ok := payload["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items = %#v, want one item", payload["items"])
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("item shape = %#v", items[0])
	}
	for _, forbiddenField := range []string{"markdown", "renderedHtml", "contentHash", "rights", "source"} {
		if _, exists := item[forbiddenField]; exists {
			t.Errorf("unsafe field %q present in admin list item", forbiddenField)
		}
	}
	if item["slug"] != "safe-story" || item["title"] != "Safe Story" || item["isPublished"] != true {
		t.Fatalf("unexpected safe list shape: %#v", item)
	}
}

func TestAdminListStoriesRejectsSignedOutUser(t *testing.T) {
	store := &fakeAdminStore{}
	rec := serveAdmin(t, store, http.MethodGet, "/api/v1/admin/stories", nil, "none", testAdminKey)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if store.listCalls != 0 {
		t.Fatal("store called for signed-out request")
	}
	if store.existsCalls != 0 {
		t.Fatal("account store called for missing session")
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("items")) {
		t.Fatalf("signed-out response leaked story data: %s", rec.Body.String())
	}
}

func TestAdminListStoriesRejectsInvalidCredentials(t *testing.T) {
	tests := []struct {
		name        string
		sessionMode string
		adminKey    string
		wantStatus  int
	}{
		{name: "missing proxy key", sessionMode: "valid", wantStatus: http.StatusForbidden},
		{name: "invalid proxy key", sessionMode: "valid", adminKey: "client-controlled-key", wantStatus: http.StatusForbidden},
		{name: "tampered signed session", sessionMode: "tampered", adminKey: testAdminKey, wantStatus: http.StatusUnauthorized},
		{name: "legacy cookies only", sessionMode: "legacy", adminKey: testAdminKey, wantStatus: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeAdminStore{}
			rec := serveAdmin(t, store, http.MethodGet, "/api/v1/admin/stories", nil, tt.sessionMode, tt.adminKey)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if store.listCalls != 0 {
				t.Fatal("store called for rejected request")
			}
		})
	}
}

func TestAdminRejectsUnknownAccountAndKeepsDatabaseFailuresDistinct(t *testing.T) {
	t.Run("unknown account", func(t *testing.T) {
		store := &fakeAdminStore{accountMissing: true}
		rec := serveAdmin(t, store, http.MethodGet, "/api/v1/admin/stories", nil, "valid", testAdminKey)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
		}
		if store.listCalls != 0 {
			t.Fatal("admin store called for an unknown session account")
		}
	})

	t.Run("database unavailable", func(t *testing.T) {
		store := &fakeAdminStore{accountErr: errors.New("database unavailable")}
		rec := serveAdmin(t, store, http.MethodGet, "/api/v1/admin/stories", nil, "valid", testAdminKey)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
		}
		if store.listCalls != 0 {
			t.Fatal("admin store called while account validation was unavailable")
		}
		if len(rec.Result().Cookies()) != 0 {
			t.Fatal("valid session was cleared because account storage failed")
		}
	})
}

func TestAdminDraftImportAcceptsUTF8PlainText(t *testing.T) {
	store := &fakeAdminStore{}
	const markdown = "# Café Panda 🐼\n\n“Olá”, said the panda. 你好。\n"
	body, err := json.Marshal(model.AdminDraftUpsertRequest{
		Slug:     "cafe-panda",
		Title:    "Café Panda 🐼",
		Markdown: markdown,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	rec := serveAdmin(t, store, http.MethodPost, "/api/v1/admin/stories/draft", body, "valid", testAdminKey)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if store.draftCalls != 1 {
		t.Fatalf("AdminDraftUpsert calls = %d, want 1", store.draftCalls)
	}
	if store.draftAccount != testAccount {
		t.Fatalf("draft account = %q, want %q", store.draftAccount, testAccount)
	}
	if store.draftRequest.Markdown != markdown {
		t.Fatalf("UTF-8 markdown changed: got %q, want %q", store.draftRequest.Markdown, markdown)
	}
}

func TestAdminDraftRejectsOversizedBody(t *testing.T) {
	store := &fakeAdminStore{}
	body := []byte(`{"slug":"large-story","title":"Large Story","markdown":"` +
		strings.Repeat("x", maxJSONBodyBytes) + `"}`)

	rec := serveAdmin(t, store, http.MethodPost, "/api/v1/admin/stories/draft", body, "valid", testAdminKey)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
	if store.draftCalls != 0 {
		t.Fatal("store called for oversized request")
	}
}
