\set ON_ERROR_STOP on

BEGIN;

DO $$
BEGIN
  IF current_database() <> 'pandapages' THEN
    RAISE EXCEPTION 'test fixtures require the pandapages database';
  END IF;

  IF NOT EXISTS (SELECT 1 FROM accounts) THEN
    RAISE EXCEPTION 'test fixtures require a migrated default account';
  END IF;

  IF EXISTS (
    SELECT 1 FROM child_profiles
    WHERE id = 'f17e0000-0000-4000-8000-000000000001'
      AND name IS DISTINCT FROM 'TEST ONLY — Reader child'
  ) THEN
    RAISE EXCEPTION 'fixed child-profile fixture ID is already in unrelated use';
  END IF;

  IF EXISTS (
    SELECT 1 FROM prompt_profiles
    WHERE id = 'f17e0000-0000-4000-8000-000000000002'
      AND name <> 'TEST ONLY — Reader prompt'
  ) THEN
    RAISE EXCEPTION 'fixed prompt-profile fixture ID is already in unrelated use';
  END IF;

  IF EXISTS (
    SELECT 1 FROM works
    WHERE id = 'f17e0000-0000-4000-8000-000000000003'
      AND canonical_title <> 'TEST ONLY — Moonlit Café'
  ) THEN
    RAISE EXCEPTION 'fixed work fixture ID is already in unrelated use';
  END IF;

  IF EXISTS (
    SELECT 1 FROM contributors
    WHERE id = 'f17e0000-0000-4000-8000-000000000004'
      AND name <> 'Panda Pages Test Fixture'
  ) THEN
    RAISE EXCEPTION 'fixed contributor fixture ID is already in unrelated use';
  END IF;

  IF EXISTS (
    SELECT 1 FROM stories
    WHERE id = 'f17e0000-0000-4000-8000-000000000010'
      AND (
        slug <> 'test-only-moonlit-cafe'
        OR source->>'origin' IS DISTINCT FROM 'explicit-test-seed'
        OR source->>'test_fixture' IS DISTINCT FROM 'true'
      )
  ) THEN
    RAISE EXCEPTION 'fixed story fixture ID is already in unrelated use';
  END IF;

  IF EXISTS (
    SELECT 1 FROM story_versions
    WHERE id = 'f17e0000-0000-4000-8000-000000000011'
      AND story_id <> 'f17e0000-0000-4000-8000-000000000010'
  ) THEN
    RAISE EXCEPTION 'fixed story-version fixture ID is already in unrelated use';
  END IF;

  IF EXISTS (
    SELECT 1 FROM generation_jobs
    WHERE id = 'f17e0000-0000-4000-8000-000000000040'
      AND model <> 'test-only-seed-model'
  ) THEN
    RAISE EXCEPTION 'fixed generation-job fixture ID is already in unrelated use';
  END IF;
END
$$;

WITH target_account AS (
  SELECT id
  FROM accounts
  ORDER BY created_at ASC, id ASC
  LIMIT 1
)
INSERT INTO child_profiles (
  id, account_id, name, age_months, interests, sensitivities
)
SELECT
  'f17e0000-0000-4000-8000-000000000001',
  target_account.id,
  'TEST ONLY — Reader child',
  84,
  '["pandas","cafés","世界"]'::jsonb,
  '["test-only"]'::jsonb
FROM target_account
ON CONFLICT (id) DO UPDATE SET
  account_id = EXCLUDED.account_id,
  name = EXCLUDED.name,
  age_months = EXCLUDED.age_months,
  interests = EXCLUDED.interests,
  sensitivities = EXCLUDED.sensitivities,
  updated_at = now();

WITH target_account AS (
  SELECT id
  FROM accounts
  ORDER BY created_at ASC, id ASC
  LIMIT 1
)
INSERT INTO prompt_profiles (
  id, account_id, name, rules, schema_version
)
SELECT
  'f17e0000-0000-4000-8000-000000000002',
  target_account.id,
  'TEST ONLY — Reader prompt',
  '{"test_fixture":true,"tone":"warm UTF-8 coverage"}'::jsonb,
  1
FROM target_account
ON CONFLICT (id) DO UPDATE SET
  account_id = EXCLUDED.account_id,
  name = EXCLUDED.name,
  rules = EXCLUDED.rules,
  schema_version = EXCLUDED.schema_version,
  updated_at = now();

