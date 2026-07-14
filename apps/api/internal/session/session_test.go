package session

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testAccountID = "11111111-1111-4111-8111-111111111111"

var testNow = time.Date(2026, time.July, 14, 17, 10, 41, 0, time.UTC)

func newTestManager(t *testing.T, secure bool) *Manager {
	t.Helper()
	manager, err := New(strings.Repeat("s", 32), secure, WithClock(func() time.Time { return testNow }))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return manager
}

func signedPayload(manager *Manager, payload tokenPayload) string {
	payloadBytes, _ := json.Marshal(payload)
	return rawURL.EncodeToString(payloadBytes) + "." + rawURL.EncodeToString(manager.sign(payloadBytes))
}

func itoa(value int) string {
	return strconv.Itoa(value)
}

func TestNewValidatesSecret(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		secret string
	}{
		{name: "missing", secret: ""},
		{name: "short", secret: strings.Repeat("s", 31)},
		{name: "leading whitespace", secret: " " + strings.Repeat("s", 32)},
		{name: "trailing whitespace", secret: strings.Repeat("s", 32) + "\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := New(test.secret, true); err == nil {
				t.Fatal("New() error = nil, want validation error")
			}
		})
	}

	if _, err := New(strings.Repeat("s", 32), true); err != nil {
		t.Fatalf("New() valid secret error = %v", err)
	}
}

func TestIssueAndVerify(t *testing.T) {
	manager := newTestManager(t, true)

	token, err := manager.Issue(testAccountID)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if strings.ContainsAny(token, "+/=") {
		t.Fatalf("Issue() token is not raw URL-safe base64: %q", token)
	}

	claims, err := manager.Verify(token)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if claims.AccountID != testAccountID {
		t.Errorf("AccountID = %q, want %q", claims.AccountID, testAccountID)
	}
	if !claims.IssuedAt.Equal(testNow) {
		t.Errorf("IssuedAt = %v, want %v", claims.IssuedAt, testNow)
	}
	if !claims.ExpiresAt.Equal(testNow.Add(Lifetime)) {
		t.Errorf("ExpiresAt = %v, want %v", claims.ExpiresAt, testNow.Add(Lifetime))
	}
}

func TestVerifyRejectsTamperingBeforeTrustingPayload(t *testing.T) {
	manager := newTestManager(t, true)
	token, err := manager.Issue(testAccountID)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	payloadPart, signaturePart, _ := strings.Cut(token, ".")

	tamperedPayloadBytes, err := rawURL.DecodeString(payloadPart)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	tamperedPayloadBytes[len(tamperedPayloadBytes)-2] ^= 1
	tamperedPayload := rawURL.EncodeToString(tamperedPayloadBytes) + "." + signaturePart
	if _, err := manager.Verify(tamperedPayload); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("tampered payload error = %v, want %v", err, ErrInvalidSignature)
	}

	signatureBytes, err := rawURL.DecodeString(signaturePart)
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	signatureBytes[0] ^= 1
	tamperedSignature := payloadPart + "." + rawURL.EncodeToString(signatureBytes)
	if _, err := manager.Verify(tamperedSignature); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("tampered signature error = %v, want %v", err, ErrInvalidSignature)
	}

	unsupportedPayload := tokenPayload{
		Version:   tokenVersion + 1,
		AccountID: testAccountID,
		IssuedAt:  testNow.Unix(),
		ExpiresAt: testNow.Add(Lifetime).Unix(),
	}
	unsupportedToken := signedPayload(manager, unsupportedPayload)
	unsupportedPayloadPart, _, _ := strings.Cut(unsupportedToken, ".")
	if _, err := manager.Verify(unsupportedPayloadPart + "." + signaturePart); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("unauthenticated unsupported version error = %v, want %v", err, ErrInvalidSignature)
	}
}

