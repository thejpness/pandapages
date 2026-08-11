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

type discoveryStub struct {
	searchCalls    int
	workCalls      int
	candidateCalls int
	providerID     sourceprovider.ID
	query          string
	limit          int
	externalID     string
	searchOut      sourceprovider.SearchResponse
	searchErr      error
	workOut        sourceprovider.Work
	workErr        error
	candidateOut   sourceprovider.SourceCandidate
	candidateErr   error
}

func (s *discoveryStub) Search(_ context.Context, provider sourceprovider.ID, query string, limit int) (sourceprovider.SearchResponse, error) {
	s.searchCalls++
	s.providerID, s.query, s.limit = provider, query, limit
	return s.searchOut, s.searchErr
}
func (s *discoveryStub) GetWork(_ context.Context, provider sourceprovider.ID, externalID string) (sourceprovider.Work, error) {
	s.workCalls++
	s.providerID, s.externalID = provider, externalID
	return s.workOut, s.workErr
}
func (s *discoveryStub) Acquire(_ context.Context, provider sourceprovider.ID, externalID string) (sourceprovider.SourceCandidate, error) {
	s.candidateCalls++
	s.providerID, s.externalID = provider, externalID
	return s.candidateOut, s.candidateErr
}

func sourceProviderHandler(store *adminStore, discovery sourceprovider.Discovery) http.Handler {
	acquisition, _ := discovery.(sourceprovider.Acquisition)
	return New(Config{
		AdminKey:            "admin-key",
		BearerAuthenticator: httpbearer.New(adminVerifier{}, store),
		SourceDiscovery:     discovery,
		SourceAcquisition:   acquisition,
	}, store)
}

func providerRequest(method, path string) *http.Request {
	request := httptest.NewRequest(method, path, nil)
	request.Header.Set("Authorization", "Bearer valid")
	request.Header.Set("X-PP-Account-ID", ownerAccount)
	request.Header.Set("X-PP-Admin-Key", "admin-key")
	return request
}

func ownerStore() *adminStore {
	return &adminStore{memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}}
}

func TestSourceProviderSearchUsesAdminBoundaryAndNeutralJSON(t *testing.T) {
	discovery := &discoveryStub{searchOut: sourceprovider.SearchResponse{
		Provider: sourceprovider.ProjectGutenberg,
		Results: []sourceprovider.WorkSummary{{
			Provider:   sourceprovider.ProjectGutenberg,
			ExternalID: "11",
			Title:      "Alice's Adventures in Wonderland",
			LandingURL: "https://www.gutenberg.org/ebooks/11",
		}},
	}}
	response := httptest.NewRecorder()
	sourceProviderHandler(ownerStore(), discovery).ServeHTTP(response, providerRequest(http.MethodGet, "/api/v1/admin/source-providers/project-gutenberg/search?q=alice"))
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") || !strings.Contains(response.Body.String(), `"externalId":"11"`) || strings.Contains(response.Body.String(), "<feed>") {
		t.Fatalf("status/headers/body=%d/%q/%s", response.Code, response.Header().Get("Cache-Control"), response.Body.String())
	}
	if discovery.searchCalls != 1 || discovery.providerID != sourceprovider.ProjectGutenberg || discovery.query != "alice" || discovery.limit != 0 {
		t.Fatalf("discovery=%+v", discovery)
	}
}

