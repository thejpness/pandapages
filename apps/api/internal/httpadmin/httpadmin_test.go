package httpadmin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	draftErr       error
	publishErr     error
	publishCalls   int
	unpublishErr   error
	unpublishCalls int
	detailErr      error
	versionErr     error
	previewErr     error
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
	if s.draftErr != nil {
		return model.AdminDraftUpsertResponse{}, s.draftErr
	}
	return model.AdminDraftUpsertResponse{
		StoryID:        "story-id",
		StoryVersionID: "version-id",
		Slug:           req.Slug,
		VersionID:      "version-id",
		Version:        1,
		SegmentsCount:  2,
		SegmentCount:   2,
		RenderedHTML:   "<h1>" + req.Title + "</h1>",
		Outcome:        model.AdminDraftOutcomeCreatedStory,
	}, nil
}

func (s *fakeAdminStore) AdminPublishStory(_, slug, versionID string) (model.AdminStoryStatusResponse, error) {
	s.publishCalls++
	return model.AdminStoryStatusResponse{
		Slug:   slug,
		Status: model.AdminStoryStatusPublished,
		PublishedVersion: &model.AdminVersionPointerSummary{
			VersionID: versionID,
			Version:   1,
		},
	}, s.publishErr
}

func (s *fakeAdminStore) AdminUnpublish(_, slug string) (model.AdminStoryStatusResponse, error) {
	s.unpublishCalls++
	return model.AdminStoryStatusResponse{Slug: slug, Status: model.AdminStoryStatusDraftOnly}, s.unpublishErr
}

func (s *fakeAdminStore) AdminPreview(req model.AdminPreviewRequest) (model.AdminPreviewResponse, error) {
	return model.AdminPreviewResponse{Slug: req.Slug, Title: req.Title, RenderedHTML: "<p>" + req.Markdown + "</p>"}, s.previewErr
}

func (s *fakeAdminStore) AdminListStories(accountID string) (model.AdminStoriesListResponse, error) {
	s.listCalls++
	s.listAccount = accountID
	return s.listResponse, nil
}

func (s *fakeAdminStore) AdminGetStory(_ string, slug string) (model.AdminStoryDetailResponse, error) {
	return model.AdminStoryDetailResponse{Slug: slug, Status: model.AdminStoryStatusDraftOnly}, s.detailErr
}

