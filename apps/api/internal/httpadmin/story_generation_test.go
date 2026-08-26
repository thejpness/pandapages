package httpadmin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pandapages/api/internal/appidentity"
	"pandapages/api/internal/httpbearer"
	"pandapages/api/internal/model"
)

const generationSourceVersionID = "11111111-1111-4111-8111-111111111111"

type storyGenerationJobStub struct {
	job           model.AdminStoryGenerationJob
	enqueueErr    error
	getErr        error
	activeErr     error
	enqueueInputs []model.AdminStoryGenerationJobCreateInput
	getIDs        []string
	activeIDs     []string
}

func (stub *storyGenerationJobStub) Enqueue(_ context.Context, input model.AdminStoryGenerationJobCreateInput) (model.AdminStoryGenerationJob, error) {
	stub.enqueueInputs = append(stub.enqueueInputs, input)
	return stub.job, stub.enqueueErr
}

func (stub *storyGenerationJobStub) Get(_ context.Context, jobID string) (model.AdminStoryGenerationJob, error) {
	stub.getIDs = append(stub.getIDs, jobID)
	return stub.job, stub.getErr
}

func (stub *storyGenerationJobStub) GetActiveForSourceVersion(_ context.Context, sourceVersionID string) (model.AdminStoryGenerationJob, error) {
	stub.activeIDs = append(stub.activeIDs, sourceVersionID)
	return stub.job, stub.activeErr
}

func storyGenerationAdminHandler(t *testing.T, store *adminStore, service StoryGenerationJobService) http.Handler {
	t.Helper()
	return New(Config{
		AdminKey:            "admin-key",
		BearerAuthenticator: httpbearer.New(adminVerifier{}, store),
		StoryGenerationJobs: service,
	}, store)
}

func storyGenerationAdminRequest(method, target, body string) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer valid")
	request.Header.Set("X-PP-Account-ID", ownerAccount)
	request.Header.Set("X-PP-Admin-Key", "admin-key")
	return request
}

func queuedStoryGenerationJob() model.AdminStoryGenerationJob {
	createdAt := time.Date(2026, 8, 26, 9, 30, 0, 0, time.UTC).Format(time.RFC3339Nano)
	return model.AdminStoryGenerationJob{
		ID:              "22222222-2222-4222-8222-222222222222",
		SourceVersionID: generationSourceVersionID,
		Status:          model.AdminStoryGenerationJobQueued,
		Stage:           model.AdminStoryGenerationJobStageQueued,
		CreatedAt:       createdAt,
	}
}

func TestAdminStoryGenerationJobRouteRequiresFullAdminAuthorization(t *testing.T) {
	tests := []struct {
		name, token, account, key string
		memberships               []appidentity.Membership
		want                      int
	}{
		{name: "missing bearer", account: ownerAccount, key: "admin-key", memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}, want: http.StatusUnauthorized},
		{name: "adult", token: "valid", account: adultAccount, key: "admin-key", memberships: []appidentity.Membership{{AccountID: adultAccount, Role: appidentity.RoleAdult}}, want: http.StatusForbidden},
		{name: "nonmember", token: "valid", account: adultAccount, key: "admin-key", memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}, want: http.StatusForbidden},
		{name: "missing key", token: "valid", account: ownerAccount, memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}, want: http.StatusForbidden},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub := &storyGenerationJobStub{job: queuedStoryGenerationJob()}
			store := &adminStore{memberships: test.memberships}
			handler := storyGenerationAdminHandler(t, store, stub)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/source-versions/"+generationSourceVersionID+"/generation-jobs", nil)
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
			handler.ServeHTTP(response, request)
			if response.Code != test.want || len(stub.enqueueInputs) != 0 {
				t.Fatalf("status/enqueues = %d/%d body=%s", response.Code, len(stub.enqueueInputs), response.Body.String())
			}
		})
	}
}

