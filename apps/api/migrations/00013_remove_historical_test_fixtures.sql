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

-- Migration 00008 generated story and version UUIDs. Delete a story root only
-- when its complete stable lifecycle still matches the historical fixture:
-- metadata and catalogue ownership, its sole v1 payload, publication pointers,
-- sections, segments, and contributor links. Any edit or added version makes
-- the row ambiguous and preserves the whole story and all of its dependants.
WITH historical_account AS (
  SELECT id
  FROM accounts
  ORDER BY created_at ASC, id ASC
  LIMIT 1
), fixture_story_fingerprints (
  slug, title, author, language, rights, source,
  work_title, work_description,
  frontmatter, markdown, rendered_html, content_hash,
  section_count, segment_count
) AS (
  VALUES
    (
      'the-fox-and-the-grapes',
      'The Fox and the Grapes',
      'Aesop (trad.)',
      'en-GB',
      '{"public_domain":true}'::jsonb,
      '{"note":"seed test","origin":"manual"}'::jsonb,
      'The Fox and the Grapes',
      'Aesop fable (public domain source dependent).',
      '{"tags":["fable","animals"],"age_months":34}'::jsonb,
      E'# The Fox and the Grapes\n\nA hungry fox saw some fine bunches of grapes hanging from a vine.\n\nHe did his best to reach them but could not.\n\nSo he walked away saying: **They are probably sour anyway.**\n',
      '<h1>The Fox and the Grapes</h1><p>A hungry fox saw some fine bunches of grapes hanging from a vine.</p><p>He did his best to reach them but could not.</p><p>So he walked away saying: <strong>They are probably sour anyway.</strong></p>',
      '2fb47f4013a00e9348fa92c2da31ee0688e3a8ef878fa4308646b1656c9adad4',
      0,
      3
    ),
    (
      'the-little-star-and-the-moon',
      'The Little Star and the Moon',
      'Panda Pages (seed)',
      'en-GB',
      '{"public_domain":false,"license":"internal-test"}'::jsonb,
      '{"note":"seed test","origin":"manual"}'::jsonb,
      'The Little Star and the Moon',
      'Original short bedtime story (test content).',
      '{"tags":["bedtime","space"],"age_months":34}'::jsonb,
      E'# The Little Star and the Moon\n\nA little star wanted to shine *all night*.\n\nBut the Moon said, "We take turns so everyone can rest."\n\nThe star tried it… and felt proud, safe, and sleepy.\n',
      '<h1>The Little Star and the Moon</h1><p>A little star wanted to shine <em>all night</em>.</p><p>But the Moon said, "We take turns so everyone can rest."</p><p>The star tried it… and felt proud, safe, and sleepy.</p>',
      'f7317461bbee29ecbd9b9f6e93a185fc18be9b8629c59b16b906bdd5cff92b39',
      0,
      3
    ),
    (
      'frankenstein-excerpt-ch1',
      'Frankenstein (Excerpt — Chapter 1)',
      'Mary Shelley',
      'en-GB',
      '{"public_domain":true}'::jsonb,
      '{"note":"seed test","origin":"manual","scope":"excerpt"}'::jsonb,
      'Frankenstein; or, The Modern Prometheus',
      'Classic novel by Mary Shelley (public domain source dependent).',
      '{"tags":["classic","excerpt"],"age_months":999,"note":"adult classic test"}'::jsonb,
      E'# Frankenstein (Excerpt — Chapter 1)\n\nThis is a short excerpt for testing long-form classics storage.\n\n## Letter I\n\nYou will rejoice to hear that no disaster has accompanied the commencement of an enterprise which you have regarded with such evil forebodings.\n',
      '<h1>Frankenstein (Excerpt — Chapter 1)</h1><p>This is a short excerpt for testing long-form classics storage.</p><h2>Letter I</h2><p>You will rejoice to hear that no disaster has accompanied the commencement of an enterprise which you have regarded with such evil forebodings.</p>',
      'fb1fae7a93f9cde79682d1f86fe175da6ebb81bb0fef89d47cdd78a8de2cfc3f',
      1,
      3
    )
), fixture_section_fingerprints (
  slug, ordinal, kind, title, start_offset, end_offset
) AS (
  VALUES
    ('frankenstein-excerpt-ch1', 1, 'chapter', 'Letter I', NULL::integer, NULL::integer)
), fixture_segment_fingerprints (
  slug, ordinal, section_ordinal, locator, markdown, rendered_html, word_count
) AS (
  VALUES
    (
      'the-fox-and-the-grapes', 1, NULL::integer,
      '{"type":"heading","h":1,"index":0}'::jsonb,
      '# The Fox and the Grapes',
      '<h1>The Fox and the Grapes</h1>',
      5
    ),
    (
      'the-fox-and-the-grapes', 2, NULL::integer,
      '{"type":"para","n":1}'::jsonb,
      'A hungry fox saw some fine bunches of grapes hanging from a vine.',
      '<p>A hungry fox saw some fine bunches of grapes hanging from a vine.</p>',
      15
    ),
    (
      'the-fox-and-the-grapes', 3, NULL::integer,
      '{"type":"para","n":2}'::jsonb,
      'So he walked away saying: <strong>They are probably sour anyway.</strong>',
      '<p>So he walked away saying: <strong>They are probably sour anyway.</strong></p>',
      12
    ),
    (
      'the-little-star-and-the-moon', 1, NULL::integer,
      '{"type":"heading","h":1,"index":0}'::jsonb,
      '# The Little Star and the Moon',
      '<h1>The Little Star and the Moon</h1>',
      6
    ),
    (
      'the-little-star-and-the-moon', 2, NULL::integer,
      '{"type":"para","n":1}'::jsonb,
      'A little star wanted to shine <em>all night</em>.',
      '<p>A little star wanted to shine <em>all night</em>.</p>',
      10
    ),
    (
      'the-little-star-and-the-moon', 3, NULL::integer,
      '{"type":"para","n":2}'::jsonb,
      'The star tried it… and felt proud, safe, and sleepy.',
      '<p>The star tried it… and felt proud, safe, and sleepy.</p>',
      11
    ),
    (
      'frankenstein-excerpt-ch1', 1, 1,
      '{"type":"heading","h":1,"index":0}'::jsonb,
      '# Frankenstein (Excerpt — Chapter 1)',
      '<h1>Frankenstein (Excerpt — Chapter 1)</h1>',
      4
    ),
    (
      'frankenstein-excerpt-ch1', 2, 1,
      '{"type":"heading","h":2,"index":0}'::jsonb,
      '## Letter I',
      '<h2>Letter I</h2>',
      2
    ),
    (
      'frankenstein-excerpt-ch1', 3, 1,
      '{"type":"para","n":1}'::jsonb,
      'You will rejoice to hear that no disaster has accompanied the commencement of an enterprise which you have regarded with such evil forebodings.',
      '<p>You will rejoice to hear that no disaster has accompanied the commencement of an enterprise which you have regarded with such evil forebodings.</p>',
      27
    )
), fixture_contributor_fingerprints (slug, name, sort_name, role) AS (
  VALUES
    ('the-fox-and-the-grapes', 'Aesop', 'Aesop', 'author'),
    ('frankenstein-excerpt-ch1', 'Mary Shelley', 'Shelley, Mary', 'author')
), exact_fixture_stories AS (
  SELECT story.id
  FROM historical_account
  JOIN stories AS story
    ON story.account_id = historical_account.id
  JOIN fixture_story_fingerprints AS fingerprint
    ON fingerprint.slug = story.slug
   AND fingerprint.title = story.title
   AND fingerprint.author = story.author
   AND fingerprint.language = story.language
   AND fingerprint.rights = story.rights
   AND fingerprint.source = story.source
  JOIN works AS work
    ON work.id = story.work_id
   AND work.canonical_title = fingerprint.work_title
   AND work.description = fingerprint.work_description
  JOIN story_versions AS version
    ON version.story_id = story.id
   AND version.version = 1
   AND version.frontmatter = fingerprint.frontmatter
   AND version.markdown = fingerprint.markdown
   AND version.rendered_html = fingerprint.rendered_html
   AND version.content_hash = fingerprint.content_hash
  WHERE story.cover_asset_id IS NULL
    AND story.is_published = true
    AND story.draft_version_id = version.id
    AND story.published_version_id = version.id
    AND (SELECT count(*) FROM story_versions WHERE story_id = story.id) = 1
    AND (
      SELECT count(*)
      FROM story_sections
      WHERE story_version_id = version.id
    ) = fingerprint.section_count
    AND (
      SELECT count(*)
      FROM story_segments
      WHERE story_version_id = version.id
    ) = fingerprint.segment_count
    AND NOT EXISTS (
      SELECT 1
      FROM fixture_section_fingerprints AS expected
      WHERE expected.slug = fingerprint.slug
        AND NOT EXISTS (
          SELECT 1
          FROM story_sections AS section
          WHERE section.story_version_id = version.id
            AND section.ordinal = expected.ordinal
            AND section.kind = expected.kind
            AND section.title = expected.title
            AND section.start_offset IS NOT DISTINCT FROM expected.start_offset
            AND section.end_offset IS NOT DISTINCT FROM expected.end_offset
        )
    )
    AND NOT EXISTS (
      SELECT 1
      FROM fixture_segment_fingerprints AS expected
      WHERE expected.slug = fingerprint.slug
        AND NOT EXISTS (
          SELECT 1
          FROM story_segments AS segment
          WHERE segment.story_version_id = version.id
            AND segment.ordinal = expected.ordinal
            AND segment.locator = expected.locator
            AND segment.markdown = expected.markdown
            AND segment.rendered_html = expected.rendered_html
            AND segment.word_count = expected.word_count
            AND (
              (expected.section_ordinal IS NULL AND segment.section_id IS NULL)
              OR (
                expected.section_ordinal IS NOT NULL
                AND EXISTS (
                  SELECT 1
                  FROM story_sections AS section
                  WHERE section.id = segment.section_id
                    AND section.story_version_id = version.id
                    AND section.ordinal = expected.section_ordinal
                )
              )
            )
        )
    )
    AND (
      SELECT count(*)
      FROM story_contributors
      WHERE story_id = story.id
    ) = (
      SELECT count(*)
      FROM fixture_contributor_fingerprints
      WHERE slug = fingerprint.slug
    )
    AND NOT EXISTS (
      SELECT 1
      FROM fixture_contributor_fingerprints AS expected
      WHERE expected.slug = fingerprint.slug
        AND NOT EXISTS (
          SELECT 1
          FROM story_contributors AS link
          JOIN contributors AS contributor
            ON contributor.id = link.contributor_id
          WHERE link.story_id = story.id
            AND link.role = expected.role
            AND contributor.name = expected.name
            AND contributor.sort_name = expected.sort_name
        )
    )
)
DELETE FROM stories AS story
USING exact_fixture_stories AS fixture
WHERE story.id = fixture.id;

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
