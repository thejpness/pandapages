-- +goose Up
BEGIN;

-- Keep the preflight and all derived ownership backfills atomic with respect to
-- application writes. Reads remain available while this migration runs.
LOCK TABLE
  accounts,
  profiles,
  child_profiles,
  prompt_profiles,
  stories,
  story_versions,
  reading_progress,
  profile_settings
IN SHARE ROW EXCLUSIVE MODE;

-- Fail closed on every ownership shape that the new constraints cannot
-- represent. This migration never deletes, reassigns, or merges invalid rows.
-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM profiles AS profile
    LEFT JOIN accounts AS account ON account.id = profile.account_id
    WHERE account.id IS NULL
  ) THEN
    RAISE EXCEPTION 'account ownership preflight failed: orphan profile'
      USING ERRCODE = '23514';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM stories AS story
    LEFT JOIN accounts AS account ON account.id = story.account_id
    WHERE account.id IS NULL
  ) THEN
    RAISE EXCEPTION 'account ownership preflight failed: orphan story'
      USING ERRCODE = '23514';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM child_profiles AS child_profile
    LEFT JOIN accounts AS account ON account.id = child_profile.account_id
    WHERE account.id IS NULL
  ) THEN
    RAISE EXCEPTION 'account ownership preflight failed: orphan child profile'
      USING ERRCODE = '23514';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM prompt_profiles AS prompt_profile
    LEFT JOIN accounts AS account ON account.id = prompt_profile.account_id
    WHERE account.id IS NULL
  ) THEN
    RAISE EXCEPTION 'account ownership preflight failed: orphan prompt profile'
      USING ERRCODE = '23514';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM reading_progress AS progress
    LEFT JOIN profiles AS profile ON profile.id = progress.profile_id
    WHERE profile.id IS NULL
  ) THEN
    RAISE EXCEPTION 'account ownership preflight failed: progress profile is missing'
      USING ERRCODE = '23514';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM reading_progress AS progress
    LEFT JOIN stories AS story ON story.id = progress.story_id
    WHERE story.id IS NULL
  ) THEN
    RAISE EXCEPTION 'account ownership preflight failed: progress story is missing'
      USING ERRCODE = '23514';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM reading_progress AS progress
    LEFT JOIN story_versions AS story_version
      ON story_version.id = progress.story_version_id
    WHERE story_version.id IS NULL
  ) THEN
    RAISE EXCEPTION 'account ownership preflight failed: progress story version is missing'
      USING ERRCODE = '23514';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM reading_progress AS progress
    JOIN profiles AS profile ON profile.id = progress.profile_id
    JOIN stories AS story ON story.id = progress.story_id
    WHERE profile.account_id <> story.account_id
  ) THEN
    RAISE EXCEPTION 'account ownership preflight failed: cross-account progress'
      USING ERRCODE = '23514';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM reading_progress AS progress
    JOIN story_versions AS story_version
      ON story_version.id = progress.story_version_id
    WHERE story_version.story_id <> progress.story_id
  ) THEN
    RAISE EXCEPTION 'account ownership preflight failed: progress version belongs to another story'
      USING ERRCODE = '23514';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM profile_settings AS settings
    LEFT JOIN profiles AS profile ON profile.id = settings.profile_id
    WHERE profile.id IS NULL
  ) THEN
    RAISE EXCEPTION 'account ownership preflight failed: settings profile is missing'
      USING ERRCODE = '23514';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM profile_settings AS settings
    LEFT JOIN child_profiles AS child_profile
      ON child_profile.id = settings.active_child_profile_id
    WHERE settings.active_child_profile_id IS NOT NULL
      AND child_profile.id IS NULL
  ) THEN
    RAISE EXCEPTION 'account ownership preflight failed: settings child profile is missing'
      USING ERRCODE = '23514';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM profile_settings AS settings
    LEFT JOIN prompt_profiles AS prompt_profile
      ON prompt_profile.id = settings.active_prompt_profile_id
    WHERE settings.active_prompt_profile_id IS NOT NULL
      AND prompt_profile.id IS NULL
  ) THEN
    RAISE EXCEPTION 'account ownership preflight failed: settings prompt profile is missing'
      USING ERRCODE = '23514';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM profile_settings AS settings
    JOIN profiles AS profile ON profile.id = settings.profile_id
    JOIN child_profiles AS child_profile
      ON child_profile.id = settings.active_child_profile_id
    WHERE child_profile.account_id <> profile.account_id
  ) THEN
    RAISE EXCEPTION 'account ownership preflight failed: cross-account child settings'
      USING ERRCODE = '23514';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM profile_settings AS settings
    JOIN profiles AS profile ON profile.id = settings.profile_id
    JOIN prompt_profiles AS prompt_profile
      ON prompt_profile.id = settings.active_prompt_profile_id
    WHERE prompt_profile.account_id <> profile.account_id
  ) THEN
    RAISE EXCEPTION 'account ownership preflight failed: cross-account prompt settings'
      USING ERRCODE = '23514';
  END IF;
