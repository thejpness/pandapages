-- +goose Up
BEGIN;

-- Ensure Default profile
INSERT INTO profiles (name)
SELECT 'Default'
WHERE NOT EXISTS (SELECT 1 FROM profiles WHERE name='Default');

-- Ensure child + prompt profiles
INSERT INTO child_profiles (name, age_months, interests, sensitivities)
SELECT 'Ted', 34, '["space","animals","trains"]'::jsonb, '["no_spiders"]'::jsonb
WHERE NOT EXISTS (SELECT 1 FROM child_profiles WHERE name='Ted');

INSERT INTO prompt_profiles (name, rules, schema_version)
SELECT
  'Bedtime calm v1',
  '{
    "tone":"calm, warm, UK English",
    "structure":{"segments":"short paragraphs", "repetition":"light"},
    "constraints":{"no_scare":true, "no_violence":true, "no_bullying":true}
  }'::jsonb,
  1
WHERE NOT EXISTS (SELECT 1 FROM prompt_profiles WHERE name='Bedtime calm v1');

-- Contributors
INSERT INTO contributors (name, sort_name)
SELECT 'Aesop', 'Aesop'
WHERE NOT EXISTS (SELECT 1 FROM contributors WHERE name='Aesop');

INSERT INTO contributors (name, sort_name)
SELECT 'Mary Shelley', 'Shelley, Mary'
WHERE NOT EXISTS (SELECT 1 FROM contributors WHERE name='Mary Shelley');

-- Works
INSERT INTO works (canonical_title, description)
SELECT 'The Fox and the Grapes', 'Aesop fable (public domain source dependent).'
WHERE NOT EXISTS (SELECT 1 FROM works WHERE canonical_title='The Fox and the Grapes');

INSERT INTO works (canonical_title, description)
SELECT 'The Little Star and the Moon', 'Original short bedtime story (test content).'
WHERE NOT EXISTS (SELECT 1 FROM works WHERE canonical_title='The Little Star and the Moon');

INSERT INTO works (canonical_title, description)
SELECT 'Frankenstein; or, The Modern Prometheus', 'Classic novel by Mary Shelley (public domain source dependent).'
WHERE NOT EXISTS (SELECT 1 FROM works WHERE canonical_title='Frankenstein; or, The Modern Prometheus');

-- ========== Story 1: Fox ==========
INSERT INTO stories (slug, title, author, is_published, language, rights, source, work_id)
SELECT
  'the-fox-and-the-grapes',
  'The Fox and the Grapes',
  'Aesop (trad.)',
  true,
  'en-GB',
  '{"public_domain": true}'::jsonb,
  '{"note":"seed test","origin":"manual"}'::jsonb,
  (SELECT id FROM works WHERE canonical_title='The Fox and the Grapes' LIMIT 1)
WHERE NOT EXISTS (SELECT 1 FROM stories WHERE slug='the-fox-and-the-grapes');

INSERT INTO story_contributors (story_id, contributor_id, role)
SELECT
  s.id,
  (SELECT id FROM contributors WHERE name='Aesop' LIMIT 1),
  'author'
FROM stories s
WHERE s.slug='the-fox-and-the-grapes'
  AND NOT EXISTS (
    SELECT 1 FROM story_contributors sc
    WHERE sc.story_id=s.id
      AND sc.contributor_id=(SELECT id FROM contributors WHERE name='Aesop' LIMIT 1)
      AND sc.role='author'
  );

INSERT INTO story_versions (story_id, version, frontmatter, markdown, rendered_html, content_hash)
SELECT
  s.id,
  1,
  '{"tags":["fable","animals"],"age_months":34}'::jsonb,
  E'# The Fox and the Grapes\n\nA hungry fox saw some fine bunches of grapes hanging from a vine.\n\nHe did his best to reach them but could not.\n\nSo he walked away saying: **They are probably sour anyway.**\n',
  '<h1>The Fox and the Grapes</h1><p>A hungry fox saw some fine bunches of grapes hanging from a vine.</p><p>He did his best to reach them but could not.</p><p>So he walked away saying: <strong>They are probably sour anyway.</strong></p>',
  encode(digest(
    E'# The Fox and the Grapes\n\nA hungry fox saw some fine bunches of grapes hanging from a vine.\n\nHe did his best to reach them but could not.\n\nSo he walked away saying: **They are probably sour anyway.**\n'
  ,'sha256'),'hex')
FROM stories s
WHERE s.slug='the-fox-and-the-grapes'
  AND NOT EXISTS (SELECT 1 FROM story_versions sv WHERE sv.story_id=s.id AND sv.version=1);

