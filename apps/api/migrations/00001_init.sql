-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- minimal placeholder so goose "up" does something
CREATE TABLE IF NOT EXISTS schema_meta (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS schema_meta;