INSERT INTO works (id, canonical_title, description)
VALUES (
  'f17e0000-0000-4000-8000-000000000003',
  'TEST ONLY — Moonlit Café',
  'Deterministic local/disposable Reader fixture; never production content.'
)
ON CONFLICT (id) DO UPDATE SET
  canonical_title = EXCLUDED.canonical_title,
  description = EXCLUDED.description,
  updated_at = now();

INSERT INTO contributors (id, name, sort_name)
VALUES (
  'f17e0000-0000-4000-8000-000000000004',
  'Panda Pages Test Fixture',
  'Test Fixture, Panda Pages'
)
ON CONFLICT (id) DO UPDATE SET
  name = EXCLUDED.name,
  sort_name = EXCLUDED.sort_name;

WITH target_account AS (
  SELECT id
  FROM accounts
  ORDER BY created_at ASC, id ASC
  LIMIT 1
)
INSERT INTO stories (
  id, account_id, slug, title, author, is_published, language, rights, source, work_id
)
SELECT
  'f17e0000-0000-4000-8000-000000000010',
  target_account.id,
  'test-only-moonlit-cafe',
  'TEST ONLY — Moonlit Café',
  'Panda Pages Test Fixture',
  false,
  'en-GB',
  '{"license":"test-only","test_fixture":true}'::jsonb,
  '{"origin":"explicit-test-seed","test_fixture":true}'::jsonb,
  'f17e0000-0000-4000-8000-000000000003'
FROM target_account
ON CONFLICT (id) DO UPDATE SET
  account_id = EXCLUDED.account_id,
  slug = EXCLUDED.slug,
  title = EXCLUDED.title,
  author = EXCLUDED.author,
  language = EXCLUDED.language,
  rights = EXCLUDED.rights,
  source = EXCLUDED.source,
  work_id = EXCLUDED.work_id,
  updated_at = now();

INSERT INTO story_contributors (story_id, contributor_id, role)
VALUES (
  'f17e0000-0000-4000-8000-000000000010',
  'f17e0000-0000-4000-8000-000000000004',
  'author'
)
ON CONFLICT (story_id, contributor_id, role) DO NOTHING;

INSERT INTO story_versions (
  id, story_id, version, frontmatter, markdown, rendered_html, content_hash
)
VALUES (
  'f17e0000-0000-4000-8000-000000000011',
  'f17e0000-0000-4000-8000-000000000010',
  1,
  '{"tags":["test-only","reader","utf-8"],"test_fixture":true}'::jsonb,
  E'# TEST ONLY — Moonlit Café\n\nPöndá carried a lantern past the café window.\n\n## Chapter One — Lanterns\n\n“Ready?” asked Pöndá. The moon replied, “Oui — allons-y!”\n\n## Chapter Two — 世界\n\n星の光 shimmered over the quiet water. 🐼\n',
  '<h1>TEST ONLY — Moonlit Café</h1><p>Pöndá carried a lantern past the café window.</p><h2>Chapter One — Lanterns</h2><p>“Ready?” asked Pöndá. The moon replied, “Oui — allons-y!”</p><h2>Chapter Two — 世界</h2><p>星の光 shimmered over the quiet water. 🐼</p>',
  encode(digest(
    E'# TEST ONLY — Moonlit Café\n\nPöndá carried a lantern past the café window.\n\n## Chapter One — Lanterns\n\n“Ready?” asked Pöndá. The moon replied, “Oui — allons-y!”\n\n## Chapter Two — 世界\n\n星の光 shimmered over the quiet water. 🐼\n',
    'sha256'
  ), 'hex')
)
ON CONFLICT (id) DO UPDATE SET
  story_id = EXCLUDED.story_id,
  version = EXCLUDED.version,
  frontmatter = EXCLUDED.frontmatter,
  markdown = EXCLUDED.markdown,
  rendered_html = EXCLUDED.rendered_html,
  content_hash = EXCLUDED.content_hash;

DELETE FROM story_segments
WHERE story_version_id = 'f17e0000-0000-4000-8000-000000000011';

DELETE FROM story_sections
WHERE story_version_id = 'f17e0000-0000-4000-8000-000000000011';

