-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS stories (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  slug         TEXT NOT NULL UNIQUE,
  title        TEXT NOT NULL,
  author       TEXT,
  cover_asset_id UUID,
  is_published BOOLEAN NOT NULL DEFAULT false,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS story_versions (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  story_id      UUID NOT NULL REFERENCES stories(id) ON DELETE CASCADE,
  version       INT NOT NULL,
  frontmatter   JSONB NOT NULL DEFAULT '{}'::jsonb,
  markdown      TEXT NOT NULL,
  rendered_html TEXT NOT NULL,
  content_hash  TEXT NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(story_id, version),
  UNIQUE(story_id, content_hash)
);

CREATE TABLE IF NOT EXISTS assets (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  sha256        TEXT NOT NULL UNIQUE,
  mime_type     TEXT NOT NULL,
  bytes         BIGINT NOT NULL,
  original_name TEXT,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS reading_progress (
  story_id         UUID NOT NULL REFERENCES stories(id) ON DELETE CASCADE,
  story_version_id UUID NOT NULL REFERENCES story_versions(id),
  locator          JSONB NOT NULL,
  percent          REAL NOT NULL DEFAULT 0,
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (story_id)
);

CREATE INDEX IF NOT EXISTS idx_story_versions_story_created ON story_versions(story_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_progress_updated ON reading_progress(updated_at DESC);

-- +goose Down
DROP TABLE IF EXISTS reading_progress;
DROP TABLE IF EXISTS assets;
DROP TABLE IF EXISTS story_versions;
DROP TABLE IF EXISTS stories;
