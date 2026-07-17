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
      AND source->>'origin' = 'explicit-test-seed'
      AND source->>'test_fixture' = 'true'
  ) THEN
    RAISE EXCEPTION 'reader fixture must be seeded before test progress';
  END IF;
END
$$;

INSERT INTO reading_progress (
  profile_id, story_id, story_version_id, locator, percent
)
SELECT
  profile.id,
  story.id,
  version.id,
  '{"type":"para","n":2}'::jsonb,
  0.6
FROM stories AS story
JOIN story_versions AS version
  ON version.id = 'f17e0000-0000-4000-8000-000000000011'
 AND version.story_id = story.id
JOIN LATERAL (
  SELECT id
  FROM profiles
  WHERE account_id = story.account_id
    AND name = 'Default'
  ORDER BY created_at ASC, id ASC
  LIMIT 1
) AS profile ON true
WHERE story.id = 'f17e0000-0000-4000-8000-000000000010'
ON CONFLICT (profile_id, story_id) DO UPDATE SET
  story_version_id = EXCLUDED.story_version_id,
  locator = EXCLUDED.locator,
  percent = EXCLUDED.percent,
  updated_at = now();

COMMIT;
