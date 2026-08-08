-- +goose Up
BEGIN;

-- A Panda Pages story has one canonical source identity whose immutable
-- revisions are independent from every adapted reading edition. Existing
-- stories are intentionally NOT backfilled from Classic: an adaptation is not
-- evidence of the original/public-domain source.
LOCK TABLE stories IN SHARE ROW EXCLUSIVE MODE;

-- The old works/work_id model was unused scaffolding and would create a second,
-- competing notion of canonical identity. Remove it only when it is provably
-- empty; never guess how historical data should map into the source lifecycle.
-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM stories WHERE work_id IS NOT NULL)
     OR EXISTS (SELECT 1 FROM works) THEN
    RAISE EXCEPTION 'canonical source migration refused: legacy works scaffolding contains data'
      USING ERRCODE = '23514';
  END IF;
END
$$;
-- +goose StatementEnd

CREATE TABLE story_sources (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  story_id uuid NOT NULL,
  current_version_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT story_sources_story_id_fkey
    FOREIGN KEY (story_id) REFERENCES stories(id)
    ON UPDATE NO ACTION ON DELETE CASCADE,

  CONSTRAINT story_sources_story_id_key
    UNIQUE (story_id),

  CONSTRAINT story_sources_id_story_id_key
    UNIQUE (id, story_id)
);

CREATE TABLE story_source_versions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  source_id uuid NOT NULL,
  story_id uuid NOT NULL,
  version integer NOT NULL,
  title text NOT NULL,
  author text,
  language text NOT NULL,
  rights jsonb NOT NULL DEFAULT '{}'::jsonb,
  source_url text,
  source_text text NOT NULL,
  snapshot_hash text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT story_source_versions_source_story_fkey
    FOREIGN KEY (source_id, story_id)
    REFERENCES story_sources(id, story_id)
    ON UPDATE NO ACTION ON DELETE CASCADE,

  CONSTRAINT story_source_versions_version_check
    CHECK (version > 0),

  CONSTRAINT story_source_versions_title_check
    CHECK (title = btrim(title) AND title <> ''),

  CONSTRAINT story_source_versions_author_check
    CHECK (author IS NULL OR (author = btrim(author) AND author <> '')),

  CONSTRAINT story_source_versions_language_check
    CHECK (language = btrim(language) AND language <> ''),

  CONSTRAINT story_source_versions_source_url_check
    CHECK (source_url IS NULL OR (source_url = btrim(source_url) AND source_url <> '')),

  CONSTRAINT story_source_versions_source_text_check
    CHECK (btrim(source_text) <> ''),

  CONSTRAINT story_source_versions_snapshot_hash_check
    CHECK (snapshot_hash ~ '^[0-9a-f]{64}$'),

  CONSTRAINT story_source_versions_source_version_key
    UNIQUE (source_id, version),

  CONSTRAINT story_source_versions_source_snapshot_hash_key
    UNIQUE (source_id, snapshot_hash),

  CONSTRAINT story_source_versions_id_source_id_key
    UNIQUE (id, source_id)
);

ALTER TABLE story_sources
  ADD CONSTRAINT story_sources_current_version_fkey
    FOREIGN KEY (current_version_id, id)
    REFERENCES story_source_versions(id, source_id)
    ON UPDATE NO ACTION ON DELETE SET NULL (current_version_id);

CREATE INDEX story_source_versions_source_created_idx
  ON story_source_versions (source_id, version DESC, created_at DESC);

CREATE INDEX story_sources_current_version_idx
  ON story_sources (current_version_id)
  WHERE current_version_id IS NOT NULL;

-- Retire the unused Work model after the empty-data preflight above.
DROP INDEX idx_stories_work_id;
ALTER TABLE stories DROP CONSTRAINT fk_stories_work;
ALTER TABLE stories DROP COLUMN work_id;
DROP INDEX idx_works_canonical_title;
DROP TABLE works;

COMMIT;

-- +goose Down
BEGIN;

LOCK TABLE stories, story_sources, story_source_versions IN SHARE ROW EXCLUSIVE MODE;

-- Once an explicit canonical source has been stored, silently dropping that
-- provenance is not a truthful rollback.
-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM story_source_versions)
     OR EXISTS (SELECT 1 FROM story_sources) THEN
    RAISE EXCEPTION 'canonical source rollback refused: source data exists'
      USING ERRCODE = '23514';
  END IF;
END
$$;
-- +goose StatementEnd

DROP INDEX story_sources_current_version_idx;
DROP INDEX story_source_versions_source_created_idx;
ALTER TABLE story_sources DROP CONSTRAINT story_sources_current_version_fkey;
DROP TABLE story_source_versions;
DROP TABLE story_sources;

-- Restore only the empty historical shape required by migration ordering.
CREATE TABLE works (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  canonical_title text NOT NULL,
  description text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_works_canonical_title ON works(canonical_title);

ALTER TABLE stories ADD COLUMN work_id uuid;

ALTER TABLE stories
  ADD CONSTRAINT fk_stories_work
  FOREIGN KEY (work_id) REFERENCES works(id)
  ON DELETE SET NULL;

CREATE INDEX idx_stories_work_id ON stories(work_id);

COMMIT;
