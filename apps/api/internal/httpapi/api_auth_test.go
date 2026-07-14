package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pandapages/api/internal/model"
	"pandapages/api/internal/session"
)

const (
	testAccountID     = "11111111-1111-4111-8111-111111111111"
	testSessionSecret = "test-session-secret-with-at-least-32-bytes"
)

var testSessionTime = time.Date(2026, time.July, 14, 17, 10, 41, 0, time.UTC)

type authTestStore struct {
	accountID        string
	ensureErr        error
	ensureCalls      int
	accountExists    bool
	accountExistsErr error
	existsCalls      int
	libraryCalls     int
	libraryAccount   string
}

func (s *authTestStore) EnsureDefaultAccount() (string, error) {
	s.ensureCalls++
	if s.ensureErr != nil {
		return "", s.ensureErr
	}
	if s.accountID == "" {
		return testAccountID, nil
	}
	return s.accountID, nil
}

func (s *authTestStore) AccountExists(accountID string) (bool, error) {
	s.existsCalls++
	if s.accountExistsErr != nil {
		return false, s.accountExistsErr
	}
	return s.accountExists && accountID == testAccountID, nil
}

func (s *authTestStore) Library(accountID string) ([]model.StoryItem, error) {
	s.libraryCalls++
	s.libraryAccount = accountID
	return []model.StoryItem{}, nil
}

func (*authTestStore) StoryLatest(string, string) (model.StoryPayload, error) {
	return model.StoryPayload{}, nil
}

func (*authTestStore) StorySegments(string, string) (model.StorySegmentsPayload, error) {
	return model.StorySegmentsPayload{}, nil
}

func (*authTestStore) ProgressGet(string, string) (model.ProgressState, error) {
	return model.ProgressState{}, nil
}

func (*authTestStore) ProgressPut(string, string, int, json.RawMessage, float64) error {
	return nil
}

func (*authTestStore) ContinueRecent(string, int) ([]model.ContinueItem, error) {
	return nil, nil
}

func (*authTestStore) SettingsGet(string) (model.SettingsPayload, error) {
	return model.SettingsPayload{}, nil
}

func (*authTestStore) SettingsPut(_ string, payload model.SettingsUpsert) (model.SettingsPayload, error) {
	return model.SettingsPayload{Child: payload.Child, Prompt: payload.Prompt}, nil
}

func testSessionManager(t *testing.T, secure bool, now func() time.Time) *session.Manager {
	t.Helper()
	manager, err := session.New(testSessionSecret, secure, session.WithClock(now))
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	return manager
}

func testHandler(t *testing.T, store *authTestStore, manager *session.Manager) http.Handler {
	t.Helper()
	return New(Config{Passcode: "123456", Sessions: manager}, store)
}

func sessionRequest(t *testing.T, manager *session.Manager, method, path string) *http.Request {
	t.Helper()
	token, err := manager.Issue(testAccountID)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	req := httptest.NewRequest(method, path, nil)
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: token})
	return req
}

func cookieMap(cookies []*http.Cookie) map[string]*http.Cookie {
	out := make(map[string]*http.Cookie, len(cookies))
	for _, cookie := range cookies {
		out[cookie.Name] = cookie
	}
	return out
}

func assertDeletionCookies(t *testing.T, cookies []*http.Cookie, secure bool) {
	t.Helper()
	byName := cookieMap(cookies)
	for _, name := range []string{session.CookieName, session.LegacyUnlockCookieName, session.LegacyAccountCookieName} {
		cookie := byName[name]
		if cookie == nil {
			t.Errorf("cookie %q was not expired", name)
			continue
		}
		if cookie.Value != "" || cookie.MaxAge >= 0 || !cookie.Expires.Before(testSessionTime) {
			t.Errorf("cookie %q was not a deletion cookie: %#v", name, cookie)
		}
		if cookie.Path != "/" || cookie.Domain != "" || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Secure != secure {
			t.Errorf("cookie %q attributes drifted: %#v", name, cookie)
		}
	}
}

