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

Panda Pages has two frontend modes, both layered over the same adult bearer
and account membership boundary. Parent mode is the default and exposes
account, profile, PIN, settings, and permitted admin controls. Child mode is a
reader-focused UI state for one explicitly selected profile; it hides those
parent controls and permits reader experiences. It does not grant an account
role or any additional server authority.

A profile may optionally have a four-digit PIN. `PUT` and `DELETE`
`/api/v1/profiles/{profile ID}/pin` set or remove that local reader-mode gate;
`POST /api/v1/profiles/{profile ID}/pin/verify` verifies it before the
frontend enters child mode. Each endpoint is account-scoped, requires the
adult bearer and selected account membership, and never needs a profile
header. The database stores only a salted bcrypt encoding and profile-local
failed-attempt/lock state. Five consecutive failures lock verification for 15
minutes; success, setting, and removal reset that state. Wrong PINs return
`pin_invalid`; locked profiles return `pin_rate_limited`; missing and
cross-account profiles remain indistinguishable as `profile_forbidden`.

The PIN is not authentication, a bearer token, a membership credential, or a
replacement for Supabase. It is never returned, logged, placed in a URL, or
persisted by the frontend. Child-mode unlock is memory-only, so a refresh
returns to parent mode and requires a configured PIN again. No custom child
cookie, token, or server session exists.

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

Account-scoped application data remains the library/catalogue, account
settings, and reader-profile management. Reading progress and Continue/recent
reading are profile-scoped: `GET` and `PUT /api/v1/progress/{slug}` and
`GET /api/v1/continue` require all three request headers, including
`X-PP-Profile-ID`. A profile switch changes only reader progress state; it
does not change adult authorization or account settings. Catalogue responses
do not infer a profile to embed progress.

Migration 00018 makes `reading_progress` keyed by
`(account_id, profile_id, story_id)` and enforces the profile/account tuple
with a composite foreign key. Account-scoped beta progress from migration
00017 has no truthful reader owner, so migration 00018 explicitly discards
only those progress rows. It does not create or select a profile, and it does
not alter accounts, profiles, stories, memberships, or settings. Its Down
migration fails explicitly because multiple profile rows cannot truthfully be
collapsed back to account scope.

Migration 00019 adds optional profile-local PIN hash and throttling state. It
does not affect progress, settings, accounts, profiles, or memberships beyond
the new security columns. Its Down migration fails explicitly because silently
removing configured reader gates would not be a truthful rollback.

Admin routes require all of: a valid bearer, an explicit member account, an
`owner` membership role, and the ingress-provided `X-PP-Admin-Key`.

Protected application errors use the finite bearer/account contract:
`bearer_required`, `invalid_bearer`, `authentication_unavailable`,
`account_required`, `invalid_account`, `onboarding_required`,
`identity_state_invalid`, `account_forbidden`, and `identity_unavailable`.
For profile-scoped endpoints the additional finite errors are
`profile_required`, `invalid_profile`, `profile_forbidden`, and
`profile_unavailable`. PIN endpoints additionally use `invalid_pin`,
`pin_invalid`, and `pin_rate_limited`.
