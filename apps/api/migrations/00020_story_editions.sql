-- +goose Up
BEGIN;

-- Story editions become the lifecycle boundary between one canonical Panda
-- Pages story identity and its immutable reading versions. Existing Reader and
-- Story Studio contracts remain on the story-level Classic compatibility
-- pointers during this migration.
LOCK TABLE stories, story_versions IN SHARE ROW EXCLUSIVE MODE;

-- Existing draft/published pointers must already belong to their story before
-- they can be copied into edition-owned composite foreign keys. Fail closed;
-- never repair, clear, or reassign an ambiguous historical pointer here.
-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM stories AS story
    LEFT JOIN story_versions AS version
      ON version.id = story.draft_version_id
    WHERE story.draft_version_id IS NOT NULL
      AND (version.id IS NULL OR version.story_id <> story.id)
  ) THEN
    RAISE EXCEPTION 'story edition preflight failed: draft pointer belongs to another story or is missing'
      USING ERRCODE = '23514';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM stories AS story
    LEFT JOIN story_versions AS version
      ON version.id = story.published_version_id
    WHERE story.published_version_id IS NOT NULL
      AND (version.id IS NULL OR version.story_id <> story.id)
  ) THEN
    RAISE EXCEPTION 'story edition preflight failed: published pointer belongs to another story or is missing'
      USING ERRCODE = '23514';
  END IF;
END
$$;
-- +goose StatementEnd

CREATE TABLE story_editions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  story_id uuid NOT NULL,
  edition_key text NOT NULL,
  draft_version_id uuid,
  published_version_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT story_editions_story_id_fkey
    FOREIGN KEY (story_id) REFERENCES stories(id)
    ON UPDATE NO ACTION ON DELETE CASCADE,

  CONSTRAINT story_editions_edition_key_check
    CHECK (
      edition_key IN (
        'classic',
        'confident-readers',
        'growing-readers',
        'story-explorers',
        'little-listeners'
      )
    ),

  CONSTRAINT story_editions_story_key_key
    UNIQUE (story_id, edition_key),

  CONSTRAINT story_editions_id_story_id_key
    UNIQUE (id, story_id)
);

INSERT INTO story_editions (
  story_id,
  edition_key,
  draft_version_id,
  published_version_id,
  created_at,
  updated_at
)
SELECT
  id,
  'classic',
  draft_version_id,
  published_version_id,
  created_at,
  updated_at
FROM stories;

ALTER TABLE story_versions
  ADD COLUMN edition_id uuid;

UPDATE story_versions AS version
SET edition_id = edition.id
FROM story_editions AS edition
WHERE edition.story_id = version.story_id
  AND edition.edition_key = 'classic';

ALTER TABLE story_versions
  ADD CONSTRAINT story_versions_edition_id_not_null_check
  CHECK (edition_id IS NOT NULL) NOT VALID;

ALTER TABLE story_versions
  VALIDATE CONSTRAINT story_versions_edition_id_not_null_check;

ALTER TABLE story_versions
  ALTER COLUMN edition_id SET NOT NULL;

ALTER TABLE story_versions
  DROP CONSTRAINT story_versions_edition_id_not_null_check;

