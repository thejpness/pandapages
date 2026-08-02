package supabaseauth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testAudience = "authenticated"
	testSubject  = "123e4567-e89b-12d3-a456-426614174000"
)

type jwksFixture struct {
	mu       sync.Mutex
	document any
	requests int
}

func (fixture *jwksFixture) serve(response http.ResponseWriter, _ *http.Request) {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	fixture.requests++
	response.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(response).Encode(fixture.document)
}

func (fixture *jwksFixture) set(document any) {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	fixture.document = document
}

func (fixture *jwksFixture) count() int {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	return fixture.requests
}

func testRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func testECKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func rsaJWK(kid string, key *rsa.PublicKey) map[string]any {
	e := big.NewInt(int64(key.E)).Bytes()
	return map[string]any{
		"kid": kid,
		"kty": "RSA",
		"alg": "RS256",
		"use": "sig",
		"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(e),
	}
}

func ecJWK(kid string, key *ecdsa.PublicKey) map[string]any {
	return map[string]any{
		"kid": kid,
		"kty": "EC",
		"alg": "ES256",
		"use": "sig",
		"crv": "P-256",
		"x":   base64.RawURLEncoding.EncodeToString(key.X.FillBytes(make([]byte, 32))),
		"y":   base64.RawURLEncoding.EncodeToString(key.Y.FillBytes(make([]byte, 32))),
	}
}

func claims(now time.Time) jwt.RegisteredClaims {
	return jwt.RegisteredClaims{
		Issuer:    "placeholder",
		Subject:   testSubject,
		Audience:  jwt.ClaimStrings{testAudience},
		ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
		NotBefore: jwt.NewNumericDate(now.Add(-time.Minute)),
		IssuedAt:  jwt.NewNumericDate(now.Add(-time.Minute)),
	}
}

func signedToken(t *testing.T, method jwt.SigningMethod, kid string, claim jwt.RegisteredClaims, key any) string {
	t.Helper()
	token := jwt.NewWithClaims(method, claim)
	token.Header["kid"] = kid
	raw, err := token.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func verifierFixture(t *testing.T, now time.Time, document any) (*Verifier, *jwksFixture, string) {
	t.Helper()
	fixture := &jwksFixture{document: document}
	server := httptest.NewTLSServer(http.HandlerFunc(fixture.serve))
	t.Cleanup(server.Close)
	issuer := server.URL + "/auth/v1"
	verifier, err := New(Config{
		Issuer:     issuer,
		Audience:   testAudience,
		JWKSURL:    issuer + "/.well-known/jwks.json",
		CacheTTL:   time.Minute,
		HTTPClient: server.Client(),
		Now:        func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return verifier, fixture, issuer
}

func TestVerifierAcceptsRS256AndES256(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	rsaKey := testRSAKey(t)
	ecKey := testECKey(t)
	verifier, fixture, issuer := verifierFixture(t, now, map[string]any{
		"keys": []any{rsaJWK("rsa-1", &rsaKey.PublicKey), ecJWK("ec-1", &ecKey.PublicKey)},
	})
	claim := claims(now)
	claim.Issuer = issuer

	for _, test := range []struct {
		name   string
		method jwt.SigningMethod
		kid    string
		key    any
	}{
		{name: "RS256", method: jwt.SigningMethodRS256, kid: "rsa-1", key: rsaKey},
		{name: "ES256", method: jwt.SigningMethodES256, kid: "ec-1", key: ecKey},
	} {
		t.Run(test.name, func(t *testing.T) {
			identity, err := verifier.Verify(context.Background(), signedToken(t, test.method, test.kid, claim, test.key))
			if err != nil {
				t.Fatalf("Verify() error = %v", err)
			}
			if identity.Provider != "supabase" || identity.Issuer != issuer || identity.Subject != testSubject {
				t.Fatalf("identity = %#v", identity)
			}
		})
	}
	if fixture.count() != 1 {
		t.Fatalf("JWKS requests = %d, want one cached fetch", fixture.count())
	}
}

func TestVerifierRejectsInvalidTokensAndClaims(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	key := testRSAKey(t)
	wrongKey := testRSAKey(t)
	verifier, _, issuer := verifierFixture(t, now, map[string]any{
		"keys": []any{rsaJWK("rsa-1", &key.PublicKey)},
	})
	valid := claims(now)
	valid.Issuer = issuer

	tests := []struct {
		name   string
		method jwt.SigningMethod
		claim  jwt.RegisteredClaims
		key    any
	}{
		{name: "HS256 algorithm confusion", method: jwt.SigningMethodHS256, claim: valid, key: []byte("not-a-public-key")},
		{name: "unsupported PS256", method: jwt.SigningMethodPS256, claim: valid, key: key},
		{name: "invalid signature", method: jwt.SigningMethodRS256, claim: valid, key: wrongKey},
		{name: "wrong issuer", method: jwt.SigningMethodRS256, claim: withIssuer(valid, "https://wrong.example/auth/v1"), key: key},
		{name: "wrong audience", method: jwt.SigningMethodRS256, claim: withAudience(valid, "other"), key: key},
		{name: "expired", method: jwt.SigningMethodRS256, claim: withExpiry(valid, now.Add(-time.Minute)), key: key},
		{name: "future not before", method: jwt.SigningMethodRS256, claim: withNotBefore(valid, now.Add(5*time.Minute)), key: key},
		{name: "missing subject", method: jwt.SigningMethodRS256, claim: withSubject(valid, ""), key: key},
		{name: "untrimmed subject", method: jwt.SigningMethodRS256, claim: withSubject(valid, " subject "), key: key},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := signedToken(t, test.method, "rsa-1", test.claim, test.key)
			_, err := verifier.Verify(context.Background(), raw)
			if !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("Verify() error = %v, want ErrInvalidToken", err)
			}
		})
	}
	for _, raw := range []string{"", "not-a-jwt", "a.b.c", "eyJhbGciOiJub25lIn0.e30."} {
		if _, err := verifier.Verify(context.Background(), raw); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("Verify(%q) error = %v, want ErrInvalidToken", raw, err)
		}
	}
}

