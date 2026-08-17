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

type orchestrationRunHistoryReaderStub struct {
	response model.AdminStoryOrchestrationRunsListResponse
	err      error
	calls    []orchestrationRunHistoryCall
}

type orchestrationRunHistoryCall struct {
	sourceVersionID string
	limit           int
}

func (stub *orchestrationRunHistoryReaderStub) ListCompletedStoryOrchestrationRuns(
	sourceVersionID string,
	limit int,
) (model.AdminStoryOrchestrationRunsListResponse, error) {
	stub.calls = append(stub.calls, orchestrationRunHistoryCall{sourceVersionID: sourceVersionID, limit: limit})
	return stub.response, stub.err
}

func orchestrationRunHistoryAdminHandler(t *testing.T, store *adminStore, reader StoryOrchestrationRunHistoryReader) http.Handler {
	t.Helper()
	return New(Config{
		AdminKey:                     "admin-key",
		BearerAuthenticator:          httpbearer.New(adminVerifier{}, store),
		StoryOrchestrationRunHistory: reader,
	}, store)
}

func orchestrationRunHistoryAdminRequest(sourceVersionID, query string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/source-versions/"+sourceVersionID+"/orchestration-runs"+query, nil)
	request.Header.Set("Authorization", "Bearer valid")
	request.Header.Set("X-PP-Account-ID", ownerAccount)
	request.Header.Set("X-PP-Admin-Key", "admin-key")
	return request
}

func TestAdminStoryOrchestrationRunHistoryReturnsBoundedSummaries(t *testing.T) {
	items := []model.AdminStoryOrchestrationRunSummary{
		{
			ID:              "11111111-1111-4111-8111-111111111111",
			SourceVersionID: orchestrationSourceVersionID,
			SourceSHA256:    strings.Repeat("a", 64),
			SemanticResult:  "fail",
			CreatedAt:       "2026-01-03T10:00:00Z",
		},
		{
			ID:              "22222222-2222-4222-8222-222222222222",
			SourceVersionID: orchestrationSourceVersionID,
			SourceSHA256:    strings.Repeat("a", 64),
			SemanticResult:  "needs_review",
			CreatedAt:       "2026-01-02T10:00:00Z",
		},
		{
			ID:              "33333333-3333-4333-8333-333333333333",
			SourceVersionID: orchestrationSourceVersionID,
			SourceSHA256:    strings.Repeat("a", 64),
			SemanticResult:  "pass",
			CreatedAt:       "2026-01-01T10:00:00Z",
		},
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
			reader := &orchestrationRunHistoryReaderStub{response: model.AdminStoryOrchestrationRunsListResponse{Items: items}}
			store := &adminStore{memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}}
			response := httptest.NewRecorder()
			orchestrationRunHistoryAdminHandler(t, store, reader).ServeHTTP(response, orchestrationRunHistoryAdminRequest(strings.ToUpper(orchestrationSourceVersionID), test.query))

			if response.Code != http.StatusOK || len(reader.calls) != 1 ||
				reader.calls[0].sourceVersionID != orchestrationSourceVersionID || reader.calls[0].limit != test.wantLimit {
				t.Fatalf("status/calls = %d/%#v body=%s", response.Code, reader.calls, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
			}

			var body map[string]json.RawMessage
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || len(body) != 1 {
				t.Fatalf("response shape = %s / %v", response.Body.String(), err)
			}
			var got []model.AdminStoryOrchestrationRunSummary
			if err := json.Unmarshal(body["items"], &got); err != nil {
				t.Fatalf("response items = %s / %v", body["items"], err)
			}
			if len(got) != len(items) {
				t.Fatalf("response item length = %d", len(got))
			}
			for index := range items {
				if got[index] != items[index] {
					t.Fatalf("response item %d = %#v, want %#v", index, got[index], items[index])
				}
			}
			if strings.Contains(response.Body.String(), "analysisArtifact") ||
				strings.Contains(response.Body.String(), "generated Markdown") ||
				strings.Contains(response.Body.String(), "canonical source text") ||
				strings.Contains(response.Body.String(), "response_id") ||
				strings.Contains(response.Body.String(), "finding") {
				t.Fatalf("response leaked detailed evidence: %s", response.Body.String())
			}
		})
	}
}