ALTER TABLE story_versions
  ADD CONSTRAINT story_versions_id_edition_id_key
    UNIQUE (id, edition_id),
  ADD CONSTRAINT story_versions_edition_story_fkey
    FOREIGN KEY (edition_id, story_id)
    REFERENCES story_editions(id, story_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION
    NOT VALID;

ALTER TABLE story_versions
  VALIDATE CONSTRAINT story_versions_edition_story_fkey;

-- Content identity is edition-scoped. Version numbers deliberately remain
-- story-scoped for the compatibility phase so existing slug+version Reader and
-- progress contracts stay unambiguous.
ALTER TABLE story_versions
  DROP CONSTRAINT story_versions_story_id_content_hash_key;

ALTER TABLE story_versions
  ADD CONSTRAINT story_versions_edition_id_content_hash_key
  UNIQUE (edition_id, content_hash);

-- Edition pointers can only target a version owned by that exact edition.
ALTER TABLE story_editions
  ADD CONSTRAINT story_editions_draft_version_fkey
    FOREIGN KEY (draft_version_id, id)
    REFERENCES story_versions(id, edition_id)
    ON UPDATE NO ACTION ON DELETE SET NULL (draft_version_id)
    NOT VALID,
  ADD CONSTRAINT story_editions_published_version_fkey
    FOREIGN KEY (published_version_id, id)
    REFERENCES story_versions(id, edition_id)
    ON UPDATE NO ACTION ON DELETE SET NULL (published_version_id)
    NOT VALID;

ALTER TABLE story_editions
  VALIDATE CONSTRAINT story_editions_draft_version_fkey;

ALTER TABLE story_editions
  VALIDATE CONSTRAINT story_editions_published_version_fkey;

-- Strengthen the temporary story-level compatibility pointers at the same
-- boundary. They may now reference only versions belonging to their story.
ALTER TABLE stories
  DROP CONSTRAINT fk_stories_draft_version,
  DROP CONSTRAINT fk_stories_published_version;

ALTER TABLE stories
  ADD CONSTRAINT fk_stories_draft_version
    FOREIGN KEY (draft_version_id, id)
    REFERENCES story_versions(id, story_id)
    ON UPDATE NO ACTION ON DELETE SET NULL (draft_version_id),
  ADD CONSTRAINT fk_stories_published_version
    FOREIGN KEY (published_version_id, id)
    REFERENCES story_versions(id, story_id)
    ON UPDATE NO ACTION ON DELETE SET NULL (published_version_id);

CREATE INDEX story_editions_story_updated_idx
  ON story_editions (story_id, updated_at DESC);

CREATE INDEX story_editions_draft_version_idx
  ON story_editions (draft_version_id)
  WHERE draft_version_id IS NOT NULL;

CREATE INDEX story_editions_published_version_idx
  ON story_editions (published_version_id)
  WHERE published_version_id IS NOT NULL;

CREATE INDEX story_versions_edition_version_idx
  ON story_versions (edition_id, version DESC);

COMMIT;

-- +goose Down
BEGIN;

LOCK TABLE stories, story_editions, story_versions IN SHARE ROW EXCLUSIVE MODE;

-- Migration 20 is reversible only while this compatibility release still owns
-- Classic-only edition data. Refuse to silently discard a later lifecycle.
-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM story_editions
    WHERE edition_key <> 'classic'
  ) THEN
    RAISE EXCEPTION 'story edition rollback refused: non-Classic editions exist'
      USING ERRCODE = '23514';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM story_versions AS version
    JOIN story_editions AS edition ON edition.id = version.edition_id
    WHERE edition.edition_key <> 'classic'
  ) THEN
    RAISE EXCEPTION 'story edition rollback refused: a version belongs to a non-Classic edition'
      USING ERRCODE = '23514';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM stories AS story
    LEFT JOIN story_editions AS edition
      ON edition.story_id = story.id
     AND edition.edition_key = 'classic'
    WHERE edition.id IS NULL
       OR edition.draft_version_id IS DISTINCT FROM story.draft_version_id
       OR edition.published_version_id IS DISTINCT FROM story.published_version_id
  ) THEN
    RAISE EXCEPTION 'story edition rollback refused: Classic and compatibility pointers differ'
      USING ERRCODE = '23514';
  END IF;
END
$$;
-- +goose StatementEnd

DROP INDEX story_versions_edition_version_idx;
DROP INDEX story_editions_published_version_idx;
DROP INDEX story_editions_draft_version_idx;
DROP INDEX story_editions_story_updated_idx;

ALTER TABLE stories
  DROP CONSTRAINT fk_stories_draft_version,
  DROP CONSTRAINT fk_stories_published_version;

ALTER TABLE stories
  ADD CONSTRAINT fk_stories_draft_version
    FOREIGN KEY (draft_version_id) REFERENCES story_versions(id)
    ON UPDATE NO ACTION ON DELETE SET NULL,
  ADD CONSTRAINT fk_stories_published_version
    FOREIGN KEY (published_version_id) REFERENCES story_versions(id)
    ON UPDATE NO ACTION ON DELETE SET NULL;

ALTER TABLE story_editions
  DROP CONSTRAINT story_editions_draft_version_fkey,
  DROP CONSTRAINT story_editions_published_version_fkey;

ALTER TABLE story_versions
  DROP CONSTRAINT story_versions_edition_story_fkey,
  DROP CONSTRAINT story_versions_id_edition_id_key,
  DROP CONSTRAINT story_versions_edition_id_content_hash_key;

ALTER TABLE story_versions
  ADD CONSTRAINT story_versions_story_id_content_hash_key
  UNIQUE (story_id, content_hash);

ALTER TABLE story_versions
  DROP COLUMN edition_id;

DROP TABLE story_editions;

COMMIT;