func (s *fakeAdminStore) AdminGetVersionSource(_, slug, versionID string) (model.AdminVersionSourceResponse, error) {
	return model.AdminVersionSourceResponse{
		Slug: slug, VersionID: versionID, Version: 1, Health: model.AdminVersionHealthReady,
	}, s.versionErr
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
			Items: []model.AdminStorySummary{{
				Slug:      "safe-story",
				Title:     "Safe Story",
				Author:    &author,
				Language:  "en-GB",
				Rights:    map[string]any{},
				Status:    model.AdminStoryStatusPublished,
				UpdatedAt: "2026-07-12T12:00:00Z",
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
	for _, forbiddenField := range []string{"markdown", "renderedHtml", "contentHash", "segments", "accountId"} {
		if _, exists := item[forbiddenField]; exists {
			t.Errorf("unsafe field %q present in admin list item", forbiddenField)
		}
	}
	if item["slug"] != "safe-story" || item["title"] != "Safe Story" || item["status"] != "published" {
		t.Fatalf("unexpected safe list shape: %#v", item)
	}
}

func TestAdminPublishReturnsSafeValidationFailure(t *testing.T) {
	store := &fakeAdminStore{publishErr: fmt.Errorf("private validation detail: %w", model.ErrAdminPublishInvalid)}
	rec := serveAdmin(
		t,
		store,
		http.MethodPost,
		"/api/v1/admin/stories/empty-story/publish",
		[]byte(`{"versionId":"11111111-1111-4111-8111-111111111111"}`),
		"valid",
		testAdminKey,
	)

	if rec.Code != http.StatusConflict || store.publishCalls != 1 {
		t.Fatalf("publish response/calls = %d/%d; body = %s", rec.Code, store.publishCalls, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"publish_repair_required"`) ||
		!strings.Contains(rec.Body.String(), "story version is unavailable or unreadable") ||
		strings.Contains(rec.Body.String(), "private validation detail") {
		t.Fatalf("safe publish validation response = %s", rec.Body.String())
	}
}

func TestAdminPublishRejectsMalformedVersionIdentifierBeforeStore(t *testing.T) {
	store := &fakeAdminStore{}
	rec := serveAdmin(
		t,
		store,
		http.MethodPost,
		"/api/v1/admin/stories/safe-story/publish",
		[]byte("{\"versionId\":\"not-a-version-id\"}"),
		"valid",
		testAdminKey,
	)

	if rec.Code != http.StatusBadRequest || store.publishCalls != 0 ||
		!strings.Contains(rec.Body.String(), "\"code\":\"publish_invalid\"") {
		t.Fatalf("malformed publish response/calls = %d/%d; body = %s", rec.Code, store.publishCalls, rec.Body.String())
	}
	assertAdminResponseHeaders(t, rec)
}

func TestAdminPublishHidesUnexpectedStorageFailure(t *testing.T) {
	const sensitiveMarker = "SENSITIVE_DATABASE_HOST_RELATION_DETAIL"
	var capturedLogs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&capturedLogs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	store := &fakeAdminStore{publishErr: errors.New(sensitiveMarker)}
	rec := serveAdmin(
		t,
		store,
		http.MethodPost,
		"/api/v1/admin/stories/safe-story/publish",
		[]byte(`{"versionId":"11111111-1111-4111-8111-111111111111"}`),
		"valid",
		testAdminKey,
	)

	response := rec.Result()
	t.Cleanup(func() { _ = response.Body.Close() })
	if rec.Code != http.StatusInternalServerError || response.StatusCode != http.StatusInternalServerError ||
		response.Status != "500 Internal Server Error" || store.publishCalls != 1 {
		t.Fatalf("publish response/calls = %d/%d; body = %s", rec.Code, store.publishCalls, rec.Body.String())
	}
	for key, values := range response.Header {
		if strings.Contains(key, sensitiveMarker) {
			t.Fatalf("unexpected publish failure leaked detail in header key %q", key)
		}
		for _, value := range values {
			if strings.Contains(value, sensitiveMarker) {
				t.Fatalf("unexpected publish failure leaked detail in header %s: %q", key, value)
			}
		}
	}
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode unexpected publish failure: %v", err)
	}
	if payload.Error.Code != "publish_failed" || payload.Error.Message != "story publication failed" {
		t.Fatalf("unexpected publish error envelope = %#v", payload.Error)
	}
	for boundary, value := range map[string]string{
		"decoded code":    payload.Error.Code,
		"decoded message": payload.Error.Message,
		"raw body":        rec.Body.String(),
		"captured logs":   capturedLogs.String(),
	} {
		if strings.Contains(value, sensitiveMarker) {
			t.Fatalf("unexpected publish failure leaked detail in %s: %s", boundary, value)
		}
	}
	if !strings.Contains(capturedLogs.String(), "admin story publication failed") {
		t.Fatalf("unexpected publish failure omitted safe diagnostic: %s", capturedLogs.String())
	}
}

func TestAdminPublishPreservesMissingVersionResponse(t *testing.T) {
	store := &fakeAdminStore{publishErr: fmt.Errorf("private ownership detail: %w", model.ErrAdminPublishNotFound)}
	rec := serveAdmin(
		t,
		store,
		http.MethodPost,
		"/api/v1/admin/stories/missing-story/publish",
		[]byte(`{"versionId":"11111111-1111-4111-8111-111111111111"}`),
		"valid",
		testAdminKey,
	)

	if rec.Code != http.StatusNotFound || store.publishCalls != 1 {
		t.Fatalf("missing publish response/calls = %d/%d; body = %s", rec.Code, store.publishCalls, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"publish_not_found"`) ||
		!strings.Contains(rec.Body.String(), "story version was not found") ||
		strings.Contains(rec.Body.String(), "private ownership detail") {
		t.Fatalf("missing publish response changed or leaked detail: %s", rec.Body.String())
	}
}

func TestAdminDraftReturnsSafeRepairRequiredConflict(t *testing.T) {
	store := &fakeAdminStore{draftErr: fmt.Errorf("private corrupt version detail: %w", model.ErrAdminVersionRepairRequired)}
	body, err := json.Marshal(model.AdminDraftUpsertRequest{Slug: "safe-story", Title: "Safe story", Markdown: "# Safe story"})
	if err != nil {
		t.Fatalf("marshal draft request: %v", err)
	}
	rec := serveAdmin(t, store, http.MethodPost, "/api/v1/admin/stories/draft", body, "valid", testAdminKey)

	if rec.Code != http.StatusConflict || store.draftCalls != 1 {
		t.Fatalf("draft response/calls = %d/%d; body = %s", rec.Code, store.draftCalls, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"draft_repair_required"`) ||
		!strings.Contains(rec.Body.String(), "stored story version requires repair") ||
		strings.Contains(rec.Body.String(), "private corrupt") {
		t.Fatalf("repair-required response leaked detail: %s", rec.Body.String())
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

func TestAdminPreviewReturnsStructuredValidationIssues(t *testing.T) {
	store := &fakeAdminStore{previewErr: &model.AdminValidationError{Issues: []model.AdminValidationIssue{{
		Field: "title", Code: "required", Message: "Enter a title",
	}}}}
	rec := serveAdmin(
		t,
		store,
		http.MethodPost,
		"/api/v1/admin/preview",
		[]byte(`{"slug":"story","title":"","markdown":"# Story"}`),
		"valid",
		testAdminKey,
	)
	if rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), `"code":"preview_invalid"`) ||
		!strings.Contains(rec.Body.String(), `"field":"title"`) ||
		strings.Contains(rec.Body.String(), "admin story input has") {
		t.Fatalf("preview validation response = %d %s", rec.Code, rec.Body.String())
	}
	assertAdminResponseHeaders(t, rec)
}

func TestAdminDetailAndVersionUseSafeScopedErrors(t *testing.T) {
	t.Run("story not found", func(t *testing.T) {
		store := &fakeAdminStore{detailErr: fmt.Errorf("foreign account detail: %w", model.ErrAdminStoryNotFound)}
		rec := serveAdmin(t, store, http.MethodGet, "/api/v1/admin/stories/private-story", nil, "valid", testAdminKey)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `"code":"story_not_found"`) ||
			strings.Contains(rec.Body.String(), "foreign account") {
			t.Fatalf("detail response = %d %s", rec.Code, rec.Body.String())
		}
		assertAdminResponseHeaders(t, rec)
	})

	t.Run("version repair required", func(t *testing.T) {
		store := &fakeAdminStore{versionErr: fmt.Errorf("private invariant: %w", model.ErrAdminVersionRepairRequired)}
		rec := serveAdmin(
			t,
			store,
			http.MethodGet,
			"/api/v1/admin/stories/safe-story/versions/11111111-1111-4111-8111-111111111111",
			nil,
			"valid",
			testAdminKey,
		)
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"code":"version_repair_required"`) ||
			strings.Contains(rec.Body.String(), "private invariant") {
			t.Fatalf("version response = %d %s", rec.Code, rec.Body.String())
		}
		assertAdminResponseHeaders(t, rec)
	})
}

func TestAdminPublishAndUnpublishReturnTypedStatus(t *testing.T) {
	versionID := "11111111-1111-4111-8111-111111111111"
	store := &fakeAdminStore{}
	publish := serveAdmin(
		t,
		store,
		http.MethodPost,
		"/api/v1/admin/stories/safe-story/publish",
		[]byte(`{"versionId":"`+versionID+`"}`),
		"valid",
		testAdminKey,
	)
	if publish.Code != http.StatusOK || !strings.Contains(publish.Body.String(), `"status":"published"`) ||
		!strings.Contains(publish.Body.String(), `"versionId":"`+versionID+`"`) {
		t.Fatalf("publish response = %d %s", publish.Code, publish.Body.String())
	}
	assertAdminResponseHeaders(t, publish)

	unpublish := serveAdmin(
		t,
		store,
		http.MethodPost,
		"/api/v1/admin/stories/safe-story/unpublish",
		nil,
		"valid",
		testAdminKey,
	)
	if unpublish.Code != http.StatusOK || store.unpublishCalls != 1 ||
		!strings.Contains(unpublish.Body.String(), `"status":"draft_only"`) {
		t.Fatalf("unpublish response/calls = %d/%d %s", unpublish.Code, store.unpublishCalls, unpublish.Body.String())
	}
	assertAdminResponseHeaders(t, unpublish)
}

func TestAdminMalformedJSONAndUnexpectedFailuresAreFixedAndSafe(t *testing.T) {
	t.Run("malformed JSON", func(t *testing.T) {
		const marker = "SENSITIVE_UNKNOWN_FIELD"
		rec := serveAdmin(
			t,
			&fakeAdminStore{},
			http.MethodPost,
			"/api/v1/admin/preview",
			[]byte(`{"markdown":"story","`+marker+`":true}`),
			"valid",
			testAdminKey,
		)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"code":"bad_json"`) ||
			strings.Contains(rec.Body.String(), marker) {
			t.Fatalf("bad JSON response = %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("malformed UTF-8", func(t *testing.T) {
		body := append([]byte(`{"slug":"story","title":"Story","markdown":"`), 0xff)
		body = append(body, []byte(`"}`)...)
		rec := serveAdmin(
			t,
			&fakeAdminStore{},
			http.MethodPost,
			"/api/v1/admin/preview",
			body,
			"valid",
			testAdminKey,
		)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"code":"bad_json"`) {
			t.Fatalf("malformed UTF-8 response = %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("unpublish storage failure", func(t *testing.T) {
		const marker = "SENSITIVE_DATABASE_DETAIL"
		store := &fakeAdminStore{unpublishErr: errors.New(marker)}
		rec := serveAdmin(
			t,
			store,
			http.MethodPost,
			"/api/v1/admin/stories/safe-story/unpublish",
			nil,
			"valid",
			testAdminKey,
		)
		if rec.Code != http.StatusInternalServerError ||
			!strings.Contains(rec.Body.String(), `"code":"unpublish_failed"`) ||
			strings.Contains(rec.Body.String(), marker) {
			t.Fatalf("unpublish failure = %d %s", rec.Code, rec.Body.String())
		}
	})
}

func assertAdminResponseHeaders(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", rec.Header().Get("Cache-Control"))
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("Content-Type = %q", contentType)
	}
}
