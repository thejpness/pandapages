-- +goose Up
BEGIN;

-- stories.source predates the immutable canonical-source and edition-version
-- models. Lock the table so the empty-data decision and column removal are one
-- race-free operation.
LOCK TABLE stories IN ACCESS EXCLUSIVE MODE;

-- +goose StatementBegin
DO $$
DECLARE
  nonempty_story_count bigint;
BEGIN
  SELECT count(*)
  INTO nonempty_story_count
  FROM stories
  WHERE source <> '{}'::jsonb;

  IF nonempty_story_count <> 0 THEN
    RAISE EXCEPTION
      'legacy story source retirement refused: nonempty_stories=%',
      nonempty_story_count
      USING ERRCODE = '23514';
  END IF;
END
$$;
-- +goose StatementEnd

-- No CASCADE and no IF EXISTS are intentional. Unexpected schema drift or a
-- dependency on the retired column must stop the migration.
ALTER TABLE stories DROP COLUMN source;

COMMIT;

-- +goose Down
BEGIN;

-- Up proves that every removed value was exactly {}. Restoring the historical
-- empty column is therefore a truthful rollback and does not fabricate data.
ALTER TABLE stories
  ADD COLUMN source jsonb NOT NULL DEFAULT '{}'::jsonb;

COMMIT;
