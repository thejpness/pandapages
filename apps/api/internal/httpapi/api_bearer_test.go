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
	"pandapages/api/internal/httpprofile"
	"pandapages/api/internal/model"
	"pandapages/api/internal/readercontract"
)

const testAccountID = "11111111-1111-4111-8111-111111111111"
const alternateAccountID = "22222222-2222-4222-8222-222222222222"
const testProfileID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
const testBearer = "header.payload.signature"

type authTestStore struct {
	readinessErr            error
	readinessCheck          func(context.Context) error
	readinessCalls          int
	identityErr             error
	memberships             []appidentity.Membership
	libraryCalls            int
	libraryAccount          string
	libraryResponse         model.LibraryReadModel
	libraryErr              error
	readerCalls             int
	readerAccount           string
	readerSlug              string
	readerResponse          model.ReaderStory
	readerErr               error
	progressGetCalls        int
	progressAccount         string
	progressProfile         string
	progressGetState        model.ProgressResponse
	progressGetErr          error
	progressPutCalls        int
	progressSlug            string
	progressVersion         int
	progressLocator         readercontract.Locator
	progressPercent         float64
	progressPutErr          error
	continueCalls           int
	continueAccount         string
	continueProfile         string
	continueLimit           int
	continueItems           []model.ContinueItem
	continueErr             error
	profilesCalls           int
	profilesAccount         string
	profiles                []model.ReaderProfile
	profilesErr             error
	profileExistsCalls      int
	profileExistsAccount    string
	profileExistsID         string
	profileForbidden        bool
	profileExistsErr        error
	profileCreateCalls      int
	profileCreateAccount    string
	profileCreateName       string
	profileCreateEdition    model.ReaderEditionKey
	profileCreate           model.ReaderProfile
	profileCreateErr        error
	profileUpdateCalls      int
	profileUpdateAccount    string
	profileUpdateID         string
	profileUpdateName       string
	profileUpdateEdition    model.ReaderEditionKey
	profileUpdate           model.ReaderProfile
	profileUpdateErr        error
	profileDeleteCalls      int
	profileDeleteAccount    string
	profileDeleteID         string
	profileDeleteErr        error
	profilePINSetCalls      int
	profilePINSetAccount    string
	profilePINSetID         string
	profilePINHash          string
	profilePINSetErr        error
	profilePINRemoveCalls   int
	profilePINRemoveAccount string
	profilePINRemoveID      string
	profilePINRemoveErr     error
	profilePINVerifyCalls   int
	profilePINVerifyAccount string
	profilePINVerifyID      string
	profilePINCandidate     string
	profilePINVerifyErr     error
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
func (s *authTestStore) ProgressGet(accountID, profileID, _ string) (model.ProgressResponse, error) {
	s.progressGetCalls++
	s.progressAccount = accountID
	s.progressProfile = profileID
	return s.progressGetState, s.progressGetErr
}
func (s *authTestStore) ProgressPut(accountID, profileID, slug string, version int, locator readercontract.Locator, percent float64) error {
	s.progressPutCalls++
	s.progressAccount = accountID
	s.progressProfile = profileID
	s.progressSlug = slug
	s.progressVersion = version
	s.progressLocator = locator
	s.progressPercent = percent
	return s.progressPutErr
}
func (s *authTestStore) ContinueRecent(accountID, profileID string, limit int) ([]model.ContinueItem, error) {
	s.continueCalls++
	s.continueAccount = accountID
	s.continueProfile = profileID
	s.continueLimit = limit
	return s.continueItems, s.continueErr
}
func (s *authTestStore) ProfileExists(accountID, profileID string) (bool, error) {
	s.profileExistsCalls++
	s.profileExistsAccount = accountID
	s.profileExistsID = profileID
	return !s.profileForbidden, s.profileExistsErr
}
func (s *authTestStore) Profiles(accountID string) ([]model.ReaderProfile, error) {
	s.profilesCalls++
	s.profilesAccount = accountID
	if s.profiles == nil {
		s.profiles = []model.ReaderProfile{}
	}
	return s.profiles, s.profilesErr
}
func (s *authTestStore) CreateProfile(
	accountID string,
	name string,
	preferredEdition model.ReaderEditionKey,
) (model.ReaderProfile, error) {
	s.profileCreateCalls++
	s.profileCreateAccount = accountID
	s.profileCreateName = name
	s.profileCreateEdition = preferredEdition
	return s.profileCreate, s.profileCreateErr
}
func (s *authTestStore) UpdateProfile(
	accountID string,
	profileID string,
	name string,
	preferredEdition model.ReaderEditionKey,
) (model.ReaderProfile, error) {
	s.profileUpdateCalls++
	s.profileUpdateAccount = accountID
	s.profileUpdateID = profileID
	s.profileUpdateName = name
	s.profileUpdateEdition = preferredEdition
	return s.profileUpdate, s.profileUpdateErr
}
func (s *authTestStore) DeleteProfile(accountID, profileID string) error {
	s.profileDeleteCalls++
	s.profileDeleteAccount = accountID
	s.profileDeleteID = profileID
	return s.profileDeleteErr
}
func (s *authTestStore) SetProfilePIN(accountID, profileID, encodedHash string) error {
	s.profilePINSetCalls++
	s.profilePINSetAccount = accountID
	s.profilePINSetID = profileID
	s.profilePINHash = encodedHash
	return s.profilePINSetErr
}
func (s *authTestStore) RemoveProfilePIN(accountID, profileID string) error {
	s.profilePINRemoveCalls++
	s.profilePINRemoveAccount = accountID
	s.profilePINRemoveID = profileID
	return s.profilePINRemoveErr
}
func (s *authTestStore) VerifyProfilePIN(accountID, profileID, candidate string) error {
	s.profilePINVerifyCalls++
	s.profilePINVerifyAccount = accountID
	s.profilePINVerifyID = profileID
	s.profilePINCandidate = candidate
	return s.profilePINVerifyErr
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
	return New(Config{
		BearerAuthenticator: httpbearer.New(bearerVerifier{}, store),
		ProfileResolver:     httpprofile.New(store),
	}, store)
}
func bearerRequest(method, path string) *http.Request {
	request := httptest.NewRequest(method, path, nil)
	request.Header.Set("Authorization", "Bearer "+testBearer)
	request.Header.Set("X-PP-Account-ID", testAccountID)
	return request
}

func profileBearerRequest(method, path string) *http.Request {
	request := bearerRequest(method, path)
	request.Header.Set("X-PP-Profile-ID", testProfileID)
	return request
}

func TestProtectedRoutesRequireBearerAccountContext(t *testing.T) {
	tests := []struct{ name, method, path, body string }{
		{"library", http.MethodGet, "/api/v1/library", ""}, {"reader", http.MethodGet, "/api/v1/reader/story", ""}, {"progress get", http.MethodGet, "/api/v1/progress/story", ""}, {"progress put", http.MethodPut, "/api/v1/progress/story", `{"version":1,"locator":{"schema":2,"segment":{"key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","occurrence":1,"ordinal":1,"offset":0},"chapter":null},"percent":0}`}, {"continue", http.MethodGet, "/api/v1/continue", ""}, {"profiles list", http.MethodGet, "/api/v1/profiles", ""}, {"profiles create", http.MethodPost, "/api/v1/profiles", `{"name":"Ted","preferredEdition":"classic"}`}, {"profiles update", http.MethodPatch, "/api/v1/profiles/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", `{"name":"Ted","preferredEdition":"classic"}`}, {"profiles delete", http.MethodDelete, "/api/v1/profiles/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", ""}, {"profiles PIN set", http.MethodPut, "/api/v1/profiles/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/pin", `{"pin":"1234"}`}, {"profiles PIN verify", http.MethodPost, "/api/v1/profiles/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/pin", `{"pin":"1234"}`}, {"profiles PIN remove", http.MethodDelete, "/api/v1/profiles/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/pin", ""},
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

func TestProfileScopedRoutesRequireValidatedProfileContext(t *testing.T) {
	for _, test := range []struct {
		name        string
		method      string
		path        string
		profile     string
		forbidden   bool
		wantStatus  int
		wantCode    string
		wantLookups int
	}{
		{name: "missing profile", method: http.MethodGet, path: "/api/v1/progress/story", wantStatus: http.StatusBadRequest, wantCode: "profile_required"},
		{name: "invalid profile", method: http.MethodGet, path: "/api/v1/progress/story", profile: "not-a-uuid", wantStatus: http.StatusBadRequest, wantCode: "invalid_profile"},
		{name: "cross account or missing profile", method: http.MethodGet, path: "/api/v1/progress/story", profile: testProfileID, forbidden: true, wantStatus: http.StatusForbidden, wantCode: "profile_forbidden", wantLookups: 1},
		{name: "continue missing profile", method: http.MethodGet, path: "/api/v1/continue", wantStatus: http.StatusBadRequest, wantCode: "profile_required"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &authTestStore{profileForbidden: test.forbidden}
			request := bearerRequest(test.method, test.path)
			if test.profile != "" {
				request.Header.Set("X-PP-Profile-ID", test.profile)
			}
			response := httptest.NewRecorder()
			testHandler(t, store).ServeHTTP(response, request)
			if response.Code != test.wantStatus || !strings.Contains(response.Body.String(), `"`+test.wantCode+`"`) {
				t.Fatalf("status/body = %d/%s", response.Code, response.Body.String())
			}
			if store.profileExistsCalls != test.wantLookups || store.progressGetCalls != 0 || store.continueCalls != 0 {
				t.Fatalf("profile/store calls = %d/%d/%d", store.profileExistsCalls, store.progressGetCalls, store.continueCalls)
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestProfileScopedRoutesPassAccountAndProfileUnchangedToStore(t *testing.T) {
	store := &authTestStore{memberships: []appidentity.Membership{{AccountID: alternateAccountID, Role: appidentity.RoleAdult}}}
	request := bearerRequest(http.MethodGet, "/api/v1/progress/story")
	request.Header.Set("X-PP-Account-ID", alternateAccountID)
	request.Header.Set("X-PP-Profile-ID", strings.ToUpper(testProfileID))
	response := httptest.NewRecorder()
	testHandler(t, store).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if store.profileExistsAccount != alternateAccountID || store.profileExistsID != testProfileID ||
		store.progressAccount != alternateAccountID || store.progressProfile != testProfileID {
		t.Fatalf("profile lookup/store context = %q/%q %q/%q", store.profileExistsAccount, store.profileExistsID, store.progressAccount, store.progressProfile)
	}

	request = bearerRequest(http.MethodGet, "/api/v1/continue")
	request.Header.Set("X-PP-Account-ID", alternateAccountID)
	request.Header.Set("X-PP-Profile-ID", testProfileID)
	response = httptest.NewRecorder()
	testHandler(t, store).ServeHTTP(response, request)
	if response.Code != http.StatusOK || store.continueAccount != alternateAccountID || store.continueProfile != testProfileID {
		t.Fatalf("continue status/context = %d %q/%q", response.Code, store.continueAccount, store.continueProfile)
	}
}

func TestAccountMembershipDenialPrecedesProfileLookup(t *testing.T) {
	store := &authTestStore{}
	request := profileBearerRequest(http.MethodGet, "/api/v1/progress/story")
	request.Header.Set("X-PP-Account-ID", alternateAccountID)
	response := httptest.NewRecorder()
	testHandler(t, store).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"account_forbidden"`) || store.profileExistsCalls != 0 {
		t.Fatalf("status/body/lookups = %d/%s/%d", response.Code, response.Body.String(), store.profileExistsCalls)
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
