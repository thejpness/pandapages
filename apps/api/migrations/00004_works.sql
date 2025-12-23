-- +goose Up

CREATE TABLE IF NOT EXISTS works (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  canonical_title TEXT NOT NULL,
  description     TEXT,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_works_canonical_title ON works(canonical_title);

ALTER TABLE stories
  ADD COLUMN IF NOT EXISTS work_id UUID NULL;

-- +goose StatementBegin
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_stories_work') THEN
    ALTER TABLE stories
      ADD CONSTRAINT fk_stories_work
      FOREIGN KEY (work_id) REFERENCES works(id)
      ON DELETE SET NULL;
  END IF;
END
$$;
-- +goose StatementEnd

CREATE INDEX IF NOT EXISTS idx_stories_work_id ON stories(work_id);

-- +goose Down

DROP INDEX IF EXISTS idx_stories_work_id;
ALTER TABLE stories DROP CONSTRAINT IF EXISTS fk_stories_work;
ALTER TABLE stories DROP COLUMN IF EXISTS work_id;

DROP INDEX IF EXISTS idx_works_canonical_title;
DROP TABLE IF EXISTS works;
