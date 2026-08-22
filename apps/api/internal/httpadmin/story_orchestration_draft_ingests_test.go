package httpadmin

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pandapages/api/internal/appidentity"
	"pandapages/api/internal/httpbearer"
	"pandapages/api/internal/model"
)

const editorialReviewIngestID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"

type storyOrchestrationDraftIngestServiceStub struct {
	inputs []model.AdminStoryOrchestrationDraftIngestInput
	out    model.AdminStoryOrchestrationDraftIngestResponse
	err    error
}

func (stub *storyOrchestrationDraftIngestServiceStub) CreateStoryOrchestrationDraftIngest(
	input model.AdminStoryOrchestrationDraftIngestInput,
) (model.AdminStoryOrchestrationDraftIngestResponse, error) {
	stub.inputs = append(stub.inputs, input)
	if stub.err != nil {
		return model.AdminStoryOrchestrationDraftIngestResponse{}, stub.err
	}
	if stub.out.ID == "" {
		stub.out = model.AdminStoryOrchestrationDraftIngestResponse{
			ID:                "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
			RunID:             input.RunID,
			EditorialReviewID: input.EditorialReviewID,
			StorySlug:         "draft-ingest-story",
			CreatedAt:         "2026-08-22T12:00:00Z",
			Outcome:           model.AdminStoryOrchestrationDraftIngestOutcomeCreated,
			Editions: []model.AdminStoryOrchestrationDraftIngestEdition{
				{EditionKey: model.AdminStoryEditionConfidentReaders, EditionID: "dddddddd-dddd-4ddd-8ddd-dddddddddddd", StoryVersionID: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"},
				{EditionKey: model.AdminStoryEditionGrowingReaders, EditionID: "ffffffff-ffff-4fff-8fff-ffffffffffff", StoryVersionID: "11111111-2222-4333-8444-555555555555"},
				{EditionKey: model.AdminStoryEditionStoryExplorers, EditionID: "66666666-7777-4888-8999-aaaaaaaaaaaa", StoryVersionID: "bbbbbbbb-cccc-4ddd-8eee-ffffffffffff"},
				{EditionKey: model.AdminStoryEditionLittleListeners, EditionID: "11111111-2222-4333-8444-666666666666", StoryVersionID: "77777777-8888-4999-8aaa-bbbbbbbbbbbb"},
			},
		}
	}
	return stub.out, nil
}

func draftIngestAdminHandler(t *testing.T, store *adminStore, service StoryOrchestrationDraftIngestService) http.Handler {
	t.Helper()
	return New(Config{
		AdminKey:                       "admin-key",
		BearerAuthenticator:            httpbearer.New(adminVerifier{}, store),
		StoryOrchestrationDraftIngests: service,
	}, store)
}

func draftIngestAdminRequest(runID, body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/story-orchestration-runs/"+runID+"/draft-ingests", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer valid")
	request.Header.Set("X-PP-Account-ID", ownerAccount)
	request.Header.Set("X-PP-Admin-Key", "admin-key")
	return request
}

func TestAdminStoryOrchestrationDraftIngestCreatesAndReusesExactApprovedReview(t *testing.T) {
	store := &adminStore{memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}}
	for _, outcome := range []struct {
		name    string
		outcome model.AdminStoryOrchestrationDraftIngestOutcome
		want    int
	}{
		{name: "created", outcome: model.AdminStoryOrchestrationDraftIngestOutcomeCreated, want: http.StatusCreated},
		{name: "reused", outcome: model.AdminStoryOrchestrationDraftIngestOutcomeReused, want: http.StatusOK},
	} {
		t.Run(outcome.name, func(t *testing.T) {
			service := &storyOrchestrationDraftIngestServiceStub{out: model.AdminStoryOrchestrationDraftIngestResponse{
				ID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", RunID: editorialReviewRunID, EditorialReviewID: editorialReviewIngestID,
				StorySlug: "draft-ingest-story", CreatedAt: "2026-08-22T12:00:00Z", Outcome: outcome.outcome,
				Editions: []model.AdminStoryOrchestrationDraftIngestEdition{},
			}}
			response := httptest.NewRecorder()
			draftIngestAdminHandler(t, store, service).ServeHTTP(response, draftIngestAdminRequest(strings.ToUpper(editorialReviewRunID), `{"editorialReviewId":"`+strings.ToUpper(editorialReviewIngestID)+`"}`))
			if response.Code != outcome.want || len(service.inputs) != 1 || service.inputs[0] != (model.AdminStoryOrchestrationDraftIngestInput{RunID: editorialReviewRunID, EditorialReviewID: editorialReviewIngestID}) {
				t.Fatalf("status/inputs = %d/%#v body=%s", response.Code, service.inputs, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), ownerPrincipal) || strings.Contains(response.Body.String(), ownerAccount) {
				t.Fatalf("headers/body = %#v/%s", response.Header(), response.Body.String())
			}
		})
	}
}

