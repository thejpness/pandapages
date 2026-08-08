-- +goose Up
BEGIN;

LOCK TABLE stories, story_editions, story_versions IN SHARE ROW EXCLUSIVE MODE;

-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM stories AS story
    LEFT JOIN story_editions AS classic
      ON classic.story_id = story.id
     AND classic.edition_key = 'classic'
    WHERE story.is_published IS DISTINCT FROM (story.published_version_id IS NOT NULL)
       OR classic.published_version_id IS DISTINCT FROM story.published_version_id
  ) THEN
    RAISE EXCEPTION 'story release migration refused: Classic publication compatibility state is inconsistent';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM story_editions
    WHERE edition_key <> 'classic'
      AND published_version_id IS NOT NULL
  ) THEN
    RAISE EXCEPTION 'story release migration refused: non-Classic publication state already exists';
  END IF;
END
$$;
-- +goose StatementEnd

CREATE TABLE story_releases (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  story_id uuid NOT NULL,
  release_number integer NOT NULL CHECK (release_number > 0),
  migration_backfill boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT story_releases_story_fkey
    FOREIGN KEY (story_id)
    REFERENCES stories(id)
    ON DELETE CASCADE,
  CONSTRAINT story_releases_story_number_key
    UNIQUE (story_id, release_number),
  CONSTRAINT story_releases_id_story_key
    UNIQUE (id, story_id)
);

CREATE TABLE story_release_editions (
  release_id uuid NOT NULL,
  story_id uuid NOT NULL,
  edition_id uuid NOT NULL,
  story_version_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT story_release_editions_pkey
    PRIMARY KEY (release_id, edition_id),
  CONSTRAINT story_release_editions_release_version_key
    UNIQUE (release_id, story_version_id),
  CONSTRAINT story_release_editions_release_story_fkey
    FOREIGN KEY (release_id, story_id)
    REFERENCES story_releases(id, story_id)
    ON DELETE CASCADE,
  CONSTRAINT story_release_editions_edition_story_fkey
    FOREIGN KEY (edition_id, story_id)
    REFERENCES story_editions(id, story_id)
    DEFERRABLE INITIALLY DEFERRED,
  CONSTRAINT story_release_editions_version_edition_fkey
    FOREIGN KEY (story_version_id, edition_id)
    REFERENCES story_versions(id, edition_id)
    DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX story_releases_story_created_idx
  ON story_releases (story_id, release_number DESC);

CREATE INDEX story_release_editions_story_version_idx
  ON story_release_editions (story_id, story_version_id);

ALTER TABLE stories
  ADD COLUMN current_release_id uuid;

ALTER TABLE stories
  ADD CONSTRAINT stories_current_release_story_fkey
  FOREIGN KEY (current_release_id, id)
  REFERENCES story_releases(id, story_id)
  ON DELETE SET NULL (current_release_id);

CREATE INDEX stories_current_release_idx
  ON stories (current_release_id)
  WHERE current_release_id IS NOT NULL;

-- A legacy Classic publication is already a legitimate one-edition release.
-- Preserve Reader behaviour while establishing the new release authority.
INSERT INTO story_releases (
  story_id,
  release_number,
  migration_backfill,
  created_at
)
SELECT
  story.id,
  1,
  true,
  story.updated_at
FROM stories AS story
WHERE story.is_published;

INSERT INTO story_release_editions (
  release_id,
  story_id,
  edition_id,
  story_version_id,
  created_at
)
SELECT
  release.id,
  story.id,
  edition.id,
  story.published_version_id,
  release.created_at
FROM stories AS story
JOIN story_releases AS release
  ON release.story_id = story.id
 AND release.release_number = 1
 AND release.migration_backfill
JOIN story_editions AS edition
  ON edition.story_id = story.id
 AND edition.edition_key = 'classic'
WHERE story.is_published;

UPDATE stories AS story
SET current_release_id = release.id
FROM story_releases AS release
WHERE release.story_id = story.id
  AND release.release_number = 1
  AND release.migration_backfill
  AND story.is_published;

COMMIT;

-- +goose Down
BEGIN;

LOCK TABLE stories, story_releases, story_release_editions IN SHARE ROW EXCLUSIVE MODE;

-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM story_releases
    WHERE NOT migration_backfill
  ) THEN
    RAISE EXCEPTION 'story release rollback refused: application release history exists';
  END IF;
END
$$;
-- +goose StatementEnd

ALTER TABLE stories
  DROP CONSTRAINT stories_current_release_story_fkey;

DROP INDEX stories_current_release_idx;

ALTER TABLE stories
  DROP COLUMN current_release_id;

DROP TABLE story_release_editions;
DROP TABLE story_releases;

COMMIT;
