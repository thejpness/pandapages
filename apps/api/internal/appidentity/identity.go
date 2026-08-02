// Package appidentity defines Panda Pages' provider-neutral adult identity
// and account-membership domain. External provider claims never become
// application account or profile identifiers.
package appidentity

import "errors"

const (
	ProviderSupabase   = "supabase"
	InitialDisplayName = "Panda Pages Adult"
	InitialAccountName = "My Panda Pages"
	RoleOwner          = "owner"
	RoleAdult          = "adult"
)

var (
	ErrNotFound     = errors.New("application identity not found")
	ErrInvalidState = errors.New("application identity state is invalid")
)

type ExternalIdentity struct {
	Provider string
	Issuer   string
	Subject  string
}

type Membership struct {
	AccountID   string
	AccountName string
	Role        string
}

type Snapshot struct {
	PrincipalID string
	DisplayName string
	Memberships []Membership
}
