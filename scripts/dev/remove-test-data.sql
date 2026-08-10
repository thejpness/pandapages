\set ON_ERROR_STOP on

BEGIN;

DO $$
BEGIN
  IF current_database() <> 'pandapages' THEN
    RAISE EXCEPTION 'test fixture removal requires the pandapages database';
  END IF;
END
$$;

DELETE FROM reading_progress
WHERE story_id = 'f17e0000-0000-4000-8000-000000000010';

DELETE FROM stories
WHERE id = 'f17e0000-0000-4000-8000-000000000010'
  AND slug = 'test-only-moonlit-cafe'
  AND visibility = 'public'
  AND owner_account_id IS NULL
  AND title = 'TEST ONLY — Moonlit Café'
  AND author = 'Panda Pages Test Fixture'
  AND language = 'en-GB'
  AND rights = '{"license":"test-only","test_fixture":true}'::jsonb;

DELETE FROM contributors AS contributor
WHERE contributor.id = 'f17e0000-0000-4000-8000-000000000004'
  AND contributor.name = 'Panda Pages Test Fixture'
  AND NOT EXISTS (
    SELECT 1
    FROM story_contributors AS story_contributor
    WHERE story_contributor.contributor_id = contributor.id
  );

DELETE FROM profiles AS profile
WHERE profile.id = 'f17e0000-0000-4000-8000-000000000002'
  AND profile.account_id = 'f17e0000-0000-4000-8000-000000000001'
  AND profile.name = 'TEST ONLY — Reader'
  AND profile.reading_level = 'classic'
  AND profile.pin_hash IS NULL
  AND profile.pin_failed_attempts = 0
  AND profile.pin_lock_until IS NULL
  AND NOT EXISTS (
    SELECT 1
    FROM reading_progress AS progress
    WHERE progress.profile_id = profile.id
  );

DELETE FROM accounts AS account
WHERE account.id = 'f17e0000-0000-4000-8000-000000000001'
  AND account.name = 'TEST ONLY — Reader Fixture Account'
  AND NOT EXISTS (
    SELECT 1 FROM profiles WHERE account_id = account.id
  )
  AND NOT EXISTS (
    SELECT 1 FROM account_memberships WHERE account_id = account.id
  )
  AND NOT EXISTS (
    SELECT 1 FROM reading_progress WHERE account_id = account.id
  );

COMMIT;