func TestSourceProviderRoutesRetainAdminAuthorization(t *testing.T) {
	for _, test := range []struct {
		name, token, account, key string
		memberships               []appidentity.Membership
		want                      int
	}{
		{"missing bearer", "", ownerAccount, "admin-key", []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}, http.StatusUnauthorized},
		{"adult membership", "valid", adultAccount, "admin-key", []appidentity.Membership{{AccountID: adultAccount, Role: appidentity.RoleAdult}}, http.StatusForbidden},
		{"missing key", "valid", ownerAccount, "", []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}, http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/source-providers/project-gutenberg/search?q=alice", nil)
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			if test.account != "" {
				request.Header.Set("X-PP-Account-ID", test.account)
			}
			if test.key != "" {
				request.Header.Set("X-PP-Admin-Key", test.key)
			}
			response := httptest.NewRecorder()
			sourceProviderHandler(&adminStore{memberships: test.memberships}, &discoveryStub{}).ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestSourceProviderErrorsAreFiniteAndSafe(t *testing.T) {
	for _, test := range []struct {
		name, path string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"unknown provider", "/api/v1/admin/source-providers/unknown/search?q=alice", sourceprovider.ErrUnknownProvider, http.StatusNotFound, "source_provider_invalid"},
		{"invalid query", "/api/v1/admin/source-providers/project-gutenberg/search?q=alice", sourceprovider.ErrQueryInvalid, http.StatusBadRequest, "source_provider_query_invalid"},
		{"timeout", "/api/v1/admin/source-providers/project-gutenberg/search?q=alice", sourceprovider.ErrTimeout, http.StatusGatewayTimeout, "source_provider_timeout"},
		{"invalid response", "/api/v1/admin/source-providers/project-gutenberg/search?q=alice", sourceprovider.ErrResponseInvalid, http.StatusBadGateway, "source_provider_response_invalid"},
		{"missing work", "/api/v1/admin/source-providers/project-gutenberg/works/11", sourceprovider.ErrWorkNotFound, http.StatusNotFound, "source_provider_work_not_found"},
		{"network detail hidden", "/api/v1/admin/source-providers/project-gutenberg/search?q=alice", errors.New("dial tcp 10.0.0.1:53: private diagnostic"), http.StatusBadGateway, "source_provider_unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			discovery := &discoveryStub{searchErr: test.err, workErr: test.err}
			response := httptest.NewRecorder()
			sourceProviderHandler(ownerStore(), discovery).ServeHTTP(response, providerRequest(http.MethodGet, test.path))
			if response.Code != test.wantStatus || !strings.Contains(response.Body.String(), `"code":"`+test.wantCode+`"`) || strings.Contains(response.Body.String(), "private diagnostic") || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status/body/headers=%d/%s/%q", response.Code, response.Body.String(), response.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestSourceProviderRequestValidationAndWorkRoute(t *testing.T) {
	discovery := &discoveryStub{workOut: sourceprovider.Work{Provider: sourceprovider.ProjectGutenberg, ExternalID: "11", Title: "Alice", LandingURL: "https://www.gutenberg.org/ebooks/11"}}
	handler := sourceProviderHandler(ownerStore(), discovery)
	for _, path := range []string{
		"/api/v1/admin/source-providers/project-gutenberg/search?q=alice&limit=not-a-number",
		"/api/v1/admin/source-providers/project-gutenberg/search?q=alice&limit=21",
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, providerRequest(http.MethodGet, path))
		if response.Code != http.StatusBadRequest || discovery.searchCalls != 0 {
			t.Fatalf("status/calls=%d/%d", response.Code, discovery.searchCalls)
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, providerRequest(http.MethodGet, "/api/v1/admin/source-providers/project-gutenberg/works/11"))
	if response.Code != http.StatusOK || discovery.workCalls != 1 || discovery.externalID != "11" {
		t.Fatalf("status/discovery=%d/%+v", response.Code, discovery)
	}
}

func TestSourceCandidateRouteUsesAdminBoundaryAndReturnsCandidateJSON(t *testing.T) {
	discovery := &discoveryStub{candidateOut: sourceprovider.SourceCandidate{
		Provider:               sourceprovider.ProjectGutenberg,
		ExternalID:             "11",
		Title:                  "Alice's Adventures in Wonderland",
		LandingURL:             "https://www.gutenberg.org/ebooks/11",
		NormalisationVersion:   "project-gutenberg-plain-text-v1",
		RetrievedContentHash:   "retrieved",
		NormalisedContentHash:  "normalised",
		SourceText:             "Down the rabbit-hole.\n",
		SelectedRepresentation: sourceprovider.Representation{MediaType: "text/plain; charset=utf-8", URL: "https://www.gutenberg.org/files/11/11-0.txt"},
	}}
	response := httptest.NewRecorder()
	sourceProviderHandler(ownerStore(), discovery).ServeHTTP(response, providerRequest(http.MethodPost, "/api/v1/admin/source-providers/project-gutenberg/works/11/candidate"))
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") || !strings.Contains(response.Body.String(), `"sourceText":"Down the rabbit-hole.\n"`) || strings.Contains(response.Body.String(), "<feed>") {
		t.Fatalf("status/headers/body=%d/%q/%s", response.Code, response.Header().Get("Cache-Control"), response.Body.String())
	}
	if discovery.candidateCalls != 1 || discovery.providerID != sourceprovider.ProjectGutenberg || discovery.externalID != "11" || discovery.workCalls != 0 {
		t.Fatalf("discovery=%+v", discovery)
	}
}

func TestSourceCandidateRouteRetainsAdminAuthorizationAndFiniteErrors(t *testing.T) {
	for _, test := range []struct {
		name, token, account, key string
		memberships               []appidentity.Membership
		err                       error
		wantStatus                int
		wantCode                  string
	}{
		{"missing bearer", "", ownerAccount, "admin-key", []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}, nil, http.StatusUnauthorized, ""},
		{"adult membership", "valid", adultAccount, "admin-key", []appidentity.Membership{{AccountID: adultAccount, Role: appidentity.RoleAdult}}, nil, http.StatusForbidden, ""},
		{"missing key", "valid", ownerAccount, "", []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}, nil, http.StatusForbidden, ""},
		{"unsupported representation", "valid", ownerAccount, "admin-key", []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}, sourceprovider.ErrRepresentationUnavailable, http.StatusUnprocessableEntity, "source_provider_representation_unavailable"},
		{"too large", "valid", ownerAccount, "admin-key", []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}, sourceprovider.ErrContentTooLarge, http.StatusRequestEntityTooLarge, "source_provider_content_too_large"},
		{"invalid content", "valid", ownerAccount, "admin-key", []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}, sourceprovider.ErrContentInvalid, http.StatusBadGateway, "source_provider_content_invalid"},
		{"normalisation failure", "valid", ownerAccount, "admin-key", []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}, sourceprovider.ErrNormalisationFailed, http.StatusUnprocessableEntity, "source_provider_normalisation_failed"},
		{"hidden provider diagnostic", "valid", ownerAccount, "admin-key", []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}, errors.New("dial tcp 10.0.0.1:53: private diagnostic"), http.StatusBadGateway, "source_provider_unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			discovery := &discoveryStub{candidateErr: test.err}
			request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/source-providers/project-gutenberg/works/11/candidate", nil)
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			if test.account != "" {
				request.Header.Set("X-PP-Account-ID", test.account)
			}
			if test.key != "" {
				request.Header.Set("X-PP-Admin-Key", test.key)
			}
			response := httptest.NewRecorder()
			sourceProviderHandler(&adminStore{memberships: test.memberships}, discovery).ServeHTTP(response, request)
			if response.Code != test.wantStatus || (test.wantCode != "" && (!strings.Contains(response.Body.String(), `"code":"`+test.wantCode+`"`) || strings.Contains(response.Body.String(), "private diagnostic"))) {
				t.Fatalf("status/body=%d/%s", response.Code, response.Body.String())
			}
			if test.wantStatus != http.StatusOK && discovery.candidateCalls != 0 && (test.token == "" || test.key == "" || test.account == adultAccount) {
				t.Fatalf("candidate invoked before authorization: %+v", discovery)
			}
		})
	}
}

