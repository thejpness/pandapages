package httpapi

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pandapages/api/internal/appidentity"
	"pandapages/api/internal/model"
)

func profileJSONRequest(method, path, body string) *http.Request {
	req := bearerRequest(method, path)
	req.Body = io.NopCloser(strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestProfilesCreateUsesSelectedAccountForOwnerAndAdult(t *testing.T) {
	tests := []struct {
		name        string
		memberships []appidentity.Membership
		accountID   string
	}{
		{name: "owner", accountID: testAccountID},
		{name: "adult", accountID: alternateAccountID, memberships: []appidentity.Membership{{AccountID: alternateAccountID, Role: appidentity.RoleAdult}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &authTestStore{
				memberships:   tt.memberships,
				profileCreate: model.ReaderProfile{ID: profileOneID, Name: "Ted", ReadingLevel: model.ReaderEditionLittleListeners},
			}
			req := profileJSONRequest(http.MethodPost, "/api/v1/profiles", `{"name":" Ted ","readingLevel":"little-listeners"}`)
			req.Header.Set("X-PP-Account-ID", tt.accountID)
			rec := httptest.NewRecorder()
			testHandler(t, store).ServeHTTP(rec, req)
			if rec.Code != http.StatusCreated {
				t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
			}
			if store.profileCreateCalls != 1 ||
				store.profileCreateAccount != tt.accountID ||
				store.profileCreateName != "Ted" ||
				store.profileCreateReadingLevel != model.ReaderEditionLittleListeners {
				t.Fatalf("create calls/account/name = %d/%q/%q", store.profileCreateCalls, store.profileCreateAccount, store.profileCreateName)
			}
			if strings.Contains(strings.ToLower(rec.Body.String()), "default") || !strings.Contains(rec.Body.String(), `"name":"Ted"`) {
				t.Fatalf("create response = %s", rec.Body.String())
			}
			if req.Header.Get("X-PP-Profile-ID") != "" {
				t.Fatal("profile CRUD unexpectedly required a profile header")
			}
		})
	}
}

func TestProfilesCreateRejectsInvalidNamesAndAccountInjection(t *testing.T) {
	tests := []struct {
		name string
		body string
		code string
	}{
		{name: "missing name", body: `{}`, code: "invalid_profile_name"},
		{name: "empty", body: `{"name":"","readingLevel":"classic"}`, code: "invalid_profile_name"},
		{name: "padded only", body: `{"name":" \t ","readingLevel":"classic"}`, code: "invalid_profile_name"},
		{name: "control", body: `{"name":"Te\nd","readingLevel":"classic"}`, code: "invalid_profile_name"},
		{name: "too long", body: `{"name":"` + strings.Repeat("a", 81) + `","readingLevel":"classic"}`, code: "invalid_profile_name"},
		{name: "account injection", body: `{"name":"Ted","readingLevel":"classic","accountId":"` + alternateAccountID + `"}`, code: "bad_json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &authTestStore{}
			rec := httptest.NewRecorder()
			testHandler(t, store).ServeHTTP(rec, profileJSONRequest(http.MethodPost, "/api/v1/profiles", tt.body))
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"`+tt.code+`"`) || store.profileCreateCalls != 0 {
				t.Fatalf("status/calls/body = %d/%d/%s", rec.Code, store.profileCreateCalls, rec.Body.String())
			}
		})
	}
}

func TestProfilesUpdateScopesByAccountAndConcealsForbiddenProfiles(t *testing.T) {
	store := &authTestStore{profileUpdate: model.ReaderProfile{ID: profileOneID, Name: "Ada Panda", ReadingLevel: model.ReaderEditionGrowingReaders}}
	rec := httptest.NewRecorder()
	testHandler(t, store).ServeHTTP(rec, profileJSONRequest(http.MethodPatch, "/api/v1/profiles/"+strings.ToUpper(profileOneID), `{"name":" Ada Panda ","readingLevel":"growing-readers"}`))
	if rec.Code != http.StatusOK ||
		store.profileUpdateAccount != testAccountID ||
		store.profileUpdateID != profileOneID ||
		store.profileUpdateName != "Ada Panda" ||
		store.profileUpdateReadingLevel != model.ReaderEditionGrowingReaders {
		t.Fatalf("status/account/id/name = %d/%q/%q/%q: %s", rec.Code, store.profileUpdateAccount, store.profileUpdateID, store.profileUpdateName, rec.Body.String())
	}

	var forbiddenBodies []string
	for _, name := range []string{"missing", "another account"} {
		t.Run(name, func(t *testing.T) {
			store := &authTestStore{profileUpdateErr: sql.ErrNoRows}
			rec := httptest.NewRecorder()
			testHandler(t, store).ServeHTTP(rec, profileJSONRequest(http.MethodPatch, "/api/v1/profiles/"+profileOneID, `{"name":"Ada","readingLevel":"classic"}`))
			if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"profile_forbidden"`) || store.profileUpdateAccount != testAccountID {
				t.Fatalf("status/account/body = %d/%q/%s", rec.Code, store.profileUpdateAccount, rec.Body.String())
			}
			forbiddenBodies = append(forbiddenBodies, rec.Body.String())
		})
	}
	if forbiddenBodies[0] != forbiddenBodies[1] {
		t.Fatalf("missing and cross-account profiles leak different responses: %q != %q", forbiddenBodies[0], forbiddenBodies[1])
	}
}

