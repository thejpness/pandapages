// Package session implements Panda Pages' stateless, signed browser session.
package session

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	CookieName              = "pp_session"
	LegacyUnlockCookieName  = "pp_unlocked"
	LegacyAccountCookieName = "pp_aid"

	Lifetime      = 30 * 24 * time.Hour
	MaxFutureSkew = 5 * time.Minute

	tokenVersion = 1
)

var (
	ErrNoSession          = errors.New("session cookie is missing")
	ErrMalformedToken     = errors.New("session token is malformed")
	ErrInvalidSignature   = errors.New("session signature is invalid")
	ErrUnsupportedVersion = errors.New("session version is unsupported")
	ErrInvalidAccountID   = errors.New("session account ID is invalid")
	ErrExpired            = errors.New("session has expired")
	ErrIssuedInFuture     = errors.New("session was issued in the future")
	ErrInvalidLifetime    = errors.New("session lifetime is invalid")
)

var rawURL = base64.RawURLEncoding.Strict()

type tokenPayload struct {
	Version   int    `json:"v"`
	AccountID string `json:"aid"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

// Claims are the verified values carried by a session token.
type Claims struct {
	AccountID string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// Option customises a Manager.
type Option func(*Manager)

// WithClock supplies the clock used to issue and verify sessions.
func WithClock(now func() time.Time) Option {
	return func(manager *Manager) {
		if now != nil {
			manager.now = now
		}
	}
}

// Manager issues, verifies and expires the Panda Pages session cookie.
type Manager struct {
	secret []byte
	secure bool
	now    func() time.Time
}

// New constructs a Manager. The secret is copied so callers cannot mutate it.
func New(secret string, secure bool, options ...Option) (*Manager, error) {
	if secret == "" {
		return nil, errors.New("session secret is required")
	}
	if strings.TrimSpace(secret) != secret {
		return nil, errors.New("session secret must not have leading or trailing whitespace")
	}
	if len([]byte(secret)) < 32 {
		return nil, errors.New("session secret must contain at least 32 bytes")
	}

	manager := &Manager{
		secret: append([]byte(nil), []byte(secret)...),
		secure: secure,
		now:    time.Now,
	}
	for _, option := range options {
		option(manager)
	}
	return manager, nil
}

// Issue creates a signed token for accountID with the fixed session lifetime.
func (m *Manager) Issue(accountID string) (string, error) {
	return m.issueAt(accountID, m.now().UTC().Truncate(time.Second))
}

func (m *Manager) issueAt(accountID string, issuedAt time.Time) (string, error) {
	if !validAccountID(accountID) {
		return "", ErrInvalidAccountID
	}

	payload := tokenPayload{
		Version:   tokenVersion,
		AccountID: accountID,
		IssuedAt:  issuedAt.Unix(),
		ExpiresAt: issuedAt.Add(Lifetime).Unix(),
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal session payload: %w", err)
	}

	signature := m.sign(payloadBytes)
	return rawURL.EncodeToString(payloadBytes) + "." + rawURL.EncodeToString(signature), nil
}

// Verify authenticates token before parsing and validating its payload.
func (m *Manager) Verify(token string) (Claims, error) {
	var empty Claims

	if token == "" || strings.Count(token, ".") != 1 {
		return empty, ErrMalformedToken
	}
	payloadPart, signaturePart, _ := strings.Cut(token, ".")
	if payloadPart == "" || signaturePart == "" {
		return empty, ErrMalformedToken
	}

	payloadBytes, err := rawURL.DecodeString(payloadPart)
	if err != nil || rawURL.EncodeToString(payloadBytes) != payloadPart {
		return empty, ErrMalformedToken
	}
	signature, err := rawURL.DecodeString(signaturePart)
	if err != nil || len(signature) != sha256.Size || rawURL.EncodeToString(signature) != signaturePart {
		return empty, ErrMalformedToken
	}

	expected := m.sign(payloadBytes)
	if !hmac.Equal(signature, expected) {
		return empty, ErrInvalidSignature
	}

	var payload tokenPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return empty, ErrMalformedToken
	}
	canonical, err := json.Marshal(payload)
	if err != nil || !bytes.Equal(canonical, payloadBytes) {
		return empty, ErrMalformedToken
	}
	if payload.Version != tokenVersion {
		return empty, ErrUnsupportedVersion
	}
	if !validAccountID(payload.AccountID) {
		return empty, ErrInvalidAccountID
	}
	if payload.IssuedAt <= 0 || payload.ExpiresAt <= payload.IssuedAt {
		return empty, ErrInvalidLifetime
	}
	if payload.ExpiresAt-payload.IssuedAt > int64(Lifetime/time.Second) {
		return empty, ErrInvalidLifetime
	}

	issuedAt := time.Unix(payload.IssuedAt, 0).UTC()
	expiresAt := time.Unix(payload.ExpiresAt, 0).UTC()
	now := m.now().UTC()
	if issuedAt.After(now.Add(MaxFutureSkew)) {
		return empty, ErrIssuedInFuture
	}
	if !now.Before(expiresAt) {
		return empty, ErrExpired
	}

	return Claims{
		AccountID: payload.AccountID,
		IssuedAt:  issuedAt,
		ExpiresAt: expiresAt,
	}, nil
}

// FromRequest verifies the session cookie on r.
func (m *Manager) FromRequest(r *http.Request) (Claims, error) {
	cookie, err := r.Cookie(CookieName)
	if errors.Is(err, http.ErrNoCookie) {
		return Claims{}, ErrNoSession
	}
	if err != nil {
		return Claims{}, ErrMalformedToken
	}
	return m.Verify(cookie.Value)
}

// Set issues the session cookie and removes both legacy authentication cookies.
func (m *Manager) Set(w http.ResponseWriter, accountID string) error {
	issuedAt := m.now().UTC().Truncate(time.Second)
	token, err := m.issueAt(accountID, issuedAt)
	if err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		Expires:  issuedAt.Add(Lifetime),
		MaxAge:   int(Lifetime / time.Second),
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteStrictMode,
	})
	m.ClearLegacy(w)
	return nil
}

// Clear expires the current and legacy session cookies.
func (m *Manager) Clear(w http.ResponseWriter) {
	m.clearCookie(w, CookieName)
	m.ClearLegacy(w)
}

// ClearLegacy expires the cookies used by the unsigned legacy session contract.
func (m *Manager) ClearLegacy(w http.ResponseWriter) {
	m.clearCookie(w, LegacyUnlockCookieName)
	m.clearCookie(w, LegacyAccountCookieName)
}

func (m *Manager) clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteStrictMode,
	})
}

func (m *Manager) sign(payload []byte) []byte {
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func validAccountID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index := range value {
		switch index {
		case 8, 13, 18, 23:
			if value[index] != '-' {
				return false
			}
		default:
			character := value[index]
			if !('0' <= character && character <= '9') && !('a' <= character && character <= 'f') {
				return false
			}
		}
	}
	return value != "00000000-0000-0000-0000-000000000000"
}
