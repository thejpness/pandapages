# ADR 0001: Supabase Auth boundary and browser-session model

- Status: Accepted; staged implementation in progress
- Date: 21 July 2026

The decision text below records the state and consequences accepted on its date.
Implementation is now staged: strict CSP and safe rendering are active, and the
provider-neutral principal/identity/membership model, RS256/ES256 bearer
verifier, `/api/auth/*` foundation, and official browser PKCE flow are active.
`/api/v1/*` remains temporarily cookie-only until the next PR performs the
membership-backed bearer cutover and deletes the legacy mechanism. See the
[current implementation contract](../supabase-identity-foundation.md).

## Context

Panda Pages currently unlocks one application account with a shared six-digit
passcode. The Go API issues an HMAC-SHA256-signed, account-scoped session in an
HTTP-only cookie, validates the account still exists, and supports explicit
browser logout. This is a sufficient temporary bridge for the current private
application, but it is not an individual identity platform.

The historical Ory Kratos work was local-only scaffolding. It is absent from
the current repository's browser, Go API, and Compose request paths, and has no
materially reusable application integration. This ADR does not assert live
production deployment state.

Future product requirements include email/password authentication, email
verification, account recovery, logout lifecycle, and Google and Facebook
sign-in. Panda Pages already owns its application accounts, profiles, stories,
reading progress, and preferences. A child's reader profile is an application
persona within an account, not an external authentication identity.

## Decision

- Supabase Auth is the selected external identity provider.
- Supabase will be used for authentication only.
- Panda Pages retains its Vue/PWA frontend, Go API, and application PostgreSQL
  database.
- Panda Pages continues to own accounts, roles and permissions, profiles,
  optional profile PINs, stories, reading progress, preferences, and admin
  authorisation.
- The frontend uses the standard browser Supabase session with PKCE for the
  isolated identity flow. It sends short-lived access tokens only to
  `/api/auth/*`; the Go API verifies them before resolving Panda Pages state.
- Supabase Database, PostgREST, Storage, and Row Level Security are outside the
  selected architecture.
- Kratos will not be revived.
- During staged implementation, every route has exactly one authentication
  mechanism. `/api/auth/*` is bearer-only; `/api/v1/*` remains temporarily
  cookie-only until the next PR deletes the legacy mechanism.
- Children's profiles will not become Supabase users.
- Strict CSP and safe story rendering are active prerequisites for the limited
  browser session now implemented. The sole remote CSP source is the configured
  Supabase Auth origin in `connect-src`.
- Provider-neutral identity mapping is active. Neither email matching nor the
  legacy oldest-account fallback grants membership; new onboarding creates its
  own account and no profile.

## Alternatives considered

### Complete Ory Kratos

This would preserve self-hosting but would require Panda Pages to operate an
identity database, self-service flows or UI, courier/email delivery, recovery
and verification, social providers, routing, cookie topology, monitoring, and
application session integration. The historical scaffold does not remove that
work. The operational and integration cost is not justified for this project.

### Extend the current passcode system

Adding individual credentials, verification, recovery, social login, session
management, linking, and deletion would turn Panda Pages into a bespoke
identity provider. The current mechanism should remain a small, temporary
bridge rather than grow into that platform.

### Go BFF with HTTP-only Supabase refresh-token cookies

A backend-for-frontend could keep refresh tokens out of browser-accessible
storage. It would also recreate substantial custom callback, token storage,
refresh, rotation, concurrency, logout, and CSRF machinery in Go. That works
against the implementation simplification sought by selecting Supabase.

### Standard browser Supabase session

The standard browser session with PKCE is selected. It uses Supabase's intended
browser lifecycle and keeps the custom Panda Pages boundary focused on token
validation, identity-to-account mapping, and application authorisation.

Browser-stored sessions are not risk-free. A successful script injection can
access browser-held credentials. Before cutover, Panda Pages must reduce that
risk with strict CSP, limited and reviewed third-party scripts, careful output
handling, short-lived access tokens, rotating refresh tokens, and server-side
authorisation for every application operation.

## Consequences

- XSS prevention becomes more important than it is with the current
  HTTP-only session cookie. CSP and a review of script and injection boundaries
  are cutover prerequisites.
- The Go API now implements exact issuer/audience/temporal validation, an
  RS256/ES256 allowlist, cached JWKS retrieval, bounded unknown-key refresh,
  and rotation handling for the isolated identity route family.
- Panda Pages now has provider-neutral identity mapping, application
  memberships/roles, and race-safe new-account provisioning. It does not infer
  authority from email or attach new identities to legacy accounts.
- Production email requires separately configured custom SMTP. Google and
  Facebook require separately governed provider applications, credentials,
  and allowlisted redirects.
- Application data remains in Panda Pages PostgreSQL. Panda Pages remains
  responsible for export, retention, account deletion, provider-deletion
  reconciliation, and profile lifecycle.
- Migration must be staged with one authoritative authentication mechanism per
  route/environment, observable failures, tested rollback, and retention of
  the legacy mechanism until the cutover has been proven.
- Supabase service credentials must never enter the browser. Future integration
  details require their own reviewed change.

## Deferred work

Remaining work is the membership-backed `/api/v1/*` cutover and deletion of
the shared passcode, signed cookies, oldest-account resolution, implicit
`Default` profile resolution, and temporary identity-entry labelling. Account
and profile selection, PINs, invitations, provider operations, email flows,
production configuration, data migration, and deployment remain separate work.
