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
)

const ownerAccount = "11111111-1111-4111-8111-111111111111"
const adultAccount = "22222222-2222-4222-8222-222222222222"

type adminVerifier struct{}

func (adminVerifier) Verify(_ context.Context, token string) (appidentity.ExternalIdentity, error) {
	if token != "valid" {
		return appidentity.ExternalIdentity{}, errors.New("invalid")
	}
	return appidentity.ExternalIdentity{Provider: appidentity.ProviderSupabase, Issuer: "https://project.supabase.co/auth/v1", Subject: "subject"}, nil
}

type adminStore struct {
	memberships        []appidentity.Membership
	listAccount        string
	listCalls          int
	editionSourceCalls int
	editionSourceKey   model.AdminStoryEditionKey
	editionSourceID    string
}

func (s *adminStore) Identity(context.Context, appidentity.ExternalIdentity) (appidentity.Snapshot, error) {
	return appidentity.Snapshot{PrincipalID: "principal", Memberships: s.memberships}, nil
}
func (*adminStore) AdminDraftUpsert(string, model.AdminDraftUpsertRequest) (model.AdminDraftUpsertResponse, error) {
	return model.AdminDraftUpsertResponse{}, nil
}
func (*adminStore) AdminPublishStory(string, string, string) (model.AdminStoryStatusResponse, error) {
	return model.AdminStoryStatusResponse{}, nil
}
func (*adminStore) AdminUnpublish(string, string) (model.AdminStoryStatusResponse, error) {
	return model.AdminStoryStatusResponse{}, nil
}
func (*adminStore) AdminPreview(model.AdminPreviewRequest) (model.AdminPreviewResponse, error) {
	return model.AdminPreviewResponse{}, nil
}
func (s *adminStore) AdminListStories(accountID string) (model.AdminStoriesListResponse, error) {
	s.listCalls++
	s.listAccount = accountID
	return model.AdminStoriesListResponse{Items: []model.AdminStorySummary{}}, nil
}
func (*adminStore) AdminGetStory(string, string) (model.AdminStoryDetailResponse, error) {
	return model.AdminStoryDetailResponse{}, nil
}
func (*adminStore) AdminGetVersionSource(string, string, string) (model.AdminVersionSourceResponse, error) {
	return model.AdminVersionSourceResponse{}, nil
}
func (s *adminStore) AdminGetEditionVersionSource(
	_ string,
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

func TestAdminUsesSelectedOwnerAccount(t *testing.T) {
	store := &adminStore{memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}}
	response := serveAdmin(t, store, ownerAccount, "valid", "admin-key")
	if response.Code != http.StatusOK || store.listAccount != ownerAccount || store.listCalls != 1 {
		t.Fatalf("status/account/calls=%d/%s/%d", response.Code, store.listAccount, store.listCalls)
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