END
$$;
-- +goose StatementEnd

-- Account-owned roots fail closed on account deletion. Existing account_id
-- columns are already NOT NULL and indexed by migration 00011.
ALTER TABLE profiles
  ADD CONSTRAINT profiles_account_id_fkey
  FOREIGN KEY (account_id) REFERENCES accounts(id)
  ON UPDATE NO ACTION ON DELETE RESTRICT NOT VALID;

ALTER TABLE stories
  ADD CONSTRAINT stories_account_id_fkey
  FOREIGN KEY (account_id) REFERENCES accounts(id)
  ON UPDATE NO ACTION ON DELETE RESTRICT NOT VALID;

ALTER TABLE child_profiles
  ADD CONSTRAINT child_profiles_account_id_fkey
  FOREIGN KEY (account_id) REFERENCES accounts(id)
  ON UPDATE NO ACTION ON DELETE RESTRICT NOT VALID;

ALTER TABLE prompt_profiles
  ADD CONSTRAINT prompt_profiles_account_id_fkey
  FOREIGN KEY (account_id) REFERENCES accounts(id)
  ON UPDATE NO ACTION ON DELETE RESTRICT NOT VALID;

ALTER TABLE profiles VALIDATE CONSTRAINT profiles_account_id_fkey;
ALTER TABLE stories VALIDATE CONSTRAINT stories_account_id_fkey;
ALTER TABLE child_profiles VALIDATE CONSTRAINT child_profiles_account_id_fkey;
ALTER TABLE prompt_profiles VALIDATE CONSTRAINT prompt_profiles_account_id_fkey;

-- Composite referenced keys are intentionally redundant with UUID primary
-- keys: PostgreSQL requires an exact unique key for each composite ownership FK.
ALTER TABLE profiles
  ADD CONSTRAINT profiles_id_account_id_key UNIQUE (id, account_id);

ALTER TABLE stories
  ADD CONSTRAINT stories_id_account_id_key UNIQUE (id, account_id);

ALTER TABLE child_profiles
  ADD CONSTRAINT child_profiles_id_account_id_key UNIQUE (id, account_id);

ALTER TABLE prompt_profiles
  ADD CONSTRAINT prompt_profiles_id_account_id_key UNIQUE (id, account_id);

ALTER TABLE story_versions
  ADD CONSTRAINT story_versions_id_story_id_key UNIQUE (id, story_id);

-- Preserve the legacy Default-profile rule. An account's existing profile
-- named exactly Default remains the default candidate; accounts without one
-- receive a new Default profile rather than repurposing another named profile.
ALTER TABLE profiles
  ADD COLUMN is_default boolean NOT NULL DEFAULT false;

WITH default_profiles AS (
  SELECT DISTINCT ON (account_id)
    id
  FROM profiles
  WHERE name = 'Default'
  ORDER BY account_id, created_at ASC, id ASC
)
UPDATE profiles AS profile
SET is_default = true
FROM default_profiles AS default_profile
WHERE default_profile.id = profile.id;

INSERT INTO profiles (account_id, name, is_default)
SELECT account.id, 'Default', true
FROM accounts AS account
WHERE NOT EXISTS (
  SELECT 1
  FROM profiles AS profile
  WHERE profile.account_id = account.id
    AND profile.name = 'Default'
);

CREATE UNIQUE INDEX profiles_one_default_per_account_idx
  ON profiles (account_id)
  WHERE is_default;

-- Store the account tuple directly on progress so PostgreSQL can reject both
-- cross-account profile/story links and cross-story version links.
ALTER TABLE reading_progress ADD COLUMN account_id uuid;

UPDATE reading_progress AS progress
SET account_id = profile.account_id
FROM profiles AS profile
JOIN stories AS story ON story.account_id = profile.account_id
WHERE profile.id = progress.profile_id
  AND story.id = progress.story_id;

