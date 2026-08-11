\set ON_ERROR_STOP on

BEGIN;

DO $$
BEGIN
  IF current_database() <> 'pandapages' THEN
    RAISE EXCEPTION 'test progress requires the pandapages database';
  END IF;

  IF NOT EXISTS (
    SELECT 1
    FROM stories
    WHERE id = 'f17e0000-0000-4000-8000-000000000010'
      AND visibility = 'public'
      AND owner_account_id IS NULL
      AND slug = 'test-only-moonlit-cafe'
      AND title = 'TEST ONLY - Moonlit Café'
      AND author = 'Panda Pages Test Fixture'
      AND language = 'en-GB'
      AND rights = '{"license":"test-only","test_fixture":true}'::jsonb
  ) THEN
    RAISE EXCEPTION 'reader fixture must be seeded before test progress';
  END IF;
END
$$;

INSERT INTO reading_progress (
  account_id, profile_id, story_id, story_version_id, locator, percent, updated_at
)
SELECT
  profile.account_id,
  profile.id,
  story.id,
  version.id,
  '{"schema":2,"segment":{"key":"aae1f4bebb97b03ea9d0cfd5431675a250ab6a14be567445b1bb250874574e19","occurrence":1,"ordinal":4,"offset":0.35},"chapter":{"key":"6f744b440fbf4fa52da46bebf4fd3e5f2de7a1c2fb11f7e9ac2794ccd1956c4e","occurrence":1}}'::jsonb,
  0.6,
  now()
FROM stories AS story
JOIN story_versions AS version
  ON version.id = 'f17e0000-0000-4000-8000-000000000011'
 AND version.story_id = story.id
JOIN profiles AS profile
  ON profile.id = 'f17e0000-0000-4000-8000-000000000002'
 AND profile.account_id = 'f17e0000-0000-4000-8000-000000000001'
 AND profile.name = 'TEST ONLY - Reader'
WHERE story.id = 'f17e0000-0000-4000-8000-000000000010'
ON CONFLICT (account_id, profile_id, story_id) DO UPDATE SET
  account_id = EXCLUDED.account_id,
  story_version_id = EXCLUDED.story_version_id,
  locator = EXCLUDED.locator,
  percent = EXCLUDED.percent,
  updated_at = now();

COMMIT;
