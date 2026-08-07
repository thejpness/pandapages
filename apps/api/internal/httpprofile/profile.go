// Package httpprofile owns the explicit reader-profile context boundary.
// It resolves profiles only within an already-authorized account context and
// never authenticates a bearer or chooses a profile implicitly.
package httpprofile

import (
	"net/http"
	"strings"

	"pandapages/api/internal/httpbearer"
)

const profileHeader = "X-PP-Profile-ID"

// Store resolves profile ownership through an account-scoped lookup. A false
// result intentionally covers both a missing profile and one owned elsewhere.
type Store interface {
	ProfileExists(accountID, profileID string) (bool, error)
}

// Context is the complete explicit context for a profile-scoped request.
type Context struct {
	PrincipalID string
	AccountID   string
	Role        string
	ProfileID   string
}

type Resolver struct {
	store Store
}

func New(store Store) *Resolver {
	if store == nil {
		panic("profile store is required")
	}
	return &Resolver{store: store}
}

// RequireProfile resolves exactly one X-PP-Profile-ID for an account context
// that has already passed bearer authentication and membership authorization.
func (r *Resolver) RequireProfile(response http.ResponseWriter, request *http.Request, account httpbearer.AccountContext) (Context, bool) {
	profileID, state := selectedProfile(request.Header.Values(profileHeader))
	switch state {
	case profileMissing:
		writeError(response, http.StatusBadRequest, "profile_required")
		return Context{}, false
	case profileMalformed:
		writeError(response, http.StatusBadRequest, "invalid_profile")
		return Context{}, false
	}

	exists, err := r.store.ProfileExists(account.AccountID, profileID)
	if err != nil {
		writeError(response, http.StatusServiceUnavailable, "profile_unavailable")
		return Context{}, false
	}
	if !exists {
		writeError(response, http.StatusForbidden, "profile_forbidden")
		return Context{}, false
	}

	return Context{
		PrincipalID: account.PrincipalID,
		AccountID:   account.AccountID,
		Role:        account.Role,
		ProfileID:   profileID,
	}, true
}

type profileState uint8

const (
	profileValid profileState = iota
	profileMissing
	profileMalformed
)

func selectedProfile(headers []string) (string, profileState) {
	if len(headers) == 0 {
		return "", profileMissing
	}
	if len(headers) != 1 {
		return "", profileMalformed
	}
	raw := headers[0]
	if raw == "" || raw != strings.TrimSpace(raw) {
		return "", profileMalformed
	}
	profileID, ok := httpbearer.CanonicalUUID(raw)
	if !ok {
		return "", profileMalformed
	}
	return profileID, profileValid
}

func writeError(response http.ResponseWriter, status int, code string) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	_, _ = response.Write([]byte("{\"error\":\"" + code + "\"}\n"))
}
