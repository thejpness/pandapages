package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pandapages/api/internal/appidentity"
	"pandapages/api/internal/model"
	"pandapages/api/internal/profilepin"
)

func TestProfilePINEndpointsUseSelectedAccountForOwnerAndAdult(t *testing.T) {
	tests := []struct {
		name        string
		memberships []appidentity.Membership
		accountID   string
		method      string
		path        string
		body        string
	}{
		{name: "owner sets", accountID: testAccountID, method: http.MethodPut, path: "/api/v1/profiles/" + profileOneID + "/pin", body: `{"pin":"1234"}`},
		{name: "adult verifies", accountID: alternateAccountID, memberships: []appidentity.Membership{{AccountID: alternateAccountID, Role: appidentity.RoleAdult}}, method: http.MethodPost, path: "/api/v1/profiles/" + profileOneID + "/pin", body: `{"pin":"1234"}`},
		{name: "adult removes", accountID: alternateAccountID, memberships: []appidentity.Membership{{AccountID: alternateAccountID, Role: appidentity.RoleAdult}}, method: http.MethodDelete, path: "/api/v1/profiles/" + profileOneID + "/pin"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &authTestStore{memberships: tt.memberships}
			req := profileJSONRequest(tt.method, tt.path, tt.body)
			req.Header.Set("X-PP-Account-ID", tt.accountID)
			rec := httptest.NewRecorder()
			testHandler(t, store).ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status/cache = %d/%q: %s", rec.Code, rec.Header().Get("Cache-Control"), rec.Body.String())
			}
			switch tt.method {
			case http.MethodPut:
				if store.profilePINSetAccount != tt.accountID || store.profilePINSetID != profileOneID || !strings.HasPrefix(store.profilePINHash, "$2") || strings.Contains(rec.Body.String(), store.profilePINHash) || strings.Contains(rec.Body.String(), "1234") {
					t.Fatalf("set account/id/hash/response = %q/%q/%q/%s", store.profilePINSetAccount, store.profilePINSetID, store.profilePINHash, rec.Body.String())
				}
			case http.MethodPost:
				if store.profilePINVerifyAccount != tt.accountID || store.profilePINVerifyID != profileOneID || store.profilePINCandidate != "1234" || !strings.Contains(rec.Body.String(), `"verified":true`) {
					t.Fatalf("verify account/id/pin/response = %q/%q/%q/%s", store.profilePINVerifyAccount, store.profilePINVerifyID, store.profilePINCandidate, rec.Body.String())
				}
			case http.MethodDelete:
				if store.profilePINRemoveAccount != tt.accountID || store.profilePINRemoveID != profileOneID || !strings.Contains(rec.Body.String(), `"pin_enabled":false`) {
					t.Fatalf("remove account/id/response = %q/%q/%s", store.profilePINRemoveAccount, store.profilePINRemoveID, rec.Body.String())
				}
			}
			if req.Header.Get("X-PP-Profile-ID") != "" {
				t.Fatal("PIN management unexpectedly required a reader profile header")
			}
		})
	}
}

func TestProfilePINEndpointsRejectInvalidPINBeforeStore(t *testing.T) {
	for _, body := range []string{`{}`, `{"pin":""}`, `{"pin":"123"}`, `{"pin":"12345"}`, `{"pin":" 1234"}`, `{"pin":"1234 "}`, `{"pin":"12a4"}`} {
		t.Run(body, func(t *testing.T) {
			store := &authTestStore{}
			rec := httptest.NewRecorder()
			testHandler(t, store).ServeHTTP(rec, profileJSONRequest(http.MethodPut, "/api/v1/profiles/"+profileOneID+"/pin", body))
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"invalid_pin"`) || store.profilePINSetCalls != 0 {
				t.Fatalf("status/calls/body = %d/%d/%s", rec.Code, store.profilePINSetCalls, rec.Body.String())
			}
		})
	}
}

func TestProfilePINVerificationFiniteFailuresConcealProfileExistence(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "wrong pin", err: model.ErrProfilePINInvalid, status: http.StatusForbidden, code: "pin_invalid"},
		{name: "rate limited", err: model.ErrProfilePINRateLimited, status: http.StatusTooManyRequests, code: "pin_rate_limited"},
		{name: "unavailable", err: errors.New("database unavailable"), status: http.StatusServiceUnavailable, code: "profile_unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &authTestStore{profilePINVerifyErr: tt.err}
			rec := httptest.NewRecorder()
			testHandler(t, store).ServeHTTP(rec, profileJSONRequest(http.MethodPost, "/api/v1/profiles/"+profileOneID+"/pin", `{"pin":"1234"}`))
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), `"`+tt.code+`"`) || strings.Contains(rec.Body.String(), "database unavailable") {
				t.Fatalf("status/body = %d/%s", rec.Code, rec.Body.String())
			}
			if tt.err == model.ErrProfilePINRateLimited && rec.Header().Get("Retry-After") != "900" {
				t.Fatalf("Retry-After = %q", rec.Header().Get("Retry-After"))
			}
		})
	}

	var forbiddenBodies []string
	for _, name := range []string{"missing", "another account"} {
		t.Run(name, func(t *testing.T) {
			store := &authTestStore{profilePINVerifyErr: sql.ErrNoRows}
			rec := httptest.NewRecorder()
			testHandler(t, store).ServeHTTP(rec, profileJSONRequest(http.MethodPost, "/api/v1/profiles/"+profileOneID+"/pin", `{"pin":"1234"}`))
			if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"profile_forbidden"`) {
				t.Fatalf("status/body = %d/%s", rec.Code, rec.Body.String())
			}
			forbiddenBodies = append(forbiddenBodies, rec.Body.String())
		})
	}
	if forbiddenBodies[0] != forbiddenBodies[1] {
		t.Fatalf("profile existence leaked: %q != %q", forbiddenBodies[0], forbiddenBodies[1])
	}
}

func TestProfilePINSetHashIsValidBcryptEncoding(t *testing.T) {
	store := &authTestStore{}
	rec := httptest.NewRecorder()
	testHandler(t, store).ServeHTTP(rec, profileJSONRequest(http.MethodPut, "/api/v1/profiles/"+profileOneID+"/pin", `{"pin":"1234"}`))
	matched, err := profilepin.Matches(store.profilePINHash, "1234")
	if rec.Code != http.StatusOK || err != nil || !matched {
		t.Fatalf("status/matched/err = %d/%v/%v", rec.Code, matched, err)
	}
}