func TestSourceAcquisitionPersistRouteUsesTrustedCandidateAndRejectsBodies(t *testing.T) {
	candidate := sourceprovider.SourceCandidate{
		Provider:     sourceprovider.ProjectGutenberg,
		ExternalID:   "11",
		Title:        "Alice's Adventures in Wonderland",
		SourceText:   "Down the rabbit-hole.\n",
		LandingURL:   "https://www.gutenberg.org/ebooks/11",
		Contributors: []sourceprovider.Contributor{{Name: "Lewis Carroll", Role: "author"}},
	}
	store := ownerStore()
	store.persistSourceAcquisitionResponse = model.AdminSourceAcquisitionPersistResponse{
		Outcome: model.AdminSourceAcquisitionOutcomeCreated,
		Acquisition: model.AdminSourceAcquisitionSummary{
			ID:    "11111111-1111-4111-8111-111111111111",
			Title: candidate.Title,
		},
	}
	discovery := &discoveryStub{candidateOut: candidate}
	handler := sourceProviderHandler(store, discovery)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, providerRequest(http.MethodPost, "/api/v1/admin/source-providers/project-gutenberg/works/11/acquisitions"))
	if response.Code != http.StatusCreated || response.Header().Get("Cache-Control") != "no-store" || !strings.Contains(response.Body.String(), `"outcome":"created"`) {
		t.Fatalf("status/headers/body = %d/%q/%s", response.Code, response.Header().Get("Cache-Control"), response.Body.String())
	}
	if discovery.candidateCalls != 1 ||
		store.persistSourceAcquisitionCalls != 1 ||
		store.persistedSourceCandidate.Provider != candidate.Provider ||
		store.persistedSourceCandidate.ExternalID != candidate.ExternalID ||
		store.persistedSourceCandidate.SourceText != candidate.SourceText {
		t.Fatalf("candidate/store calls = %+v/%+v", discovery, store)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/source-providers/project-gutenberg/works/11/acquisitions",
		strings.NewReader(`{"representationUrl":"https://evil.example/book.txt"}`),
	)
	request.Header.Set("Authorization", "Bearer valid")
	request.Header.Set("X-PP-Account-ID", ownerAccount)
	request.Header.Set("X-PP-Admin-Key", "admin-key")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"source_acquisition_invalid"`) || discovery.candidateCalls != 1 || store.persistSourceAcquisitionCalls != 1 {
		t.Fatalf("body status/calls/body = %d/%d/%d/%s", response.Code, discovery.candidateCalls, store.persistSourceAcquisitionCalls, response.Body.String())
	}
}

func TestSourceAcquisitionReadAndReviewRoutesKeepSourceTextExplicit(t *testing.T) {
	const acquisitionID = "11111111-1111-4111-8111-111111111111"
	summary := model.AdminSourceAcquisitionSummary{
		ID:    acquisitionID,
		Title: "Alice's Adventures in Wonderland",
		Review: model.AdminSourceAcquisitionReview{
			Rights:    model.AdminSourceAcquisitionReviewDimension{Status: model.AdminSourceAcquisitionReviewPending},
			Editorial: model.AdminSourceAcquisitionReviewDimension{Status: model.AdminSourceAcquisitionReviewPending},
		},
	}
	store := ownerStore()
	store.listSourceAcquisitionsResponse = model.AdminSourceAcquisitionsListResponse{Items: []model.AdminSourceAcquisitionSummary{summary}}
	store.getSourceAcquisitionResponse = model.AdminSourceAcquisitionDetail{AdminSourceAcquisitionSummary: summary, SourceText: "The saved source text.\n"}
	store.rightsReviewResponse = summary
	store.editorialReviewResponse = summary
	discovery := &discoveryStub{}
	handler := sourceProviderHandler(store, discovery)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, providerRequest(http.MethodGet, "/api/v1/admin/source-acquisitions?limit=20"))
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "sourceText") || store.listSourceAcquisitionCalls != 1 || store.listSourceAcquisitionLimit != 20 {
		t.Fatalf("list status/headers/body/store = %d/%q/%s/%+v", response.Code, response.Header().Get("Cache-Control"), response.Body.String(), store)
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, providerRequest(http.MethodGet, "/api/v1/admin/source-acquisitions/"+acquisitionID))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"sourceText":"The saved source text.\n"`) || store.getSourceAcquisitionCalls != 1 || store.getSourceAcquisitionID != acquisitionID {
		t.Fatalf("detail status/body/store = %d/%s/%+v", response.Code, response.Body.String(), store)
	}

	for _, test := range []struct {
		path                                string
		wantRightsCalls, wantEditorialCalls int
	}{
		{"/api/v1/admin/source-acquisitions/" + acquisitionID + "/rights-review", 1, 0},
		{"/api/v1/admin/source-acquisitions/" + acquisitionID + "/editorial-review", 1, 1},
	} {
		request := httptest.NewRequest(http.MethodPut, test.path, strings.NewReader(`{"status":"approved","note":"Reviewed manually."}`))
		request.Header.Set("Authorization", "Bearer valid")
		request.Header.Set("X-PP-Account-ID", ownerAccount)
		request.Header.Set("X-PP-Admin-Key", "admin-key")
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || store.rightsReviewCalls != test.wantRightsCalls || store.editorialReviewCalls != test.wantEditorialCalls {
			t.Fatalf("review status/headers/calls = %d/%q/%d/%d body=%s", response.Code, response.Header().Get("Cache-Control"), store.rightsReviewCalls, store.editorialReviewCalls, response.Body.String())
		}
	}
	if discovery.searchCalls != 0 || discovery.workCalls != 0 || discovery.candidateCalls != 0 {
		t.Fatalf("saved acquisition operations called provider: %+v", discovery)
	}
}