func TestAdminStoryOrchestrationDraftIngestRejectsInvalidInputAndMapsSafeErrors(t *testing.T) {
	store := &adminStore{memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}}
	for _, test := range []struct {
		name, runID, body string
		want              int
	}{
		{name: "malformed run", runID: "not-a-uuid", body: `{"editorialReviewId":"` + editorialReviewIngestID + `"}`, want: http.StatusBadRequest},
		{name: "zero run", runID: zeroStoryOrchestrationRunID, body: `{"editorialReviewId":"` + editorialReviewIngestID + `"}`, want: http.StatusBadRequest},
		{name: "empty body", runID: editorialReviewRunID, want: http.StatusBadRequest},
		{name: "malformed json", runID: editorialReviewRunID, body: `{"editorialReviewId":`, want: http.StatusBadRequest},
		{name: "unknown field", runID: editorialReviewRunID, body: `{"editorialReviewId":"` + editorialReviewIngestID + `","storySlug":"forged"}`, want: http.StatusBadRequest},
		{name: "trailing json", runID: editorialReviewRunID, body: `{"editorialReviewId":"` + editorialReviewIngestID + `"}{}`, want: http.StatusBadRequest},
		{name: "malformed review", runID: editorialReviewRunID, body: `{"editorialReviewId":"bad"}`, want: http.StatusBadRequest},
		{name: "zero review", runID: editorialReviewRunID, body: `{"editorialReviewId":"` + zeroStoryOrchestrationRunID + `"}`, want: http.StatusBadRequest},
		{name: "too large", runID: editorialReviewRunID, body: strings.Repeat("x", maxStoryOrchestrationDraftIngestBody+1), want: http.StatusRequestEntityTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &storyOrchestrationDraftIngestServiceStub{}
			response := httptest.NewRecorder()
			draftIngestAdminHandler(t, store, service).ServeHTTP(response, draftIngestAdminRequest(test.runID, test.body))
			if response.Code != test.want || len(service.inputs) != 0 {
				t.Fatalf("status/inputs = %d/%#v body=%s", response.Code, service.inputs, response.Body.String())
			}
		})
	}

	for _, test := range []struct {
		name string
		err  error
		want int
	}{
		{name: "not found", err: sql.ErrNoRows, want: http.StatusNotFound},
		{name: "run repair", err: model.ErrAdminStoryOrchestrationRunRepairRequired, want: http.StatusConflict},
		{name: "conflict", err: model.ErrAdminStoryOrchestrationDraftIngestConflict, want: http.StatusConflict},
		{name: "internal", err: errors.New("private postgres detail"), want: http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &storyOrchestrationDraftIngestServiceStub{err: test.err}
			response := httptest.NewRecorder()
			draftIngestAdminHandler(t, store, service).ServeHTTP(response, draftIngestAdminRequest(editorialReviewRunID, `{"editorialReviewId":"`+editorialReviewIngestID+`"}`))
			if response.Code != test.want || len(service.inputs) != 1 || strings.Contains(response.Body.String(), "private postgres detail") {
				t.Fatalf("status/inputs/body = %d/%#v/%s", response.Code, service.inputs, response.Body.String())
			}
		})
	}

	response := httptest.NewRecorder()
	draftIngestAdminHandler(t, store, nil).ServeHTTP(response, draftIngestAdminRequest(editorialReviewRunID, `{"editorialReviewId":"`+editorialReviewIngestID+`"}`))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured service status = %d body=%s", response.Code, response.Body.String())
	}
}

func TestAdminStoryOrchestrationDraftIngestAuthorizesBeforeInputInspection(t *testing.T) {
	service := &storyOrchestrationDraftIngestServiceStub{}
	request := draftIngestAdminRequest(zeroStoryOrchestrationRunID, `not json`)
	request.Header.Del("Authorization")
	response := httptest.NewRecorder()
	draftIngestAdminHandler(t, &adminStore{memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}}, service).ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || len(service.inputs) != 0 {
		t.Fatalf("status/inputs = %d/%#v body=%s", response.Code, service.inputs, response.Body.String())
	}
}
