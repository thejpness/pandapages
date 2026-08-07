package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pandapages/api/internal/appidentity"
	"pandapages/api/internal/httpbearer"
	"pandapages/api/internal/model"
	"pandapages/api/internal/readercontract"
)

const testAccountID = "11111111-1111-4111-8111-111111111111"
const alternateAccountID = "22222222-2222-4222-8222-222222222222"
const testBearer = "header.payload.signature"

type authTestStore struct {
	readinessErr        error
	readinessCheck      func(context.Context) error
	readinessCalls      int
	identityErr         error
	memberships         []appidentity.Membership
	libraryCalls        int
	libraryAccount      string
	libraryResponse     model.LibraryReadModel
	libraryErr          error
	readerCalls         int
	readerAccount       string
	readerSlug          string
	readerResponse      model.ReaderStory
	readerErr           error
	progressGetCalls    int
	progressAccount     string
	progressGetState    model.ProgressResponse
	progressGetErr      error
	progressPutCalls    int
	progressSlug        string
	progressVersion     int
	progressLocator     readercontract.Locator
	progressPercent     float64
	progressPutErr      error
	continueCalls       int
	continueAccount     string
	continueLimit       int
	continueItems       []model.ContinueItem
	continueErr         error
	profilesCalls       int
	profilesAccount     string
	profiles            []model.ReaderProfile
	profilesErr         error
	settingsGetCalls    int
	settingsGetAccount  string
	settingsGet         model.SettingsPayload
	settingsGetErr      error
	settingsPutCalls    int
	settingsPutAccount  string
	settingsPutPayload  model.SettingsUpsert
	settingsPutResponse model.SettingsPayload
	settingsPutErr      error
}

func (s *authTestStore) Identity(context.Context, appidentity.ExternalIdentity) (appidentity.Snapshot, error) {
	if s.identityErr != nil {
		return appidentity.Snapshot{}, s.identityErr
	}
	memberships := s.memberships
	if memberships == nil {
		memberships = []appidentity.Membership{{AccountID: testAccountID, AccountName: "Account", Role: appidentity.RoleOwner}}
	}
	return appidentity.Snapshot{PrincipalID: "principal-1", Memberships: memberships}, nil
}
func (s *authTestStore) CheckReadiness(ctx context.Context) error {
	s.readinessCalls++
	if s.readinessCheck != nil {
		return s.readinessCheck(ctx)
	}
	return s.readinessErr
}
func (s *authTestStore) Library(accountID string) (model.LibraryReadModel, error) {
	s.libraryCalls++
	s.libraryAccount = accountID
	if s.libraryResponse.Items == nil {
		s.libraryResponse.Items = []model.StoryItem{}
	}
	return s.libraryResponse, s.libraryErr
}
func (s *authTestStore) ReaderStory(accountID, slug string) (model.ReaderStory, error) {
	s.readerCalls++
	s.readerAccount = accountID
	s.readerSlug = slug
	return s.readerResponse, s.readerErr
}
func (s *authTestStore) ProgressGet(accountID, _ string) (model.ProgressResponse, error) {
	s.progressGetCalls++
	s.progressAccount = accountID
	return s.progressGetState, s.progressGetErr
}
func (s *authTestStore) ProgressPut(accountID, slug string, version int, locator readercontract.Locator, percent float64) error {
	s.progressPutCalls++
	s.progressAccount = accountID
	s.progressSlug = slug
	s.progressVersion = version
	s.progressLocator = locator
	s.progressPercent = percent
	return s.progressPutErr
}
func (s *authTestStore) ContinueRecent(accountID string, limit int) ([]model.ContinueItem, error) {
	s.continueCalls++
	s.continueAccount = accountID
	s.continueLimit = limit
	return s.continueItems, s.continueErr
}
func (s *authTestStore) Profiles(accountID string) ([]model.ReaderProfile, error) {
	s.profilesCalls++
	s.profilesAccount = accountID
	if s.profiles == nil {
		s.profiles = []model.ReaderProfile{}
	}
	return s.profiles, s.profilesErr
}
func (s *authTestStore) SettingsGet(accountID string) (model.SettingsPayload, error) {
	s.settingsGetCalls++
	s.settingsGetAccount = accountID
	return s.settingsGet, s.settingsGetErr
}
func (s *authTestStore) SettingsPut(accountID string, payload model.SettingsUpsert) (model.SettingsPayload, error) {
	s.settingsPutCalls++
	s.settingsPutAccount = accountID
	s.settingsPutPayload = payload
	return s.settingsPutResponse, s.settingsPutErr
}

type bearerVerifier struct{ err error }

func (v bearerVerifier) Verify(_ context.Context, token string) (appidentity.ExternalIdentity, error) {
	if v.err != nil {
		return appidentity.ExternalIdentity{}, v.err
	}
	if token != testBearer {
		return appidentity.ExternalIdentity{}, errors.New("invalid token")
	}
	return appidentity.ExternalIdentity{Provider: appidentity.ProviderSupabase, Issuer: "https://project.supabase.co/auth/v1", Subject: "subject-1"}, nil
}

