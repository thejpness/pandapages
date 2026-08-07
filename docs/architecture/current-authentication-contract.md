# Current authentication contract

Panda Pages uses Supabase only to authenticate an external adult identity.
Panda Pages owns principals, external identities, accounts, memberships, roles,
profiles, and application data.

Every protected application request supplies `Authorization: Bearer <token>`
and `X-PP-Account-ID: <account UUID>`. The bearer resolves to a Panda Pages
principal; the explicitly named account is authorised only when it is one of
that principal's current memberships. Account context is never inferred.

Reader identity is a separate, explicit context. A profile-scoped endpoint
also requires `X-PP-Profile-ID: <profile UUID>` after account authorization.
Panda Pages resolves that profile only with the already-selected account:
`WHERE account_id = selected_account AND id = selected_profile`. Profile
context is never inferred, and a profile from another account is externally
indistinguishable from an unknown profile.

`POST /api/auth/onboard` creates a principal, account, and owner membership for
a newly authenticated identity. It creates no profile. `GET /api/auth/me`
returns the principal and current memberships without provisioning state. Zero
profiles is a valid account state. Authentication never creates a profile.

`GET /api/v1/profiles`, `POST /api/v1/profiles`,
`PATCH /api/v1/profiles/{profile ID}`, and
`DELETE /api/v1/profiles/{profile ID}` are account-scoped reader-profile
management. They require bearer authentication and an explicit authorised
account, but not `X-PP-Profile-ID`. Both adult and owner memberships may manage
profiles. Reads are deterministic; mutations scope SQL directly by account and
profile ID, so a missing profile and a profile from another account both fail
closed as `profile_forbidden`. A final profile may be deleted without deleting
account or story data.

The frontend restores the official Supabase PKCE session, resolves identity,
and sends the bearer plus its explicit account selection with every protected
request. A selected account stored locally is only a UI preference; membership
is re-authorised by the API every time. A selected reader profile must likewise
be sent explicitly only to profile-scoped endpoints and is rechecked against
the selected account on every request. The frontend stores only a selected
profile ID as UX state, reconciles it against the server list after account
selection, and clears it when it is stale or its account changes. It may choose
the sole returned profile as a UI convenience; multiple profiles require a
choice. The backend never selects or creates a Default profile.

Admin routes require all of: a valid bearer, an explicit member account, an
`owner` membership role, and the ingress-provided `X-PP-Admin-Key`.

Protected application errors use the finite bearer/account contract:
`bearer_required`, `invalid_bearer`, `authentication_unavailable`,
`account_required`, `invalid_account`, `onboarding_required`,
`identity_state_invalid`, `account_forbidden`, and `identity_unavailable`.
For profile-scoped endpoints the additional finite errors are
`profile_required`, `invalid_profile`, `profile_forbidden`, and
`profile_unavailable`.
