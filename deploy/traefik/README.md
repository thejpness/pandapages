# Panda Pages admin ingress

Production Story Studio and administrator API access use layered application
and ingress controls.

The browser authenticates with Supabase. Panda Pages then resolves the
authenticated principal's account membership, and administrator routes require
owner authority.

Traefik provides an additional independent server-side boundary by injecting
`X-PP-Admin-Key` into requests routed to `/api/v1/admin/`.

The browser must never receive, persist, or send the admin key.

## Production boundary

Administrator access requires:

1. a valid Supabase bearer session;
2. an explicit Panda Pages account membership;
3. owner authority for that account;
4. the independent `PP_ADMIN_KEY` injected by Traefik.

The former production source-IP allowlist has been retired. Administrator
access is therefore not coupled to a particular broadband address or network.

The Go API remains authoritative for authentication and owner authorisation.
The Traefik admin key is an additional server-side ingress credential, not a
replacement for Supabase authentication or Panda Pages membership checks.

## Required environment

Production requires:

`PP_ADMIN_KEY=<independent-random-secret>`

Keep the real value only in the permission-restricted deployment environment.
It must not be committed to Git, exposed through Vite, or sent by browser code.

Production Compose fails closed when `PP_ADMIN_KEY` is missing or empty.

## Routing

The dedicated administrator router handles:

`/api/v1/admin/`

Traefik injects the configured admin key before forwarding the request to the
Go API.

Ordinary public API traffic does not receive this injected credential.

## Deployment validation

After updating production configuration:

1. confirm the deployment `.env` remains permission restricted;
2. run `docker compose config --quiet`;
3. recreate the API/web stack through the normal deployment procedure;
4. sign in through Supabase;
5. verify an owner can access Story Studio;
6. verify a non-owner authenticated account cannot use owner-only routes;
7. verify a signed-out request cannot use protected administrator routes.

Google and other external identity providers are configured through Supabase.
Their provider secrets do not belong in Panda Pages browser configuration.

Do not commit `.env`, admin keys, OAuth provider secrets, bearer tokens, or
other production credentials.
