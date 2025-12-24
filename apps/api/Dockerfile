# -------------------------
# DEV (hot reload)
# -------------------------
FROM golang:1.25.5-alpine AS dev
WORKDIR /app
RUN apk add --no-cache git ca-certificates curl
RUN go install github.com/air-verse/air@latest
CMD ["air", "-c", ".air.toml"]

# -------------------------
# MIGRATIONS (goose)
# -------------------------
FROM golang:1.25.5-alpine AS migrate
WORKDIR /app
RUN apk add --no-cache ca-certificates
RUN go install github.com/pressly/goose/v3/cmd/goose@latest
ENTRYPOINT ["goose"]

# -------------------------
# BUILDER (prod build)
# -------------------------
FROM golang:1.25.5-alpine AS builder
WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG API_MAIN=./cmd/api
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
RUN go build -trimpath -ldflags="-s -w" -o /out/api ${API_MAIN}

# -------------------------
# PROD RUNTIME (small, non-root)
# -------------------------
FROM alpine:3.20 AS prod
RUN apk add --no-cache ca-certificates tzdata \
  && addgroup -S app \
  && adduser -S app -G app
WORKDIR /app
COPY --from=builder /out/api /app/api
USER app
EXPOSE 8080
ENTRYPOINT ["/app/api"]
