package httpadmin

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pandapages/api/internal/appidentity"
	"pandapages/api/internal/httpbearer"
	"pandapages/api/internal/model"
)

const editorialReviewRunID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"

type editorialReviewServiceStub struct {
	createInputs []model.AdminStoryOrchestrationEditorialReviewCreateInput
	createErr    error
	listCalls    []editorialReviewListCall
	listResponse model.AdminStoryOrchestrationEditorialReviewsListResponse
	listErr      error
}

type editorialReviewListCall struct {
	runID string
	limit int
}

func (stub *editorialReviewServiceStub) Create(
	input model.AdminStoryOrchestrationEditorialReviewCreateInput,
) (model.AdminStoryOrchestrationEditorialReview, error) {
	stub.createInputs = append(stub.createInputs, input)
	if stub.createErr != nil {
		return model.AdminStoryOrchestrationEditorialReview{}, stub.createErr
	}
	return model.AdminStoryOrchestrationEditorialReview{
		ID:                  "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		RunID:               input.RunID,
		Decision:            input.Decision,
		ReviewerPrincipalID: input.ReviewerPrincipalID,
		ReviewerAccountID:   input.ReviewerAccountID,
		CreatedAt:           "2026-08-22T12:00:00Z",
	}, nil
}

func (stub *editorialReviewServiceStub) List(
	runID string,
	limit int,
) (model.AdminStoryOrchestrationEditorialReviewsListResponse, error) {
	stub.listCalls = append(stub.listCalls, editorialReviewListCall{runID: runID, limit: limit})
	return stub.listResponse, stub.listErr
}

func editorialReviewAdminHandler(t *testing.T, store *adminStore, service StoryOrchestrationEditorialReviewService) http.Handler {
	t.Helper()
	return New(Config{
		AdminKey:                           "admin-key",
		BearerAuthenticator:                httpbearer.New(adminVerifier{}, store),
		StoryOrchestrationEditorialReviews: service,
	}, store)
}

func editorialReviewAdminRequest(method, runID, query, body string) *http.Request {
	request := httptest.NewRequest(method, "/api/v1/admin/story-orchestration-runs/"+runID+"/editorial-reviews"+query, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer valid")
	request.Header.Set("X-PP-Account-ID", ownerAccount)
	request.Header.Set("X-PP-Admin-Key", "admin-key")
	return request
}

func editorialReviewOwnerStore() *adminStore {
	return &adminStore{memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}}
}

