# Local `/api/v1/auth/status` 502 runbook

Use this runbook for an intermittent local HTTP 502 from
`/api/v1/auth/status`. Collect evidence before restarting or recreating any
container: a restart removes the process and routing state needed to explain
the failure.

The endpoint is served directly by the Panda Pages Go API. The local path is
browser → Traefik → `api:8080`; Kratos is not in that path. A Traefik 502 means
the proxy could not complete its upstream exchange. A Panda Pages JSON 503
means the request reached Go but the application or readiness dependency was
unavailable.

The development API container starts Air before Air compiles and starts its Go
child process. It can also temporarily lose the listener during a hot reload.
The container health check makes those gaps observable, but cannot eliminate
every request interruption during a reload.

This local diagnostic procedure does not authorise a database-role or
production change. Before any orchestrator, monitor, or deployment gate starts
using `/readyz`, follow the current
[forward readiness role-grant rollout](../operations/postgresql-least-privilege-roles.md#forward-readyz-role-grant-rollout).

## Capture evidence

Record the time and run the following before changing container state:

```bash
date -Is

curl --noproxy '*' --max-time 5 -i \
  -H 'Host: pandapages.localhost' \
  http://127.0.0.1/api/v1/auth/status

curl --noproxy '*' --max-time 5 -i \
  http://127.0.0.1:8081/api/v1/auth/status

curl --noproxy '*' --max-time 5 -i \
  -H 'Host: pandapages.localhost' \
  http://127.0.0.1/healthz

curl --noproxy '*' --max-time 5 -i \
  http://127.0.0.1:8081/healthz

curl --noproxy '*' --max-time 5 -i \
  -H 'Host: pandapages.localhost' \
  http://127.0.0.1/readyz

curl --noproxy '*' --max-time 5 -i \
  http://127.0.0.1:8081/readyz

docker compose -f docker-compose.dev.yml ps -a

docker inspect --format \
  '{{.Name}} state={{.State.Status}} running={{.State.Running}} restart={{.RestartCount}} health={{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' \
  pandapages-api-1 pandapages-traefik-1 pandapages-postgres-1

docker inspect --format \
  '{{range .State.Health.Log}}start={{.Start}} end={{.End}} exit={{.ExitCode}} output={{printf "%q" .Output}}{{println}}{{end}}' \
  pandapages-api-1

docker compose -f docker-compose.dev.yml logs \
  --no-color --since 10m --tail 200 \
  api traefik postgres migrate
```

The API-only health history retains recent failed-probe timestamps even when
the aggregate health status has already recovered to `healthy`.

Record the exact response time, status, response `X-Request-ID`, and whether
the failure was through Traefik, direct, or both. Preserve the command output
before any restart. Logs and responses must be reviewed for secrets before
they are shared; never share cookies, passcodes, database URLs, admin keys,
Authorization headers, or other credentials.

Local Traefik writes INFO-level error/general logs plus JSON access records for
HTTP 4xx and 5xx responses. Its access log allowlists only timestamps, routing,
upstream, method, safe path, status, retry, and timing fields; client addresses,
request/response headers, and query parameters are excluded. API completion
and panic logs carry `X-Request-ID`, which can correlate a browser failure with
an API record. A
proxy-generated 502 might have only a Traefik record because no Go handler ran.

## Interpret the results

- Proxied 502 but direct API 200: investigate the Traefik router, Docker DNS,
  selected network, and API network attachment.
- Both paths fail while Air is compiling or rebuilding: this is a development
  listener gap. Preserve its start/end times and build output.
- Both paths fail and the API is stopped or restarting: inspect API startup
  validation, migration, and database logs before restarting it.
- Direct API returns Panda Pages JSON 503: the request reached the application;
  this is a database/session/readiness failure, not a proxy 502.
- Direct API returns 500: the request reached an application failure or
  recovered panic. Correlate the request ID with the API stack log.
- `/healthz` is 200 while `/readyz` is 503: the Go process and HTTP listener are
  alive, but PostgreSQL or the expected successful schema version is not ready.
- `/healthz` also fails directly: the Go child process is not listening, even
  if the Air container itself is running.

Do not use production hostnames or commands for this local investigation. Do
not destroy or reset the local database to make readiness pass.