func TestVerifyRejectsMalformedAndNonCanonicalTokens(t *testing.T) {
	manager := newTestManager(t, true)
	token, err := manager.Issue(testAccountID)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	payloadPart, signaturePart, _ := strings.Cut(token, ".")

	tests := []string{
		"",
		"not-a-token",
		"one.two.three",
		"." + signaturePart,
		payloadPart + ".",
		payloadPart + "=." + signaturePart,
		payloadPart + "." + signaturePart + "=",
		payloadPart + "." + signaturePart[:len(signaturePart)-2],
		"*." + signaturePart,
	}
	for _, malformed := range tests {
		if _, err := manager.Verify(malformed); !errors.Is(err, ErrMalformedToken) {
			t.Errorf("Verify(%q) error = %v, want %v", malformed, err, ErrMalformedToken)
		}
	}

	nonCanonicalPayload := []byte(`{ "v":1,"aid":"` + testAccountID + `","iat":` + strconv.FormatInt(testNow.Unix(), 10) + `,"exp":` + strconv.FormatInt(testNow.Add(Lifetime).Unix(), 10) + `}`)
	nonCanonical := base64.RawURLEncoding.EncodeToString(nonCanonicalPayload) + "." + rawURL.EncodeToString(manager.sign(nonCanonicalPayload))
	if _, err := manager.Verify(nonCanonical); !errors.Is(err, ErrMalformedToken) {
		t.Errorf("non-canonical payload error = %v, want %v", err, ErrMalformedToken)
	}

	invalidJSON := []byte(`not-json`)
	signedInvalidJSON := rawURL.EncodeToString(invalidJSON) + "." + rawURL.EncodeToString(manager.sign(invalidJSON))
	if _, err := manager.Verify(signedInvalidJSON); !errors.Is(err, ErrMalformedToken) {
		t.Errorf("invalid JSON error = %v, want %v", err, ErrMalformedToken)
	}
}