func TestSourceAcquisitionRoutesRetainAdminAuthorizationAndFiniteErrors(t *testing.T) {
	for _, test := range []struct {
		name, token, account, key string
		memberships               []appidentity.Membership
		want                      int
	}{
		{"missing bearer", "", ownerAccount, "admin-key", []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}, http.StatusUnauthorized},
		{"adult membership", "valid", adultAccount, "admin-key", []appidentity.Membership{{AccountID: adultAccount, Role: appidentity.RoleAdult}}, http.StatusForbidden},
		{"missing key", "valid", ownerAccount, "", []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}, http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &adminStore{memberships: test.memberships}
			discovery := &discoveryStub{}
			request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/source-providers/project-gutenberg/works/11/acquisitions", nil)
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			if test.account != "" {
				request.Header.Set("X-PP-Account-ID", test.account)
			}
			if test.key != "" {
				request.Header.Set("X-PP-Admin-Key", test.key)
			}
			response := httptest.NewRecorder()
			sourceProviderHandler(store, discovery).ServeHTTP(response, request)
			if response.Code != test.want || discovery.candidateCalls != 0 || store.persistSourceAcquisitionCalls != 0 {
				t.Fatalf("status/provider/store = %d/%d/%d body=%s", response.Code, discovery.candidateCalls, store.persistSourceAcquisitionCalls, response.Body.String())
			}
		})
	}

	store := ownerStore()
	store.getSourceAcquisitionErr = model.ErrAdminSourceAcquisitionNotFound
	response := httptest.NewRecorder()
	sourceProviderHandler(store, &discoveryStub{}).ServeHTTP(response, providerRequest(http.MethodGet, "/api/v1/admin/source-acquisitions/11111111-1111-4111-8111-111111111111"))
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"code":"source_acquisition_not_found"`) {
		t.Fatalf("status/body = %d/%s", response.Code, response.Body.String())
	}

	store = ownerStore()
	store.listSourceAcquisitionsErr = errors.New("database detail must not leak")
	response = httptest.NewRecorder()
	sourceProviderHandler(store, &discoveryStub{}).ServeHTTP(response, providerRequest(http.MethodGet, "/api/v1/admin/source-acquisitions?limit=101"))
	if response.Code != http.StatusBadRequest || store.listSourceAcquisitionCalls != 0 {
		t.Fatalf("invalid list status/calls = %d/%d", response.Code, store.listSourceAcquisitionCalls)
	}
	response = httptest.NewRecorder()
	sourceProviderHandler(store, &discoveryStub{}).ServeHTTP(response, providerRequest(http.MethodGet, "/api/v1/admin/source-acquisitions"))
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), `"code":"source_acquisition_failed"`) || strings.Contains(response.Body.String(), "database detail") {
		t.Fatalf("safe store error status/body = %d/%s", response.Code, response.Body.String())
	}
}
