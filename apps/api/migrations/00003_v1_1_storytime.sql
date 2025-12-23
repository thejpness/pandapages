-- +goose Up

-- A) Publishing pointers (explicit version selection)
ALTER TABLE stories
  ADD COLUMN IF NOT EXISTS published_version_id UUID NULL,
  ADD COLUMN IF NOT EXISTS draft_version_id UUID NULL;

-- +goose StatementBegin
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'fk_stories_published_version'
  ) THEN
    ALTER TABLE stories
      ADD CONSTRAINT fk_stories_published_version
      FOREIGN KEY (published_version_id) REFERENCES story_versions(id)
      ON DELETE SET NULL;
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'fk_stories_draft_version'
  ) THEN
    ALTER TABLE stories
      ADD CONSTRAINT fk_stories_draft_version
      FOREIGN KEY (draft_version_id) REFERENCES story_versions(id)
      ON DELETE SET NULL;
  END IF;
END
$$;
-- +goose StatementEnd

CREATE INDEX IF NOT EXISTS idx_stories_published_version_id ON stories(published_version_id);
CREATE INDEX IF NOT EXISTS idx_stories_draft_version_id ON stories(draft_version_id);

-- B) Universal metadata for classics/import provenance
ALTER TABLE stories
  ADD COLUMN IF NOT EXISTS language TEXT NOT NULL DEFAULT 'en-GB',
  ADD COLUMN IF NOT EXISTS rights JSONB NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS source JSONB NOT NULL DEFAULT '{}'::jsonb;

-- C) Child profiles (dynamic age/interests)
CREATE TABLE IF NOT EXISTS child_profiles (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name          TEXT,
  age_months    INT NOT NULL,
  interests     JSONB NOT NULL DEFAULT '[]'::jsonb,
  sensitivities JSONB NOT NULL DEFAULT '[]'::jsonb,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- D) Prompt profiles (how to write)
CREATE TABLE IF NOT EXISTS prompt_profiles (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name           TEXT NOT NULL,
  rules          JSONB NOT NULL DEFAULT '{}'::jsonb,
  schema_version INT NOT NULL DEFAULT 1,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- E) Generation jobs (audit + retries + status)

-- +goose StatementBegin
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'generation_status') THEN
    CREATE TYPE generation_status AS ENUM ('queued','running','succeeded','failed');
  END IF;
END
$$;
-- +goose StatementEnd

CREATE TABLE IF NOT EXISTS generation_jobs (
  id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  status           generation_status NOT NULL DEFAULT 'queued',

  story_id          UUID NULL REFERENCES stories(id) ON DELETE SET NULL,
  story_version_id  UUID NULL REFERENCES story_versions(id) ON DELETE SET NULL,

  child_profile_id  UUID NULL REFERENCES child_profiles(id) ON DELETE SET NULL,
  prompt_profile_id UUID NULL REFERENCES prompt_profiles(id) ON DELETE SET NULL,

  theme             TEXT,
  request_payload   JSONB NOT NULL DEFAULT '{}'::jsonb,
  response_payload  JSONB NOT NULL DEFAULT '{}'::jsonb,

  model             TEXT,
  prompt_version    TEXT,
  error             TEXT,

  tokens_in         INT,
  tokens_out        INT,

  created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_gen_jobs_status_created ON generation_jobs(status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_gen_jobs_story_created ON generation_jobs(story_id, created_at DESC);

-- +goose Down

DROP INDEX IF EXISTS idx_gen_jobs_story_created;
DROP INDEX IF EXISTS idx_gen_jobs_status_created;
DROP TABLE IF EXISTS generation_jobs;

-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_type WHERE typname = 'generation_status') THEN
    DROP TYPE generation_status;
  END IF;
END
$$;
-- +goose StatementEnd

DROP TABLE IF EXISTS prompt_profiles;
DROP TABLE IF EXISTS child_profiles;

ALTER TABLE stories
  DROP COLUMN IF EXISTS source,
  DROP COLUMN IF EXISTS rights,
  DROP COLUMN IF EXISTS language;

DROP INDEX IF EXISTS idx_stories_draft_version_id;
DROP INDEX IF EXISTS idx_stories_published_version_id;

ALTER TABLE stories DROP CONSTRAINT IF EXISTS fk_stories_draft_version;
ALTER TABLE stories DROP CONSTRAINT IF EXISTS fk_stories_published_version;

ALTER TABLE stories
  DROP COLUMN IF EXISTS draft_version_id,
  DROP COLUMN IF EXISTS published_version_id;