func TestAdminStoryOrchestrationEditorialReviewCreateUsesTrustedReviewerContext(t *testing.T) {
	for _, decision := range []string{"approved", "rejected"} {
		t.Run(decision, func(t *testing.T) {
			service := &editorialReviewServiceStub{}
			response := httptest.NewRecorder()
			editorialReviewAdminHandler(t, editorialReviewOwnerStore(), service).ServeHTTP(response, editorialReviewAdminRequest(http.MethodPost, strings.ToUpper(editorialReviewRunID), "", `{"decision":"`+decision+`"}`))

			if response.Code != http.StatusCreated || len(service.createInputs) != 1 {
				t.Fatalf("status/calls = %d/%d body=%s", response.Code, len(service.createInputs), response.Body.String())
			}
			input := service.createInputs[0]
			if input.RunID != editorialReviewRunID || input.Decision != model.AdminStoryOrchestrationEditorialDecision(decision) ||
				input.ReviewerPrincipalID != ownerPrincipal || input.ReviewerAccountID != ownerAccount {
				t.Fatalf("create input = %#v", input)
			}
			if response.Header().Get("Cache-Control") != "no-store" ||
				strings.Contains(response.Body.String(), ownerPrincipal) || strings.Contains(response.Body.String(), ownerAccount) {
				t.Fatalf("response headers/body = %#v/%s", response.Header(), response.Body.String())
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || len(body) != 4 || body["id"] == nil || body["runId"] == nil || body["decision"] == nil || body["createdAt"] == nil {
				t.Fatalf("response body = %s / %v", response.Body.String(), err)
			}
		})
	}
}

func TestAdminStoryOrchestrationEditorialReviewCreateRejectsInvalidRequestsAndSafeErrors(t *testing.T) {
	for _, test := range []struct {
		name, runID, body string
		want              int
	}{
		{name: "malformed run", runID: "not-a-uuid", body: `{"decision":"approved"}`, want: http.StatusBadRequest},
		{name: "zero run", runID: zeroStoryOrchestrationRunID, body: `{"decision":"approved"}`, want: http.StatusBadRequest},
		{name: "empty body", runID: editorialReviewRunID, want: http.StatusBadRequest},
		{name: "malformed json", runID: editorialReviewRunID, body: `{"decision":`, want: http.StatusBadRequest},
		{name: "unknown field", runID: editorialReviewRunID, body: `{"decision":"approved","reviewerPrincipalId":"forged"}`, want: http.StatusBadRequest},
		{name: "trailing json", runID: editorialReviewRunID, body: `{"decision":"approved"}{}`, want: http.StatusBadRequest},
		{name: "invalid decision", runID: editorialReviewRunID, body: `{"decision":"needs_review"}`, want: http.StatusBadRequest},
		{name: "body too large", runID: editorialReviewRunID, body: strings.Repeat("x", maxStoryOrchestrationEditorialReviewBody+1), want: http.StatusRequestEntityTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &editorialReviewServiceStub{}
			response := httptest.NewRecorder()
			editorialReviewAdminHandler(t, editorialReviewOwnerStore(), service).ServeHTTP(response, editorialReviewAdminRequest(http.MethodPost, test.runID, "", test.body))
			if response.Code != test.want || len(service.createInputs) != 0 {
				t.Fatalf("status/calls = %d/%d body=%s", response.Code, len(service.createInputs), response.Body.String())
			}
		})
	}

	for _, test := range []struct {
		name string
		err  error
		want int
	}{
		{name: "unknown run", err: sql.ErrNoRows, want: http.StatusNotFound},
		{name: "repair required", err: model.ErrAdminStoryOrchestrationRunRepairRequired, want: http.StatusConflict},
		{name: "write failure", err: errors.New("private postgres detail"), want: http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &editorialReviewServiceStub{createErr: test.err}
			response := httptest.NewRecorder()
			editorialReviewAdminHandler(t, editorialReviewOwnerStore(), service).ServeHTTP(response, editorialReviewAdminRequest(http.MethodPost, editorialReviewRunID, "", `{"decision":"approved"}`))
			if response.Code != test.want || len(service.createInputs) != 1 || strings.Contains(response.Body.String(), "private postgres detail") {
				t.Fatalf("status/calls/body = %d/%d/%s", response.Code, len(service.createInputs), response.Body.String())
			}
		})
	}

	response := httptest.NewRecorder()
	editorialReviewAdminHandler(t, editorialReviewOwnerStore(), nil).ServeHTTP(response, editorialReviewAdminRequest(http.MethodPost, editorialReviewRunID, "", `{"decision":"approved"}`))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured service status = %d body=%s", response.Code, response.Body.String())
	}
}

