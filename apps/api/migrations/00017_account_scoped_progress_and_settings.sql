-- +goose Up
BEGIN;

-- Progress becomes account-scoped. For duplicate historical rows with the
-- same account/story, the greatest updated_at wins; ties use the greatest
-- profile_id UUID. Values are never merged.
CREATE TABLE reading_progress_account_scope (
  account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  story_id uuid NOT NULL,
  story_version_id uuid NOT NULL,
  locator jsonb NOT NULL,
  percent double precision NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (account_id, story_id),
  CONSTRAINT reading_progress_account_scope_story_fkey
    FOREIGN KEY (story_id, account_id) REFERENCES stories(id, account_id) ON DELETE CASCADE,
  CONSTRAINT reading_progress_account_scope_version_fkey
    FOREIGN KEY (story_version_id, story_id) REFERENCES story_versions(id, story_id)
);

INSERT INTO reading_progress_account_scope (account_id, story_id, story_version_id, locator, percent, updated_at)
SELECT account_id, story_id, story_version_id, locator, percent, updated_at
FROM (
  SELECT progress.*, row_number() OVER (
    PARTITION BY account_id, story_id
    ORDER BY updated_at DESC, profile_id DESC
  ) AS winner
  FROM reading_progress AS progress
) AS ranked
WHERE winner = 1;

DROP TABLE reading_progress;
ALTER TABLE reading_progress_account_scope RENAME TO reading_progress;
CREATE INDEX reading_progress_account_updated_idx ON reading_progress (account_id, updated_at DESC);

-- Settings become account-scoped. For duplicate historical rows in one
-- account, the greatest updated_at wins; ties use the greatest profile_id UUID.
-- Selected child/prompt identifiers are copied as one whole row, never merged.
CREATE TABLE account_settings (
  account_id uuid PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
  active_child_profile_id uuid,
  active_prompt_profile_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT account_settings_child_account_fkey
    FOREIGN KEY (active_child_profile_id, account_id) REFERENCES child_profiles(id, account_id)
    ON DELETE SET NULL (active_child_profile_id),
  CONSTRAINT account_settings_prompt_account_fkey
    FOREIGN KEY (active_prompt_profile_id, account_id) REFERENCES prompt_profiles(id, account_id)
    ON DELETE SET NULL (active_prompt_profile_id)
);

INSERT INTO account_settings (account_id, active_child_profile_id, active_prompt_profile_id, created_at, updated_at)
SELECT account_id, active_child_profile_id, active_prompt_profile_id, created_at, updated_at
FROM (
  SELECT settings.*, row_number() OVER (
    PARTITION BY account_id
    ORDER BY updated_at DESC, profile_id DESC
  ) AS winner
  FROM profile_settings AS settings
) AS ranked
WHERE winner = 1;

DROP TABLE profile_settings;

COMMIT;

-- +goose Down
-- This conversion intentionally discards profile scope. A truthful rollback
-- cannot recreate profile ownership without inventing profile data.
DO $$ BEGIN
  RAISE EXCEPTION 'account-scoped progress/settings migration is irreversible';
END $$;