func decodeStatus(t *testing.T, recorder *httptest.ResponseRecorder) bool {
	t.Helper()
	var payload struct {
		Unlocked bool `json:"unlocked"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	return payload.Unlocked
}

func TestUnlockSetsSignedSessionAndExpiresLegacyCookies(t *testing.T) {
	manager := testSessionManager(t, true, func() time.Time { return testSessionTime })
	store := &authTestStore{}
	handler := testHandler(t, store, manager)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/unlock", strings.NewReader(`{"passcode":"123456"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if store.ensureCalls != 1 {
		t.Fatalf("EnsureDefaultAccount calls = %d, want 1", store.ensureCalls)
	}

	cookies := cookieMap(rec.Result().Cookies())
	if len(cookies) != 3 {
		t.Fatalf("cookie names = %#v, want signed session plus two legacy expiries", cookies)
	}
	signed := cookies[session.CookieName]
	if signed == nil {
		t.Fatal("signed session cookie was not set")
	}
	if signed.Path != "/" || signed.Domain != "" || !signed.Secure || !signed.HttpOnly || signed.SameSite != http.SameSiteStrictMode {
		t.Fatalf("signed session attributes drifted: %#v", signed)
	}
	if signed.MaxAge != int(session.Lifetime/time.Second) || !signed.Expires.Equal(testSessionTime.Add(session.Lifetime)) {
		t.Fatalf("signed session lifetime drifted: %#v", signed)
	}
	claims, err := manager.Verify(signed.Value)
	if err != nil {
		t.Fatalf("issued cookie did not verify: %v", err)
	}
	if claims.AccountID != testAccountID {
		t.Fatalf("account ID = %q, want %q", claims.AccountID, testAccountID)
	}
	for _, name := range []string{session.LegacyUnlockCookieName, session.LegacyAccountCookieName} {
		legacy := cookies[name]
		if legacy == nil || legacy.MaxAge >= 0 || legacy.Value != "" {
			t.Errorf("legacy cookie %q was not expired: %#v", name, legacy)
		}
	}
}

func TestUnlockDoesNotTrimPasscodeInput(t *testing.T) {
	manager := testSessionManager(t, false, func() time.Time { return testSessionTime })
	store := &authTestStore{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/unlock", strings.NewReader(`{"passcode":" 123456 "}`))
	rec := httptest.NewRecorder()

	testHandler(t, store, manager).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if store.ensureCalls != 0 {
		t.Fatal("default account was selected for an invalid passcode")
	}
}

func TestLegacyCookiesAloneAreRejected(t *testing.T) {
	manager := testSessionManager(t, false, func() time.Time { return testSessionTime })
	store := &authTestStore{accountExists: true}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/library", nil)
	req.AddCookie(&http.Cookie{Name: session.LegacyUnlockCookieName, Value: "1"})
	req.AddCookie(&http.Cookie{Name: session.LegacyAccountCookieName, Value: testAccountID})
	rec := httptest.NewRecorder()

	testHandler(t, store, manager).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if store.existsCalls != 0 || store.libraryCalls != 0 {
		t.Fatal("legacy cookies reached account or library storage")
	}
	assertDeletionCookies(t, rec.Result().Cookies(), false)
}

func TestStatusSessionSemantics(t *testing.T) {
	t.Run("before unlock", func(t *testing.T) {
		manager := testSessionManager(t, false, func() time.Time { return testSessionTime })
		store := &authTestStore{accountExists: true}
		rec := httptest.NewRecorder()
		testHandler(t, store, manager).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/auth/status", nil))

		if rec.Code != http.StatusOK || decodeStatus(t, rec) {
			t.Fatalf("status response = %d %s", rec.Code, rec.Body.String())
		}
		if store.existsCalls != 0 {
			t.Fatal("missing session queried the account store")
		}
		assertDeletionCookies(t, rec.Result().Cookies(), false)
	})

	t.Run("valid", func(t *testing.T) {
		manager := testSessionManager(t, false, func() time.Time { return testSessionTime })
		store := &authTestStore{accountExists: true}
		rec := httptest.NewRecorder()
		testHandler(t, store, manager).ServeHTTP(rec, sessionRequest(t, manager, http.MethodGet, "/api/v1/auth/status"))

		if rec.Code != http.StatusOK || !decodeStatus(t, rec) {
			t.Fatalf("status response = %d %s", rec.Code, rec.Body.String())
		}
		if store.existsCalls != 1 {
			t.Fatalf("AccountExists calls = %d, want 1", store.existsCalls)
		}
	})

	t.Run("tampered", func(t *testing.T) {
		manager := testSessionManager(t, false, func() time.Time { return testSessionTime })
		store := &authTestStore{accountExists: true}
		req := sessionRequest(t, manager, http.MethodGet, "/api/v1/auth/status")
		cookie := req.Cookies()[0]
		if strings.HasSuffix(cookie.Value, "A") {
			cookie.Value = cookie.Value[:len(cookie.Value)-1] + "B"
		} else {
			cookie.Value = cookie.Value[:len(cookie.Value)-1] + "A"
		}
		req.Header.Del("Cookie")
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		testHandler(t, store, manager).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK || decodeStatus(t, rec) {
			t.Fatalf("status response = %d %s", rec.Code, rec.Body.String())
		}
		if store.existsCalls != 0 {
			t.Fatal("tampered session queried the account store")
		}
		assertDeletionCookies(t, rec.Result().Cookies(), false)
	})

	t.Run("expired", func(t *testing.T) {
		now := testSessionTime
		manager := testSessionManager(t, false, func() time.Time { return now })
		req := sessionRequest(t, manager, http.MethodGet, "/api/v1/auth/status")
		now = now.Add(session.Lifetime + time.Second)
		store := &authTestStore{accountExists: true}
		rec := httptest.NewRecorder()
		testHandler(t, store, manager).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK || decodeStatus(t, rec) {
			t.Fatalf("status response = %d %s", rec.Code, rec.Body.String())
		}
		if store.existsCalls != 0 {
			t.Fatal("expired session queried the account store")
		}
	})

	t.Run("unknown account", func(t *testing.T) {
		manager := testSessionManager(t, false, func() time.Time { return testSessionTime })
		store := &authTestStore{accountExists: false}
		rec := httptest.NewRecorder()
		testHandler(t, store, manager).ServeHTTP(rec, sessionRequest(t, manager, http.MethodGet, "/api/v1/auth/status"))

		if rec.Code != http.StatusOK || decodeStatus(t, rec) {
			t.Fatalf("status response = %d %s", rec.Code, rec.Body.String())
		}
		assertDeletionCookies(t, rec.Result().Cookies(), false)
	})

	t.Run("database failure", func(t *testing.T) {
		manager := testSessionManager(t, false, func() time.Time { return testSessionTime })
		store := &authTestStore{accountExistsErr: errors.New("database unavailable")}
		rec := httptest.NewRecorder()
		testHandler(t, store, manager).ServeHTTP(rec, sessionRequest(t, manager, http.MethodGet, "/api/v1/auth/status"))

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
		}
		if len(rec.Result().Cookies()) != 0 {
			t.Fatal("valid session was cleared because account storage failed")
		}
	})
}

func TestProtectedRouteUsesOnlyVerifiedSessionAccount(t *testing.T) {
	manager := testSessionManager(t, false, func() time.Time { return testSessionTime })
	store := &authTestStore{accountExists: true}
	rec := httptest.NewRecorder()

	testHandler(t, store, manager).ServeHTTP(rec, sessionRequest(t, manager, http.MethodGet, "/api/v1/library"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if store.libraryCalls != 1 || store.libraryAccount != testAccountID {
		t.Fatalf("library calls/account = %d/%q", store.libraryCalls, store.libraryAccount)
	}
}

func TestProtectedRouteRejectsInvalidSessionsAndKeepsDatabaseFailuresDistinct(t *testing.T) {
	t.Run("tampered session", func(t *testing.T) {
		manager := testSessionManager(t, false, func() time.Time { return testSessionTime })
		store := &authTestStore{accountExists: true}
		request := sessionRequest(t, manager, http.MethodGet, "/api/v1/library")
		cookie := request.Cookies()[0]
		cookie.Value = cookie.Value[:len(cookie.Value)-1] + "x"
		request.Header.Del("Cookie")
		request.AddCookie(cookie)
		response := httptest.NewRecorder()

		testHandler(t, store, manager).ServeHTTP(response, request)

		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
		}
		if store.existsCalls != 0 || store.libraryCalls != 0 {
			t.Fatal("tampered session reached account or library storage")
		}
		assertDeletionCookies(t, response.Result().Cookies(), false)
	})

	t.Run("unknown account", func(t *testing.T) {
		manager := testSessionManager(t, false, func() time.Time { return testSessionTime })
		store := &authTestStore{accountExists: false}
		response := httptest.NewRecorder()

		testHandler(t, store, manager).ServeHTTP(
			response,
			sessionRequest(t, manager, http.MethodGet, "/api/v1/library"),
		)

		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
		}
		if store.existsCalls != 1 || store.libraryCalls != 0 {
			t.Fatalf("account/library calls = %d/%d", store.existsCalls, store.libraryCalls)
		}
		assertDeletionCookies(t, response.Result().Cookies(), false)
	})

	t.Run("database unavailable", func(t *testing.T) {
		manager := testSessionManager(t, false, func() time.Time { return testSessionTime })
		store := &authTestStore{accountExistsErr: errors.New("database unavailable")}
		response := httptest.NewRecorder()

		testHandler(t, store, manager).ServeHTTP(
			response,
			sessionRequest(t, manager, http.MethodGet, "/api/v1/library"),
		)

		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
		}
		if store.libraryCalls != 0 {
			t.Fatal("library storage called while session validation was unavailable")
		}
		if len(response.Result().Cookies()) != 0 {
			t.Fatal("valid session was cleared because account storage failed")
		}
	})
}

func TestLogoutIsDatabaseIndependentAndIdempotent(t *testing.T) {
	manager := testSessionManager(t, true, func() time.Time { return testSessionTime })
	store := &authTestStore{accountExistsErr: errors.New("database unavailable")}
	handler := testHandler(t, store, manager)

	for attempt := 1; attempt <= 2; attempt++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
		if attempt == 1 {
			token, err := manager.Issue(testAccountID)
			if err != nil {
				t.Fatal(err)
			}
			req.AddCookie(&http.Cookie{Name: session.CookieName, Value: token})
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("attempt %d response = %d %s", attempt, rec.Code, rec.Body.String())
		}
		assertDeletionCookies(t, rec.Result().Cookies(), true)
	}
	if store.existsCalls != 0 {
		t.Fatal("logout consulted account storage")
	}
}

func TestUnlockRejectsOversizedBody(t *testing.T) {
	manager := testSessionManager(t, false, func() time.Time { return testSessionTime })
	handler := testHandler(t, &authTestStore{}, manager)
	body := `{"passcode":"` + strings.Repeat("0", maxJSONBodyBytes) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/unlock", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
}
