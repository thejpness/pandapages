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

COMMIT;
