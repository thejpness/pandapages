# Panda Pages admin ingress

Production admin API routes are protected by two Traefik middlewares: an IP
allowlist and server-side X-PP-Admin-Key injection. The browser must never
receive or send the admin key.

## Required environment

PP_ADMIN_IPS is required in production and must contain CIDR values. A single
IPv4 address uses /32; multiple CIDRs are comma-separated. Keep real IP
addresses and all secrets in the deployment environment, never in Git.

If the administrator's public IP changes, Traefik will return 403 Forbidden
until the allowlist is updated. A bare-text, 9-byte Forbidden response means
Traefik rejected the request before it reached Go. A JSON 401 response means
the request passed ingress and reached application authentication.

The administrator's home broadband uses a dynamic public IP. The current /32
is therefore a temporary operational correction, and the same Traefik 403 may
recur whenever the ISP assigns a different address. Do not add automatic
public-IP detection or automatic allowlist rewriting; update and validate the
deployment environment manually.

## Longer-term boundary

Keep the current IP allowlist and server-side admin-key injection until a
replacement has been implemented and validated.

- For one administrator, put the admin interface behind Tailscale or WireGuard
  and restrict ingress to that private network.
- For future multi-user access, complete the Kratos integration or use an
  identity-aware proxy.

Do not weaken or remove the existing boundary during that migration.

## Safe update procedure

On the production host:

1. Edit the permission-restricted deployment .env and set
   PP_ADMIN_IPS=<ADMIN_PUBLIC_IP>/32 (or the required comma-separated CIDRs).
2. Confirm the file still has mode 0600 and contains no duplicate
   PP_ADMIN_IPS assignment.
3. Validate without printing the rendered configuration or secrets:
   docker compose config --quiet.
4. Recreate only the API service so Traefik reloads its container labels:
   docker compose up -d --no-deps --force-recreate api.
5. Verify an authorised session receives 200, a signed-out session receives
   the application's 401, and a source outside the allowlist still receives
   403.

Do not commit .env, administrator IP addresses, admin keys, session secrets,
cookies, or tokens.
