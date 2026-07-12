package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pandapages/api/internal/model"
)

type authTestStore struct{}

func (authTestStore) EnsureDefaultAccount() (string, error) {
	return "11111111-1111-4111-8111-111111111111", nil
}

func (authTestStore) Library(string) ([]model.StoryItem, error) {
	return nil, nil
}

func (authTestStore) StoryLatest(string, string) (model.StoryPayload, error) {
	return model.StoryPayload{}, nil
}

func (authTestStore) StorySegments(string, string) (model.StorySegmentsPayload, error) {
	return model.StorySegmentsPayload{}, nil
}

func (authTestStore) ProgressGet(string, string) (model.ProgressState, error) {
	return model.ProgressState{}, nil
}

func (authTestStore) ProgressPut(string, string, int, json.RawMessage, float64) error {
	return nil
}

func (authTestStore) ContinueRecent(string, int) ([]model.ContinueItem, error) {
	return nil, nil
}

func (authTestStore) SettingsGet(string) (model.SettingsPayload, error) {
	return model.SettingsPayload{}, nil
}

func (authTestStore) SettingsPut(_ string, payload model.SettingsUpsert) (model.SettingsPayload, error) {
	return model.SettingsPayload{Child: payload.Child, Prompt: payload.Prompt}, nil
}

func TestUnlockSetsSecureHostOnlyCookiesForAllRoutes(t *testing.T) {
	handler := New(Config{Passcode: "123456", CookieSecure: true}, authTestStore{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/unlock", strings.NewReader(`{"passcode":"123456"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	cookies := rec.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("cookies = %d, want 2", len(cookies))
	}
	seen := map[string]bool{}
	for _, cookie := range cookies {
		seen[cookie.Name] = true
		if cookie.Path != "/" {
			t.Errorf("%s Path = %q, want /", cookie.Name, cookie.Path)
		}
		if cookie.Domain != "" {
			t.Errorf("%s Domain = %q, want host-only", cookie.Name, cookie.Domain)
		}
		if !cookie.Secure {
			t.Errorf("%s Secure = false, want true", cookie.Name)
		}
		if !cookie.HttpOnly {
			t.Errorf("%s HttpOnly = false, want true", cookie.Name)
		}
		if cookie.SameSite != http.SameSiteStrictMode {
			t.Errorf("%s SameSite = %v, want Strict", cookie.Name, cookie.SameSite)
		}
		if cookie.MaxAge != sessionMaxAgeSeconds {
			t.Errorf("%s MaxAge = %d, want %d", cookie.Name, cookie.MaxAge, sessionMaxAgeSeconds)
		}
	}
	for _, name := range []string{cookieName, accountCookieName} {
		if !seen[name] {
			t.Errorf("cookie %s was not set", name)
		}
	}
}
