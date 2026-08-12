package httpadmin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pandapages/api/internal/appidentity"
	"pandapages/api/internal/copyrighteligibility"
	"pandapages/api/internal/httpbearer"
	"pandapages/api/internal/model"
	"pandapages/api/internal/sourceeligibility"
	"pandapages/api/internal/sourceprovider"
)

type discoveryStub struct {
	searchCalls, workCalls, candidateCalls, evidenceCalls int
	providerID                                            sourceprovider.ID
	query                                                 string
	limit                                                 int
	externalID                                            string
	searchOut                                             sourceprovider.SearchResponse
	searchErr                                             error
	workOut                                               sourceprovider.Work
	workErr                                               error
	candidateOut                                          sourceprovider.SourceCandidate
	candidateErr                                          error
	evidenceOut                                           copyrighteligibility.ProviderEvidence
	evidenceErr                                           error
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
func (s *discoveryStub) AcquireEvidence(ctx context.Context, provider sourceprovider.ID, externalID string) (sourceprovider.AcquisitionEvidence, error) {
	candidate, err := s.Acquire(ctx, provider, externalID)
	if err != nil {
		return sourceprovider.AcquisitionEvidence{}, err
	}
	return sourceprovider.AcquisitionEvidence{Candidate: candidate, OPDSRights: copyrighteligibility.ProviderRightsPublicDomain, HeaderRights: copyrighteligibility.SourceHeaderRightsPublicDomain}, nil
}
func (s *discoveryStub) CopyrightEvidence(_ context.Context, provider sourceprovider.ID, externalID string) (copyrighteligibility.ProviderEvidence, error) {
	s.evidenceCalls++
	s.providerID, s.externalID = provider, externalID
	return s.evidenceOut, s.evidenceErr
}

func sourceProviderHandler(t *testing.T, store *adminStore, discovery *discoveryStub) http.Handler {
	t.Helper()
	service, err := sourceeligibility.New(sourceeligibility.Config{Gateway: discovery, Now: func() time.Time { return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	return New(Config{AdminKey: "admin-key", BearerAuthenticator: httpbearer.New(adminVerifier{}, store), SourceDiscovery: discovery, SourceAcquisition: discovery, SourceEligibility: service}, store)
}

func providerRequest(method, path string, body string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer valid")
	request.Header.Set("X-PP-Account-ID", ownerAccount)
	request.Header.Set("X-PP-Admin-Key", "admin-key")
	return request
}
func ownerStore() *adminStore {
	return &adminStore{memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}}
}

func TestSourceProviderSearchUsesAdminBoundaryAndNeutralJSON(t *testing.T) {
	discovery := &discoveryStub{searchOut: sourceprovider.SearchResponse{Provider: sourceprovider.ProjectGutenberg, Results: []sourceprovider.WorkSummary{{Provider: sourceprovider.ProjectGutenberg, ExternalID: "11", Title: "Alice", LandingURL: "https://www.gutenberg.org/ebooks/11"}}}}
	response := httptest.NewRecorder()
	sourceProviderHandler(t, ownerStore(), discovery).ServeHTTP(response, providerRequest(http.MethodGet, "/api/v1/admin/source-providers/project-gutenberg/search?q=alice", ""))
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || !strings.Contains(response.Body.String(), `"externalId":"11"`) || strings.Contains(response.Body.String(), "<feed>") {
		t.Fatalf("status/body=%d/%s", response.Code, response.Body.String())
	}
	if discovery.searchCalls != 1 || discovery.providerID != sourceprovider.ProjectGutenberg || discovery.query != "alice" {
		t.Fatalf("discovery=%+v", discovery)
	}
}

func TestSourceCandidateRouteRemainsExplicitAndProtected(t *testing.T) {
	discovery := eligibilityDiscovery()
	response := httptest.NewRecorder()
	sourceProviderHandler(t, ownerStore(), discovery).ServeHTTP(response, providerRequest(http.MethodPost, "/api/v1/admin/source-providers/project-gutenberg/works/11/candidate", ""))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"sourceText"`) || discovery.candidateCalls != 1 {
		t.Fatalf("status/body/calls=%d/%s/%d", response.Code, response.Body.String(), discovery.candidateCalls)
	}
}

func TestEligibilityPreflightIsZeroWriteAndFailsClosed(t *testing.T) {
	store, discovery := ownerStore(), eligibilityDiscovery()
	response := httptest.NewRecorder()
	sourceProviderHandler(t, store, discovery).ServeHTTP(response, providerRequest(http.MethodPost, "/api/v1/admin/source-providers/project-gutenberg/works/11/copyright-eligibility", `{}`))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"overall":"blocked"`) || strings.Contains(response.Body.String(), "sourceText") || store.persistSourceAcquisitionCalls != 0 || discovery.candidateCalls != 1 || discovery.evidenceCalls != 1 {
		t.Fatalf("preflight=%d/%s calls store=%d provider=%+v", response.Code, response.Body.String(), store.persistSourceAcquisitionCalls, discovery)
	}
	if strings.Contains(response.Body.String(), "<rdf") {
		t.Fatalf("raw RDF leaked: %s", response.Body.String())
	}
	for _, expected := range []string{
		`"automaticResolution"`,
		`"workCategory":"insufficient"`,
		`"firstPublication":"insufficient"`,
		`"translation":"insufficient"`,
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("automatic resolution missing %s: %s", expected, response.Body.String())
		}
	}
}

func TestEligibleSaveAcceptsOnlyHumanFactsAndPersistsEvaluation(t *testing.T) {
	store, discovery := ownerStore(), eligibilityDiscovery()
	store.persistSourceAcquisitionResponse = model.AdminSourceAcquisitionPersistResponse{Outcome: model.AdminSourceAcquisitionOutcomeCreated, Acquisition: model.AdminSourceAcquisitionSummary{ID: "11111111-1111-4111-8111-111111111111"}}
	body := eligibleHumanFactsJSON()
	response := httptest.NewRecorder()
	sourceProviderHandler(t, store, discovery).ServeHTTP(response, providerRequest(http.MethodPost, "/api/v1/admin/source-providers/project-gutenberg/works/11/acquisitions", body))
	if response.Code != http.StatusCreated || store.persistSourceAcquisitionCalls != 1 || store.persistedEligibility.Assessment.Overall != copyrighteligibility.OverallEligible {
		t.Fatalf("save=%d/%s evaluation=%#v", response.Code, response.Body.String(), store.persistedEligibility)
	}
	if discovery.candidateCalls != 1 || discovery.evidenceCalls != 1 {
		t.Fatalf("provider calls=%+v", discovery)
	}
	bad := httptest.NewRecorder()
	sourceProviderHandler(t, ownerStore(), eligibilityDiscovery()).ServeHTTP(bad, providerRequest(http.MethodPost, "/api/v1/admin/source-providers/project-gutenberg/works/11/acquisitions", `{"eligible":true}`))
	if bad.Code != http.StatusBadRequest || !strings.Contains(bad.Body.String(), `"code":"source_eligibility_invalid"`) {
		t.Fatalf("authority body=%d/%s", bad.Code, bad.Body.String())
	}
	obsolete := httptest.NewRecorder()
	sourceProviderHandler(t, ownerStore(), eligibilityDiscovery()).ServeHTTP(obsolete, providerRequest(http.MethodPost, "/api/v1/admin/source-providers/project-gutenberg/works/11/acquisitions", `{"specialCategory":{"state":"none_confirmed","references":[]}}`))
	if obsolete.Code != http.StatusBadRequest || !strings.Contains(obsolete.Body.String(), `"code":"source_eligibility_invalid"`) {
		t.Fatalf("obsolete special category body=%d/%s", obsolete.Code, obsolete.Body.String())
	}
}

func TestBlockedSaveDoesNotCallStoreAndProviderFactsCannotBeOverridden(t *testing.T) {
	store, discovery := ownerStore(), eligibilityDiscovery()
	discovery.evidenceOut.Contributors = append(discovery.evidenceOut.Contributors, copyrighteligibility.ContributorEvidence{Name: "A Translator", Role: "translator"})
	body := `{"workCategory":"ordinary_literary","workCategoryReferences":[{"source":"Catalogue","fact":"ordinary literary work"}],"firstPublicationYear":1865,"firstPublicationReferences":[{"source":"Catalogue","fact":"published in 1865"}],"translation":{"state":"none_confirmed","references":[{"source":"Catalogue","fact":"no translation"}]}}`
	response := httptest.NewRecorder()
	sourceProviderHandler(t, store, discovery).ServeHTTP(response, providerRequest(http.MethodPost, "/api/v1/admin/source-providers/project-gutenberg/works/11/acquisitions", body))
	if response.Code != http.StatusUnprocessableEntity || store.persistSourceAcquisitionCalls != 0 || !strings.Contains(response.Body.String(), `"overall":"blocked"`) {
		t.Fatalf("blocked=%d/%s store=%d", response.Code, response.Body.String(), store.persistSourceAcquisitionCalls)
	}
}

func TestFinalSaveRechecksCurrentProviderEvidence(t *testing.T) {
	store, discovery := ownerStore(), eligibilityDiscovery()
	preflight := httptest.NewRecorder()
	handler := sourceProviderHandler(t, store, discovery)
	handler.ServeHTTP(preflight, providerRequest(http.MethodPost, "/api/v1/admin/source-providers/project-gutenberg/works/11/copyright-eligibility", eligibleHumanFactsJSON()))
	if preflight.Code != http.StatusOK || !strings.Contains(preflight.Body.String(), `"overall":"eligible"`) {
		t.Fatalf("preflight=%d/%s", preflight.Code, preflight.Body.String())
	}
	discovery.evidenceOut.Rights = copyrighteligibility.ProviderRightsRestricted
	blocked := httptest.NewRecorder()
	handler.ServeHTTP(blocked, providerRequest(http.MethodPost, "/api/v1/admin/source-providers/project-gutenberg/works/11/acquisitions", eligibleHumanFactsJSON()))
	if blocked.Code != http.StatusUnprocessableEntity || store.persistSourceAcquisitionCalls != 0 || !strings.Contains(blocked.Body.String(), `"overall":"blocked"`) {
		t.Fatalf("save=%d/%s store=%d", blocked.Code, blocked.Body.String(), store.persistSourceAcquisitionCalls)
	}
}

func TestSavedSourceRoutesExposeTextOnlyOnDetailAndSourceQualityOnly(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	summary := model.AdminSourceAcquisitionSummary{ID: id, Title: "Alice", SourceQuality: model.AdminSourceQualityReview{Status: model.AdminSourceQualityPending}}
	store := ownerStore()
	store.listSourceAcquisitionsResponse = model.AdminSourceAcquisitionsListResponse{Items: []model.AdminSourceAcquisitionSummary{summary}}
	store.getSourceAcquisitionResponse = model.AdminSourceAcquisitionDetail{AdminSourceAcquisitionSummary: summary, SourceText: "Saved source\n"}
	store.qualityReviewResponse = summary
	handler := sourceProviderHandler(t, store, eligibilityDiscovery())
	list := httptest.NewRecorder()
	handler.ServeHTTP(list, providerRequest(http.MethodGet, "/api/v1/admin/source-acquisitions", ""))
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), "sourceText") {
		t.Fatalf("list=%d/%s", list.Code, list.Body.String())
	}
	detail := httptest.NewRecorder()
	handler.ServeHTTP(detail, providerRequest(http.MethodGet, "/api/v1/admin/source-acquisitions/"+id, ""))
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"sourceText":"Saved source\n"`) {
		t.Fatalf("detail=%d/%s", detail.Code, detail.Body.String())
	}
	quality := httptest.NewRecorder()
	handler.ServeHTTP(quality, providerRequest(http.MethodPut, "/api/v1/admin/source-acquisitions/"+id+"/source-quality-review", `{"status":"approved","note":"Complete."}`))
	if quality.Code != http.StatusOK || store.qualityReviewCalls != 1 || store.qualityReviewRequest.Status != model.AdminSourceQualityApproved {
		t.Fatalf("quality=%d/%s/%+v", quality.Code, quality.Body.String(), store)
	}
	old := httptest.NewRecorder()
	handler.ServeHTTP(old, providerRequest(http.MethodPut, "/api/v1/admin/source-acquisitions/"+id+"/rights-review", `{}`))
	if old.Code != http.StatusNotFound {
		t.Fatalf("old rights route=%d", old.Code)
	}
}

func TestEligibilityRoutesRetainAdminBoundaryAndSafeErrors(t *testing.T) {
	for _, test := range []struct {
		token, account, key string
		want                int
	}{{"", ownerAccount, "admin-key", http.StatusUnauthorized}, {"valid", adultAccount, "admin-key", http.StatusForbidden}, {"valid", ownerAccount, "", http.StatusForbidden}} {
		store := &adminStore{memberships: []appidentity.Membership{{AccountID: test.account, Role: appidentity.RoleAdult}}}
		if test.account == ownerAccount {
			store.memberships = []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/source-providers/project-gutenberg/works/11/copyright-eligibility", nil)
		if test.token != "" {
			req.Header.Set("Authorization", "Bearer "+test.token)
		}
		if test.account != "" {
			req.Header.Set("X-PP-Account-ID", test.account)
		}
		if test.key != "" {
			req.Header.Set("X-PP-Admin-Key", test.key)
		}
		response := httptest.NewRecorder()
		sourceProviderHandler(t, store, eligibilityDiscovery()).ServeHTTP(response, req)
		if response.Code != test.want {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	}
	stub := eligibilityDiscovery()
	stub.evidenceErr = errors.New("dial tcp 10.0.0.1: private diagnostic")
	response := httptest.NewRecorder()
	sourceProviderHandler(t, ownerStore(), stub).ServeHTTP(response, providerRequest(http.MethodPost, "/api/v1/admin/source-providers/project-gutenberg/works/11/copyright-eligibility", "{}"))
	if response.Code != http.StatusBadGateway || strings.Contains(response.Body.String(), "private diagnostic") {
		t.Fatalf("safe error=%d/%s", response.Code, response.Body.String())
	}
}

func eligibilityDiscovery() *discoveryStub {
	death := 1898
	return &discoveryStub{candidateOut: sourceprovider.SourceCandidate{Provider: sourceprovider.ProjectGutenberg, ExternalID: "11", Title: "Alice", Contributors: []sourceprovider.Contributor{{Name: "Lewis Carroll", Role: "author"}}, Languages: []string{"en"}, LandingURL: "https://www.gutenberg.org/ebooks/11", ProviderRights: "Public domain in the USA.", SelectedRepresentation: sourceprovider.Representation{MediaType: "text/plain", URL: "https://www.gutenberg.org/files/11/11.txt"}, NormalisationVersion: "project-gutenberg-plain-text-v1", RetrievedContentHash: strings.Repeat("a", 64), NormalisedContentHash: strings.Repeat("b", 64), SourceText: "Saved source\n"}, evidenceOut: copyrighteligibility.ProviderEvidence{Provider: string(sourceprovider.ProjectGutenberg), ExternalID: "11", Title: "Alice", Rights: copyrighteligibility.ProviderRightsPublicDomain, EvidenceDigest: strings.Repeat("d", 64), Contributors: []copyrighteligibility.ContributorEvidence{{Name: "Lewis Carroll", Role: "author", DeathYear: &death}, {Name: "Lewis Carroll", Role: "illustrator"}}}}
}

func eligibleHumanFactsJSON() string {
	return `{"workCategory":"ordinary_literary","workCategoryReferences":[{"source":"Catalogue","fact":"ordinary literary work"}],"firstPublicationYear":1865,"firstPublicationReferences":[{"source":"Catalogue","fact":"published in 1865"}],"translation":{"state":"none_confirmed","references":[{"source":"Catalogue","fact":"no translation in acquired text"}]},"additionalTextualContribution":{"state":"none_confirmed","references":[{"source":"Catalogue","fact":"no additional textual content"}]},"unpublishedAtEnd1988":{"state":"none_confirmed","references":[{"source":"Catalogue","fact":"published before 1988"}]}}`
}
