-- +goose Up
BEGIN;

-- Migration 00007 created a reader profile before accounts existed, then
-- migration 00011 created the oldest Default account and attached that profile
-- to it. Lock every current table that can reference the candidate so the
-- provenance check and any deletion are one race-free decision.
LOCK TABLE
  accounts,
  profiles,
  account_memberships,
  stories,
  reading_progress
IN ACCESS EXCLUSIVE MODE;

-- +goose StatementBegin
DO $$
DECLARE
  candidate_account_id uuid;
  candidate_account_created_at timestamptz;
  candidate_account_updated_at timestamptz;

  candidate_profile_id uuid;
  candidate_profile_name text;
  candidate_profile_created_at timestamptz;
  candidate_profile_updated_at timestamptz;
  candidate_profile_pin_hash text;
  candidate_profile_failed_attempts integer;
  candidate_profile_lock_until timestamptz;

  profile_count bigint := 0;
  membership_count bigint := 0;
  story_count bigint := 0;
  account_progress_count bigint := 0;
  profile_progress_count bigint := 0;

  account_untouched boolean := false;
  profile_shape_ok boolean := false;
  profile_is_oldest boolean := false;
BEGIN
  -- Do not identify bootstrap data by name alone. The historical account can
  -- only be the oldest account, and only while it still carries its original
  -- Default name. A renamed account is treated as explicitly repurposed data.
  SELECT account.id, account.created_at, account.updated_at
  INTO
    candidate_account_id,
    candidate_account_created_at,
    candidate_account_updated_at
  FROM accounts AS account
  WHERE account.name = 'Default'
    AND NOT EXISTS (
      SELECT 1
      FROM accounts AS older
      WHERE older.created_at < account.created_at
         OR (
           older.created_at = account.created_at
           AND older.id < account.id
         )
    );

  IF candidate_account_id IS NULL THEN
    RETURN;
  END IF;

  SELECT count(*)
  INTO profile_count
  FROM profiles
  WHERE account_id = candidate_account_id;

  IF profile_count = 1 THEN
    SELECT
      profile.id,
      profile.name,
      profile.created_at,
      profile.updated_at,
      profile.pin_hash,
      profile.pin_failed_attempts,
      profile.pin_lock_until
    INTO
      candidate_profile_id,
      candidate_profile_name,
      candidate_profile_created_at,
      candidate_profile_updated_at,
      candidate_profile_pin_hash,
      candidate_profile_failed_attempts,
      candidate_profile_lock_until
    FROM profiles AS profile
    WHERE profile.account_id = candidate_account_id;

    SELECT NOT EXISTS (
      SELECT 1
      FROM profiles AS older
      WHERE older.created_at < candidate_profile_created_at
         OR (
           older.created_at = candidate_profile_created_at
           AND older.id < candidate_profile_id
         )
    )
    INTO profile_is_oldest;

    SELECT count(*)
    INTO profile_progress_count
    FROM reading_progress
    WHERE profile_id = candidate_profile_id;

    account_untouched :=
      candidate_account_updated_at = candidate_account_created_at;

    profile_shape_ok :=
      candidate_profile_name = 'Default'
      AND candidate_profile_created_at < candidate_account_created_at
      AND candidate_profile_updated_at = candidate_profile_created_at
      AND candidate_profile_pin_hash IS NULL
      AND candidate_profile_failed_attempts = 0
      AND candidate_profile_lock_until IS NULL
      AND profile_is_oldest;
  END IF;

  SELECT count(*)
  INTO membership_count
  FROM account_memberships
  WHERE account_id = candidate_account_id;

  SELECT count(*)
  INTO story_count
  FROM stories
  WHERE account_id = candidate_account_id;

  SELECT count(*)
  INTO account_progress_count
  FROM reading_progress
  WHERE account_id = candidate_account_id;

  -- Any sign that the historical bootstrap has been used, renamed, secured,
  -- expanded, or otherwise repurposed requires an explicit operator decision.
  -- This migration never guesses that such data is disposable.
  IF profile_count <> 1
     OR NOT account_untouched
     OR NOT profile_shape_ok
     OR membership_count <> 0
     OR story_count <> 0
     OR account_progress_count <> 0
     OR profile_progress_count <> 0 THEN
    RAISE EXCEPTION
      'legacy Default bootstrap retirement refused: profiles=%, memberships=%, stories=%, account_progress=%, profile_progress=%, account_untouched=%, profile_shape=%',
      profile_count,
      membership_count,
      story_count,
      account_progress_count,
      profile_progress_count,
      account_untouched,
      profile_shape_ok
      USING ERRCODE = '23514';
  END IF;

  DELETE FROM profiles
  WHERE id = candidate_profile_id
    AND account_id = candidate_account_id;

  DELETE FROM accounts
  WHERE id = candidate_account_id;
END
$$;
-- +goose StatementEnd

-- Current account creation always supplies an explicit product name. Retaining
-- the migration-11 Default would allow an omitted name to manufacture another
-- fake bootstrap account.
ALTER TABLE accounts
  ALTER COLUMN name DROP DEFAULT;

COMMIT;

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
  RAISE EXCEPTION
    'legacy Default bootstrap retirement is irreversible'
    USING ERRCODE = '0A000';
END
$$;
-- +goose StatementEnd
