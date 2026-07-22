# Current authentication contract

Status: current repository behavior as of 21 July 2026.

This document describes the authentication mechanism implemented today. The
future identity direction is recorded separately in
[ADR 0001](decisions/0001-supabase-auth-boundary-and-browser-session-model.md).
No Supabase or Kratos component participates in this contract.

## Endpoints

All three endpoints return JSON and set `Cache-Control: no-store`. Error
responses use the normal Panda Pages `{ "error": { "code", "message" } }` shape.

### `POST /api/v1/auth/unlock`

- Authentication requirement: none.
- Request: `{ "passcode": "<six decimal digits>" }`.
  Its body is limited to 1 MiB and rejects unknown fields and trailing JSON.
- Success: HTTP 200 with `{ "ok": true }`.
- Cookie behavior: issues `pp_session` for the selected default account and
  expires both legacy cookies, `pp_unlocked` and `pp_aid`.
- Relevant failures: 400 for malformed or unexpected JSON, 401 for an invalid
  passcode, 413 for an oversized body, 405 for another method, and 500 if the
  account cannot be initialised or the signed session cannot be issued.
- Frontend caller: the `/unlock` view. On success it confirms the frontend
  `unlocked` state and follows an allowlisted internal `next` destination.

Unlock selects the oldest existing account, creating an account named
`Default` when none exists. It does not select or identify an individual user.

### `GET /api/v1/auth/status`

- Authentication requirement: optional `pp_session` cookie.
- Request: no body.
- Valid session success: HTTP 200 with `{ "unlocked": true }`.
- Missing, malformed, tampered, expired, or unknown-account session: HTTP 200
  with `{ "unlocked": false }`, while expiring current and legacy cookies.
- Relevant failures: 503 with code `session_unavailable` when account-existence
  validation cannot be completed, and 405 for another method.
- Frontend caller: the shared auth-state cache used by the router.

A false result is definitive signed-out state. A connectivity error, malformed
response, 502, 503, or other 5xx result is availability failure and must not be
presented as logout.

### `POST /api/v1/auth/logout`

- Authentication requirement: none. The operation is deliberately idempotent
  and does not require a working database.
- Request: no body.
- Success: HTTP 200 with `{ "ok": true }`.
- Cookie behavior: expires `pp_session`, `pp_unlocked`, and `pp_aid` with the
  matching cookie path/security attributes.
- Relevant failure: 405 for another method.
- Frontend callers: the Library and Admin lock actions.

Logout ends this browser's stored session. It does not revoke a copied token
on the server.

## Session contract

`pp_session` contains canonical, versioned claims for an account UUID, issue
time, and expiry time, protected with HMAC-SHA256. The fixed duration is 30
days. Verification authenticates the signature and validates the shape,
version, account UUID, timing, and lifetime before the API queries whether the
account still exists.

The cookie is:

- host-only (no `Domain` attribute);
- `HttpOnly`;
- scoped to `Path=/`;
- `SameSite=Strict`;
- `Secure` in production;
- non-secure in the current local HTTP development configuration.

Issuing or clearing a session also expires the obsolete unsigned
`pp_unlocked` and `pp_aid` cookies. Those cookies are never accepted as
authentication.

The session is stateless. There is no server-side session record, immediate
revocation list, idle expiry, refresh flow, or key identifier/overlap scheme.
Rotating the single session secret invalidates every existing session. The
account claim identifies an application account, not an individual person.

## Frontend state model

The frontend auth-state cache has four states:

- `unknown`: no current decision, including initial application/bootstrap
  state and explicit cache invalidation;
- `unlocked`: a recent status response confirmed a valid session;
- `locked`: a recent status response definitively confirmed no valid session;
- `unavailable`: status could not be decided because the request failed, the
  server returned an error, or the response was malformed.

Protected routes verify this state. Definitive unauthenticated state goes to
`/unlock` with an allowlisted internal `next` path. Connectivity failures,
malformed responses, and 5xx conditions go to `/session-unavailable`, where a
fresh status request can be retried. A server or dependency failure must never
be presented as signed-out state.

Confirmed unlock and logout transitions update the cache explicitly so an
older in-flight status request cannot overwrite the newer state.

## Availability boundary

`/healthz` and `/readyz` are unauthenticated operational endpoints, not
authentication mechanisms. `/healthz` proves only that the Go process and HTTP
listener can answer. `/readyz` additionally proves PostgreSQL connectivity and
the expected successful Goose schema state. A readiness 503 is an availability
signal and is not evidence that a browser session is signed out.

Any future readiness consumer for the version-15 API must follow the separately
authorised
[coordinated account-integrity rollout](../operations/postgresql-least-privilege-roles.md#migration-00015-coordinated-forward-rollout)
before `/readyz` can be used as a gate.
The earlier
[PR #30 readiness role-grant procedure](../operations/postgresql-least-privilege-roles.md#forward-readyz-role-grant-rollout)
is historical evidence only and must not be reused as the current procedure.

## Admin boundary

The current admin API has three defense layers:

1. the signed application session and account-existence check;
2. the production Traefik source-IP allowlist;
3. an admin key injected by Traefik into `X-PP-Admin-Key` after the ingress
   boundary. The browser neither receives nor sends this key.

Panda Pages does not yet have identity-linked application memberships or roles.
The current admin boundary is transitional and must not be weakened during a
future authentication migration.

## Limitations

The current shared passcode is not an individual identity and has no signup,
email verification, password recovery/change, social login, session refresh,
server-side revocation, account management, account selection, profile
selection, or profile PIN. Normal unlock still reaches one implicit legacy
account. Within that account, application behavior uses the explicit
`profiles.is_default` marker; transactional resolution creates or repairs one
deterministically without treating a profile display name as identity. See the
[current account ownership model](account-ownership-model.md).

## Transition rule

This mechanism is a temporary bridge. It may receive proportionate safety,
reliability, and observability fixes, but it must not be expanded into a
bespoke identity platform. Until a controlled future cutover, it remains the
sole active authentication mechanism. A request must not be accepted through
both this cookie and an external identity token.