UPDATE stories s
SET published_version_id = sv.id,
    draft_version_id = sv.id,
    is_published = true
FROM story_versions sv
WHERE s.slug='the-fox-and-the-grapes'
  AND sv.story_id=s.id AND sv.version=1;

DELETE FROM story_segments
WHERE story_version_id = (
  SELECT sv.id FROM story_versions sv
  JOIN stories s ON s.id=sv.story_id
  WHERE s.slug='the-fox-and-the-grapes' AND sv.version=1
);

INSERT INTO story_segments (story_version_id, ordinal, locator, markdown, rendered_html, word_count)
SELECT sv.id, x.ordinal, x.locator, x.markdown, x.rendered_html, x.word_count
FROM story_versions sv
JOIN stories s ON s.id=sv.story_id,
LATERAL (VALUES
  (1, '{"type":"heading","h":1,"index":0}'::jsonb, '# The Fox and the Grapes', '<h1>The Fox and the Grapes</h1>', 5),
  (2, '{"type":"para","n":1}'::jsonb, 'A hungry fox saw some fine bunches of grapes hanging from a vine.', '<p>A hungry fox saw some fine bunches of grapes hanging from a vine.</p>', 15),
  (3, '{"type":"para","n":2}'::jsonb, 'So he walked away saying: <strong>They are probably sour anyway.</strong>', '<p>So he walked away saying: <strong>They are probably sour anyway.</strong></p>', 12)
) AS x(ordinal, locator, markdown, rendered_html, word_count)
WHERE s.slug='the-fox-and-the-grapes' AND sv.version=1;

INSERT INTO reading_progress (profile_id, story_id, story_version_id, locator, percent)
SELECT
  (SELECT id FROM profiles WHERE name='Default' LIMIT 1),
  s.id,
  sv.id,
  '{"type":"para","n":1}'::jsonb,
  0.33
FROM stories s
JOIN story_versions sv ON sv.story_id=s.id AND sv.version=1
WHERE s.slug='the-fox-and-the-grapes'
ON CONFLICT (profile_id, story_id)
DO UPDATE SET story_version_id=EXCLUDED.story_version_id, locator=EXCLUDED.locator, percent=EXCLUDED.percent, updated_at=now();

-- ========== Story 2: Little Star ==========
INSERT INTO stories (slug, title, author, is_published, language, rights, source, work_id)
SELECT
  'the-little-star-and-the-moon',
  'The Little Star and the Moon',
  'Panda Pages (seed)',
  true,
  'en-GB',
  '{"public_domain": false, "license":"internal-test"}'::jsonb,
  '{"note":"seed test","origin":"manual"}'::jsonb,
  (SELECT id FROM works WHERE canonical_title='The Little Star and the Moon' LIMIT 1)
WHERE NOT EXISTS (SELECT 1 FROM stories WHERE slug='the-little-star-and-the-moon');

INSERT INTO story_versions (story_id, version, frontmatter, markdown, rendered_html, content_hash)
SELECT
  s.id,
  1,
  '{"tags":["bedtime","space"],"age_months":34}'::jsonb,
  E'# The Little Star and the Moon\n\nA little star wanted to shine *all night*.\n\nBut the Moon said, \"We take turns so everyone can rest.\"\n\nThe star tried it… and felt proud, safe, and sleepy.\n',
  '<h1>The Little Star and the Moon</h1><p>A little star wanted to shine <em>all night</em>.</p><p>But the Moon said, "We take turns so everyone can rest."</p><p>The star tried it… and felt proud, safe, and sleepy.</p>',
  encode(digest(
    E'# The Little Star and the Moon\n\nA little star wanted to shine *all night*.\n\nBut the Moon said, \"We take turns so everyone can rest.\"\n\nThe star tried it… and felt proud, safe, and sleepy.\n'
  ,'sha256'),'hex')
FROM stories s
WHERE s.slug='the-little-star-and-the-moon'
  AND NOT EXISTS (SELECT 1 FROM story_versions sv WHERE sv.story_id=s.id AND sv.version=1);

UPDATE stories s
SET published_version_id = sv.id,
    draft_version_id = sv.id,
    is_published = true
FROM story_versions sv
WHERE s.slug='the-little-star-and-the-moon'
  AND sv.story_id=s.id AND sv.version=1;

DELETE FROM story_segments
WHERE story_version_id = (
  SELECT sv.id FROM story_versions sv
  JOIN stories s ON s.id=sv.story_id
  WHERE s.slug='the-little-star-and-the-moon' AND sv.version=1
);

