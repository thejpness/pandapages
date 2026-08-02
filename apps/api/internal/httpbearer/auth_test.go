package httpbearer

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

const (
	testToken      = "header.payload.signature"
	ownerAccount   = "11111111-1111-4111-8111-111111111111"
	adultAccount   = "22222222-2222-4222-8222-222222222222"
	unknownAccount = "33333333-3333-4333-8333-333333333333"
)

type verifierStub struct {
	identity appidentity.ExternalIdentity
	err      error
	calls    int
	token    string
}

func (stub *verifierStub) Verify(_ context.Context, token string) (appidentity.ExternalIdentity, error) {
	stub.calls++
	stub.token = token
	return stub.identity, stub.err
}

type storeStub struct {
	snapshot appidentity.Snapshot
	err      error
	calls    int
}

func (stub *storeStub) Identity(_ context.Context, _ appidentity.ExternalIdentity) (appidentity.Snapshot, error) {
	stub.calls++
	return stub.snapshot, stub.err
}

func validDependencies() (*verifierStub, *storeStub) {
	return &verifierStub{identity: appidentity.ExternalIdentity{
			Provider: appidentity.ProviderSupabase,
			Issuer:   "https://project.supabase.co/auth/v1",
			Subject:  "subject-1",
		}}, &storeStub{snapshot: appidentity.Snapshot{
			PrincipalID: "principal-1",
			DisplayName: "Panda Pages Adult",
			Memberships: []appidentity.Membership{
				{AccountID: ownerAccount, AccountName: "Owner", Role: appidentity.RoleOwner},
				{AccountID: adultAccount, AccountName: "Adult", Role: appidentity.RoleAdult},
			},
		}}
}

func authenticatedRequest(method string) *http.Request {
	request := httptest.NewRequest(method, "/api/v1/example", nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	return request
}

func requireAccount(t *testing.T, auth *Authenticator, request *http.Request) (AccountContext, *httptest.ResponseRecorder, bool) {
	t.Helper()
	response := httptest.NewRecorder()
	account, ok := auth.RequireAccount(response, request)
	return account, response, ok
}

func TestAuthenticateFailsClosed(t *testing.T) {
	tests := []struct {
		name        string
		headers     []string
		verifierErr error
		wantStatus  int
		wantCode    string
		wantCalls   int
	}{
		{name: "missing", wantStatus: http.StatusUnauthorized, wantCode: "bearer_required"},
		{name: "basic", headers: []string{"Basic value"}, wantStatus: http.StatusUnauthorized, wantCode: "invalid_bearer"},
		{name: "lowercase", headers: []string{"bearer value"}, wantStatus: http.StatusUnauthorized, wantCode: "invalid_bearer"},
		{name: "empty", headers: []string{"Bearer "}, wantStatus: http.StatusUnauthorized, wantCode: "invalid_bearer"},
		{name: "whitespace", headers: []string{"Bearer value extra"}, wantStatus: http.StatusUnauthorized, wantCode: "invalid_bearer"},
		{name: "comma", headers: []string{"Bearer value,second"}, wantStatus: http.StatusUnauthorized, wantCode: "invalid_bearer"},
		{name: "duplicate", headers: []string{"Bearer one", "Bearer two"}, wantStatus: http.StatusUnauthorized, wantCode: "invalid_bearer"},
		{name: "invalid token", headers: []string{"Bearer " + testToken}, verifierErr: errors.New("private token detail"), wantStatus: http.StatusUnauthorized, wantCode: "invalid_bearer", wantCalls: 1},
		{name: "keys unavailable", headers: []string{"Bearer " + testToken}, verifierErr: supabaseauth.ErrKeysUnavailable, wantStatus: http.StatusServiceUnavailable, wantCode: "authentication_unavailable", wantCalls: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verifier, store := validDependencies()
			verifier.err = test.verifierErr
			auth := New(verifier, store)
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			for _, header := range test.headers {
				request.Header.Add("Authorization", header)
			}
			response := httptest.NewRecorder()
			_, ok := auth.Authenticate(response, request)
			if ok || response.Code != test.wantStatus || !strings.Contains(response.Body.String(), `"`+test.wantCode+`"`) {
				t.Fatalf("ok=%t status=%d body=%s", ok, response.Code, response.Body.String())
			}
			if verifier.calls != test.wantCalls || store.calls != 0 {
				t.Fatalf("verifier=%d store=%d", verifier.calls, store.calls)
			}
			if test.wantStatus == http.StatusUnauthorized && response.Header().Get("WWW-Authenticate") == "" {
				t.Fatal("missing WWW-Authenticate")
			}
			if response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "private") {
				t.Fatalf("unsafe response: headers=%v body=%s", response.Header(), response.Body.String())
			}
		})
	}
}

func TestAuthenticateAcceptsValidBearerAndIgnoresCookie(t *testing.T) {
	verifier, store := validDependencies()
	auth := New(verifier, store)
	request := authenticatedRequest(http.MethodGet)
	request.AddCookie(&http.Cookie{Name: "pp_session", Value: "legacy"})
	response := httptest.NewRecorder()
	identity, ok := auth.Authenticate(response, request)
	if !ok || identity.Subject != "subject-1" || verifier.token != testToken || store.calls != 0 {
		t.Fatalf("ok=%t identity=%+v token=%q store=%d", ok, identity, verifier.token, store.calls)
	}
}