func TestProfilesDeleteScopesByAccountAndAllowsFinalProfileDeletion(t *testing.T) {
	store := &authTestStore{}
	rec := httptest.NewRecorder()
	testHandler(t, store).ServeHTTP(rec, bearerRequest(http.MethodDelete, "/api/v1/profiles/"+profileOneID))
	if rec.Code != http.StatusNoContent || store.profileDeleteCalls != 1 || store.profileDeleteAccount != testAccountID || store.profileDeleteID != profileOneID {
		t.Fatalf("status/calls/account/id = %d/%d/%q/%q", rec.Code, store.profileDeleteCalls, store.profileDeleteAccount, store.profileDeleteID)
	}

	for _, name := range []string{"missing", "another account"} {
		t.Run(name, func(t *testing.T) {
			store := &authTestStore{profileDeleteErr: sql.ErrNoRows}
			rec := httptest.NewRecorder()
			testHandler(t, store).ServeHTTP(rec, bearerRequest(http.MethodDelete, "/api/v1/profiles/"+profileOneID))
			if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"profile_forbidden"`) || store.profileDeleteAccount != testAccountID {
				t.Fatalf("status/account/body = %d/%q/%s", rec.Code, store.profileDeleteAccount, rec.Body.String())
			}
		})
	}
}

func TestProfilesCRUDMapsStorageAndNameConflictsWithoutLeakingDatabaseErrors(t *testing.T) {
	tests := []struct {
		name   string
		req    *http.Request
		store  *authTestStore
		status int
		code   string
	}{
		{name: "create conflict", req: profileJSONRequest(http.MethodPost, "/api/v1/profiles", `{"name":"Ted","readingLevel":"classic"}`), store: &authTestStore{profileCreateErr: model.ErrProfileNameConflict}, status: http.StatusBadRequest, code: "invalid_profile_name"},
		{name: "update unavailable", req: profileJSONRequest(http.MethodPatch, "/api/v1/profiles/"+profileOneID, `{"name":"Ted","readingLevel":"classic"}`), store: &authTestStore{profileUpdateErr: errors.New("database unavailable")}, status: http.StatusServiceUnavailable, code: "profile_unavailable"},
		{name: "delete unavailable", req: bearerRequest(http.MethodDelete, "/api/v1/profiles/"+profileOneID), store: &authTestStore{profileDeleteErr: errors.New("database unavailable")}, status: http.StatusServiceUnavailable, code: "profile_unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			testHandler(t, tt.store).ServeHTTP(rec, tt.req)
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), `"`+tt.code+`"`) || strings.Contains(rec.Body.String(), "database unavailable") {
				t.Fatalf("status/body = %d/%s", rec.Code, rec.Body.String())
			}
		})
	}
}
