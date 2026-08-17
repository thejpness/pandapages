package httpadmin

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/appidentity"
	"pandapages/api/internal/httpbearer"
	"pandapages/api/internal/storygeneration"
	"pandapages/api/internal/storyorchestration"
	"pandapages/api/internal/storyvalidation"
)

const orchestrationRunID = "44444444-4444-4444-8444-444444444444"
const orchestrationSourceVersionID = "55555555-5555-4555-8555-555555555555"

type orchestrationRunReaderStub struct {
	persisted storyorchestration.PersistedRun
	err       error
	calls     []string
}

func (stub *orchestrationRunReaderStub) GetCompletedStoryOrchestrationRun(runID string) (storyorchestration.PersistedRun, error) {
	stub.calls = append(stub.calls, runID)
	return stub.persisted, stub.err
}

func orchestrationRunAdminHandler(t *testing.T, store *adminStore, reader StoryOrchestrationRunReader) http.Handler {
	t.Helper()
	return New(Config{
		AdminKey:               "admin-key",
		BearerAuthenticator:    httpbearer.New(adminVerifier{}, store),
		StoryOrchestrationRuns: reader,
	}, store)
}

func orchestrationRunAdminRequest(runID string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/story-orchestration-runs/"+runID, nil)
	request.Header.Set("Authorization", "Bearer valid")
	request.Header.Set("X-PP-Account-ID", ownerAccount)
	request.Header.Set("X-PP-Admin-Key", "admin-key")
	return request
}

func TestAdminStoryOrchestrationRunReturnsCompleteEvidence(t *testing.T) {
	for _, semanticResult := range []adaptationcontract.Result{
		adaptationcontract.ResultPass,
		adaptationcontract.ResultNeedsReview,
		adaptationcontract.ResultFail,
	} {
		t.Run(string(semanticResult), func(t *testing.T) {
			persisted := testPersistedOrchestrationRun(semanticResult)
			reader := &orchestrationRunReaderStub{persisted: persisted}
			store := &adminStore{memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}}
			response := httptest.NewRecorder()
			orchestrationRunAdminHandler(t, store, reader).ServeHTTP(response, orchestrationRunAdminRequest(strings.ToUpper(orchestrationRunID)))

			if response.Code != http.StatusOK || len(reader.calls) != 1 || reader.calls[0] != orchestrationRunID {
				t.Fatalf("status/calls = %d/%v body=%s", response.Code, reader.calls, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
			}

			var body map[string]json.RawMessage
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || len(body) != 9 {
				t.Fatalf("response shape = %s / %v", response.Body.String(), err)
			}
			assertJSONField(t, body, "id", orchestrationRunID)
			assertJSONField(t, body, "sourceVersionId", orchestrationSourceVersionID)
			assertJSONField(t, body, "sourceSha256", persisted.Result.SourceSHA256)
			assertJSONField(t, body, "semanticResult", string(semanticResult))
			assertJSONField(t, body, "createdAt", persisted.CreatedAt.UTC().Format(time.RFC3339Nano))

			var analysisArtifact storygeneration.StoryAnalysisArtifact
			if err := json.Unmarshal(body["analysisArtifact"], &analysisArtifact); err != nil || analysisArtifact.PromptVersion != storygeneration.SourceAnalysisPromptVersionV3 || analysisArtifact.RequestedModel != "gpt-5.6-terra" || analysisArtifact.ResponseID != "response_analysis" {
				t.Fatalf("analysis artifact = %s / %#v / %v", body["analysisArtifact"], analysisArtifact, err)
			}

			var editions []storygeneration.GeneratedEditionArtifact
			if err := json.Unmarshal(body["editions"], &editions); err != nil || len(editions) != 4 {
				t.Fatalf("editions = %s / %v", body["editions"], err)
			}
			for index, key := range storygeneration.DerivedEditionKeysV2() {
				if editions[index].EditionKey != key || editions[index].Markdown != "# Generated "+string(key)+"\n" {
					t.Fatalf("edition %d = %#v", index, editions[index])
				}
			}
			if editions[0].PromptVersion != storygeneration.EditionPromptVersionV4 || editions[0].RequestedModel != "gpt-5.6-terra" || editions[0].ResponseID != "response_edition" {
				t.Fatalf("edition provenance = %#v", editions[0])
			}

			var assessments []storyvalidation.AssessmentArtifact
			if err := json.Unmarshal(body["editionAssessments"], &assessments); err != nil || len(assessments) != 4 {
				t.Fatalf("edition assessments = %s / %v", body["editionAssessments"], err)
			}
			for index, key := range storygeneration.DerivedEditionKeysV2() {
				if assessments[index].EditionKey == nil || *assessments[index].EditionKey != key {
					t.Fatalf("assessment order %d = %#v", index, assessments[index])
				}
			}
			finding := assessments[0].Assessment.Findings[0]
			if finding.Message != "review this generated passage" || len(finding.Evidence) != 1 || finding.Evidence[0].Excerpt != "generated excerpt" {
				t.Fatalf("semantic evidence = %#v", finding)
			}
			if assessments[0].PromptVersion != storygeneration.PromptVersion("panda-pages-semantic-validation-prompt-v3") || assessments[0].RequestedModel != "gpt-5.6-luna" || assessments[0].ResponseID != "response_assessment" {
				t.Fatalf("assessment provenance = %#v", assessments[0])
			}
			var bundle storyvalidation.AssessmentArtifact
			if err := json.Unmarshal(body["bundleAssessment"], &bundle); err != nil || bundle.AssessmentScope != adaptationcontract.AssessmentScopeBundle || len(bundle.EditionKeys) != 4 || bundle.ResponseID != "response_bundle" {
				t.Fatalf("bundle assessment = %s / %#v / %v", body["bundleAssessment"], bundle, err)
			}

			var analysis map[string]json.RawMessage
			if err := json.Unmarshal(body["analysisArtifact"], &analysis); err != nil {
				t.Fatal(err)
			}
			var analysisValue map[string]json.RawMessage
			if err := json.Unmarshal(analysis["Analysis"], &analysisValue); err != nil {
				t.Fatal(err)
			}
			if string(analysisValue["characters"]) != "null" || string(analysisValue["relationships"]) != "[]" {
				t.Fatalf("analysis nil/empty representation = characters:%s relationships:%s", analysisValue["characters"], analysisValue["relationships"])
			}
			if strings.Contains(response.Body.String(), "canonical source text must not be returned") || strings.Contains(response.Body.String(), "raw-provider-payload") || strings.Contains(response.Body.String(), "OPENAI_API_KEY") {
				t.Fatalf("response leaked unapproved data: %s", response.Body.String())
			}
		})
	}
}

