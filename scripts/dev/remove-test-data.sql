\set ON_ERROR_STOP on

BEGIN;

DO $$
BEGIN
  IF current_database() <> 'pandapages' THEN
    RAISE EXCEPTION 'test fixture removal requires the pandapages database';
  END IF;
END
$$;

DELETE FROM generation_jobs
WHERE id = 'f17e0000-0000-4000-8000-000000000040'
  AND model = 'test-only-seed-model';

DELETE FROM reading_progress
WHERE story_id = 'f17e0000-0000-4000-8000-000000000010';

DELETE FROM stories
WHERE id = 'f17e0000-0000-4000-8000-000000000010'
  AND source->>'origin' = 'explicit-test-seed'
  AND source->>'test_fixture' = 'true';

DELETE FROM child_profiles
WHERE id = 'f17e0000-0000-4000-8000-000000000001'
  AND name = 'TEST ONLY — Reader child';

DELETE FROM prompt_profiles
WHERE id = 'f17e0000-0000-4000-8000-000000000002'
  AND name = 'TEST ONLY — Reader prompt';

DELETE FROM works AS work
WHERE work.id = 'f17e0000-0000-4000-8000-000000000003'
  AND work.canonical_title = 'TEST ONLY — Moonlit Café'
  AND NOT EXISTS (
    SELECT 1 FROM stories AS story WHERE story.work_id = work.id
  );

DELETE FROM contributors AS contributor
WHERE contributor.id = 'f17e0000-0000-4000-8000-000000000004'
  AND contributor.name = 'Panda Pages Test Fixture'
  AND NOT EXISTS (
    SELECT 1
    FROM story_contributors AS story_contributor
    WHERE story_contributor.contributor_id = contributor.id
  );

COMMIT;
