-- +goose Up
BEGIN;

-- 1) accounts
CREATE TABLE IF NOT EXISTS accounts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL DEFAULT 'Default',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

-- ensure at least one account exists
INSERT INTO accounts (name)
SELECT 'Default'
WHERE NOT EXISTS (SELECT 1 FROM accounts);

-- helper: pick the oldest account as the "default"
-- (we'll mirror this in Go too)
-- 2) add account_id to key tables and backfill

ALTER TABLE profiles
  ADD COLUMN IF NOT EXISTS account_id uuid;

UPDATE profiles
SET account_id = (SELECT id FROM accounts ORDER BY created_at ASC LIMIT 1)
WHERE account_id IS NULL;

ALTER TABLE profiles
  ALTER COLUMN account_id SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_profiles_account ON profiles(account_id);
CREATE UNIQUE INDEX IF NOT EXISTS ux_profiles_account_name ON profiles(account_id, name);

ALTER TABLE child_profiles
  ADD COLUMN IF NOT EXISTS account_id uuid;

UPDATE child_profiles
SET account_id = (SELECT id FROM accounts ORDER BY created_at ASC LIMIT 1)
WHERE account_id IS NULL;

ALTER TABLE child_profiles
  ALTER COLUMN account_id SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_child_profiles_account ON child_profiles(account_id);

ALTER TABLE prompt_profiles
  ADD COLUMN IF NOT EXISTS account_id uuid;

UPDATE prompt_profiles
SET account_id = (SELECT id FROM accounts ORDER BY created_at ASC LIMIT 1)
WHERE account_id IS NULL;

ALTER TABLE prompt_profiles
  ALTER COLUMN account_id SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_prompt_profiles_account ON prompt_profiles(account_id);

ALTER TABLE stories
  ADD COLUMN IF NOT EXISTS account_id uuid;

UPDATE stories
SET account_id = (SELECT id FROM accounts ORDER BY created_at ASC LIMIT 1)
WHERE account_id IS NULL;

ALTER TABLE stories
  ALTER COLUMN account_id SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_stories_account ON stories(account_id);

COMMIT;

-- +goose Down
BEGIN;

-- Down is intentionally destructive; only use in dev.
ALTER TABLE stories DROP COLUMN IF EXISTS account_id;
ALTER TABLE prompt_profiles DROP COLUMN IF EXISTS account_id;
ALTER TABLE child_profiles DROP COLUMN IF EXISTS account_id;
ALTER TABLE profiles DROP COLUMN IF EXISTS account_id;

DROP TABLE IF EXISTS accounts;

COMMIT;