func testHandler(t *testing.T, store *authTestStore) http.Handler {
	t.Helper()
	return New(Config{BearerAuthenticator: httpbearer.New(bearerVerifier{}, store)}, store)
}
func bearerRequest(method, path string) *http.Request {
	request := httptest.NewRequest(method, path, nil)
	request.Header.Set("Authorization", "Bearer "+testBearer)
	request.Header.Set("X-PP-Account-ID", testAccountID)
	return request
}

func TestProtectedRoutesRequireBearerAccountContext(t *testing.T) {
	tests := []struct{ name, method, path, body string }{
		{"library", http.MethodGet, "/api/v1/library", ""}, {"reader", http.MethodGet, "/api/v1/reader/story", ""}, {"progress get", http.MethodGet, "/api/v1/progress/story", ""}, {"progress put", http.MethodPut, "/api/v1/progress/story", `{"version":1,"locator":{"schema":2,"segment":{"key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","occurrence":1,"ordinal":1,"offset":0},"chapter":null},"percent":0}`}, {"continue", http.MethodGet, "/api/v1/continue", ""}, {"profiles", http.MethodGet, "/api/v1/profiles", ""}, {"settings get", http.MethodGet, "/api/v1/settings", ""}, {"settings put", http.MethodPut, "/api/v1/settings", `{}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &authTestStore{}
			handler := testHandler(t, store)
			cookieOnly := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			cookieOnly.AddCookie(&http.Cookie{Name: "obsolete_cookie", Value: "obsolete"})
			r := httptest.NewRecorder()
			handler.ServeHTTP(r, cookieOnly)
			if r.Code != http.StatusUnauthorized || !strings.Contains(r.Body.String(), `"bearer_required"`) {
				t.Fatalf("cookie-only = %d %s", r.Code, r.Body.String())
			}
			invalid := bearerRequest(tt.method, tt.path)
			invalid.Header.Set("Authorization", "Bearer invalid")
			invalid.AddCookie(&http.Cookie{Name: "obsolete_cookie", Value: "obsolete"})
			r = httptest.NewRecorder()
			handler.ServeHTTP(r, invalid)
			if r.Code != http.StatusUnauthorized || !strings.Contains(r.Body.String(), `"invalid_bearer"`) {
				t.Fatalf("invalid bearer = %d %s", r.Code, r.Body.String())
			}
			missing := bearerRequest(tt.method, tt.path)
			missing.Header.Del("X-PP-Account-ID")
			missing.AddCookie(&http.Cookie{Name: "obsolete_cookie", Value: "obsolete"})
			r = httptest.NewRecorder()
			handler.ServeHTTP(r, missing)
			if r.Code != http.StatusBadRequest || !strings.Contains(r.Body.String(), `"account_required"`) {
				t.Fatalf("missing account = %d %s", r.Code, r.Body.String())
			}
		})
	}
}

func TestProtectedRoutesPassSelectedMembershipAccountToStore(t *testing.T) {
	store := &authTestStore{memberships: []appidentity.Membership{{AccountID: alternateAccountID, Role: appidentity.RoleAdult}}}
	handler := testHandler(t, store)
	req := bearerRequest(http.MethodGet, "/api/v1/library")
	req.Header.Set("X-PP-Account-ID", alternateAccountID)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || store.libraryAccount != alternateAccountID {
		t.Fatalf("status/account = %d/%q", rec.Code, store.libraryAccount)
	}
	for _, path := range []string{"/api/v1/auth/unlock", "/api/v1/auth/status", "/api/v1/auth/logout"} {
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s = %d, want 404", path, rec.Code)
		}
	}
}

func TestProtectedRoutesRejectNonMemberAccount(t *testing.T) {
	store := &authTestStore{}
	req := bearerRequest(http.MethodGet, "/api/v1/library")
	req.Header.Set("X-PP-Account-ID", alternateAccountID)
	rec := httptest.NewRecorder()
	testHandler(t, store).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"account_forbidden"`) || store.libraryCalls != 0 {
		t.Fatalf("status/calls/body = %d/%d/%s", rec.Code, store.libraryCalls, rec.Body.String())
	}
}

func TestProtectedRoutesMapIdentityErrorsWithoutFallback(t *testing.T) {
	store := &authTestStore{identityErr: appidentity.ErrNotFound}
	req := bearerRequest(http.MethodGet, "/api/v1/library")
	req.AddCookie(&http.Cookie{Name: "obsolete_cookie", Value: "obsolete"})
	rec := httptest.NewRecorder()
	testHandler(t, store).ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"onboarding_required"`) {
		t.Fatalf("status/body = %d/%s", rec.Code, rec.Body.String())
	}
	store.identityErr = errors.New("database down")
	rec = httptest.NewRecorder()
	testHandler(t, store).ServeHTTP(rec, bearerRequest(http.MethodGet, "/api/v1/library"))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"identity_unavailable"`) {
		t.Fatalf("status/body = %d/%s", rec.Code, rec.Body.String())
	}
}
