package httpadmin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pandapages/api/internal/appidentity"
	"pandapages/api/internal/httpbearer"
	"pandapages/api/internal/model"
	"pandapages/api/internal/sourceprovider"
)

const ownerAccount = "11111111-1111-4111-8111-111111111111"
const adultAccount = "22222222-2222-4222-8222-222222222222"
const secondOwnerAccount = "33333333-3333-4333-8333-333333333333"

type adminVerifier struct{}

func (adminVerifier) Verify(_ context.Context, token string) (appidentity.ExternalIdentity, error) {
	if token != "valid" {
		return appidentity.ExternalIdentity{}, errors.New("invalid")
	}
	return appidentity.ExternalIdentity{Provider: appidentity.ProviderSupabase, Issuer: "https://project.supabase.co/auth/v1", Subject: "subject"}, nil
}

type adminStore struct {
	memberships                      []appidentity.Membership
	listCalls                        int
	editionSourceCalls               int
	editionSourceKey                 model.AdminStoryEditionKey
	editionSourceID                  string
	persistSourceAcquisitionCalls    int
	persistedSourceCandidate         sourceprovider.SourceCandidate
	persistSourceAcquisitionResponse model.AdminSourceAcquisitionPersistResponse
	persistSourceAcquisitionErr      error
	listSourceAcquisitionCalls       int
	listSourceAcquisitionLimit       int
	listSourceAcquisitionsResponse   model.AdminSourceAcquisitionsListResponse
	listSourceAcquisitionsErr        error
	getSourceAcquisitionCalls        int
	getSourceAcquisitionID           string
	getSourceAcquisitionResponse     model.AdminSourceAcquisitionDetail
	getSourceAcquisitionErr          error
	rightsReviewCalls                int
	rightsReviewID                   string
	rightsReviewRequest              model.AdminSourceAcquisitionReviewUpdateRequest
	rightsReviewResponse             model.AdminSourceAcquisitionSummary
	rightsReviewErr                  error
	editorialReviewCalls             int
	editorialReviewID                string
	editorialReviewRequest           model.AdminSourceAcquisitionReviewUpdateRequest
	editorialReviewResponse          model.AdminSourceAcquisitionSummary
	editorialReviewErr               error
}

func (s *adminStore) Identity(context.Context, appidentity.ExternalIdentity) (appidentity.Snapshot, error) {
	return appidentity.Snapshot{PrincipalID: "principal", Memberships: s.memberships}, nil
}
func (*adminStore) AdminDraftUpsert(model.AdminDraftUpsertRequest) (model.AdminDraftUpsertResponse, error) {
	return model.AdminDraftUpsertResponse{}, nil
}
func (*adminStore) AdminEditionBundleUpsert(model.AdminEditionBundleUpsertRequest) (model.AdminEditionBundleUpsertResponse, error) {
	return model.AdminEditionBundleUpsertResponse{}, nil
}
func (*adminStore) AdminPublishStory(string, string) (model.AdminStoryStatusResponse, error) {
	return model.AdminStoryStatusResponse{}, nil
}
func (*adminStore) AdminCreateRelease(string, model.AdminCreateReleaseRequest) (model.AdminCreateReleaseResponse, error) {
	return model.AdminCreateReleaseResponse{}, nil
}
func (*adminStore) AdminUnpublish(string) (model.AdminStoryStatusResponse, error) {
	return model.AdminStoryStatusResponse{}, nil
}
func (*adminStore) AdminPreview(model.AdminPreviewRequest) (model.AdminPreviewResponse, error) {
	return model.AdminPreviewResponse{}, nil
}
func (s *adminStore) AdminListStories() (model.AdminStoriesListResponse, error) {
	s.listCalls++
	return model.AdminStoriesListResponse{Items: []model.AdminStorySummary{}}, nil
}
func (*adminStore) AdminGetStory(string) (model.AdminStoryDetailResponse, error) {
	return model.AdminStoryDetailResponse{}, nil
}
func (*adminStore) AdminGetVersionSource(string, string) (model.AdminVersionSourceResponse, error) {
	return model.AdminVersionSourceResponse{}, nil
}
func (*adminStore) AdminSourceUpsert(string, model.AdminSourceUpsertRequest) (model.AdminSourceUpsertResponse, error) {
	return model.AdminSourceUpsertResponse{}, nil
}
func (*adminStore) AdminGetSource(string) (model.AdminSourceDetailResponse, error) {
	return model.AdminSourceDetailResponse{}, nil
}
func (*adminStore) AdminGetSourceVersion(string, string) (model.AdminSourceVersionResponse, error) {
	return model.AdminSourceVersionResponse{}, nil
}

func (s *adminStore) AdminPersistSourceAcquisition(candidate sourceprovider.SourceCandidate) (model.AdminSourceAcquisitionPersistResponse, error) {
	s.persistSourceAcquisitionCalls++
	s.persistedSourceCandidate = candidate
	return s.persistSourceAcquisitionResponse, s.persistSourceAcquisitionErr
}

func (s *adminStore) AdminListSourceAcquisitions(limit int) (model.AdminSourceAcquisitionsListResponse, error) {
	s.listSourceAcquisitionCalls++
	s.listSourceAcquisitionLimit = limit
	return s.listSourceAcquisitionsResponse, s.listSourceAcquisitionsErr
}

