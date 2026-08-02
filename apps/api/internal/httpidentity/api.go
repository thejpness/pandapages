// Package httpidentity exposes the isolated Supabase-bearer identity
// foundation. It never reads or falls back to Panda Pages' legacy cookies.
package httpidentity

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"pandapages/api/internal/appidentity"
	"pandapages/api/internal/httpbearer"
)

type IdentityStore interface {
	EnsureIdentity(context.Context, appidentity.ExternalIdentity) (appidentity.Snapshot, bool, error)
	Identity(context.Context, appidentity.ExternalIdentity) (appidentity.Snapshot, error)
}

type API struct {
	auth  *httpbearer.Authenticator
	store IdentityStore
}

func New(verifier httpbearer.TokenVerifier, store IdentityStore) http.Handler {
	api := &API{
		auth:  httpbearer.New(verifier, store),
		store: store,
	}
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
	identity, ok := api.auth.Authenticate(response, request)
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
	identity, ok := api.auth.Authenticate(response, request)
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