func TestAdminStoryOrchestrationRunRejectsInvalidAndUnavailableReads(t *testing.T) {
	store := &adminStore{memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}}
	reader := &orchestrationRunReaderStub{}
	response := httptest.NewRecorder()
	orchestrationRunAdminHandler(t, store, reader).ServeHTTP(response, orchestrationRunAdminRequest("not-a-uuid"))
	if response.Code != http.StatusBadRequest || len(reader.calls) != 0 {
		t.Fatalf("malformed status/calls = %d/%d", response.Code, len(reader.calls))
	}

	response = httptest.NewRecorder()
	orchestrationRunAdminHandler(t, store, nil).ServeHTTP(response, orchestrationRunAdminRequest(orchestrationRunID))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured reader status = %d", response.Code)
	}

	for _, test := range []struct {
		name string
		err  error
		want int
	}{
		{name: "not found", err: sql.ErrNoRows, want: http.StatusNotFound},
		{name: "corrupt evidence", err: errors.New("private artifact validation detail"), want: http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := &orchestrationRunReaderStub{err: test.err}
			response := httptest.NewRecorder()
			orchestrationRunAdminHandler(t, store, reader).ServeHTTP(response, orchestrationRunAdminRequest(orchestrationRunID))
			if response.Code != test.want || len(reader.calls) != 1 || strings.Contains(response.Body.String(), "private artifact validation detail") {
				t.Fatalf("status/calls/body = %d/%d/%s", response.Code, len(reader.calls), response.Body.String())
			}
		})
	}
}

func TestAdminStoryOrchestrationRunRequiresFullAuthorization(t *testing.T) {
	for _, test := range []struct {
		name, token, account, key string
		memberships               []appidentity.Membership
		want                      int
	}{
		{name: "missing bearer", account: ownerAccount, key: "admin-key", memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}, want: http.StatusUnauthorized},
		{name: "adult", token: "valid", account: adultAccount, key: "admin-key", memberships: []appidentity.Membership{{AccountID: adultAccount, Role: appidentity.RoleAdult}}, want: http.StatusForbidden},
		{name: "nonmember", token: "valid", account: adultAccount, key: "admin-key", memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}, want: http.StatusForbidden},
		{name: "missing key", token: "valid", account: ownerAccount, memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}, want: http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := &orchestrationRunReaderStub{}
			store := &adminStore{memberships: test.memberships}
			request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/story-orchestration-runs/"+orchestrationRunID, nil)
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
			orchestrationRunAdminHandler(t, store, reader).ServeHTTP(response, request)
			if response.Code != test.want || len(reader.calls) != 0 {
				t.Fatalf("status/calls = %d/%d", response.Code, len(reader.calls))
			}
		})
	}
}

func assertJSONField(t *testing.T, body map[string]json.RawMessage, key, want string) {
	t.Helper()
	var got string
	if err := json.Unmarshal(body[key], &got); err != nil || got != want {
		t.Fatalf("response %s = %s / %q / %v", key, body[key], got, err)
	}
}

