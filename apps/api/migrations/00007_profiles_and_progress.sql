-- +goose Up

-- 1) Reader profiles (start with one default profile; later map to Kratos identity)
CREATE TABLE IF NOT EXISTS profiles (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name       TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 2) Add profile_id to reading_progress
ALTER TABLE reading_progress
  ADD COLUMN IF NOT EXISTS profile_id UUID NULL;

-- 3) Create a default profile and backfill existing rows
-- +goose StatementBegin
DO $$
DECLARE
  default_profile_id UUID;
BEGIN
  SELECT id INTO default_profile_id
  FROM profiles
  WHERE name = 'Default';

  IF default_profile_id IS NULL THEN
    INSERT INTO profiles (name) VALUES ('Default') RETURNING id INTO default_profile_id;
  END IF;

  UPDATE reading_progress
  SET profile_id = default_profile_id
  WHERE profile_id IS NULL;
END
$$;
-- +goose StatementEnd

-- 4) Make profile_id NOT NULL going forward
ALTER TABLE reading_progress
  ALTER COLUMN profile_id SET NOT NULL;

-- 5) Replace PK (story_id) -> (profile_id, story_id)
ALTER TABLE reading_progress
  DROP CONSTRAINT IF EXISTS reading_progress_pkey;

ALTER TABLE reading_progress
  ADD CONSTRAINT reading_progress_pkey PRIMARY KEY (profile_id, story_id);

-- 6) FK to profiles
-- +goose StatementBegin
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'reading_progress_profile_id_fkey') THEN
    ALTER TABLE reading_progress
      ADD CONSTRAINT reading_progress_profile_id_fkey
      FOREIGN KEY (profile_id) REFERENCES profiles(id)
      ON DELETE CASCADE;
  END IF;
END
$$;
-- +goose StatementEnd

CREATE INDEX IF NOT EXISTS idx_progress_profile_updated ON reading_progress(profile_id, updated_at DESC);

-- +goose Down

DROP INDEX IF EXISTS idx_progress_profile_updated;

ALTER TABLE reading_progress DROP CONSTRAINT IF EXISTS reading_progress_profile_id_fkey;

ALTER TABLE reading_progress DROP CONSTRAINT IF EXISTS reading_progress_pkey;
ALTER TABLE reading_progress ADD CONSTRAINT reading_progress_pkey PRIMARY KEY (story_id);

ALTER TABLE reading_progress
  ALTER COLUMN profile_id DROP NOT NULL;

ALTER TABLE reading_progress
  DROP COLUMN IF EXISTS profile_id;

DROP TABLE IF EXISTS profiles;
