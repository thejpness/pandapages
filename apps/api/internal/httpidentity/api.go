// Package httpidentity exposes the isolated Supabase-bearer identity
// foundation. It never reads or falls back to Panda Pages' legacy cookies.
package httpidentity

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"pandapages/api/internal/appidentity"
	"pandapages/api/internal/supabaseauth"
)

const maxBearerBytes = 16 * 1024

type TokenVerifier interface {
	Verify(context.Context, string) (appidentity.ExternalIdentity, error)
}

type IdentityStore interface {
	EnsureIdentity(context.Context, appidentity.ExternalIdentity) (appidentity.Snapshot, bool, error)
	Identity(context.Context, appidentity.ExternalIdentity) (appidentity.Snapshot, error)
}

type API struct {
	verifier TokenVerifier
	store    IdentityStore
}

func New(verifier TokenVerifier, store IdentityStore) http.Handler {
	api := &API{verifier: verifier, store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/onboard", api.onboard)
	mux.HandleFunc("GET /api/auth/me", api.me)
	return mux
}

type errorResponse struct {
	Error string `json:"error"`
}

type membershipResponse struct {
	AccountID   string `json:"accountId"`
	AccountName string `json:"accountName"`
	Role        string `json:"role"`
}

type identityResponse struct {
	Authenticated bool                 `json:"authenticated"`
	Created       *bool                `json:"created,omitempty"`
	Principal     principalResponse    `json:"principal"`
	Memberships   []membershipResponse `json:"memberships"`
}

type principalResponse struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

func (api *API) onboard(response http.ResponseWriter, request *http.Request) {
	one := make([]byte, 1)
	read, err := request.Body.Read(one)
	if read != 0 || err != nil && !errors.Is(err, io.EOF) {
		writeError(response, http.StatusBadRequest, "invalid_request")
		return
	}
	identity, ok := api.authenticate(response, request)
	if !ok {
		return
	}
	snapshot, created, err := api.store.EnsureIdentity(request.Context(), identity)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeIdentity(response, snapshot, &created)
}

func (api *API) me(response http.ResponseWriter, request *http.Request) {
	identity, ok := api.authenticate(response, request)
	if !ok {
		return
	}
	snapshot, err := api.store.Identity(request.Context(), identity)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeIdentity(response, snapshot, nil)
}

func (api *API) authenticate(response http.ResponseWriter, request *http.Request) (appidentity.ExternalIdentity, bool) {
	token, state := bearerToken(request.Header.Values("Authorization"))
	switch state {
	case bearerMissing:
		writeBearerError(response, "bearer_required")
		return appidentity.ExternalIdentity{}, false
	case bearerMalformed:
		writeBearerError(response, "invalid_bearer")
		return appidentity.ExternalIdentity{}, false
	}

	identity, err := api.verifier.Verify(request.Context(), token)
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
	if len(header) <= len("Bearer ") || len(header) > maxBearerBytes+len("Bearer ") || !strings.HasPrefix(header, "Bearer ") {
		return "", bearerMalformed
	}
	token := strings.TrimPrefix(header, "Bearer ")
	if token == "" || strings.ContainsAny(token, " \t\r\n,") {
		return "", bearerMalformed
	}
	return token, bearerValid
}

func writeBearerError(response http.ResponseWriter, code string) {
	response.Header().Set("WWW-Authenticate", `Bearer realm="panda-pages"`)
	writeError(response, http.StatusUnauthorized, code)
}

func writeStoreError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, appidentity.ErrNotFound):
		writeError(response, http.StatusConflict, "onboarding_required")
	case errors.Is(err, appidentity.ErrInvalidState):
		writeError(response, http.StatusConflict, "identity_state_invalid")
	default:
		writeError(response, http.StatusServiceUnavailable, "identity_unavailable")
	}
}

func writeError(response http.ResponseWriter, status int, code string) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(errorResponse{Error: code})
}

func writeIdentity(response http.ResponseWriter, snapshot appidentity.Snapshot, created *bool) {
	memberships := make([]membershipResponse, 0, len(snapshot.Memberships))
	for _, membership := range snapshot.Memberships {
		memberships = append(memberships, membershipResponse{
			AccountID:   membership.AccountID,
			AccountName: membership.AccountName,
			Role:        membership.Role,
		})
	}
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(response).Encode(identityResponse{
		Authenticated: true,
		Created:       created,
		Principal: principalResponse{
			ID:          snapshot.PrincipalID,
			DisplayName: snapshot.DisplayName,
		},
		Memberships: memberships,
	})
}