func TestRequireAccountHeaderValidation(t *testing.T) {
	tests := []struct {
		name       string
		headers    []string
		wantStatus int
		wantCode   string
	}{
		{name: "missing", wantStatus: http.StatusBadRequest, wantCode: "account_required"},
		{name: "empty", headers: []string{""}, wantStatus: http.StatusBadRequest, wantCode: "invalid_account"},
		{name: "padded", headers: []string{" " + ownerAccount}, wantStatus: http.StatusBadRequest, wantCode: "invalid_account"},
		{name: "malformed", headers: []string{"not-a-uuid"}, wantStatus: http.StatusBadRequest, wantCode: "invalid_account"},
		{name: "duplicate", headers: []string{ownerAccount, adultAccount}, wantStatus: http.StatusBadRequest, wantCode: "invalid_account"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verifier, store := validDependencies()
			auth := New(verifier, store)
			request := authenticatedRequest(http.MethodGet)
			for _, header := range test.headers {
				request.Header.Add(accountHeader, header)
			}
			_, response, ok := requireAccount(t, auth, request)
			if ok || response.Code != test.wantStatus || !strings.Contains(response.Body.String(), `"`+test.wantCode+`"`) {
				t.Fatalf("ok=%t status=%d body=%s", ok, response.Code, response.Body.String())
			}
			if store.calls != 0 {
				t.Fatalf("identity lookup occurred before header validation: %d", store.calls)
			}
		})
	}
}

func TestRequireAccountSelectsExplicitMembership(t *testing.T) {
	verifier, store := validDependencies()
	auth := New(verifier, store)
	request := authenticatedRequest(http.MethodGet)
	request.Header.Set(accountHeader, strings.ToUpper(adultAccount))
	account, response, ok := requireAccount(t, auth, request)
	if !ok || response.Code != http.StatusOK {
		t.Fatalf("ok=%t status=%d body=%s", ok, response.Code, response.Body.String())
	}
	if account.PrincipalID != "principal-1" || account.AccountID != adultAccount || account.Role != appidentity.RoleAdult {
		t.Fatalf("account context = %+v", account)
	}
}

func TestRequireAccountNeverFallsBackToFirstMembership(t *testing.T) {
	verifier, store := validDependencies()
	auth := New(verifier, store)
	request := authenticatedRequest(http.MethodGet)
	request.Header.Set(accountHeader, unknownAccount)
	_, response, ok := requireAccount(t, auth, request)
	if ok || response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"account_forbidden"`) {
		t.Fatalf("ok=%t status=%d body=%s", ok, response.Code, response.Body.String())
	}
}

func TestRequireAccountRejectsInvalidIdentityState(t *testing.T) {
	tests := []struct {
		name        string
		memberships []appidentity.Membership
	}{
		{name: "unknown role", memberships: []appidentity.Membership{{AccountID: ownerAccount, Role: "admin"}}},
		{name: "duplicate membership", memberships: []appidentity.Membership{
			{AccountID: ownerAccount, Role: appidentity.RoleOwner},
			{AccountID: strings.ToUpper(ownerAccount), Role: appidentity.RoleAdult},
		}},
		{name: "malformed membership account", memberships: []appidentity.Membership{{AccountID: "bad", Role: appidentity.RoleOwner}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verifier, store := validDependencies()
			store.snapshot.Memberships = test.memberships
			auth := New(verifier, store)
			request := authenticatedRequest(http.MethodGet)
			request.Header.Set(accountHeader, ownerAccount)
			_, response, ok := requireAccount(t, auth, request)
			if ok || response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"identity_state_invalid"`) {
				t.Fatalf("ok=%t status=%d body=%s", ok, response.Code, response.Body.String())
			}
		})
	}
}

func TestRequireAccountMapsIdentityStoreErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "onboarding required", err: appidentity.ErrNotFound, wantStatus: http.StatusConflict, wantCode: "onboarding_required"},
		{name: "invalid state", err: appidentity.ErrInvalidState, wantStatus: http.StatusConflict, wantCode: "identity_state_invalid"},
		{name: "unavailable", err: errors.New("private SQL detail"), wantStatus: http.StatusServiceUnavailable, wantCode: "identity_unavailable"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verifier, store := validDependencies()
			store.err = test.err
			auth := New(verifier, store)
			request := authenticatedRequest(http.MethodGet)
			request.Header.Set(accountHeader, ownerAccount)
			_, response, ok := requireAccount(t, auth, request)
			if ok || response.Code != test.wantStatus || !strings.Contains(response.Body.String(), `"`+test.wantCode+`"`) {
				t.Fatalf("ok=%t status=%d body=%s", ok, response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "private") {
				t.Fatalf("response leaks store detail: %s", response.Body.String())
			}
		})
	}
}
