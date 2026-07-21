# ADR 0001: Supabase Auth boundary and browser-session model

- Status: Accepted for future implementation
- Date: 21 July 2026

This decision introduces no Supabase runtime capability. The current
shared-passcode and signed-cookie mechanism remains the sole active Panda Pages
authentication mechanism until a separately reviewed, controlled cutover.

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

- Supabase Auth is the selected future external identity provider.
- Supabase will be used for authentication only.
- Panda Pages retains its Vue/PWA frontend, Go API, and application PostgreSQL
  database.
- Panda Pages continues to own accounts, roles and permissions, profiles,
  optional profile PINs, stories, reading progress, preferences, and admin
  authorisation.
- The future frontend integration will use the standard browser Supabase
  session with the Proof Key for Code Exchange (PKCE) flow.
- The frontend will eventually send short-lived Supabase access tokens to the
  Go API. The Go API will validate them and perform all application
  authorisation.
- Supabase Database, PostgREST, Storage, and Row Level Security are outside the
  selected architecture.
- Kratos will not be revived.
- The current passcode and signed-cookie session remains the sole active
  mechanism until controlled cutover. A request must never be authorised
  ambiguously by both that cookie and a Supabase token.
- Children's profiles will not become Supabase users.
- Strict Content Security Policy and the associated XSS-risk reduction are
  prerequisites for browser-token cutover, but are deferred from this
  readiness change.
- Provider-neutral identity mapping and an explicit existing-account claim
  process must precede cutover. Neither an email match nor the current
  oldest-account fallback is sufficient proof of ownership.

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
- The Go API will need exact issuer and audience validation, an algorithm
  allowlist, temporal-claim checks, cached JWKS retrieval, unknown-key refresh,
  and safe key-rotation behavior. This ADR does not implement them.
- Panda Pages needs provider-neutral external-identity mapping, application
  membership/role resolution, race-safe provisioning, and an explicit claim
  procedure for existing accounts before accepting external tokens.
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

This decision does not add Supabase packages, configuration, projects, local
containers, callbacks, JWT/JWKS validation, bearer authentication, identity
tables, account claiming, membership roles, profile selection or PINs, CSP,
social providers, email flows, or current-auth removal.
