-- +goose Up

CREATE TABLE IF NOT EXISTS contributors (
  id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name      TEXT NOT NULL,
  sort_name TEXT
);

CREATE INDEX IF NOT EXISTS idx_contributors_name ON contributors(name);

CREATE TABLE IF NOT EXISTS story_contributors (
  story_id       UUID NOT NULL REFERENCES stories(id) ON DELETE CASCADE,
  contributor_id UUID NOT NULL REFERENCES contributors(id) ON DELETE CASCADE,
  role           TEXT NOT NULL, -- author|translator|editor|illustrator
  PRIMARY KEY (story_id, contributor_id, role)
);

CREATE INDEX IF NOT EXISTS idx_story_contrib_story ON story_contributors(story_id);
CREATE INDEX IF NOT EXISTS idx_story_contrib_contributor ON story_contributors(contributor_id);

-- +goose Down

DROP INDEX IF EXISTS idx_story_contrib_contributor;
DROP INDEX IF EXISTS idx_story_contrib_story;

DROP TABLE IF EXISTS story_contributors;

DROP INDEX IF EXISTS idx_contributors_name;
DROP TABLE IF EXISTS contributors;
