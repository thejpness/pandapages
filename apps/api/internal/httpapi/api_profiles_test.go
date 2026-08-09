package httpapi

import (
	"encoding/json"
	"io"
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
			{
				ID:               profileOneID,
				Name:             "Ada",
				PreferredEdition: model.ReaderEditionClassic,
			},
			{
				ID:               profileTwoID,
				Name:             "Zoe",
				PreferredEdition: model.ReaderEditionLittleListeners,
			},
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

func TestProfilesEndpointRequiresExplicitReadingLevelOnCreateAndUpdate(t *testing.T) {
	little := model.ReaderEditionLittleListeners
	store := &authTestStore{
		profileCreate: model.ReaderProfile{
			ID:               profileOneID,
			Name:             "Ted",
			PreferredEdition: little,
		},
		profileUpdate: model.ReaderProfile{
			ID:               profileOneID,
			Name:             "Theo",
			PreferredEdition: model.ReaderEditionClassic,
		},
	}

	create := httptest.NewRecorder()
	req := bearerRequest(http.MethodPost, "/api/v1/profiles")
	req.Body = io.NopCloser(strings.NewReader(
		`{"name":" Ted ","preferredEdition":"little-listeners"}`,
	))
	testHandler(t, store).ServeHTTP(create, req)
	if create.Code != http.StatusCreated ||
		store.profileCreateName != "Ted" ||
		store.profileCreateEdition != little {
		t.Fatalf(
			"create status/name/edition = %d/%q/%q: %s",
			create.Code,
			store.profileCreateName,
			store.profileCreateEdition,
			create.Body.String(),
		)
	}

	update := httptest.NewRecorder()
	req = bearerRequest(http.MethodPatch, "/api/v1/profiles/"+profileOneID)
	req.Body = io.NopCloser(strings.NewReader(
		`{"name":"Theo","preferredEdition":"classic"}`,
	))
	testHandler(t, store).ServeHTTP(update, req)
	if update.Code != http.StatusOK ||
		store.profileUpdateName != "Theo" ||
		store.profileUpdateEdition != model.ReaderEditionClassic {
		t.Fatalf(
			"update status/name/edition = %d/%q/%q: %s",
			update.Code,
			store.profileUpdateName,
			store.profileUpdateEdition,
			update.Body.String(),
		)
	}

	invalid := httptest.NewRecorder()
	before := store.profileCreateCalls
	req = bearerRequest(http.MethodPost, "/api/v1/profiles")
	req.Body = io.NopCloser(strings.NewReader(
		`{"name":"Ted","preferredEdition":"bedtime-ultra"}`,
	))
	testHandler(t, store).ServeHTTP(invalid, req)
	if invalid.Code != http.StatusBadRequest ||
		store.profileCreateCalls != before ||
		!strings.Contains(invalid.Body.String(), `"invalid_reading_level"`) {
		t.Fatalf(
			"invalid level status/calls = %d/%d->%d: %s",
			invalid.Code,
			before,
			store.profileCreateCalls,
			invalid.Body.String(),
		)
	}
}

func TestProfilesEndpointRejectsMethodsAfterAuthorization(t *testing.T) {
	store := &authTestStore{}
	rec := httptest.NewRecorder()
	testHandler(t, store).ServeHTTP(rec, bearerRequest(http.MethodPut, "/api/v1/profiles"))
	if rec.Code != http.StatusMethodNotAllowed || store.profilesCalls != 0 {
		t.Fatalf("status/calls = %d/%d: %s", rec.Code, store.profilesCalls, rec.Body.String())
	}
}
