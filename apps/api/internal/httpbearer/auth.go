// Package httpbearer owns Panda Pages' reusable bearer authentication and
// explicit account-membership selection boundary. It never reads cookies.
package httpbearer

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"pandapages/api/internal/appidentity"
	"pandapages/api/internal/supabaseauth"
)

const (
	maxBearerBytes = 16 * 1024
	accountHeader  = "X-PP-Account-ID"
)

type TokenVerifier interface {
	Verify(context.Context, string) (appidentity.ExternalIdentity, error)
}

type IdentityStore interface {
	Identity(context.Context, appidentity.ExternalIdentity) (appidentity.Snapshot, error)
}

type AccountContext struct {
	PrincipalID string
	AccountID   string
	Role        string
}

type Authenticator struct {
	verifier TokenVerifier
	store    IdentityStore
}

func New(verifier TokenVerifier, store IdentityStore) *Authenticator {
	if verifier == nil {
		panic("bearer token verifier is required")
	}
	if store == nil {
		panic("identity store is required")
	}
	return &Authenticator{verifier: verifier, store: store}
}

// Authenticate verifies exactly one bearer token. It never reads or falls
// back to Panda Pages' transitional cookie session.
func (a *Authenticator) Authenticate(response http.ResponseWriter, request *http.Request) (appidentity.ExternalIdentity, bool) {
	token, state := bearerToken(request.Header.Values("Authorization"))
	switch state {
	case bearerMissing:
		writeBearerError(response, "bearer_required")
		return appidentity.ExternalIdentity{}, false
	case bearerMalformed:
		writeBearerError(response, "invalid_bearer")
		return appidentity.ExternalIdentity{}, false
	}

	identity, err := a.verifier.Verify(request.Context(), token)
	if errors.Is(err, supabaseauth.ErrKeysUnavailable) {
		writeError(response, http.StatusServiceUnavailable, "authentication_unavailable")
		return appidentity.ExternalIdentity{}, false
	}
	if err != nil {
		writeBearerError(response, "invalid_bearer")
		return appidentity.ExternalIdentity{}, false
	}
	return identity, true
}

// RequireAccount authenticates the external identity and authorizes the
// account named explicitly by X-PP-Account-ID. Membership ordering is never
// treated as selection state.
func (a *Authenticator) RequireAccount(response http.ResponseWriter, request *http.Request) (AccountContext, bool) {
	identity, ok := a.Authenticate(response, request)
	if !ok {
		return AccountContext{}, false
	}

	selectedAccountID, state := selectedAccount(request.Header.Values(accountHeader))
	switch state {
	case accountMissing:
		writeError(response, http.StatusBadRequest, "account_required")
		return AccountContext{}, false
	case accountMalformed:
		writeError(response, http.StatusBadRequest, "invalid_account")
		return AccountContext{}, false
	}

	snapshot, err := a.store.Identity(request.Context(), identity)
	switch {
	case errors.Is(err, appidentity.ErrNotFound):
		writeError(response, http.StatusConflict, "onboarding_required")
		return AccountContext{}, false
	case errors.Is(err, appidentity.ErrInvalidState):
		writeError(response, http.StatusConflict, "identity_state_invalid")
		return AccountContext{}, false
	case err != nil:
		writeError(response, http.StatusServiceUnavailable, "identity_unavailable")
		return AccountContext{}, false
	}

	membership, membershipStatus := matchingMembership(snapshot, selectedAccountID)
	switch membershipStatus {
	case membershipInvalid:
		writeError(response, http.StatusConflict, "identity_state_invalid")
		return AccountContext{}, false
	case membershipMissing:
		writeError(response, http.StatusForbidden, "account_forbidden")
		return AccountContext{}, false
	}

	return AccountContext{
		PrincipalID: snapshot.PrincipalID,
		AccountID:   selectedAccountID,
		Role:        membership.Role,
	}, true
}

type bearerState uint8

const (
	bearerValid bearerState = iota
	bearerMissing
	bearerMalformed
)

func bearerToken(headers []string) (string, bearerState) {
	if len(headers) == 0 {
		return "", bearerMissing
	}
	if len(headers) != 1 {
		return "", bearerMalformed
	}
	header := headers[0]
	if len(header) <= len("Bearer ") ||
		len(header) > maxBearerBytes+len("Bearer ") ||
		!strings.HasPrefix(header, "Bearer ") {
		return "", bearerMalformed
	}
	token := strings.TrimPrefix(header, "Bearer ")
	if token == "" || strings.ContainsAny(token, " \t\r\n,") {
		return "", bearerMalformed
	}
	return token, bearerValid
}

type accountState uint8

const (
	accountValid accountState = iota
	accountMissing
	accountMalformed
)

func selectedAccount(headers []string) (string, accountState) {
	if len(headers) == 0 {
		return "", accountMissing
	}
	if len(headers) != 1 {
		return "", accountMalformed
	}
	raw := headers[0]
	if raw == "" || raw != strings.TrimSpace(raw) {
		return "", accountMalformed
	}
	canonical, ok := CanonicalUUID(raw)
	if !ok {
		return "", accountMalformed
	}
	return canonical, accountValid
}

// CanonicalUUID validates the UUID spelling used by explicit request context
// headers and returns its lower-case canonical form.
func CanonicalUUID(raw string) (string, bool) {
	if len(raw) != 36 ||
		raw[8] != '-' ||
		raw[13] != '-' ||
		raw[18] != '-' ||
		raw[23] != '-' {
		return "", false
	}
	for index := 0; index < len(raw); index++ {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		character := raw[index]
		if !((character >= '0' && character <= '9') ||
			(character >= 'a' && character <= 'f') ||
			(character >= 'A' && character <= 'F')) {
			return "", false
		}
	}
	return strings.ToLower(raw), true
}

type membershipState uint8

const (
	membershipFound membershipState = iota
	membershipMissing
	membershipInvalid
)

func matchingMembership(snapshot appidentity.Snapshot, selectedAccountID string) (appidentity.Membership, membershipState) {
	if snapshot.PrincipalID == "" || len(snapshot.Memberships) == 0 {
		return appidentity.Membership{}, membershipInvalid
	}

	seen := make(map[string]struct{}, len(snapshot.Memberships))
	var selected appidentity.Membership
	found := false

	for _, membership := range snapshot.Memberships {
		accountID, ok := CanonicalUUID(membership.AccountID)
		if !ok || !appidentity.ValidRole(membership.Role) {
			return appidentity.Membership{}, membershipInvalid
		}
		if _, duplicate := seen[accountID]; duplicate {
			return appidentity.Membership{}, membershipInvalid
		}
		seen[accountID] = struct{}{}
		if accountID == selectedAccountID {
			selected = membership
			found = true
		}
	}

	if !found {
		return appidentity.Membership{}, membershipMissing
	}
	return selected, membershipFound
}

func writeBearerError(response http.ResponseWriter, code string) {
	response.Header().Set("WWW-Authenticate", `Bearer realm="panda-pages"`)
	writeError(response, http.StatusUnauthorized, code)
}

func writeError(response http.ResponseWriter, status int, code string) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(struct {
		Error string `json:"error"`
	}{Error: code})
}
