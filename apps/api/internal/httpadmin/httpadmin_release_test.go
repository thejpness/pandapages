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

type recordingReleaseStore struct {
	*adminStore
	slug    string
	request model.AdminCreateReleaseRequest
	calls   int
	err     error
}

func (s *recordingReleaseStore) AdminCreateRelease(
	slug string,
	req model.AdminCreateReleaseRequest,
) (model.AdminCreateReleaseResponse, error) {
	s.calls++
	s.slug = slug
	s.request = req
	if s.err != nil {
		return model.AdminCreateReleaseResponse{}, s.err
	}
	return model.AdminCreateReleaseResponse{
		Slug:    slug,
		Outcome: model.AdminReleaseOutcomeCreated,
		Release: model.AdminReleaseSummary{
			Release:   1,
			CreatedAt: "2026-08-08T18:00:00Z",
			Editions: []model.AdminReleaseEditionSummary{{
				EditionKey: model.AdminStoryEditionGrowingReaders,
				VersionID:  "11111111-1111-4111-8111-111111111111",
				Version:    4,
			}},
		},
	}, nil
}

func releaseHandler(store *recordingReleaseStore) http.Handler {
	return New(
		Config{
			AdminKey:            "admin-key",
			BearerAuthenticator: httpbearer.New(adminVerifier{}, store),
		},
		store,
	)
}

func releaseHTTPRequest(t *testing.T, store *recordingReleaseStore, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/stories/partial-story/releases",
		strings.NewReader(body),
	)
	request.Header.Set("Authorization", "Bearer valid")
	request.Header.Set("X-PP-Account-ID", ownerAccount)
	request.Header.Set("X-PP-Admin-Key", "admin-key")
	response := httptest.NewRecorder()
	releaseHandler(store).ServeHTTP(response, request)
	return response
}

func TestAdminReleaseRouteAcceptsPartialCanonicalRelease(t *testing.T) {
	store := &recordingReleaseStore{adminStore: &adminStore{
		memberships: []appidentity.Membership{{
			AccountID: ownerAccount,
			Role:      appidentity.RoleOwner,
		}},
	}}
	response := releaseHTTPRequest(t, store, `{
		"editions":[{
			"editionKey":"growing-readers",
			"versionId":"11111111-1111-4111-8111-111111111111"
		}]
	}`)
	if response.Code != http.StatusOK ||
		store.calls != 1 ||
		store.slug != "partial-story" ||
		len(store.request.Editions) != 1 ||
		store.request.Editions[0].EditionKey != model.AdminStoryEditionGrowingReaders {
		t.Fatalf(
			"release status/calls/slug/request = %d/%d/%q/%#v body=%s",
			response.Code,
			store.calls,
			store.slug,
			store.request,
			response.Body.String(),
		)
	}
	if strings.Contains(response.Body.String(), "accountId") ||
		strings.Contains(response.Body.String(), "storyId") {
		t.Fatalf("release response leaked internal ownership: %s", response.Body.String())
	}
}

func TestAdminReleaseRouteKeepsValidationAndRepairFailuresDistinct(t *testing.T) {
	base := &adminStore{memberships: []appidentity.Membership{{
		AccountID: ownerAccount,
		Role:      appidentity.RoleOwner,
	}}}

	validation := &recordingReleaseStore{
		adminStore: base,
		err: &model.AdminValidationError{Issues: []model.AdminValidationIssue{{
			Field:   "editions",
			Code:    "invalid_count",
			Message: "Choose between one and five reading editions",
		}}},
	}
	response := releaseHTTPRequest(t, validation, `{"editions":[]}`)
	if response.Code != http.StatusBadRequest ||
		!strings.Contains(response.Body.String(), `"code":"release_invalid"`) {
		t.Fatalf("validation status/body = %d/%s", response.Code, response.Body.String())
	}

	repair := &recordingReleaseStore{
		adminStore: base,
		err:        model.ErrAdminReleaseInvalid,
	}
	response = releaseHTTPRequest(t, repair, `{"editions":[{"editionKey":"classic","versionId":"11111111-1111-4111-8111-111111111111"}]}`)
	if response.Code != http.StatusConflict ||
		!strings.Contains(response.Body.String(), `"code":"release_repair_required"`) {
		t.Fatalf("repair status/body = %d/%s", response.Code, response.Body.String())
	}
}
