package httpadmin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"pandapages/api/internal/model"
)

const (
	testAdminKey = "server-only-admin-key"
	testAccount  = "11111111-1111-4111-8111-111111111111"
)

type fakeAdminStore struct {
	listResponse model.AdminStoriesListResponse
	listCalls    int
	draftRequest model.AdminDraftUpsertRequest
	draftCalls   int
}

func (s *fakeAdminStore) AdminDraftUpsert(_ string, req model.AdminDraftUpsertRequest) (model.AdminDraftUpsertResponse, error) {
	s.draftCalls++
	s.draftRequest = req
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

func (s *fakeAdminStore) AdminListStories(_ string) (model.AdminStoriesListResponse, error) {
	s.listCalls++
	return s.listResponse, nil
}

func addAdminSession(req *http.Request, unlockedValue, accountID string) {
	if unlockedValue != "" {
		req.AddCookie(&http.Cookie{Name: cookieName, Value: unlockedValue})
	}
	if accountID != "" {
		req.AddCookie(&http.Cookie{Name: accountCookieName, Value: accountID})
	}
}

func serveAdmin(t *testing.T, store *fakeAdminStore, method, path string, body []byte, unlockedValue, accountID, adminKey string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	addAdminSession(req, unlockedValue, accountID)
	if adminKey != "" {
		req.Header.Set("X-PP-Admin-Key", adminKey)
	}

	rec := httptest.NewRecorder()
	New(Config{AdminKey: testAdminKey}, store).ServeHTTP(rec, req)
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

	rec := serveAdmin(t, store, http.MethodGet, "/api/v1/admin/stories", nil, "1", testAccount, testAdminKey)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if store.listCalls != 1 {
		t.Fatalf("AdminListStories calls = %d, want 1", store.listCalls)
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
	rec := serveAdmin(t, store, http.MethodGet, "/api/v1/admin/stories", nil, "", "", testAdminKey)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if store.listCalls != 0 {
		t.Fatal("store called for signed-out request")
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("items")) {
		t.Fatalf("signed-out response leaked story data: %s", rec.Body.String())
	}
}

func TestAdminListStoriesRejectsInvalidCredentials(t *testing.T) {
	tests := []struct {
		name          string
		unlockedValue string
		accountID     string
		adminKey      string
		wantStatus    int
	}{
		{name: "missing proxy key", unlockedValue: "1", accountID: testAccount, wantStatus: http.StatusForbidden},
		{name: "invalid proxy key", unlockedValue: "1", accountID: testAccount, adminKey: "client-controlled-key", wantStatus: http.StatusForbidden},
		{name: "invalid or expired session", unlockedValue: "expired", accountID: testAccount, adminKey: testAdminKey, wantStatus: http.StatusUnauthorized},
		{name: "malformed account cookie", unlockedValue: "1", accountID: "not-an-account", adminKey: testAdminKey, wantStatus: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeAdminStore{}
			rec := serveAdmin(t, store, http.MethodGet, "/api/v1/admin/stories", nil, tt.unlockedValue, tt.accountID, tt.adminKey)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if store.listCalls != 0 {
				t.Fatal("store called for rejected request")
			}
		})
	}
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

	rec := serveAdmin(t, store, http.MethodPost, "/api/v1/admin/stories/draft", body, "1", testAccount, testAdminKey)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if store.draftCalls != 1 {
		t.Fatalf("AdminDraftUpsert calls = %d, want 1", store.draftCalls)
	}
	if store.draftRequest.Markdown != markdown {
		t.Fatalf("UTF-8 markdown changed: got %q, want %q", store.draftRequest.Markdown, markdown)
	}
}