func TestAdminStoryOrchestrationEditorialReviewCreateIsAppendOnlyAndAuthorizedFirst(t *testing.T) {
	service := &editorialReviewServiceStub{}
	handler := editorialReviewAdminHandler(t, editorialReviewOwnerStore(), service)
	for _, body := range []string{`{"decision":"approved"}`, `{"decision":"approved"}`, `{"decision":"rejected"}`} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, editorialReviewAdminRequest(http.MethodPost, editorialReviewRunID, "", body))
		if response.Code != http.StatusCreated {
			t.Fatalf("append status = %d body=%s", response.Code, response.Body.String())
		}
	}
	if len(service.createInputs) != 3 {
		t.Fatalf("append-only calls = %#v", service.createInputs)
	}

	for _, test := range []struct {
		name, runID, token, account, key string
		memberships                      []appidentity.Membership
		want                             int
	}{
		{name: "missing bearer", runID: zeroStoryOrchestrationRunID, account: ownerAccount, key: "admin-key", memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}, want: http.StatusUnauthorized},
		{name: "adult", token: "valid", account: adultAccount, key: "admin-key", memberships: []appidentity.Membership{{AccountID: adultAccount, Role: appidentity.RoleAdult}}, want: http.StatusForbidden},
		{name: "missing admin key", token: "valid", account: ownerAccount, memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}, want: http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			stub := &editorialReviewServiceStub{}
			runID := test.runID
			if runID == "" {
				runID = "not-a-uuid"
			}
			request := editorialReviewAdminRequest(http.MethodPost, runID, "", `not json`)
			request.Header.Del("Authorization")
			request.Header.Del("X-PP-Account-ID")
			request.Header.Del("X-PP-Admin-Key")
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
			editorialReviewAdminHandler(t, &adminStore{memberships: test.memberships}, stub).ServeHTTP(response, request)
			if response.Code != test.want || len(stub.createInputs) != 0 {
				t.Fatalf("status/calls = %d/%d body=%s", response.Code, len(stub.createInputs), response.Body.String())
			}
		})
	}
}

