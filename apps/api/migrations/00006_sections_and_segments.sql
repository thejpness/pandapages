-- +goose Up

-- Logical structure (chapters/parts/sections) for a specific story_version
CREATE TABLE IF NOT EXISTS story_sections (
  id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  story_version_id UUID NOT NULL REFERENCES story_versions(id) ON DELETE CASCADE,
  kind             TEXT NOT NULL,  -- part|chapter|section
  title            TEXT,
  ordinal          INT NOT NULL,   -- 1..N within the version
  start_offset     INT,            -- optional: char offset into canonical markdown
  end_offset       INT,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(story_version_id, ordinal)
);

CREATE INDEX IF NOT EXISTS idx_sections_version_ordinal
  ON story_sections(story_version_id, ordinal);

-- Reader-friendly chunks (fast paging / offline / progress anchors)
CREATE TABLE IF NOT EXISTS story_segments (
  id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  story_version_id UUID NOT NULL REFERENCES story_versions(id) ON DELETE CASCADE,
  section_id       UUID NULL REFERENCES story_sections(id) ON DELETE SET NULL,
  ordinal          INT NOT NULL,         -- reading order across whole version
  locator          JSONB NOT NULL,       -- {"type":"offset","start":123,"end":456} or {"type":"heading","h":2,"index":5}
  markdown         TEXT NOT NULL,
  rendered_html    TEXT NOT NULL,
  word_count       INT NOT NULL DEFAULT 0,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(story_version_id, ordinal)
);

CREATE INDEX IF NOT EXISTS idx_segments_version_ordinal
  ON story_segments(story_version_id, ordinal);

CREATE INDEX IF NOT EXISTS idx_segments_section_ordinal
  ON story_segments(section_id, ordinal);

-- +goose Down

DROP INDEX IF EXISTS idx_segments_section_ordinal;
DROP INDEX IF EXISTS idx_segments_version_ordinal;
DROP TABLE IF EXISTS story_segments;

DROP INDEX IF EXISTS idx_sections_version_ordinal;
DROP TABLE IF EXISTS story_sections;
