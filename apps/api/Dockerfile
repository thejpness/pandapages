FROM golang:1.25.5-alpine AS dev
WORKDIR /app
RUN apk add --no-cache git ca-certificates curl
RUN go install github.com/air-verse/air@latest
CMD ["air", "-c", ".air.toml"]

# ---- migrations runner (no GHCR) ----
FROM golang:1.25.5-alpine AS migrate
WORKDIR /app
RUN apk add --no-cache ca-certificates
RUN go install github.com/pressly/goose/v3/cmd/goose@latest
ENTRYPOINT ["goose"]

# (optional) runtime stage later when you do prod build
