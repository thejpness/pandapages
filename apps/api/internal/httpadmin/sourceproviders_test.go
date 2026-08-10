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
	"pandapages/api/internal/sourceprovider"
)

type discoveryStub struct {
	searchCalls int
	workCalls   int
	providerID  sourceprovider.ID
	query       string
	limit       int
	externalID  string
	searchOut   sourceprovider.SearchResponse
	searchErr   error
	workOut     sourceprovider.Work
	workErr     error
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

func sourceProviderHandler(store *adminStore, discovery sourceprovider.Discovery) http.Handler {
	return New(Config{
		AdminKey:            "admin-key",
		BearerAuthenticator: httpbearer.New(adminVerifier{}, store),
		SourceDiscovery:     discovery,
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
