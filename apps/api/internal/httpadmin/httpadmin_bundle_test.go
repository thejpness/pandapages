package httpadmin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pandapages/api/internal/appidentity"
	"pandapages/api/internal/httpbearer"
	"pandapages/api/internal/model"
)

type recordingBundleStore struct {
	*adminStore
	accountID string
	request   model.AdminEditionBundleUpsertRequest
	calls     int
	err       error
}

func (s *recordingBundleStore) AdminEditionBundleUpsert(accountID string, req model.AdminEditionBundleUpsertRequest) (model.AdminEditionBundleUpsertResponse, error) {
	s.calls++
	s.accountID = accountID
	s.request = req
	if s.err != nil {
		return model.AdminEditionBundleUpsertResponse{}, s.err
	}
	return model.AdminEditionBundleUpsertResponse{Slug: req.Slug, Results: []model.AdminEditionBundleResult{
		{EditionKey: model.AdminStoryEditionClassic, VersionID: "11111111-1111-4111-8111-111111111111", Version: 1, Outcome: model.AdminEditionIngestOutcomeCreated},
		{EditionKey: model.AdminStoryEditionConfidentReaders, VersionID: "22222222-2222-4222-8222-222222222222", Version: 2, Outcome: model.AdminEditionIngestOutcomeCreated},
		{EditionKey: model.AdminStoryEditionGrowingReaders, VersionID: "33333333-3333-4333-8333-333333333333", Version: 3, Outcome: model.AdminEditionIngestOutcomeCreated},
		{EditionKey: model.AdminStoryEditionStoryExplorers, VersionID: "44444444-4444-4444-8444-444444444444", Version: 4, Outcome: model.AdminEditionIngestOutcomeCreated},
		{EditionKey: model.AdminStoryEditionLittleListeners, VersionID: "55555555-5555-4555-8555-555555555555", Version: 5, Outcome: model.AdminEditionIngestOutcomeCreated},
	}}, nil
}

func bundleHandler(store *recordingBundleStore) http.Handler {
	return New(Config{AdminKey: "admin-key", BearerAuthenticator: httpbearer.New(adminVerifier{}, store)}, store)
}

func bundleRequestBody() string {
	return `{"slug":"five-edition-story","title":"Five Edition Story","author":null,"language":"en-GB","sourceUrl":null,"rights":{"label":"Public domain"},"editions":[{"editionKey":"classic","markdown":"# Five Edition Story\n\nClassic.\n"},{"editionKey":"confident-readers","markdown":"# Five Edition Story\n\nConfident.\n"},{"editionKey":"growing-readers","markdown":"# Five Edition Story\n\nGrowing.\n"},{"editionKey":"story-explorers","markdown":"# Five Edition Story\n\nExplorers.\n"},{"editionKey":"little-listeners","markdown":"# Five Edition Story\n\nListeners.\n"}]}`
}

func TestAdminEditionBundleRouteUsesSelectedOwnerAccount(t *testing.T) {
	base := &adminStore{memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}}
	store := &recordingBundleStore{adminStore: base}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/stories/editions/ingest", strings.NewReader(bundleRequestBody()))
	request.Header.Set("Authorization", "Bearer valid")
	request.Header.Set("X-PP-Account-ID", ownerAccount)
	request.Header.Set("X-PP-Admin-Key", "admin-key")
	response := httptest.NewRecorder()
	bundleHandler(store).ServeHTTP(response, request)
	if response.Code != http.StatusOK || store.calls != 1 || store.accountID != ownerAccount || store.request.Slug != "five-edition-story" || len(store.request.Editions) != 5 {
		t.Fatalf("bundle status/calls/account/slug/editions = %d/%d/%q/%q/%d body=%s", response.Code, store.calls, store.accountID, store.request.Slug, len(store.request.Editions), response.Body.String())
	}
	if strings.Contains(response.Body.String(), "markdown") {
		t.Fatalf("bundle response leaked submitted Markdown: %s", response.Body.String())
	}
}

func TestAdminEditionBundleRouteKeepsRepairConflictDistinct(t *testing.T) {
	base := &adminStore{memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}}
	store := &recordingBundleStore{adminStore: base, err: model.ErrAdminVersionRepairRequired}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/stories/editions/ingest", strings.NewReader(bundleRequestBody()))
	request.Header.Set("Authorization", "Bearer valid")
	request.Header.Set("X-PP-Account-ID", ownerAccount)
	request.Header.Set("X-PP-Admin-Key", "admin-key")
	response := httptest.NewRecorder()
	bundleHandler(store).ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"edition_ingest_repair_required"`) {
		t.Fatalf("repair bundle status/body = %d/%s", response.Code, response.Body.String())
	}
}