INSERT INTO story_segments (story_version_id, ordinal, locator, markdown, rendered_html, word_count)
SELECT sv.id, x.ordinal, x.locator, x.markdown, x.rendered_html, x.word_count
FROM story_versions sv
JOIN stories s ON s.id=sv.story_id,
LATERAL (VALUES
  (1, '{"type":"heading","h":1,"index":0}'::jsonb, '# The Little Star and the Moon', '<h1>The Little Star and the Moon</h1>', 6),
  (2, '{"type":"para","n":1}'::jsonb, 'A little star wanted to shine <em>all night</em>.', '<p>A little star wanted to shine <em>all night</em>.</p>', 10),
  (3, '{"type":"para","n":2}'::jsonb, 'The star tried it… and felt proud, safe, and sleepy.', '<p>The star tried it… and felt proud, safe, and sleepy.</p>', 11)
) AS x(ordinal, locator, markdown, rendered_html, word_count)
WHERE s.slug='the-little-star-and-the-moon' AND sv.version=1;

INSERT INTO generation_jobs (
  status, story_id, story_version_id, child_profile_id, prompt_profile_id, theme,
  request_payload, response_payload, model, prompt_version, tokens_in, tokens_out
)
SELECT
  'succeeded'::generation_status,
  s.id,
  sv.id,
  (SELECT id FROM child_profiles WHERE name='Ted' LIMIT 1),
  (SELECT id FROM prompt_profiles WHERE name='Bedtime calm v1' LIMIT 1),
  'space bedtime',
  '{"note":"seed test request"}'::jsonb,
  '{"note":"seed test response"}'::jsonb,
  'seed-model',
  'v1',
  123,
  456
FROM stories s
JOIN story_versions sv ON sv.story_id=s.id AND sv.version=1
WHERE s.slug='the-little-star-and-the-moon'
  AND NOT EXISTS (SELECT 1 FROM generation_jobs WHERE model='seed-model' AND prompt_version='v1');

-- ========== Story 3: Frankenstein excerpt ==========
INSERT INTO stories (slug, title, author, is_published, language, rights, source, work_id)
SELECT
  'frankenstein-excerpt-ch1',
  'Frankenstein (Excerpt — Chapter 1)',
  'Mary Shelley',
  true,
  'en-GB',
  '{"public_domain": true}'::jsonb,
  '{"note":"seed test","origin":"manual","scope":"excerpt"}'::jsonb,
  (SELECT id FROM works WHERE canonical_title='Frankenstein; or, The Modern Prometheus' LIMIT 1)
WHERE NOT EXISTS (SELECT 1 FROM stories WHERE slug='frankenstein-excerpt-ch1');

INSERT INTO story_contributors (story_id, contributor_id, role)
SELECT
  s.id,
  (SELECT id FROM contributors WHERE name='Mary Shelley' LIMIT 1),
  'author'
FROM stories s
WHERE s.slug='frankenstein-excerpt-ch1'
  AND NOT EXISTS (
    SELECT 1 FROM story_contributors sc
    WHERE sc.story_id=s.id
      AND sc.contributor_id=(SELECT id FROM contributors WHERE name='Mary Shelley' LIMIT 1)
      AND sc.role='author'
  );

INSERT INTO story_versions (story_id, version, frontmatter, markdown, rendered_html, content_hash)
SELECT
  s.id,
  1,
  '{"tags":["classic","excerpt"],"age_months":999,"note":"adult classic test"}'::jsonb,
  E'# Frankenstein (Excerpt — Chapter 1)\n\nThis is a short excerpt for testing long-form classics storage.\n\n## Letter I\n\nYou will rejoice to hear that no disaster has accompanied the commencement of an enterprise which you have regarded with such evil forebodings.\n',
  '<h1>Frankenstein (Excerpt — Chapter 1)</h1><p>This is a short excerpt for testing long-form classics storage.</p><h2>Letter I</h2><p>You will rejoice to hear that no disaster has accompanied the commencement of an enterprise which you have regarded with such evil forebodings.</p>',
  encode(digest(
    E'# Frankenstein (Excerpt — Chapter 1)\n\nThis is a short excerpt for testing long-form classics storage.\n\n## Letter I\n\nYou will rejoice to hear that no disaster has accompanied the commencement of an enterprise which you have regarded with such evil forebodings.\n'
  ,'sha256'),'hex')
