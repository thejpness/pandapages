# syntax=docker/dockerfile:1.7

ARG GO_IMAGE=golang:1.26.5-alpine3.24@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2
ARG ALPINE_IMAGE=alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

# -------------------------
# DEV (hot reload)
# -------------------------
FROM ${GO_IMAGE} AS dev
ARG AIR_VERSION=v1.65.3
WORKDIR /app
RUN apk add --no-cache git ca-certificates curl \
  && go install github.com/air-verse/air@${AIR_VERSION}
CMD ["air", "-c", ".air.toml"]

# -------------------------
# MIGRATIONS (goose)
# -------------------------
FROM ${GO_IMAGE} AS goose-builder
ARG GOOSE_VERSION=v3.27.2
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
RUN go install \
  -trimpath \
  -ldflags="-s -w" \
  -tags="no_clickhouse no_libsql no_mssql no_mysql no_sqlite3 no_vertica no_ydb" \
  github.com/pressly/goose/v3/cmd/goose@${GOOSE_VERSION}

# -------------------------
# BUILDER (prod build)
# -------------------------
FROM ${GO_IMAGE} AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
ARG API_MAIN=./cmd/api

ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
RUN --mount=type=cache,target=/go/pkg/mod \
  --mount=type=cache,target=/root/.cache/go-build \
  go build -trimpath -ldflags="-s -w" -o /out/api ${API_MAIN}

# -------------------------
# RUNTIME BASE (small, non-root)
# -------------------------
FROM ${ALPINE_IMAGE} AS runtime-base
RUN apk add --no-cache ca-certificates tzdata \
  && addgroup -S app \
  && adduser -S app -G app
WORKDIR /app

FROM runtime-base AS migrate
COPY --from=goose-builder /go/bin/goose /usr/local/bin/goose
USER app
ENTRYPOINT ["goose", "-env=none"]

FROM runtime-base AS prod
COPY --from=builder /out/api /app/api
USER app
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/app/api"]