ALTER TABLE reading_progress
  ADD CONSTRAINT reading_progress_account_id_not_null_check
  CHECK (account_id IS NOT NULL) NOT VALID;
ALTER TABLE reading_progress
  VALIDATE CONSTRAINT reading_progress_account_id_not_null_check;
ALTER TABLE reading_progress ALTER COLUMN account_id SET NOT NULL;
ALTER TABLE reading_progress
  DROP CONSTRAINT reading_progress_account_id_not_null_check;

CREATE INDEX reading_progress_story_account_idx
  ON reading_progress (story_id, account_id);

CREATE INDEX reading_progress_story_version_story_idx
  ON reading_progress (story_version_id, story_id);

ALTER TABLE reading_progress
  ADD CONSTRAINT reading_progress_profile_account_fkey
  FOREIGN KEY (profile_id, account_id)
  REFERENCES profiles(id, account_id)
  ON UPDATE NO ACTION ON DELETE CASCADE NOT VALID,
  ADD CONSTRAINT reading_progress_story_account_fkey
  FOREIGN KEY (story_id, account_id)
  REFERENCES stories(id, account_id)
  ON UPDATE NO ACTION ON DELETE CASCADE NOT VALID,
  ADD CONSTRAINT reading_progress_story_version_story_fkey
  FOREIGN KEY (story_version_id, story_id)
  REFERENCES story_versions(id, story_id)
  ON UPDATE NO ACTION ON DELETE NO ACTION NOT VALID;

ALTER TABLE reading_progress
  VALIDATE CONSTRAINT reading_progress_profile_account_fkey;
ALTER TABLE reading_progress
  VALIDATE CONSTRAINT reading_progress_story_account_fkey;
ALTER TABLE reading_progress
  VALIDATE CONSTRAINT reading_progress_story_version_story_fkey;

ALTER TABLE reading_progress
  DROP CONSTRAINT reading_progress_profile_id_fkey,
  DROP CONSTRAINT reading_progress_story_id_fkey,
  DROP CONSTRAINT reading_progress_story_version_id_fkey;

-- Settings inherit ownership from their reader profile. Nullable active
-- selections remain nullable, and deleting a selected configuration clears
-- only its identifier rather than the settings row's non-null account tuple.
ALTER TABLE profile_settings ADD COLUMN account_id uuid;

UPDATE profile_settings AS settings
SET account_id = profile.account_id
FROM profiles AS profile
WHERE profile.id = settings.profile_id;

ALTER TABLE profile_settings
  ADD CONSTRAINT profile_settings_account_id_not_null_check
  CHECK (account_id IS NOT NULL) NOT VALID;
ALTER TABLE profile_settings
  VALIDATE CONSTRAINT profile_settings_account_id_not_null_check;
ALTER TABLE profile_settings ALTER COLUMN account_id SET NOT NULL;
ALTER TABLE profile_settings
  DROP CONSTRAINT profile_settings_account_id_not_null_check;

ALTER TABLE profile_settings
  ADD CONSTRAINT profile_settings_profile_account_fkey
  FOREIGN KEY (profile_id, account_id)
  REFERENCES profiles(id, account_id)
  ON UPDATE NO ACTION ON DELETE CASCADE NOT VALID,
  ADD CONSTRAINT profile_settings_child_account_fkey
  FOREIGN KEY (active_child_profile_id, account_id)
  REFERENCES child_profiles(id, account_id)
  ON UPDATE NO ACTION ON DELETE SET NULL (active_child_profile_id) NOT VALID,
  ADD CONSTRAINT profile_settings_prompt_account_fkey
  FOREIGN KEY (active_prompt_profile_id, account_id)
  REFERENCES prompt_profiles(id, account_id)
  ON UPDATE NO ACTION ON DELETE SET NULL (active_prompt_profile_id) NOT VALID;

ALTER TABLE profile_settings
  VALIDATE CONSTRAINT profile_settings_profile_account_fkey;
ALTER TABLE profile_settings
  VALIDATE CONSTRAINT profile_settings_child_account_fkey;
ALTER TABLE profile_settings
  VALIDATE CONSTRAINT profile_settings_prompt_account_fkey;

ALTER TABLE profile_settings
  DROP CONSTRAINT profile_settings_profile_id_fkey,
  DROP CONSTRAINT profile_settings_active_child_profile_id_fkey,
  DROP CONSTRAINT profile_settings_active_prompt_profile_id_fkey;