func TestAdminStoryGenerationJobRouteAcceptsDurableJob(t *testing.T) {
	store := &adminStore{memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}}
	stub := &storyGenerationJobStub{job: queuedStoryGenerationJob()}
	response := httptest.NewRecorder()
	storyGenerationAdminHandler(t, store, stub).ServeHTTP(response, storyGenerationAdminRequest(
		http.MethodPost,
		"/api/v1/admin/source-versions/"+generationSourceVersionID+"/generation-jobs",
		"",
	))
	if response.Code != http.StatusAccepted || len(stub.enqueueInputs) != 1 {
		t.Fatalf("status/enqueues = %d/%d body=%s", response.Code, len(stub.enqueueInputs), response.Body.String())
	}
	input := stub.enqueueInputs[0]
	if input.SourceVersionID != generationSourceVersionID || input.RequesterPrincipalID == "" || input.RequesterAccountID != ownerAccount {
		t.Fatalf("enqueue input = %#v", input)
	}
	var job model.AdminStoryGenerationJob
	if err := json.Unmarshal(response.Body.Bytes(), &job); err != nil || job != stub.job {
		t.Fatalf("response job = %#v / %v", job, err)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
	}
}

func TestAdminStoryGenerationJobRouteRejectsInvalidRequestsAndUnavailableGeneration(t *testing.T) {
	store := &adminStore{memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}}
	stub := &storyGenerationJobStub{job: queuedStoryGenerationJob()}
	handler := storyGenerationAdminHandler(t, store, stub)
	for _, test := range []struct {
		name, target, body, code string
		status                   int
	}{
		{name: "malformed source version", target: "/api/v1/admin/source-versions/not-a-uuid/generation-jobs", status: http.StatusBadRequest, code: "generation_source_version_invalid"},
		{name: "nonempty body", target: "/api/v1/admin/source-versions/" + generationSourceVersionID + "/generation-jobs", body: "{}", status: http.StatusBadRequest, code: "generation_request_invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, storyGenerationAdminRequest(http.MethodPost, test.target, test.body))
			if response.Code != test.status || !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) || len(stub.enqueueInputs) != 0 {
				t.Fatalf("status/enqueues/body = %d/%d/%s", response.Code, len(stub.enqueueInputs), response.Body.String())
			}
		})
	}

	response := httptest.NewRecorder()
	storyGenerationAdminHandler(t, store, nil).ServeHTTP(response, storyGenerationAdminRequest(
		http.MethodPost,
		"/api/v1/admin/source-versions/"+generationSourceVersionID+"/generation-jobs",
		"",
	))
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"code":"generation_unavailable"`) {
		t.Fatalf("unavailable response = %d/%s", response.Code, response.Body.String())
	}
}

func TestAdminStoryGenerationJobLookups(t *testing.T) {
	store := &adminStore{memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}}
	for _, test := range []struct {
		name, target string
		wantGet      int
		wantActive   int
	}{
		{name: "job", target: "/api/v1/admin/generation-jobs/22222222-2222-4222-8222-222222222222", wantGet: 1},
		{name: "active source job", target: "/api/v1/admin/source-versions/" + generationSourceVersionID + "/generation-jobs/active", wantActive: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			stub := &storyGenerationJobStub{job: queuedStoryGenerationJob()}
			handler := storyGenerationAdminHandler(t, store, stub)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, storyGenerationAdminRequest(http.MethodGet, test.target, ""))
			if response.Code != http.StatusOK || len(stub.getIDs) != test.wantGet || len(stub.activeIDs) != test.wantActive {
				t.Fatalf("status/get/active = %d/%d/%d body=%s", response.Code, len(stub.getIDs), len(stub.activeIDs), response.Body.String())
			}
		})
	}

	stub := &storyGenerationJobStub{job: queuedStoryGenerationJob(), getErr: sql.ErrNoRows}
	handler := storyGenerationAdminHandler(t, store, stub)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, storyGenerationAdminRequest(http.MethodGet, "/api/v1/admin/generation-jobs/"+stub.job.ID, ""))
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"code":"generation_job_not_found"`) {
		t.Fatalf("not found response = %d/%s", response.Code, response.Body.String())
	}
	if !errors.Is(stub.getErr, sql.ErrNoRows) {
		t.Fatal("test setup lost not-found error")
	}
}