func TestAdminStoryOrchestrationRunHistoryReturnsEmptyItems(t *testing.T) {
	reader := &orchestrationRunHistoryReaderStub{response: model.AdminStoryOrchestrationRunsListResponse{Items: []model.AdminStoryOrchestrationRunSummary{}}}
	store := &adminStore{memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}}
	response := httptest.NewRecorder()
	orchestrationRunHistoryAdminHandler(t, store, reader).ServeHTTP(response, orchestrationRunHistoryAdminRequest(orchestrationSourceVersionID, ""))
	if response.Code != http.StatusOK || len(reader.calls) != 1 || !strings.Contains(response.Body.String(), `"items":[]`) {
		t.Fatalf("status/calls/body = %d/%d/%s", response.Code, len(reader.calls), response.Body.String())
	}
}

func TestAdminStoryOrchestrationRunHistoryRejectsInvalidRequestsAndReadFailures(t *testing.T) {
	store := &adminStore{memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}}
	for _, test := range []struct {
		name, sourceVersionID, query string
		want                         int
	}{
		{name: "malformed source version", sourceVersionID: "not-a-uuid", want: http.StatusBadRequest},
		{name: "malformed limit", sourceVersionID: orchestrationSourceVersionID, query: "?limit=nope", want: http.StatusBadRequest},
		{name: "zero limit", sourceVersionID: orchestrationSourceVersionID, query: "?limit=0", want: http.StatusBadRequest},
		{name: "negative limit", sourceVersionID: orchestrationSourceVersionID, query: "?limit=-1", want: http.StatusBadRequest},
		{name: "too large limit", sourceVersionID: orchestrationSourceVersionID, query: "?limit=101", want: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := &orchestrationRunHistoryReaderStub{}
			response := httptest.NewRecorder()
			orchestrationRunHistoryAdminHandler(t, store, reader).ServeHTTP(response, orchestrationRunHistoryAdminRequest(test.sourceVersionID, test.query))
			if response.Code != test.want || len(reader.calls) != 0 {
				t.Fatalf("status/calls = %d/%d body=%s", response.Code, len(reader.calls), response.Body.String())
			}
		})
	}

	response := httptest.NewRecorder()
	orchestrationRunHistoryAdminHandler(t, store, nil).ServeHTTP(response, orchestrationRunHistoryAdminRequest(orchestrationSourceVersionID, ""))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured reader status = %d", response.Code)
	}

	for _, test := range []struct {
		name string
		err  error
		want int
	}{
		{name: "not found", err: sql.ErrNoRows, want: http.StatusNotFound},
		{name: "read failure", err: errors.New("private database detail"), want: http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := &orchestrationRunHistoryReaderStub{err: test.err}
			response := httptest.NewRecorder()
			orchestrationRunHistoryAdminHandler(t, store, reader).ServeHTTP(response, orchestrationRunHistoryAdminRequest(orchestrationSourceVersionID, ""))
			if response.Code != test.want || len(reader.calls) != 1 || strings.Contains(response.Body.String(), "private database detail") {
				t.Fatalf("status/calls/body = %d/%d/%s", response.Code, len(reader.calls), response.Body.String())
			}
		})
	}
}

func TestAdminStoryOrchestrationRunHistoryRequiresFullAuthorization(t *testing.T) {
	for _, test := range []struct {
		name, token, account, key string
		memberships               []appidentity.Membership
		want                      int
	}{
		{name: "missing bearer", account: ownerAccount, key: "admin-key", memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}, want: http.StatusUnauthorized},
		{name: "adult", token: "valid", account: adultAccount, key: "admin-key", memberships: []appidentity.Membership{{AccountID: adultAccount, Role: appidentity.RoleAdult}}, want: http.StatusForbidden},
		{name: "nonmember", token: "valid", account: ownerAccount, key: "admin-key", memberships: []appidentity.Membership{{AccountID: secondOwnerAccount, Role: appidentity.RoleOwner}}, want: http.StatusForbidden},
		{name: "missing admin key", token: "valid", account: ownerAccount, memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}, want: http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := &orchestrationRunHistoryReaderStub{}
			store := &adminStore{memberships: test.memberships}
			request := orchestrationRunHistoryAdminRequest(orchestrationSourceVersionID, "")
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
			orchestrationRunHistoryAdminHandler(t, store, reader).ServeHTTP(response, request)
			if response.Code != test.want || len(reader.calls) != 0 {
				t.Fatalf("status/calls = %d/%d body=%s", response.Code, len(reader.calls), response.Body.String())
			}
		})
	}
}