func TestAdminStoryOrchestrationEditorialReviewHistoryIsBoundedAndMetadataOnly(t *testing.T) {
	items := []model.AdminStoryOrchestrationEditorialReview{
		{ID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", RunID: editorialReviewRunID, Decision: model.AdminStoryOrchestrationEditorialDecisionRejected, ReviewerPrincipalID: ownerPrincipal, ReviewerAccountID: ownerAccount, CreatedAt: "2026-08-22T12:01:00Z"},
		{ID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", RunID: editorialReviewRunID, Decision: model.AdminStoryOrchestrationEditorialDecisionApproved, ReviewerPrincipalID: ownerPrincipal, ReviewerAccountID: ownerAccount, CreatedAt: "2026-08-22T12:00:00Z"},
	}
	for _, test := range []struct {
		name, query string
		wantLimit   int
	}{
		{name: "default", wantLimit: 50},
		{name: "one", query: "?limit=1", wantLimit: 1},
		{name: "maximum", query: "?limit=100", wantLimit: 100},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &editorialReviewServiceStub{listResponse: model.AdminStoryOrchestrationEditorialReviewsListResponse{Items: items}}
			response := httptest.NewRecorder()
			editorialReviewAdminHandler(t, editorialReviewOwnerStore(), service).ServeHTTP(response, editorialReviewAdminRequest(http.MethodGet, strings.ToUpper(editorialReviewRunID), test.query, ""))
			if response.Code != http.StatusOK || len(service.listCalls) != 1 || service.listCalls[0] != (editorialReviewListCall{runID: editorialReviewRunID, limit: test.wantLimit}) {
				t.Fatalf("status/calls = %d/%#v body=%s", response.Code, service.listCalls, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), ownerPrincipal) || strings.Contains(response.Body.String(), ownerAccount) {
				t.Fatalf("response headers/body = %#v/%s", response.Header(), response.Body.String())
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || len(body) != 1 || body["items"] == nil {
				t.Fatalf("response body = %s / %v", response.Body.String(), err)
			}
			var got []model.AdminStoryOrchestrationEditorialReview
			if err := json.Unmarshal(body["items"], &got); err != nil || len(got) != len(items) || got[0].ID != items[0].ID || got[1].ID != items[1].ID {
				t.Fatalf("history = %#v / %v", got, err)
			}
		})
	}

	service := &editorialReviewServiceStub{listResponse: model.AdminStoryOrchestrationEditorialReviewsListResponse{Items: []model.AdminStoryOrchestrationEditorialReview{}}}
	response := httptest.NewRecorder()
	editorialReviewAdminHandler(t, editorialReviewOwnerStore(), service).ServeHTTP(response, editorialReviewAdminRequest(http.MethodGet, editorialReviewRunID, "", ""))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"items":[]`) {
		t.Fatalf("zero history = %d/%s", response.Code, response.Body.String())
	}
}

func TestAdminStoryOrchestrationEditorialReviewHistoryRejectsInvalidAndSafeErrors(t *testing.T) {
	for _, test := range []struct {
		name, runID, query string
		want               int
	}{
		{name: "malformed run", runID: "bad", want: http.StatusBadRequest},
		{name: "zero run", runID: zeroStoryOrchestrationRunID, want: http.StatusBadRequest},
		{name: "bad limit", runID: editorialReviewRunID, query: "?limit=x", want: http.StatusBadRequest},
		{name: "zero limit", runID: editorialReviewRunID, query: "?limit=0", want: http.StatusBadRequest},
		{name: "too large limit", runID: editorialReviewRunID, query: "?limit=101", want: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &editorialReviewServiceStub{}
			response := httptest.NewRecorder()
			editorialReviewAdminHandler(t, editorialReviewOwnerStore(), service).ServeHTTP(response, editorialReviewAdminRequest(http.MethodGet, test.runID, test.query, ""))
			if response.Code != test.want || len(service.listCalls) != 0 {
				t.Fatalf("status/calls = %d/%d body=%s", response.Code, len(service.listCalls), response.Body.String())
			}
		})
	}

	for _, test := range []struct {
		name string
		err  error
		want int
	}{
		{name: "unknown", err: sql.ErrNoRows, want: http.StatusNotFound},
		{name: "read failure", err: errors.New("private database detail"), want: http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &editorialReviewServiceStub{listErr: test.err}
			response := httptest.NewRecorder()
			editorialReviewAdminHandler(t, editorialReviewOwnerStore(), service).ServeHTTP(response, editorialReviewAdminRequest(http.MethodGet, editorialReviewRunID, "", ""))
			if response.Code != test.want || len(service.listCalls) != 1 || strings.Contains(response.Body.String(), "private database detail") {
				t.Fatalf("status/calls/body = %d/%d/%s", response.Code, len(service.listCalls), response.Body.String())
			}
		})
	}

	response := httptest.NewRecorder()
	editorialReviewAdminHandler(t, editorialReviewOwnerStore(), nil).ServeHTTP(response, editorialReviewAdminRequest(http.MethodGet, editorialReviewRunID, "", ""))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured history service status = %d body=%s", response.Code, response.Body.String())
	}
}

func TestAdminStoryOrchestrationEditorialReviewHistoryRequiresFullAuthorization(t *testing.T) {
	for _, test := range []struct {
		name, runID, token, account, key string
		memberships                      []appidentity.Membership
		want                             int
	}{
		{name: "missing bearer", runID: zeroStoryOrchestrationRunID, account: ownerAccount, key: "admin-key", memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}, want: http.StatusUnauthorized},
		{name: "adult", token: "valid", account: adultAccount, key: "admin-key", memberships: []appidentity.Membership{{AccountID: adultAccount, Role: appidentity.RoleAdult}}, want: http.StatusForbidden},
		{name: "missing admin key", token: "valid", account: ownerAccount, memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}, want: http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &editorialReviewServiceStub{}
			runID := test.runID
			if runID == "" {
				runID = "not-a-uuid"
			}
			request := editorialReviewAdminRequest(http.MethodGet, runID, "", "")
			request.Header.Del("Authorization")
			request.Header.Del("X-PP-Account-ID")
			request.Header.Del("X-PP-Admin-Key")
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
			editorialReviewAdminHandler(t, &adminStore{memberships: test.memberships}, service).ServeHTTP(response, request)
			if response.Code != test.want || len(service.listCalls) != 0 {
				t.Fatalf("status/calls = %d/%d body=%s", response.Code, len(service.listCalls), response.Body.String())
			}
		})
	}
}