COMMIT;

-- +goose Down
BEGIN;

LOCK TABLE
  accounts,
  profiles,
  child_profiles,
  prompt_profiles,
  stories,
  story_versions,
  reading_progress,
  profile_settings
IN SHARE ROW EXCLUSIVE MODE;

ALTER TABLE profile_settings
  DROP CONSTRAINT profile_settings_profile_account_fkey,
  DROP CONSTRAINT profile_settings_child_account_fkey,
  DROP CONSTRAINT profile_settings_prompt_account_fkey;

ALTER TABLE reading_progress
  DROP CONSTRAINT reading_progress_profile_account_fkey,
  DROP CONSTRAINT reading_progress_story_account_fkey,
  DROP CONSTRAINT reading_progress_story_version_story_fkey;

-- Restore the exact v14 foreign-key shape and delete actions before removing
-- the redundant ownership columns.
ALTER TABLE reading_progress
  ADD CONSTRAINT reading_progress_profile_id_fkey
  FOREIGN KEY (profile_id) REFERENCES profiles(id)
  ON UPDATE NO ACTION ON DELETE CASCADE NOT VALID,
  ADD CONSTRAINT reading_progress_story_id_fkey
  FOREIGN KEY (story_id) REFERENCES stories(id)
  ON UPDATE NO ACTION ON DELETE CASCADE NOT VALID,
  ADD CONSTRAINT reading_progress_story_version_id_fkey
  FOREIGN KEY (story_version_id) REFERENCES story_versions(id)
  ON UPDATE NO ACTION ON DELETE NO ACTION NOT VALID;

ALTER TABLE profile_settings
  ADD CONSTRAINT profile_settings_profile_id_fkey
  FOREIGN KEY (profile_id) REFERENCES profiles(id)
  ON UPDATE NO ACTION ON DELETE CASCADE NOT VALID,
  ADD CONSTRAINT profile_settings_active_child_profile_id_fkey
  FOREIGN KEY (active_child_profile_id) REFERENCES child_profiles(id)
  ON UPDATE NO ACTION ON DELETE SET NULL NOT VALID,
  ADD CONSTRAINT profile_settings_active_prompt_profile_id_fkey
  FOREIGN KEY (active_prompt_profile_id) REFERENCES prompt_profiles(id)
  ON UPDATE NO ACTION ON DELETE SET NULL NOT VALID;

ALTER TABLE reading_progress
  VALIDATE CONSTRAINT reading_progress_profile_id_fkey;
ALTER TABLE reading_progress
  VALIDATE CONSTRAINT reading_progress_story_id_fkey;
ALTER TABLE reading_progress
  VALIDATE CONSTRAINT reading_progress_story_version_id_fkey;
ALTER TABLE profile_settings
  VALIDATE CONSTRAINT profile_settings_profile_id_fkey;
ALTER TABLE profile_settings
  VALIDATE CONSTRAINT profile_settings_active_child_profile_id_fkey;
ALTER TABLE profile_settings
  VALIDATE CONSTRAINT profile_settings_active_prompt_profile_id_fkey;

DROP INDEX reading_progress_story_version_story_idx;
DROP INDEX reading_progress_story_account_idx;

ALTER TABLE profile_settings DROP COLUMN account_id;
ALTER TABLE reading_progress DROP COLUMN account_id;

DROP INDEX profiles_one_default_per_account_idx;
ALTER TABLE profiles DROP COLUMN is_default;

ALTER TABLE profiles DROP CONSTRAINT profiles_account_id_fkey;
ALTER TABLE stories DROP CONSTRAINT stories_account_id_fkey;
ALTER TABLE child_profiles DROP CONSTRAINT child_profiles_account_id_fkey;
ALTER TABLE prompt_profiles DROP CONSTRAINT prompt_profiles_account_id_fkey;

ALTER TABLE profiles DROP CONSTRAINT profiles_id_account_id_key;
ALTER TABLE stories DROP CONSTRAINT stories_id_account_id_key;
ALTER TABLE child_profiles DROP CONSTRAINT child_profiles_id_account_id_key;
ALTER TABLE prompt_profiles DROP CONSTRAINT prompt_profiles_id_account_id_key;
ALTER TABLE story_versions DROP CONSTRAINT story_versions_id_story_id_key;

COMMIT;