func testPersistedOrchestrationRun(result adaptationcontract.Result) storyorchestration.PersistedRun {
	keys := storygeneration.DerivedEditionKeysV2()
	analysis := storygeneration.StoryAnalysisArtifact{
		SpecificationVersion: storygeneration.SpecificationV2,
		PromptVersion:        storygeneration.SourceAnalysisPromptVersionV3,
		RequestedModel:       "gpt-5.6-terra",
		ReturnedModel:        "gpt-5.6-terra",
		ReasoningEffort:      storygeneration.ReasoningEffortMedium,
		SourceSHA256:         "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		AnalysisSHA256:       "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Analysis: storygeneration.StoryAnalysis{
			CentralPlot:   "central plot",
			Characters:    nil,
			Relationships: []storygeneration.Relationship{},
		},
		ResponseID: "response_analysis",
	}
	editions := make([]storygeneration.GeneratedEditionArtifact, 0, len(keys))
	assessments := make([]storyvalidation.AssessmentArtifact, 0, len(keys))
	for index, key := range keys {
		editions = append(editions, storygeneration.GeneratedEditionArtifact{
			SpecificationVersion: storygeneration.SpecificationV2,
			PromptVersion:        storygeneration.EditionPromptVersionV4,
			EditionKey:           key,
			RequestedModel:       "gpt-5.6-terra",
			ReturnedModel:        "gpt-5.6-terra",
			ReasoningEffort:      storygeneration.ReasoningEffortMedium,
			SourceSHA256:         analysis.SourceSHA256,
			AnalysisSHA256:       analysis.AnalysisSHA256,
			ContentSHA256:        "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			Markdown:             "# Generated " + string(key) + "\n",
			ResponseID:           "response_edition",
			StructuralValidation: adaptationcontract.StructuralValidation{Findings: []adaptationcontract.Finding{}},
		})
		findings := []storyvalidation.Finding{}
		if index == 0 {
			findings = []storyvalidation.Finding{{
				Code:     adaptationcontract.FindingScopeTooThin,
				Severity: adaptationcontract.FindingSeverityReview,
				Message:  "review this generated passage",
				Evidence: []storyvalidation.Evidence{{Location: storyvalidation.EvidenceGeneratedEdition, EditionKey: &key, Excerpt: "generated excerpt", Explanation: "scope evidence"}},
			}}
		}
		assessments = append(assessments, storyvalidation.AssessmentArtifact{
			ValidationVersion:    storyvalidation.ValidationV3,
			SpecificationVersion: storygeneration.SpecificationV2,
			PromptVersion:        storygeneration.PromptVersion("panda-pages-semantic-validation-prompt-v3"),
			AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
			EditionKey:           &key,
			RequestedModel:       "gpt-5.6-luna",
			ReturnedModel:        "gpt-5.6-luna",
			ReasoningEffort:      storygeneration.ReasoningEffortMedium,
			SourceSHA256:         analysis.SourceSHA256,
			AnalysisSHA256:       analysis.AnalysisSHA256,
			AssessmentSHA256:     "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
			Assessment: storyvalidation.Assessment{
				ValidationVersion:    storyvalidation.ValidationV3,
				SpecificationVersion: storygeneration.SpecificationV2,
				AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
				EditionKey:           &key,
				Result:               result,
				Findings:             findings,
			},
			ResponseID: "response_assessment",
		})
	}
	return storyorchestration.PersistedRun{
		ID:              orchestrationRunID,
		SourceVersionID: orchestrationSourceVersionID,
		CreatedAt:       time.Date(2026, time.August, 17, 12, 0, 0, 123, time.UTC),
		Result: storyorchestration.Result{
			SourceIdentity:     orchestrationSourceVersionID,
			SourceSHA256:       analysis.SourceSHA256,
			AnalysisArtifact:   analysis,
			Editions:           editions,
			EditionAssessments: assessments,
			BundleAssessment: storyvalidation.AssessmentArtifact{
				ValidationVersion:    storyvalidation.ValidationV3,
				SpecificationVersion: storygeneration.SpecificationV2,
				PromptVersion:        storygeneration.PromptVersion("panda-pages-semantic-validation-prompt-v3"),
				AssessmentScope:      adaptationcontract.AssessmentScopeBundle,
				EditionKeys:          keys,
				RequestedModel:       "gpt-5.6-luna",
				ReturnedModel:        "gpt-5.6-luna",
				ReasoningEffort:      storygeneration.ReasoningEffortMedium,
				SourceSHA256:         analysis.SourceSHA256,
				AnalysisSHA256:       analysis.AnalysisSHA256,
				AssessmentSHA256:     "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
				Assessment: storyvalidation.Assessment{
					ValidationVersion:    storyvalidation.ValidationV3,
					SpecificationVersion: storygeneration.SpecificationV2,
					AssessmentScope:      adaptationcontract.AssessmentScopeBundle,
					EditionKeys:          keys,
					Result:               result,
					Findings:             []storyvalidation.Finding{},
				},
				ResponseID: "response_bundle",
			},
			SemanticResult: result,
		},
	}
}
