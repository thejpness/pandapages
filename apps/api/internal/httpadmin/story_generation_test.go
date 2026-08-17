package httpadmin

import (
	"context"
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
)

const generationSourceVersionID = "11111111-1111-4111-8111-111111111111"

type storyGenerationStub struct {
	persisted storyorchestration.PersistedRun
	err       error
	calls     []string
	contexts  []context.Context
	run       func(context.Context, string) (storyorchestration.PersistedRun, error)
}

func (stub *storyGenerationStub) Run(ctx context.Context, sourceVersionID string) (storyorchestration.PersistedRun, error) {
	stub.calls = append(stub.calls, sourceVersionID)
	stub.contexts = append(stub.contexts, ctx)
	if stub.run != nil {
		return stub.run(ctx, sourceVersionID)
	}
	return stub.persisted, stub.err
}

func storyGenerationAdminHandler(t *testing.T, store *adminStore, service StoryGenerationService) http.Handler {
	t.Helper()
	return New(Config{
		AdminKey:            "admin-key",
		BearerAuthenticator: httpbearer.New(adminVerifier{}, store),
		StoryGeneration:     service,
	}, store)
}

func storyGenerationAdminRequest(ctx context.Context, sourceVersionID, body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/source-versions/"+sourceVersionID+"/generate", strings.NewReader(body)).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer valid")
	request.Header.Set("X-PP-Account-ID", ownerAccount)
	request.Header.Set("X-PP-Admin-Key", "admin-key")
	return request
}

func TestAdminStoryGenerationRouteRequiresFullAdminAuthorization(t *testing.T) {
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
			stub := &storyGenerationStub{}
			store := &adminStore{memberships: test.memberships}
			handler := storyGenerationAdminHandler(t, store, stub)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/source-versions/"+generationSourceVersionID+"/generate", nil)
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
			if response.Code != test.want || len(stub.calls) != 0 {
				t.Fatalf("status/calls = %d/%d body=%s", response.Code, len(stub.calls), response.Body.String())
			}
		})
	}
}

func TestAdminStoryGenerationRouteCreatesEverySemanticOutcome(t *testing.T) {
	for _, semanticResult := range []adaptationcontract.Result{
		adaptationcontract.ResultPass,
		adaptationcontract.ResultNeedsReview,
		adaptationcontract.ResultFail,
	} {
		t.Run(string(semanticResult), func(t *testing.T) {
			createdAt := time.Date(2026, 8, 17, 12, 0, 0, 123, time.UTC)
			persisted := storyorchestration.PersistedRun{
				ID:              "22222222-2222-4222-8222-222222222222",
				SourceVersionID: generationSourceVersionID,
				CreatedAt:       createdAt,
				Result:          storyorchestration.Result{SourceIdentity: generationSourceVersionID, SemanticResult: semanticResult},
			}
			stub := &storyGenerationStub{persisted: persisted}
			store := &adminStore{memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}}
			response := httptest.NewRecorder()
			storyGenerationAdminHandler(t, store, stub).ServeHTTP(response, storyGenerationAdminRequest(context.Background(), generationSourceVersionID, ""))
			if response.Code != http.StatusCreated || len(stub.calls) != 1 || stub.calls[0] != generationSourceVersionID {
				t.Fatalf("status/calls = %d/%v body=%s", response.Code, stub.calls, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil || len(fields) != 4 {
				t.Fatalf("response shape = %s / %v", response.Body.String(), err)
			}
			var body storyGenerationResponse
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.ID != persisted.ID || body.SourceVersionID != generationSourceVersionID || body.SemanticResult != string(semanticResult) || body.CreatedAt != createdAt.Format(time.RFC3339Nano) {
				t.Fatalf("response = %#v / %v", body, err)
			}
			deadline, ok := stub.contexts[0].Deadline()
			if !ok || time.Until(deadline) > adminStoryGenerationTimeout || time.Until(deadline) < adminStoryGenerationTimeout-time.Second {
				t.Fatalf("generation context deadline = %v", deadline)
			}
		})
	}
}

func TestAdminStoryGenerationRouteRejectsInvalidRequestsAndMissingConfiguration(t *testing.T) {
	store := &adminStore{memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}}
	stub := &storyGenerationStub{}
	handler := storyGenerationAdminHandler(t, store, stub)
	for _, test := range []struct {
		name, sourceVersionID, body, code string
		status                            int
	}{
		{name: "malformed source version", sourceVersionID: "not-a-uuid", status: http.StatusBadRequest, code: "generation_source_version_invalid"},
		{name: "nonempty body", sourceVersionID: generationSourceVersionID, body: "{}", status: http.StatusBadRequest, code: "generation_request_invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, storyGenerationAdminRequest(context.Background(), test.sourceVersionID, test.body))
			if response.Code != test.status || !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) || len(stub.calls) != 0 {
				t.Fatalf("status/calls/body = %d/%d/%s", response.Code, len(stub.calls), response.Body.String())
			}
		})
	}

	response := httptest.NewRecorder()
	storyGenerationAdminHandler(t, store, nil).ServeHTTP(response, storyGenerationAdminRequest(context.Background(), generationSourceVersionID, ""))
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"code":"generation_unavailable"`) {
		t.Fatalf("unavailable response = %d/%s", response.Code, response.Body.String())
	}
}

func TestAdminStoryGenerationRouteMapsOperationalErrorsSafely(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
		code string
	}{
		{name: "deadline", err: context.DeadlineExceeded, want: http.StatusGatewayTimeout, code: "generation_timeout"},
		{name: "rate limited", err: storygeneration.ErrOpenAIRateLimited, want: http.StatusTooManyRequests, code: "generation_rate_limited"},
		{name: "unavailable", err: storygeneration.ErrOpenAIUnavailable, want: http.StatusServiceUnavailable, code: "generation_unavailable"},
		{name: "invalid provider response", err: storygeneration.ErrOpenAIResponseInvalid, want: http.StatusBadGateway, code: "generation_upstream_invalid"},
		{name: "internal", err: errors.New("private provider payload"), want: http.StatusInternalServerError, code: "generation_failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub := &storyGenerationStub{err: test.err}
			store := &adminStore{memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}}
			response := httptest.NewRecorder()
			storyGenerationAdminHandler(t, store, stub).ServeHTTP(response, storyGenerationAdminRequest(context.Background(), generationSourceVersionID, ""))
			if response.Code != test.want || !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) || strings.Contains(response.Body.String(), "private provider payload") {
				t.Fatalf("status/body = %d/%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestAdminStoryGenerationRoutePropagatesRequestCancellation(t *testing.T) {
	type contextKey struct{}
	parent, cancel := context.WithCancel(context.WithValue(context.Background(), contextKey{}, "request"))
	defer cancel()
	stub := &storyGenerationStub{}
	stub.run = func(ctx context.Context, _ string) (storyorchestration.PersistedRun, error) {
		cancel()
		<-ctx.Done()
		if ctx.Value(contextKey{}) != "request" || !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatalf("generation context = value:%v error:%v", ctx.Value(contextKey{}), ctx.Err())
		}
		return storyorchestration.PersistedRun{}, ctx.Err()
	}
	store := &adminStore{memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: appidentity.RoleOwner}}}
	response := httptest.NewRecorder()
	storyGenerationAdminHandler(t, store, stub).ServeHTTP(response, storyGenerationAdminRequest(parent, generationSourceVersionID, ""))
	if len(stub.calls) != 1 {
		t.Fatalf("generation calls = %d", len(stub.calls))
	}
}