FROM stories s
WHERE s.slug='frankenstein-excerpt-ch1'
  AND NOT EXISTS (SELECT 1 FROM story_versions sv WHERE sv.story_id=s.id AND sv.version=1);

UPDATE stories s
SET published_version_id = sv.id,
    draft_version_id = sv.id,
    is_published = true
FROM story_versions sv
WHERE s.slug='frankenstein-excerpt-ch1'
  AND sv.story_id=s.id AND sv.version=1;

DELETE FROM story_sections
WHERE story_version_id = (
  SELECT sv.id FROM story_versions sv
  JOIN stories s ON s.id=sv.story_id
  WHERE s.slug='frankenstein-excerpt-ch1' AND sv.version=1
);

INSERT INTO story_sections (story_version_id, kind, title, ordinal)
SELECT sv.id, 'chapter', 'Letter I', 1
FROM story_versions sv
JOIN stories s ON s.id=sv.story_id
WHERE s.slug='frankenstein-excerpt-ch1' AND sv.version=1
  AND NOT EXISTS (
    SELECT 1 FROM story_sections sec WHERE sec.story_version_id=sv.id AND sec.ordinal=1
  );

DELETE FROM story_segments
WHERE story_version_id = (
  SELECT sv.id FROM story_versions sv
  JOIN stories s ON s.id=sv.story_id
  WHERE s.slug='frankenstein-excerpt-ch1' AND sv.version=1
);

INSERT INTO story_segments (story_version_id, section_id, ordinal, locator, markdown, rendered_html, word_count)
SELECT
  sv.id,
  sec.id,
  x.ordinal,
  x.locator,
  x.markdown,
  x.rendered_html,
  x.word_count
FROM story_versions sv
JOIN stories s ON s.id=sv.story_id
LEFT JOIN story_sections sec ON sec.story_version_id=sv.id AND sec.ordinal=1,
LATERAL (VALUES
  (1, NULL::uuid, '{"type":"heading","h":1,"index":0}'::jsonb, '# Frankenstein (Excerpt — Chapter 1)', '<h1>Frankenstein (Excerpt — Chapter 1)</h1>', 4),
  (2, sec.id,     '{"type":"heading","h":2,"index":0}'::jsonb, '## Letter I', '<h2>Letter I</h2>', 2),
  (3, sec.id,     '{"type":"para","n":1}'::jsonb,
      'You will rejoice to hear that no disaster has accompanied the commencement of an enterprise which you have regarded with such evil forebodings.',
      '<p>You will rejoice to hear that no disaster has accompanied the commencement of an enterprise which you have regarded with such evil forebodings.</p>', 27)
) AS x(ordinal, section_id, locator, markdown, rendered_html, word_count)
WHERE s.slug='frankenstein-excerpt-ch1' AND sv.version=1;

COMMIT;

-- +goose Down
BEGIN;

DELETE FROM generation_jobs WHERE model='seed-model' AND prompt_version='v1';

DELETE FROM reading_progress rp
USING stories s
WHERE rp.story_id=s.id AND s.slug IN ('the-fox-and-the-grapes','the-little-star-and-the-moon','frankenstein-excerpt-ch1');

DELETE FROM story_segments seg
USING story_versions sv, stories s
WHERE seg.story_version_id=sv.id AND sv.story_id=s.id
  AND s.slug IN ('the-fox-and-the-grapes','the-little-star-and-the-moon','frankenstein-excerpt-ch1');

DELETE FROM story_sections sec
USING story_versions sv, stories s
WHERE sec.story_version_id=sv.id AND sv.story_id=s.id
  AND s.slug IN ('frankenstein-excerpt-ch1');

DELETE FROM story_versions sv
USING stories s
WHERE sv.story_id=s.id AND s.slug IN ('the-fox-and-the-grapes','the-little-star-and-the-moon','frankenstein-excerpt-ch1');

DELETE FROM story_contributors sc
USING stories s
WHERE sc.story_id=s.id AND s.slug IN ('the-fox-and-the-grapes','frankenstein-excerpt-ch1');

DELETE FROM stories
WHERE slug IN ('the-fox-and-the-grapes','the-little-star-and-the-moon','frankenstein-excerpt-ch1');

DELETE FROM prompt_profiles WHERE name='Bedtime calm v1';
DELETE FROM child_profiles WHERE name='Ted';

DELETE FROM works WHERE canonical_title IN (
  'The Fox and the Grapes',
  'The Little Star and the Moon',
  'Frankenstein; or, The Modern Prometheus'
);

DELETE FROM contributors WHERE name IN ('Aesop','Mary Shelley');

COMMIT;
