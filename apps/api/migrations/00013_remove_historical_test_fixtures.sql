-- +goose Up
BEGIN;

-- Migration 00008 is already part of deployed Goose history, so it must not be
-- edited or removed. It predates fixed fixture UUIDs. The predicates below use
-- the complete fixture-only values written by 00008 rather than broad names or
-- product-table deletes. The shared Default account/profile are deliberately
-- preserved: they are runtime bootstrap records, not fixture-only ownership.

DELETE FROM generation_jobs
WHERE model = 'seed-model'
  AND prompt_version = 'v1'
  AND theme = 'space bedtime'
  AND request_payload = '{"note":"seed test request"}'::jsonb
  AND response_payload = '{"note":"seed test response"}'::jsonb
  AND tokens_in = 123
  AND tokens_out = 456
  AND EXISTS (
    SELECT 1
    FROM child_profiles AS child
    WHERE child.id = generation_jobs.child_profile_id
      AND child.account_id = (
        SELECT id FROM accounts ORDER BY created_at ASC, id ASC LIMIT 1
      )
      AND child.name = 'Ted'
      AND child.age_months = 34
      AND child.interests = '["space","animals","trains"]'::jsonb
      AND child.sensitivities = '["no_spiders"]'::jsonb
  )
  AND EXISTS (
    SELECT 1
    FROM prompt_profiles AS prompt
    WHERE prompt.id = generation_jobs.prompt_profile_id
      AND prompt.account_id = (
        SELECT id FROM accounts ORDER BY created_at ASC, id ASC LIMIT 1
      )
      AND prompt.name = 'Bedtime calm v1'
      AND prompt.schema_version = 1
  );

-- Deleting an exactly identified fixture story safely cascades only through
-- that story's versions, sections, segments, contributor links, and progress.
DELETE FROM stories
WHERE account_id = (SELECT id FROM accounts ORDER BY created_at ASC, id ASC LIMIT 1)
  AND ((
    slug = 'the-fox-and-the-grapes'
    AND title = 'The Fox and the Grapes'
    AND author = 'Aesop (trad.)'
    AND language = 'en-GB'
    AND rights = '{"public_domain":true}'::jsonb
    AND source = '{"note":"seed test","origin":"manual"}'::jsonb
  )
  OR (
    slug = 'the-little-star-and-the-moon'
    AND title = 'The Little Star and the Moon'
    AND author = 'Panda Pages (seed)'
    AND language = 'en-GB'
    AND rights = '{"public_domain":false,"license":"internal-test"}'::jsonb
    AND source = '{"note":"seed test","origin":"manual"}'::jsonb
  )
  OR (
    slug = 'frankenstein-excerpt-ch1'
    AND title = 'Frankenstein (Excerpt — Chapter 1)'
    AND author = 'Mary Shelley'
    AND language = 'en-GB'
    AND rights = '{"public_domain":true}'::jsonb
    AND source = '{"note":"seed test","origin":"manual","scope":"excerpt"}'::jsonb
  ));

DELETE FROM child_profiles
WHERE name = 'Ted'
  AND account_id = (SELECT id FROM accounts ORDER BY created_at ASC, id ASC LIMIT 1)
  AND age_months = 34
  AND interests = '["space","animals","trains"]'::jsonb
  AND sensitivities = '["no_spiders"]'::jsonb;

DELETE FROM prompt_profiles
WHERE name = 'Bedtime calm v1'
  AND account_id = (SELECT id FROM accounts ORDER BY created_at ASC, id ASC LIMIT 1)
  AND schema_version = 1
  AND rules = '{
    "tone":"calm, warm, UK English",
    "structure":{"segments":"short paragraphs", "repetition":"light"},
    "constraints":{"no_scare":true, "no_violence":true, "no_bullying":true}
  }'::jsonb;

-- Catalogue rows may have become shared with real stories. Remove them only
-- when the fixture-story cleanup left them unreferenced.
DELETE FROM works AS work
WHERE (
    (work.canonical_title = 'The Fox and the Grapes'
      AND work.description = 'Aesop fable (public domain source dependent).')
    OR (work.canonical_title = 'The Little Star and the Moon'
      AND work.description = 'Original short bedtime story (test content).')
    OR (work.canonical_title = 'Frankenstein; or, The Modern Prometheus'
      AND work.description = 'Classic novel by Mary Shelley (public domain source dependent).')
  )
  AND NOT EXISTS (
    SELECT 1
    FROM stories AS story
    WHERE story.work_id = work.id
  );

DELETE FROM contributors AS contributor
WHERE (
    (contributor.name = 'Aesop' AND contributor.sort_name = 'Aesop')
    OR (contributor.name = 'Mary Shelley' AND contributor.sort_name = 'Shelley, Mary')
  )
  AND NOT EXISTS (
    SELECT 1
    FROM story_contributors AS story_contributor
    WHERE story_contributor.contributor_id = contributor.id
  );

COMMIT;

-- +goose Down
-- Data cleanup is intentionally irreversible. Reversing this migration must
-- not silently reinstall test content into a real database. Developers use the
-- explicit fail-closed seed command instead.
SELECT 1;