INSERT INTO story_sections (id, story_version_id, kind, title, ordinal)
VALUES
  (
    'f17e0000-0000-4000-8000-000000000020',
    'f17e0000-0000-4000-8000-000000000011',
    'chapter',
    'Chapter One — Lanterns',
    1
  ),
  (
    'f17e0000-0000-4000-8000-000000000021',
    'f17e0000-0000-4000-8000-000000000011',
    'chapter',
    'Chapter Two — 世界',
    2
  );

INSERT INTO story_segments (
  id, story_version_id, section_id, ordinal, locator, markdown, rendered_html, word_count
)
VALUES
  (
    'f17e0000-0000-4000-8000-000000000030',
    'f17e0000-0000-4000-8000-000000000011',
    NULL,
    1,
    '{"type":"heading","h":1,"index":0}'::jsonb,
    '# TEST ONLY — Moonlit Café',
    '<h1>TEST ONLY — Moonlit Café</h1>',
    5
  ),
  (
    'f17e0000-0000-4000-8000-000000000031',
    'f17e0000-0000-4000-8000-000000000011',
    NULL,
    2,
    '{"type":"para","n":1}'::jsonb,
    'Pöndá carried a lantern past the café window.',
    '<p>Pöndá carried a lantern past the café window.</p>',
    8
  ),
  (
    'f17e0000-0000-4000-8000-000000000032',
    'f17e0000-0000-4000-8000-000000000011',
    'f17e0000-0000-4000-8000-000000000020',
    3,
    '{"type":"heading","h":2,"index":0}'::jsonb,
    '## Chapter One — Lanterns',
    '<h2>Chapter One — Lanterns</h2>',
    4
  ),
  (
    'f17e0000-0000-4000-8000-000000000033',
    'f17e0000-0000-4000-8000-000000000011',
    'f17e0000-0000-4000-8000-000000000020',
    4,
    '{"type":"para","n":2}'::jsonb,
    '“Ready?” asked Pöndá. The moon replied, “Oui — allons-y!”',
    '<p>“Ready?” asked Pöndá. The moon replied, “Oui — allons-y!”</p>',
    9
  ),
  (
    'f17e0000-0000-4000-8000-000000000034',
    'f17e0000-0000-4000-8000-000000000011',
    'f17e0000-0000-4000-8000-000000000021',
    5,
    '{"type":"heading","h":2,"index":1}'::jsonb,
    E'## Chapter Two — 世界\n\n星の光 shimmered over the quiet water. 🐼',
    '<h2>Chapter Two — 世界</h2><p>星の光 shimmered over the quiet water. 🐼</p>',
    10
  );

UPDATE stories
SET published_version_id = 'f17e0000-0000-4000-8000-000000000011',
    draft_version_id = 'f17e0000-0000-4000-8000-000000000011',
    is_published = true,
    updated_at = now()
WHERE id = 'f17e0000-0000-4000-8000-000000000010';

INSERT INTO generation_jobs (
  id, status, story_id, story_version_id, child_profile_id, prompt_profile_id,
  theme, request_payload, response_payload, model, prompt_version, tokens_in, tokens_out
)
VALUES (
  'f17e0000-0000-4000-8000-000000000040',
  'succeeded'::generation_status,
  'f17e0000-0000-4000-8000-000000000010',
  'f17e0000-0000-4000-8000-000000000011',
  'f17e0000-0000-4000-8000-000000000001',
  'f17e0000-0000-4000-8000-000000000002',
  'TEST ONLY — UTF-8 Reader fixture',
  '{"test_fixture":true}'::jsonb,
  '{"test_fixture":true,"result":"deterministic"}'::jsonb,
  'test-only-seed-model',
  'fixture-v1',
  10,
  20
)
ON CONFLICT (id) DO UPDATE SET
  status = EXCLUDED.status,
  story_id = EXCLUDED.story_id,
  story_version_id = EXCLUDED.story_version_id,
  child_profile_id = EXCLUDED.child_profile_id,
  prompt_profile_id = EXCLUDED.prompt_profile_id,
  theme = EXCLUDED.theme,
  request_payload = EXCLUDED.request_payload,
  response_payload = EXCLUDED.response_payload,
  model = EXCLUDED.model,
  prompt_version = EXCLUDED.prompt_version,
  tokens_in = EXCLUDED.tokens_in,
  tokens_out = EXCLUDED.tokens_out,
  updated_at = now();

COMMIT;
