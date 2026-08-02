// Package supabaseauth verifies Supabase-issued adult identity tokens without
// assigning Panda Pages accounts, roles, profiles, or other application state.
package supabaseauth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
	"pandapages/api/internal/appidentity"
)

const (
	defaultCacheTTL           = 5 * time.Minute
	maxCacheTTL               = 10 * time.Minute
	maxJWKSBytes              = 1 << 20
	unknownKeyRefreshCooldown = 30 * time.Second
)

var (
	ErrInvalidToken    = errors.New("invalid bearer token")
	ErrKeysUnavailable = errors.New("signing keys unavailable")
)

type Config struct {
	Provider   string
	Issuer     string
	Audience   string
	JWKSURL    string
	CacheTTL   time.Duration
	HTTPClient *http.Client
	Now        func() time.Time
}

type Verifier struct {
	provider string
	issuer   string
	audience string
	jwksURL  string
	cacheTTL time.Duration
	client   *http.Client
	now      func() time.Time

	mu                 sync.Mutex
	keys               map[string]any
	expiresAt          time.Time
	lastUnknownRefresh time.Time
}

func New(config Config) (*Verifier, error) {
	provider := strings.TrimSpace(config.Provider)
	if provider == "" {
		provider = appidentity.ProviderSupabase
	}
	if provider != appidentity.ProviderSupabase {
		return nil, fmt.Errorf("unsupported external identity provider")
	}
	issuer, err := validatedHTTPSURL(config.Issuer, true)
	if err != nil {
		return nil, fmt.Errorf("issuer: %w", err)
	}
	audience := strings.TrimSpace(config.Audience)
	if audience == "" || audience != config.Audience || len(audience) > 128 {
		return nil, fmt.Errorf("audience must be a non-empty trimmed value")
	}

	jwksRaw := strings.TrimSpace(config.JWKSURL)
	if jwksRaw == "" {
		jwksRaw = strings.TrimSuffix(issuer.String(), "/") + "/.well-known/jwks.json"
	}
	jwksURL, err := validatedHTTPSURL(jwksRaw, false)
	if err != nil {
		return nil, fmt.Errorf("JWKS URL: %w", err)
	}
	if !sameOrigin(issuer, jwksURL) {
		return nil, fmt.Errorf("JWKS URL must use the issuer origin")
	}

	cacheTTL := config.CacheTTL
	if cacheTTL == 0 {
		cacheTTL = defaultCacheTTL
	}
	if cacheTTL <= 0 || cacheTTL > maxCacheTTL {
		return nil, fmt.Errorf("JWKS cache TTL must be greater than zero and at most %s", maxCacheTTL)
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: 5 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}

	return &Verifier{
		provider: provider,
		issuer:   issuer.String(),
		audience: audience,
		jwksURL:  jwksURL.String(),
		cacheTTL: cacheTTL,
		client:   client,
		now:      now,
		keys:     make(map[string]any),
	}, nil
}

func validatedHTTPSURL(raw string, issuer bool) (*url.URL, error) {
	if raw == "" || strings.TrimSpace(raw) != raw {
		return nil, fmt.Errorf("must be a non-empty absolute HTTPS URL")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("must be an absolute HTTPS URL without credentials, query, or fragment")
	}
	if issuer && len(raw) > 512 {
		return nil, fmt.Errorf("must be at most 512 bytes")
	}
	if issuer && strings.TrimSuffix(parsed.Path, "/") != "/auth/v1" {
		return nil, fmt.Errorf("must end in /auth/v1")
	}
	return parsed, nil
}

func sameOrigin(left, right *url.URL) bool {
	return left.Scheme == right.Scheme && strings.EqualFold(left.Host, right.Host)
}

func (v *Verifier) Verify(ctx context.Context, raw string) (appidentity.ExternalIdentity, error) {
	if raw == "" || len(raw) > 16*1024 || !utf8.ValidString(raw) {
		return appidentity.ExternalIdentity{}, ErrInvalidToken
	}
	claims := &jwt.RegisteredClaims{}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg(), jwt.SigningMethodES256.Alg()}),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(v.now),
	)
	token, err := parser.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		algorithm := token.Method.Alg()
		if algorithm != jwt.SigningMethodRS256.Alg() && algorithm != jwt.SigningMethodES256.Alg() {
			return nil, ErrInvalidToken
		}
		kid, ok := token.Header["kid"].(string)
		if !ok || kid == "" || len(kid) > 256 {
			return nil, ErrInvalidToken
		}
		return v.key(ctx, kid, algorithm)
	})
	if err != nil || token == nil || !token.Valid {
		if errors.Is(err, ErrKeysUnavailable) {
			return appidentity.ExternalIdentity{}, ErrKeysUnavailable
		}
		return appidentity.ExternalIdentity{}, ErrInvalidToken
	}
	subject := claims.Subject
	if subject == "" || strings.TrimSpace(subject) != subject || len(subject) > 255 || !utf8.ValidString(subject) {
		return appidentity.ExternalIdentity{}, ErrInvalidToken
	}
	return appidentity.ExternalIdentity{
		Provider: v.provider,
		Issuer:   v.issuer,
		Subject:  subject,
	}, nil
}

func keyID(kid, algorithm string) string { return algorithm + "\x00" + kid }

