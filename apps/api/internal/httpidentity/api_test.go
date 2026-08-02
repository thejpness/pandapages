package httpidentity

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pandapages/api/internal/appidentity"
	"pandapages/api/internal/supabaseauth"
)

const secretToken = "header.payload.private-signature"

type verifierStub struct {
	identity appidentity.ExternalIdentity
	err      error
	token    string
	calls    int
}

func (stub *verifierStub) Verify(_ context.Context, token string) (appidentity.ExternalIdentity, error) {
	stub.calls++
	stub.token = token
	return stub.identity, stub.err
}

type storeStub struct {
	snapshot    appidentity.Snapshot
	created     bool
	ensureErr   error
	identityErr error
	ensureCalls int
	lookupCalls int
}

func (stub *storeStub) EnsureIdentity(_ context.Context, _ appidentity.ExternalIdentity) (appidentity.Snapshot, bool, error) {
	stub.ensureCalls++
	return stub.snapshot, stub.created, stub.ensureErr
}

func (stub *storeStub) Identity(_ context.Context, _ appidentity.ExternalIdentity) (appidentity.Snapshot, error) {
	stub.lookupCalls++
	return stub.snapshot, stub.identityErr
}

func validStubs() (*verifierStub, *storeStub) {
	return &verifierStub{identity: appidentity.ExternalIdentity{
			Provider: appidentity.ProviderSupabase,
			Issuer:   "https://project.supabase.co/auth/v1",
			Subject:  "subject-1",
		}}, &storeStub{snapshot: appidentity.Snapshot{
			PrincipalID: "principal-1",
			DisplayName: "Panda Pages Adult",
			Memberships: []appidentity.Membership{{
				AccountID:   "account-1",
				AccountName: "My Panda Pages",
				Role:        appidentity.RoleOwner,
			}},
		}, created: true}
}

func request(t *testing.T, handler http.Handler, method, target string, bearer bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	if bearer {
		req.Header.Set("Authorization", "Bearer "+secretToken)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func TestOnboardReturnsFiniteIdentityState(t *testing.T) {
	verifier, store := validStubs()
	response := request(t, New(verifier, store), http.MethodPost, "/api/auth/onboard", true)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	for _, expected := range []string{`"authenticated":true`, `"created":true`, `"id":"principal-1"`, `"accountId":"account-1"`, `"role":"owner"`} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Errorf("body missing %s: %s", expected, response.Body.String())
		}
	}
	if strings.Contains(response.Body.String(), secretToken) || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response leaks token or is cacheable: headers=%v body=%s", response.Header(), response.Body.String())
	}
	if verifier.token != secretToken || store.ensureCalls != 1 || store.lookupCalls != 0 {
		t.Fatalf("calls: verifier token=%q ensure=%d lookup=%d", verifier.token, store.ensureCalls, store.lookupCalls)
	}
}

func TestMeDoesNotProvision(t *testing.T) {
	verifier, store := validStubs()
	response := request(t, New(verifier, store), http.MethodGet, "/api/auth/me", true)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), `"created"`) {
		t.Fatalf("status/body = %d %s", response.Code, response.Body.String())
	}
	if store.ensureCalls != 0 || store.lookupCalls != 1 {
		t.Fatalf("ensure=%d lookup=%d", store.ensureCalls, store.lookupCalls)
	}
}

func TestBearerRoutesRejectCookieOnlyAuthentication(t *testing.T) {
	verifier, store := validStubs()
	request := httptest.NewRequest(http.MethodPost, "/api/auth/onboard", nil)
	request.AddCookie(&http.Cookie{Name: "pp_session", Value: "valid-looking-legacy-cookie"})
	response := httptest.NewRecorder()
	New(verifier, store).ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"bearer_required"`) {
		t.Fatalf("status/body = %d %s", response.Code, response.Body.String())
	}
	if verifier.calls != 0 || store.ensureCalls != 0 {
		t.Fatalf("cookie reached bearer dependencies: verify=%d ensure=%d", verifier.calls, store.ensureCalls)
	}
}

func TestBearerParsingFailsClosed(t *testing.T) {
	for _, header := range []string{"Basic value", "bearer value", "Bearer ", "Bearer value extra", "Bearer value,second"} {
		t.Run(header, func(t *testing.T) {
			verifier, store := validStubs()
			req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
			req.Header.Set("Authorization", header)
			response := httptest.NewRecorder()
			New(verifier, store).ServeHTTP(response, req)
			if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"invalid_bearer"`) {
				t.Fatalf("status/body = %d %s", response.Code, response.Body.String())
			}
			if verifier.calls != 0 {
				t.Fatalf("verifier calls = %d", verifier.calls)
			}
		})
	}
}

func TestBearerVerificationErrorsAreFinite(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "invalid", err: errors.New("private jwt detail"), wantStatus: http.StatusUnauthorized, wantCode: "invalid_bearer"},
		{name: "JWKS unavailable", err: supabaseauth.ErrKeysUnavailable, wantStatus: http.StatusServiceUnavailable, wantCode: "authentication_unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verifier, store := validStubs()
			verifier.err = test.err
			response := request(t, New(verifier, store), http.MethodGet, "/api/auth/me", true)
			if response.Code != test.wantStatus || !strings.Contains(response.Body.String(), `"`+test.wantCode+`"`) {
				t.Fatalf("status/body = %d %s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "private") || strings.Contains(response.Body.String(), secretToken) {
				t.Fatalf("response leaks verification detail: %s", response.Body.String())
			}
			if store.lookupCalls != 0 {
				t.Fatalf("store calls = %d", store.lookupCalls)
			}
		})
	}
}

func TestStoreErrorsAreFinite(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "not onboarded", err: appidentity.ErrNotFound, wantStatus: http.StatusConflict, wantCode: "onboarding_required"},
		{name: "invalid state", err: appidentity.ErrInvalidState, wantStatus: http.StatusConflict, wantCode: "identity_state_invalid"},
		{name: "database", err: errors.New("private SQL detail"), wantStatus: http.StatusServiceUnavailable, wantCode: "identity_unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verifier, store := validStubs()
			store.identityErr = test.err
			response := request(t, New(verifier, store), http.MethodGet, "/api/auth/me", true)
			if response.Code != test.wantStatus || !strings.Contains(response.Body.String(), `"`+test.wantCode+`"`) {
				t.Fatalf("status/body = %d %s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "private") {
				t.Fatalf("response leaks store detail: %s", response.Body.String())
			}
		})
	}
}

func TestOnboardRejectsAnyRequestBody(t *testing.T) {
	verifier, store := validStubs()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/onboard", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+secretToken)
	response := httptest.NewRecorder()
	New(verifier, store).ServeHTTP(response, req)
	if response.Code != http.StatusBadRequest || verifier.calls != 0 || store.ensureCalls != 0 {
		t.Fatalf("status=%d verify=%d ensure=%d body=%s", response.Code, verifier.calls, store.ensureCalls, response.Body.String())
	}
}