func TestVerifyRejectsInvalidClaims(t *testing.T) {
	manager := newTestManager(t, true)
	valid := tokenPayload{
		Version:   tokenVersion,
		AccountID: testAccountID,
		IssuedAt:  testNow.Unix(),
		ExpiresAt: testNow.Add(Lifetime).Unix(),
	}

	tests := []struct {
		name    string
		mutate  func(*tokenPayload)
		wantErr error
	}{
		{
			name: "unsupported version",
			mutate: func(payload *tokenPayload) {
				payload.Version++
			},
			wantErr: ErrUnsupportedVersion,
		},
		{
			name: "missing account ID",
			mutate: func(payload *tokenPayload) {
				payload.AccountID = ""
			},
			wantErr: ErrInvalidAccountID,
		},
		{
			name: "malformed account ID",
			mutate: func(payload *tokenPayload) {
				payload.AccountID = "not-a-uuid"
			},
			wantErr: ErrInvalidAccountID,
		},
		{
			name: "non-canonical account ID",
			mutate: func(payload *tokenPayload) {
				payload.AccountID = strings.ToUpper("abcdefab-cdef-4abc-8def-abcdefabcdef")
			},
			wantErr: ErrInvalidAccountID,
		},
		{
			name: "nil account ID",
			mutate: func(payload *tokenPayload) {
				payload.AccountID = "00000000-0000-0000-0000-000000000000"
			},
			wantErr: ErrInvalidAccountID,
		},
		{
			name: "expired",
			mutate: func(payload *tokenPayload) {
				payload.IssuedAt = testNow.Add(-Lifetime).Unix()
				payload.ExpiresAt = testNow.Unix()
			},
			wantErr: ErrExpired,
		},
		{
			name: "issued materially in future",
			mutate: func(payload *tokenPayload) {
				payload.IssuedAt = testNow.Add(MaxFutureSkew + time.Second).Unix()
				payload.ExpiresAt = time.Unix(payload.IssuedAt, 0).Add(Lifetime).Unix()
			},
			wantErr: ErrIssuedInFuture,
		},
		{
			name: "excessive lifetime",
			mutate: func(payload *tokenPayload) {
				payload.ExpiresAt = time.Unix(payload.IssuedAt, 0).Add(Lifetime + time.Second).Unix()
			},
			wantErr: ErrInvalidLifetime,
		},
		{
			name: "expiry before issue",
			mutate: func(payload *tokenPayload) {
				payload.ExpiresAt = payload.IssuedAt
			},
			wantErr: ErrInvalidLifetime,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := valid
			test.mutate(&payload)
			if _, err := manager.Verify(signedPayload(manager, payload)); !errors.Is(err, test.wantErr) {
				t.Errorf("Verify() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestIssueRejectsInvalidAccountID(t *testing.T) {
	manager := newTestManager(t, true)
	if _, err := manager.Issue("not-a-uuid"); !errors.Is(err, ErrInvalidAccountID) {
		t.Fatalf("Issue() error = %v, want %v", err, ErrInvalidAccountID)
	}
}

func TestFromRequest(t *testing.T) {
	manager := newTestManager(t, true)

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, err := manager.FromRequest(request); !errors.Is(err, ErrNoSession) {
		t.Fatalf("FromRequest() without cookie error = %v, want %v", err, ErrNoSession)
	}

	token, err := manager.Issue(testAccountID)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	request.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	claims, err := manager.FromRequest(request)
	if err != nil {
		t.Fatalf("FromRequest() error = %v", err)
	}
	if claims.AccountID != testAccountID {
		t.Errorf("AccountID = %q, want %q", claims.AccountID, testAccountID)
	}
}

func TestSetUsesSecureHostOnlyCookieAndClearsLegacyCookies(t *testing.T) {
	manager := newTestManager(t, true)
	recorder := httptest.NewRecorder()

	if err := manager.Set(recorder, testAccountID); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 3 {
		t.Fatalf("cookies = %d, want 3", len(cookies))
	}

	sessionCookie := cookies[0]
	if sessionCookie.Name != CookieName {
		t.Errorf("session cookie name = %q, want %q", sessionCookie.Name, CookieName)
	}
	if sessionCookie.Path != "/" || sessionCookie.Domain != "" || !sessionCookie.HttpOnly || !sessionCookie.Secure || sessionCookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("session cookie attributes = %+v", sessionCookie)
	}
	if sessionCookie.MaxAge != int(Lifetime/time.Second) {
		t.Errorf("MaxAge = %d, want %d", sessionCookie.MaxAge, int(Lifetime/time.Second))
	}
	if !sessionCookie.Expires.Equal(testNow.Add(Lifetime)) {
		t.Errorf("Expires = %v, want %v", sessionCookie.Expires, testNow.Add(Lifetime))
	}
	if _, err := manager.Verify(sessionCookie.Value); err != nil {
		t.Errorf("Verify(session cookie) error = %v", err)
	}

	for index, wantName := range []string{LegacyUnlockCookieName, LegacyAccountCookieName} {
		cookie := cookies[index+1]
		if cookie.Name != wantName {
			t.Errorf("legacy cookie %d name = %q, want %q", index, cookie.Name, wantName)
		}
		assertDeletedCookie(t, cookie, true)
	}
}

func TestClearExpiresCurrentAndLegacyCookies(t *testing.T) {
	t.Parallel()

	for _, secure := range []bool{false, true} {
		t.Run("secure="+itoa(map[bool]int{false: 0, true: 1}[secure]), func(t *testing.T) {
			t.Parallel()
			manager := newTestManager(t, secure)
			recorder := httptest.NewRecorder()
			manager.Clear(recorder)

			cookies := recorder.Result().Cookies()
			if len(cookies) != 3 {
				t.Fatalf("cookies = %d, want 3", len(cookies))
			}
			for index, name := range []string{CookieName, LegacyUnlockCookieName, LegacyAccountCookieName} {
				if cookies[index].Name != name {
					t.Errorf("cookie %d name = %q, want %q", index, cookies[index].Name, name)
				}
				assertDeletedCookie(t, cookies[index], secure)
			}
		})
	}
}

func assertDeletedCookie(t *testing.T, cookie *http.Cookie, secure bool) {
	t.Helper()
	if cookie.Value != "" || cookie.Path != "/" || cookie.Domain != "" || cookie.MaxAge >= 0 || !cookie.Expires.Before(testNow) || !cookie.HttpOnly || cookie.Secure != secure || cookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("deleted cookie attributes = %+v", cookie)
	}
}
