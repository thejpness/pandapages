# Supabase identity foundation

Status: active replacement foundation; application-route cutover pending.

Supabase authenticates external adults. Panda Pages continues to own internal
principals, accounts, memberships, roles, reader profiles, stories, progress,
preferences, and every application-authorisation decision. Supabase Database,
PostgREST, Storage, RLS, provider metadata, email addresses, and JWT role claims
are not Panda Pages authority.

## Provider-neutral identity model

Migration `00016_identity_foundation.sql` adds exactly three tables and does not
alter or populate profiles:

- `principals` is Panda Pages' generated internal adult identifier and display
  name. Its UUID never comes from an external provider.
- `external_identities` links one principal to a bounded non-empty `provider`,
  `issuer`, and `subject`. The tuple is unique. Its principal foreign key is
  restrictive, so deleting a linked principal requires a deliberate application
  lifecycle rather than an accidental cascade.
- `account_memberships` links a principal to an existing Panda Pages account.
  `(principal_id, account_id)` is the primary key, both foreign keys are
  restrictive, and `role` is exactly `owner` or `adult`.

There are no child identities, profile memberships, invitations, sharing
records, organisation abstractions, or granular permission tables. External
subjects are neither account IDs nor principal IDs. No existing account,
principal, membership, or profile is manufactured as a speculative legacy
backfill; development data may be recreated at the clean cutover.

## Verified bearer boundary

The Go API accepts Supabase bearer tokens only on:

- `POST /api/auth/onboard`;
- `GET /api/auth/me`.

The verifier accepts only `RS256` and `ES256`, requires a `kid`, validates the
configured issuer and audience, expiry, optional not-before time, signature,
and non-empty subject, and retrieves keys from the same-origin configured or
issuer-derived JWKS endpoint. Keys have a bounded cache. An unknown `kid`
causes a bounded refresh for rotation. `HS256`, `none`, unsupported algorithms,
wrong claims, missing subjects, and unavailable keys fail with finite responses
that contain no token, raw claim, key, SQL, or verifier detail.

Only the verified configured provider, verified issuer, and verified subject
enter identity resolution. Email and provider metadata may be presentation
data in future, but neither is stored or trusted by this foundation.

## Onboarding transaction

For a first verified `(provider, issuer, subject)`, one PostgreSQL transaction:

1. takes a transaction-scoped advisory lock derived from the complete tuple;
2. rechecks the unique external identity;
3. creates one internal principal;
4. creates one external identity link;
5. creates one generated account named `My Panda Pages`;
6. creates one `owner` membership;
7. returns the principal and complete membership list.

Database uniqueness and the transaction, not a process mutex, are the
correctness boundary. A concurrent out-of-band winner is resolved through the
unique tuple after the losing transaction rolls back. Failure leaves no partial
principal, identity, account, or membership. Repeat onboarding is idempotent.
The new account is not inferred from existing rows or selected for general
application use. Onboarding creates no reader profile, does not query for the
oldest account, and has no dependency on a profile named `Default`.

`GET /api/auth/me` is read-only. A verified but not-onboarded identity receives
the finite `onboarding_required` state rather than implicit provisioning.

## Browser flow and route separation

The official `@supabase/supabase-js` client owns browser session persistence,
refresh, PKCE verifier state, callback exchange, and logout. Panda Pages adds no
custom token storage. `/account/login` begins Google OAuth with PKCE and an
internal `/auth/callback` redirect. The callback exchanges the code, calls the
bearer-only onboarding endpoint, and displays `/account` identity and membership
state. Bearer requests omit cookies and cannot call `/api/v1/*` through this
client.

The existing `/api/v1/*` handlers remain cookie-only for this one implementation stage.
They ignore bearer headers. Conversely, `/api/auth/*` ignores `pp_session` and
rejects a cookie-only request. No endpoint tries one mechanism and falls back
to the other.

This separation is temporary, not a commitment to two authentication systems.
The next authentication PR will move application routes to membership-backed
bearer authorisation and delete the shared passcode, signed Panda Pages cookie,
legacy cookie clearing, oldest-account resolution, and implicit exact-name
`Default` profile resolution. Current development sessions and data have no
compatibility promise.

## Configuration and CSP

Root Compose requires:

- `SUPABASE_URL`: exact HTTPS project origin, with no path or trailing slash;
- `SUPABASE_PUBLISHABLE_KEY`: browser-safe publishable key, never a service key;
- `SUPABASE_JWT_AUDIENCE`: expected access-token audience.

Compose derives `PP_SUPABASE_ISSUER`, `PP_SUPABASE_JWKS_URL`,
`VITE_SUPABASE_URL`, and `VITE_SUPABASE_PUBLISHABLE_KEY`. Direct API processes
set `PP_SUPABASE_ISSUER`, `PP_SUPABASE_AUDIENCE`, and optionally the issuer-
derived `PP_SUPABASE_JWKS_URL`. Production web builds require the same exact
origin for `connect-src`; the build rejects a missing or non-HTTPS origin.
`script-src`, `style-src`, `object-src`, `base-uri`, and `frame-ancestors` remain
unchanged and no wildcard is accepted.

Local development needs a developer-owned Supabase project only to exercise a
real external login. Unit and CI tests instead use local RSA/EC keys, an HTTPS
JWKS fixture, intercepted browser Auth responses, and non-routable `.invalid`
origins. CI never requires or contacts a live Supabase project. Do not put a
service-role key, provider secret, access token, or refresh token in `.env`,
Compose, GitHub Actions, logs, or Panda Pages PostgreSQL.

## Deliberately deferred

This foundation does not cut over Reader, Library, Journey, Story Studio, or
admin routes; select an account; create/select/manage a profile; add PINs,
invitations, sharing, or child users; claim existing development accounts; or
deploy provider configuration. Membership roles become application authority
only when the next PR moves the application route family and removes the legacy
authentication path.
