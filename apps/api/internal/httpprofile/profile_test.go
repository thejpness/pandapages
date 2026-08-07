package httpprofile

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pandapages/api/internal/appidentity"
	"pandapages/api/internal/httpbearer"
)

const (
	profileAccountID = "11111111-1111-4111-8111-111111111111"
	profileID        = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
)

type storeStub struct {
	exists    bool
	err       error
	calls     int
	accountID string
	profileID string
}

func (s *storeStub) ProfileExists(accountID, profileID string) (bool, error) {
	s.calls++
	s.accountID = accountID
	s.profileID = profileID
	return s.exists, s.err
}

func profileRequest(headers ...string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/profile-scoped", nil)
	for _, header := range headers {
		req.Header.Add(profileHeader, header)
	}
	return req
}

func authorizedAccount() httpbearer.AccountContext {
	return httpbearer.AccountContext{
		PrincipalID: "principal-1",
		AccountID:   profileAccountID,
		Role:        appidentity.RoleAdult,
	}
}

func TestRequireProfileValidatesProfileHeader(t *testing.T) {
	tests := []struct {
		name    string
		headers []string
		status  int
		code    string
		wantID  string
	}{
		{name: "missing", status: http.StatusBadRequest, code: "profile_required"},
		{name: "empty", headers: []string{""}, status: http.StatusBadRequest, code: "invalid_profile"},
		{name: "leading padding", headers: []string{" " + profileID}, status: http.StatusBadRequest, code: "invalid_profile"},
		{name: "trailing padding", headers: []string{profileID + " "}, status: http.StatusBadRequest, code: "invalid_profile"},
		{name: "duplicate", headers: []string{profileID, profileID}, status: http.StatusBadRequest, code: "invalid_profile"},
		{name: "malformed", headers: []string{"not-a-uuid"}, status: http.StatusBadRequest, code: "invalid_profile"},
		{name: "canonical", headers: []string{profileID}, status: http.StatusOK, wantID: profileID},
		{name: "uppercase follows account UUID policy", headers: []string{strings.ToUpper(profileID)}, status: http.StatusOK, wantID: profileID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &storeStub{exists: true}
			rec := httptest.NewRecorder()
			ctx, ok := New(store).RequireProfile(rec, profileRequest(tt.headers...), authorizedAccount())
			if got := rec.Code; got != tt.status {
				t.Fatalf("status = %d, want %d (%s)", got, tt.status, rec.Body.String())
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-store" && tt.status != http.StatusOK {
				t.Fatalf("Cache-Control = %q, want no-store", got)
			}
			if tt.status != http.StatusOK {
				if ok || !strings.Contains(rec.Body.String(), `"`+tt.code+`"`) || store.calls != 0 {
					t.Fatalf("ok/calls/body = %t/%d/%s", ok, store.calls, rec.Body.String())
				}
				return
			}
			if !ok || ctx.ProfileID != tt.wantID || store.profileID != tt.wantID {
				t.Fatalf("context/store profile = %q/%q, want %q", ctx.ProfileID, store.profileID, tt.wantID)
			}
		})
	}
}

func TestRequireProfileScopesLookupAndPreservesAuthorizedAccount(t *testing.T) {
	store := &storeStub{exists: true}
	rec := httptest.NewRecorder()
	ctx, ok := New(store).RequireProfile(rec, profileRequest(profileID), authorizedAccount())
	if !ok || rec.Code != http.StatusOK {
		t.Fatalf("status/ok = %d/%t: %s", rec.Code, ok, rec.Body.String())
	}
	if store.accountID != profileAccountID || store.profileID != profileID {
		t.Fatalf("lookup = account %q, profile %q", store.accountID, store.profileID)
	}
	if ctx.PrincipalID != "principal-1" || ctx.AccountID != profileAccountID || ctx.Role != appidentity.RoleAdult || ctx.ProfileID != profileID {
		t.Fatalf("unexpected context: %#v", ctx)
	}
}