func TestVerifierRefreshesOnceForRotatedKidAndCachesWinner(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	first := testRSAKey(t)
	second := testRSAKey(t)
	verifier, fixture, issuer := verifierFixture(t, now, map[string]any{
		"keys": []any{rsaJWK("first", &first.PublicKey)},
	})
	claim := claims(now)
	claim.Issuer = issuer

	if _, err := verifier.Verify(context.Background(), signedToken(t, jwt.SigningMethodRS256, "first", claim, first)); err != nil {
		t.Fatal(err)
	}
	fixture.set(map[string]any{"keys": []any{rsaJWK("second", &second.PublicKey)}})
	secondToken := signedToken(t, jwt.SigningMethodRS256, "second", claim, second)
	if _, err := verifier.Verify(context.Background(), secondToken); err != nil {
		t.Fatalf("rotated token error = %v", err)
	}
	if _, err := verifier.Verify(context.Background(), secondToken); err != nil {
		t.Fatalf("cached rotated token error = %v", err)
	}
	if fixture.count() != 2 {
		t.Fatalf("JWKS requests = %d, want initial plus unknown-kid refresh", fixture.count())
	}
}

func TestVerifierReportsUnavailableJWKSWithoutLeakingResponse(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	key := testRSAKey(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		http.Error(response, "private-upstream-detail", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	issuer := server.URL + "/auth/v1"
	verifier, err := New(Config{Issuer: issuer, Audience: testAudience, HTTPClient: server.Client(), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	claim := claims(now)
	claim.Issuer = issuer
	_, err = verifier.Verify(context.Background(), signedToken(t, jwt.SigningMethodRS256, "unavailable", claim, key))
	if !errors.Is(err, ErrKeysUnavailable) || err.Error() != ErrKeysUnavailable.Error() {
		t.Fatalf("Verify() error = %q", err)
	}
}

func TestNewRejectsUnsafeConfiguration(t *testing.T) {
	tests := []Config{
		{},
		{Issuer: "http://project.supabase.co/auth/v1", Audience: testAudience},
		{Issuer: "https://project.supabase.co/not-auth", Audience: testAudience},
		{Issuer: "https://project.supabase.co/auth/v1", Audience: ""},
		{Issuer: "https://project.supabase.co/auth/v1", Audience: testAudience, JWKSURL: "https://attacker.example/jwks"},
		{Issuer: "https://project.supabase.co/auth/v1", Audience: testAudience, CacheTTL: 11 * time.Minute},
		{Issuer: "https://" + strings.Repeat("a", 500) + ".example/auth/v1", Audience: testAudience},
	}
	for index, config := range tests {
		if _, err := New(config); err == nil {
			t.Errorf("case %d: New() error = nil", index)
		}
	}
}

func withIssuer(claim jwt.RegisteredClaims, issuer string) jwt.RegisteredClaims {
	claim.Issuer = issuer
	return claim
}

func withAudience(claim jwt.RegisteredClaims, audience string) jwt.RegisteredClaims {
	claim.Audience = jwt.ClaimStrings{audience}
	return claim
}

func withExpiry(claim jwt.RegisteredClaims, expiry time.Time) jwt.RegisteredClaims {
	claim.ExpiresAt = jwt.NewNumericDate(expiry)
	return claim
}

func withNotBefore(claim jwt.RegisteredClaims, notBefore time.Time) jwt.RegisteredClaims {
	claim.NotBefore = jwt.NewNumericDate(notBefore)
	return claim
}

func withSubject(claim jwt.RegisteredClaims, subject string) jwt.RegisteredClaims {
	claim.Subject = subject
	return claim
}
