package httpauth

import (
	"errors"
	"fmt"
	"net/http"

	"pandapages/api/internal/session"
)

var ErrInvalidSession = errors.New("invalid session")

type AccountStore interface {
	AccountExists(accountID string) (bool, error)
}

type Authenticator struct {
	sessions *session.Manager
	accounts AccountStore
}

func New(sessions *session.Manager, accounts AccountStore) *Authenticator {
	if sessions == nil {
		panic("session manager is required")
	}
	if accounts == nil {
		panic("account store is required")
	}
	return &Authenticator{sessions: sessions, accounts: accounts}
}

func (a *Authenticator) Authenticate(r *http.Request) (string, error) {
	claims, err := a.sessions.FromRequest(r)
	if err != nil {
		return "", ErrInvalidSession
	}

	exists, err := a.accounts.AccountExists(claims.AccountID)
	if err != nil {
		return "", fmt.Errorf("validate session account: %w", err)
	}
	if !exists {
		return "", ErrInvalidSession
	}

	return claims.AccountID, nil
}
