package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pandapages/api/internal/appidentity"
	"pandapages/api/internal/model"
)

const (
	profileOneID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	profileTwoID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
)

func TestProfilesEndpointReturnsOnlySelectedAccountProfiles(t *testing.T) {
	store := &authTestStore{
		profiles: []model.ReaderProfile{
			{ID: profileOneID, Name: "Ada"},
			{ID: profileTwoID, Name: "Zoe"},
		},
	}
	rec := httptest.NewRecorder()
	testHandler(t, store).ServeHTTP(rec, bearerRequest(http.MethodGet, "/api/v1/profiles"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if store.profilesCalls != 1 || store.profilesAccount != testAccountID {
		t.Fatalf("profiles lookup calls/account = %d/%q", store.profilesCalls, store.profilesAccount)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	var response struct {
		Profiles []model.ReaderProfile `json:"profiles"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if len(response.Profiles) != 2 || response.Profiles[0].ID != profileOneID || response.Profiles[0].Name != "Ada" || response.Profiles[1].ID != profileTwoID || response.Profiles[1].Name != "Zoe" {
		t.Fatalf("profiles = %#v", response.Profiles)
	}
	if strings.Contains(rec.Body.String(), alternateAccountID) {
		t.Fatalf("response exposed a profile from another account: %s", rec.Body.String())
	}
}

func TestProfilesEndpointAllowsAdultMembershipAndEmptyProfiles(t *testing.T) {
	store := &authTestStore{memberships: []appidentity.Membership{{AccountID: alternateAccountID, Role: appidentity.RoleAdult}}}
	req := bearerRequest(http.MethodGet, "/api/v1/profiles")
	req.Header.Set("X-PP-Account-ID", alternateAccountID)
	rec := httptest.NewRecorder()
	testHandler(t, store).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || store.profilesAccount != alternateAccountID {
		t.Fatalf("status/account = %d/%q: %s", rec.Code, store.profilesAccount, rec.Body.String())
	}
	var response struct {
		Profiles []model.ReaderProfile `json:"profiles"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Profiles == nil || len(response.Profiles) != 0 {
		t.Fatalf("profiles = %#v, want an explicit empty list", response.Profiles)
	}
	if strings.Contains(rec.Body.String(), "Default") {
		t.Fatalf("empty account response invented a Default profile: %s", rec.Body.String())
	}
}

func TestProfilesEndpointRejectsMethodsAfterAuthorization(t *testing.T) {
	store := &authTestStore{}
	rec := httptest.NewRecorder()
	testHandler(t, store).ServeHTTP(rec, bearerRequest(http.MethodPost, "/api/v1/profiles"))
	if rec.Code != http.StatusMethodNotAllowed || store.profilesCalls != 0 {
		t.Fatalf("status/calls = %d/%d: %s", rec.Code, store.profilesCalls, rec.Body.String())
	}
}
