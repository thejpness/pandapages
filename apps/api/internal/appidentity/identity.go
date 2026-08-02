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

// ValidRole reports whether role is part of the deliberately small adult
// membership vocabulary. Provider claims never define Panda Pages roles.
func ValidRole(role string) bool {
	return role == RoleOwner || role == RoleAdult
}

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