func (v *Verifier) key(ctx context.Context, kid, algorithm string) (any, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	now := v.now()
	if now.Before(v.expiresAt) {
		if key, ok := v.keys[keyID(kid, algorithm)]; ok {
			return key, nil
		}
		// An unknown kid may signal rotation, so refresh once immediately. A
		// cooldown prevents attacker-chosen kids from turning verification into
		// an unbounded fetch loop while the ordinary TTL remains active.
		if !v.lastUnknownRefresh.IsZero() && now.Sub(v.lastUnknownRefresh) < unknownKeyRefreshCooldown {
			return nil, ErrInvalidToken
		}
		v.lastUnknownRefresh = now
	}
	if err := v.refresh(ctx, now); err != nil {
		return nil, err
	}
	key, ok := v.keys[keyID(kid, algorithm)]
	if !ok {
		return nil, ErrInvalidToken
	}
	return key, nil
}

type jwksDocument struct {
	Keys []json.RawMessage `json:"keys"`
}

type jwkCommon struct {
	Kid    string   `json:"kid"`
	Kty    string   `json:"kty"`
	Alg    string   `json:"alg"`
	Use    string   `json:"use"`
	KeyOps []string `json:"key_ops"`
}

func (v *Verifier) refresh(ctx context.Context, now time.Time) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return ErrKeysUnavailable
	}
	request.Header.Set("Accept", "application/json")
	response, err := v.client.Do(request)
	if err != nil {
		return ErrKeysUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ErrKeysUnavailable
	}

	decoder := json.NewDecoder(io.LimitReader(response.Body, maxJWKSBytes+1))
	decoder.DisallowUnknownFields()
	var document jwksDocument
	if err := decoder.Decode(&document); err != nil || len(document.Keys) == 0 || len(document.Keys) > 32 {
		return ErrKeysUnavailable
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return ErrKeysUnavailable
	}

	keys := make(map[string]any, len(document.Keys))
	seenKids := make(map[string]struct{}, len(document.Keys))
	for _, raw := range document.Keys {
		var common jwkCommon
		if err := json.Unmarshal(raw, &common); err != nil || !validJWKMetadata(common) {
			return ErrKeysUnavailable
		}
		if _, duplicate := seenKids[common.Kid]; duplicate {
			return ErrKeysUnavailable
		}
		seenKids[common.Kid] = struct{}{}
		key, algorithm, err := parseJWK(raw, common)
		if err != nil {
			return ErrKeysUnavailable
		}
		index := keyID(common.Kid, algorithm)
		if _, duplicate := keys[index]; duplicate {
			return ErrKeysUnavailable
		}
		keys[index] = key
	}
	v.keys = keys
	v.expiresAt = now.Add(v.cacheTTL)
	return nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}

func validJWKMetadata(key jwkCommon) bool {
	if key.Kid == "" || len(key.Kid) > 256 || key.Use != "" && key.Use != "sig" {
		return false
	}
	for _, operation := range key.KeyOps {
		if operation != "verify" {
			return false
		}
	}
	return true
}

func parseJWK(raw json.RawMessage, common jwkCommon) (any, string, error) {
	switch common.Kty {
	case "RSA":
		if common.Alg != "" && common.Alg != jwt.SigningMethodRS256.Alg() {
			return nil, "", ErrKeysUnavailable
		}
		var key struct {
			N string `json:"n"`
			E string `json:"e"`
		}
		if err := json.Unmarshal(raw, &key); err != nil {
			return nil, "", err
		}
		n, err := decodeBigInt(key.N)
		if err != nil || n.Sign() <= 0 || n.BitLen() < 2048 {
			return nil, "", ErrKeysUnavailable
		}
		eValue, err := base64.RawURLEncoding.DecodeString(key.E)
		if err != nil || len(eValue) == 0 || len(eValue) > 4 {
			return nil, "", ErrKeysUnavailable
		}
		e := 0
		for _, value := range eValue {
			e = e<<8 | int(value)
		}
		if e < 3 || e%2 == 0 {
			return nil, "", ErrKeysUnavailable
		}
		return &rsa.PublicKey{N: n, E: e}, jwt.SigningMethodRS256.Alg(), nil
	case "EC":
		if common.Alg != "" && common.Alg != jwt.SigningMethodES256.Alg() {
			return nil, "", ErrKeysUnavailable
		}
		var key struct {
			Curve string `json:"crv"`
			X     string `json:"x"`
			Y     string `json:"y"`
		}
		if err := json.Unmarshal(raw, &key); err != nil || key.Curve != "P-256" {
			return nil, "", ErrKeysUnavailable
		}
		xBytes, errX := base64.RawURLEncoding.DecodeString(key.X)
		yBytes, errY := base64.RawURLEncoding.DecodeString(key.Y)
		if errX != nil || errY != nil || len(xBytes) != 32 || len(yBytes) != 32 {
			return nil, "", ErrKeysUnavailable
		}
		x, y := new(big.Int).SetBytes(xBytes), new(big.Int).SetBytes(yBytes)
		curve := elliptic.P256()
		if !curve.IsOnCurve(x, y) {
			return nil, "", ErrKeysUnavailable
		}
		return &ecdsa.PublicKey{Curve: curve, X: x, Y: y}, jwt.SigningMethodES256.Alg(), nil
	default:
		return nil, "", ErrKeysUnavailable
	}
}

func decodeBigInt(encoded string) (*big.Int, error) {
	value, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(value) == 0 || len(value) > 1024 {
		return nil, ErrKeysUnavailable
	}
	return new(big.Int).SetBytes(value), nil
}
