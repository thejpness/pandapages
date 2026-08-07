# Current authentication contract

Panda Pages uses Supabase only to authenticate an external adult identity.
Panda Pages owns principals, external identities, accounts, memberships, roles,
profiles, and application data.

Every protected application request supplies `Authorization: Bearer <token>`
and `X-PP-Account-ID: <account UUID>`. The bearer resolves to a Panda Pages
principal; the explicitly named account is authorised only when it is one of
that principal's current memberships. The server never chooses an account or a
profile on the caller's behalf.

`POST /api/auth/onboard` creates a principal, account, and owner membership for
a newly authenticated identity. It creates no profile. `GET /api/auth/me`
returns the principal and current memberships without provisioning state.

The frontend restores the official Supabase PKCE session, resolves identity,
and sends the bearer plus its explicit account selection with every protected
request. A selected account stored locally is only a UI preference; membership
is re-authorised by the API every time. Profile context is intentionally a
future explicit concern.

Admin routes require all of: a valid bearer, an explicit member account, an
`owner` membership role, and the ingress-provided `X-PP-Admin-Key`.

Protected application errors use the finite bearer/account contract:
`bearer_required`, `invalid_bearer`, `authentication_unavailable`,
`account_required`, `invalid_account`, `onboarding_required`,
`identity_state_invalid`, `account_forbidden`, and `identity_unavailable`.