func (s *adminStore) AdminGetSourceAcquisition(id string) (model.AdminSourceAcquisitionDetail, error) {
	s.getSourceAcquisitionCalls++
	s.getSourceAcquisitionID = id
	return s.getSourceAcquisitionResponse, s.getSourceAcquisitionErr
}

func (s *adminStore) AdminUpdateSourceAcquisitionRightsReview(id string, req model.AdminSourceAcquisitionReviewUpdateRequest) (model.AdminSourceAcquisitionSummary, error) {
	s.rightsReviewCalls++
	s.rightsReviewID = id
	s.rightsReviewRequest = req
	return s.rightsReviewResponse, s.rightsReviewErr
}

func (s *adminStore) AdminUpdateSourceAcquisitionEditorialReview(id string, req model.AdminSourceAcquisitionReviewUpdateRequest) (model.AdminSourceAcquisitionSummary, error) {
	s.editorialReviewCalls++
	s.editorialReviewID = id
	s.editorialReviewRequest = req
	return s.editorialReviewResponse, s.editorialReviewErr
}

func (s *adminStore) AdminGetEditionVersionSource(
	_ string,
	editionKey model.AdminStoryEditionKey,
	versionID string,
) (model.AdminVersionSourceResponse, error) {
	s.editionSourceCalls++
	s.editionSourceKey = editionKey
	s.editionSourceID = versionID
	return model.AdminVersionSourceResponse{EditionKey: editionKey}, nil
}

func serveAdmin(t *testing.T, store *adminStore, account, token, key string) *httptest.ResponseRecorder {
	t.Helper()
	handler := New(Config{AdminKey: "admin-key", BearerAuthenticator: httpbearer.New(adminVerifier{}, store)}, store)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/stories", nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if account != "" {
		request.Header.Set("X-PP-Account-ID", account)
	}
	if key != "" {
		request.Header.Set("X-PP-Admin-Key", key)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestAdminRequiresOwnerBearerAccountAndKey(t *testing.T) {
	tests := []struct {
		name, account, token, key string
		memberships               []appidentity.Membership
		want                      int
	}{
		{"owner and key", ownerAccount, "valid", "admin-key", []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}, http.StatusOK},
		{"adult denied", adultAccount, "valid", "admin-key", []appidentity.Membership{{AccountID: adultAccount, Role: appidentity.RoleAdult}}, http.StatusForbidden},
		{"owner missing key", ownerAccount, "valid", "", []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}, http.StatusForbidden},
		{"key without bearer", ownerAccount, "", "admin-key", []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}, http.StatusUnauthorized},
		{"nonmember denied", adultAccount, "valid", "admin-key", []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}, http.StatusForbidden},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := serveAdmin(t, &adminStore{memberships: test.memberships}, test.account, test.token, test.key)
			if response.Code != test.want {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if test.want != http.StatusOK && strings.Contains(response.Body.String(), "admin-key") {
				t.Fatal("admin credential detail leaked")
			}
		})
	}
}

func TestAdminOwnerAccountsListSameGlobalCatalogue(t *testing.T) {
	store := &adminStore{memberships: []appidentity.Membership{
		{AccountID: ownerAccount, Role: appidentity.RoleOwner},
		{AccountID: secondOwnerAccount, Role: appidentity.RoleOwner},
	}}
	first := serveAdmin(t, store, ownerAccount, "valid", "admin-key")
	second := serveAdmin(t, store, secondOwnerAccount, "valid", "admin-key")
	if first.Code != http.StatusOK || second.Code != http.StatusOK || store.listCalls != 2 {
		t.Fatalf("statuses/calls=%d/%d/%d", first.Code, second.Code, store.listCalls)
	}
}

func TestAdminEditionVersionSourceUsesFiniteEditionPath(t *testing.T) {
	store := &adminStore{memberships: []appidentity.Membership{{
		AccountID: ownerAccount,
		Role:      appidentity.RoleOwner,
	}}}
	handler := New(
		Config{AdminKey: "admin-key", BearerAuthenticator: httpbearer.New(adminVerifier{}, store)},
		store,
	)
	versionID := "11111111-1111-4111-8111-111111111111"

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/stories/story/editions/growing-readers/versions/"+versionID,
		nil,
	)
	request.Header.Set("Authorization", "Bearer valid")
	request.Header.Set("X-PP-Account-ID", ownerAccount)
	request.Header.Set("X-PP-Admin-Key", "admin-key")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK ||
		store.editionSourceCalls != 1 ||
		store.editionSourceKey != model.AdminStoryEditionGrowingReaders ||
		store.editionSourceID != versionID {
		t.Fatalf(
			"valid edition source status/calls/key/id = %d/%d/%q/%q body=%s",
			response.Code,
			store.editionSourceCalls,
			store.editionSourceKey,
			store.editionSourceID,
			response.Body.String(),
		)
	}

	request = httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/stories/story/editions/bedtime-ultra/versions/"+versionID,
		nil,
	)
	request.Header.Set("Authorization", "Bearer valid")
	request.Header.Set("X-PP-Account-ID", ownerAccount)
	request.Header.Set("X-PP-Admin-Key", "admin-key")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest ||
		!strings.Contains(response.Body.String(), `"code":"edition_invalid"`) ||
		store.editionSourceCalls != 1 {
		t.Fatalf(
			"invalid edition source status/calls/body = %d/%d/%s",
			response.Code,
			store.editionSourceCalls,
			response.Body.String(),
		)
	}
}