func TestRequireProfileConcealsMissingAndCrossAccountProfiles(t *testing.T) {
	var bodies []string
	for _, name := range []string{"nonexistent", "belongs to another account"} {
		t.Run(name, func(t *testing.T) {
			store := &storeStub{exists: false}
			rec := httptest.NewRecorder()
			_, ok := New(store).RequireProfile(rec, profileRequest(profileID), authorizedAccount())
			if ok || rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"profile_forbidden"`) {
				t.Fatalf("status/ok/body = %d/%t/%s", rec.Code, ok, rec.Body.String())
			}
			bodies = append(bodies, rec.Body.String())
		})
	}
	if bodies[0] != bodies[1] {
		t.Fatalf("externally distinguishable forbidden responses: %q != %q", bodies[0], bodies[1])
	}
}

func TestRequireProfileMapsStorageFailure(t *testing.T) {
	rec := httptest.NewRecorder()
	_, ok := New(&storeStub{err: errors.New("database unavailable")}).RequireProfile(rec, profileRequest(profileID), authorizedAccount())
	if ok || rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"profile_unavailable"`) {
		t.Fatalf("status/ok/body = %d/%t/%s", rec.Code, ok, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", rec.Header().Get("Cache-Control"))
	}
}

type identityStoreStub struct {
	snapshot appidentity.Snapshot
	err      error
}

func (s identityStoreStub) Identity(context.Context, appidentity.ExternalIdentity) (appidentity.Snapshot, error) {
	return s.snapshot, s.err
}

type verifierStub struct{ err error }

func (v verifierStub) Verify(_ context.Context, token string) (appidentity.ExternalIdentity, error) {
	if v.err != nil || token != "valid-token" {
		return appidentity.ExternalIdentity{}, errors.New("invalid token")
	}
	return appidentity.ExternalIdentity{Provider: appidentity.ProviderSupabase, Issuer: "https://project.supabase.co/auth/v1", Subject: "subject-1"}, nil
}

func TestAccountAuthorizationPrecedesProfileLookup(t *testing.T) {
	tests := []struct {
		name       string
		authorize  bool
		accountID  string
		bearer     string
		wantStatus int
		wantCode   string
		wantCalls  int
	}{
		{name: "cookie only", accountID: profileAccountID, wantStatus: http.StatusUnauthorized, wantCode: "bearer_required"},
		{name: "invalid bearer", accountID: profileAccountID, bearer: "invalid", wantStatus: http.StatusUnauthorized, wantCode: "invalid_bearer"},
		{name: "missing account", bearer: "valid-token", wantStatus: http.StatusBadRequest, wantCode: "account_required"},
		{name: "account forbidden", bearer: "valid-token", accountID: "22222222-2222-4222-8222-222222222222", wantStatus: http.StatusForbidden, wantCode: "account_forbidden"},
		{name: "authorized", bearer: "valid-token", accountID: profileAccountID, wantStatus: http.StatusOK, wantCalls: 1},
	}

	identity := identityStoreStub{snapshot: appidentity.Snapshot{PrincipalID: "principal-1", Memberships: []appidentity.Membership{{AccountID: profileAccountID, Role: appidentity.RoleOwner}}}}
	authenticator := httpbearer.New(verifierStub{}, identity)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &storeStub{exists: true}
			resolver := New(store)
			req := profileRequest(profileID)
			if tt.bearer != "" {
				req.Header.Set("Authorization", "Bearer "+tt.bearer)
			}
			if tt.accountID != "" {
				req.Header.Set("X-PP-Account-ID", tt.accountID)
			}
			if tt.name == "cookie only" {
				req.AddCookie(&http.Cookie{Name: "obsolete_cookie", Value: "obsolete"})
			}

			rec := httptest.NewRecorder()
			account, ok := authenticator.RequireAccount(rec, req)
			if ok {
				_, ok = resolver.RequireProfile(rec, req, account)
			}
			if got := rec.Code; got != tt.wantStatus {
				t.Fatalf("status = %d, want %d (%s)", got, tt.wantStatus, rec.Body.String())
			}
			if !ok && tt.wantCode != "" && !strings.Contains(rec.Body.String(), `"`+tt.wantCode+`"`) {
				t.Fatalf("body = %s, want %q", rec.Body.String(), tt.wantCode)
			}
			if store.calls != tt.wantCalls {
				t.Fatalf("profile lookup calls = %d, want %d", store.calls, tt.wantCalls)
			}
		})
	}
}
