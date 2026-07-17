-- +goose Up
BEGIN;

-- Reader 2 is an intentional beta clean break. Reader 1 locators are not
-- translated: every historical progress row is discarded before the strict
-- Locator v2 constraint is installed. A Down migration cannot recover it.
DELETE FROM reading_progress;

ALTER TABLE story_segments
  ADD COLUMN segment_kind TEXT,
  ADD COLUMN heading_level INTEGER,
  ADD COLUMN content_key TEXT,
  ADD COLUMN content_occurrence INTEGER,
  ADD COLUMN chapter_key TEXT,
  ADD COLUMN chapter_occurrence INTEGER;

-- Migration 00006 stored block kind in locator JSON. Classify defensively so
-- every existing segment receives a deterministic identity, including rows
-- whose legacy metadata is incomplete.
WITH classified AS (
  SELECT
    segment.id,
    segment.story_version_id,
    segment.ordinal,
    segment.markdown,
    CASE
      WHEN segment.locator->>'type' = 'heading'
        AND segment.locator->>'h' ~ '^[1-6]$'
        THEN 'heading'
      WHEN segment.markdown ~ '^#{1,6}([[:space:]]|$)'
        THEN 'heading'
      WHEN segment.locator->>'type' = 'para'
        OR btrim(segment.rendered_html) ~* '^<p([[:space:]>])'
        THEN 'paragraph'
      ELSE 'other'
    END AS segment_kind,
    CASE
      WHEN segment.locator->>'type' = 'heading'
        AND segment.locator->>'h' ~ '^[1-6]$'
        THEN (segment.locator->>'h')::integer
      WHEN segment.markdown ~ '^#{1,6}([[:space:]]|$)'
        THEN char_length(substring(segment.markdown FROM '^(#{1,6})'))
      ELSE NULL
    END AS heading_level
  FROM story_segments AS segment
), keyed AS (
  SELECT
    classified.*,
    encode(
      digest(
        classified.segment_kind
          || chr(31)
          || COALESCE(classified.heading_level, 0)::text
          || chr(31)
          || replace(replace(classified.markdown, E'\r\n', E'\n'), E'\r', E'\n'),
        'sha256'
      ),
      'hex'
    ) AS content_key
  FROM classified
), numbered AS (
  SELECT
    keyed.*,
    row_number() OVER (
      PARTITION BY keyed.story_version_id, keyed.content_key
      ORDER BY keyed.ordinal, keyed.id
    )::integer AS content_occurrence
  FROM keyed
)
UPDATE story_segments AS segment
SET
  segment_kind = numbered.segment_kind,
  heading_level = numbered.heading_level,
  content_key = numbered.content_key,
  content_occurrence = numbered.content_occurrence
FROM numbered
WHERE numbered.id = segment.id;

-- Only H2 headings establish Reader chapters. The H2 belongs to its own
-- chapter and every following segment inherits it until the next H2.
WITH nearest_chapter AS (
  SELECT
    segment.id AS segment_id,
    chapter.content_key AS chapter_key,
    chapter.chapter_occurrence
  FROM story_segments AS segment
  LEFT JOIN LATERAL (
    SELECT
      heading.content_key,
      row_number() OVER (
        PARTITION BY heading.story_version_id, heading.content_key
        ORDER BY heading.ordinal, heading.id
      )::integer AS chapter_occurrence,
      heading.ordinal,
      heading.id
    FROM story_segments AS heading
    WHERE heading.story_version_id = segment.story_version_id
      AND heading.segment_kind = 'heading'
      AND heading.heading_level = 2
      AND heading.ordinal <= segment.ordinal
    ORDER BY heading.ordinal DESC, heading.id DESC
    LIMIT 1
  ) AS chapter ON true
)
UPDATE story_segments AS segment
SET
  chapter_key = nearest_chapter.chapter_key,
  chapter_occurrence = nearest_chapter.chapter_occurrence
FROM nearest_chapter
WHERE nearest_chapter.segment_id = segment.id;

ALTER TABLE story_segments
  ALTER COLUMN segment_kind SET NOT NULL,
  ALTER COLUMN content_key SET NOT NULL,
  ALTER COLUMN content_occurrence SET NOT NULL,
  ADD CONSTRAINT story_segments_ordinal_positive_check
    CHECK (ordinal >= 1),
  ADD CONSTRAINT story_segments_kind_check
    CHECK (segment_kind IN ('heading', 'paragraph', 'other')),
  ADD CONSTRAINT story_segments_heading_level_check
    CHECK (
      (segment_kind = 'heading' AND heading_level BETWEEN 1 AND 6)
      OR (segment_kind <> 'heading' AND heading_level IS NULL)
    ),
  ADD CONSTRAINT story_segments_content_key_check
    CHECK (content_key ~ '^[0-9a-f]{64}$'),
  ADD CONSTRAINT story_segments_content_occurrence_check
    CHECK (content_occurrence >= 1),
  ADD CONSTRAINT story_segments_chapter_identity_check
    CHECK (
      (chapter_key IS NULL AND chapter_occurrence IS NULL)
      OR (
        chapter_key ~ '^[0-9a-f]{64}$'
        AND chapter_occurrence >= 1
      )
    );

CREATE UNIQUE INDEX story_segments_version_content_identity_idx
  ON story_segments (story_version_id, content_key, content_occurrence);

CREATE INDEX story_segments_version_chapter_identity_idx
  ON story_segments (story_version_id, chapter_key, chapter_occurrence, ordinal)
  WHERE chapter_key IS NOT NULL;

ALTER TABLE story_segments DROP COLUMN locator;

ALTER TABLE reading_progress
  ADD CONSTRAINT reading_progress_percent_range_check
    CHECK (percent >= 0 AND percent <= 1),
  ADD CONSTRAINT reading_progress_reader_locator_v2_check
    CHECK (
      jsonb_typeof(locator) = 'object'
      AND locator ? 'schema'
      AND locator ? 'segment'
      AND (locator - ARRAY['schema', 'segment', 'chapter']) = '{}'::jsonb
      AND jsonb_typeof(locator->'schema') = 'number'
      AND locator->>'schema' = '2'
      AND jsonb_typeof(locator->'segment') = 'object'
      AND (locator->'segment') ?& ARRAY['key', 'occurrence', 'ordinal', 'offset']
      AND ((locator->'segment') - ARRAY['key', 'occurrence', 'ordinal', 'offset']) = '{}'::jsonb
      AND jsonb_typeof(locator#>'{segment,key}') = 'string'
      AND locator#>>'{segment,key}' ~ '^[0-9a-f]{64}$'
      AND jsonb_typeof(locator#>'{segment,occurrence}') = 'number'
      AND locator#>>'{segment,occurrence}' ~ '^[1-9][0-9]*$'
      AND jsonb_typeof(locator#>'{segment,ordinal}') = 'number'
      AND locator#>>'{segment,ordinal}' ~ '^[1-9][0-9]*$'
      AND CASE
        WHEN jsonb_typeof(locator#>'{segment,offset}') = 'number'
          THEN (locator#>>'{segment,offset}')::numeric BETWEEN 0 AND 1
        ELSE false
      END
      AND CASE
        WHEN locator ? 'chapter' THEN
          jsonb_typeof(locator->'chapter') = 'object'
          AND (locator->'chapter') ?& ARRAY['key', 'occurrence']
          AND ((locator->'chapter') - ARRAY['key', 'occurrence']) = '{}'::jsonb
          AND jsonb_typeof(locator#>'{chapter,key}') = 'string'
          AND locator#>>'{chapter,key}' ~ '^[0-9a-f]{64}$'
          AND jsonb_typeof(locator#>'{chapter,occurrence}') = 'number'
          AND locator#>>'{chapter,occurrence}' ~ '^[1-9][0-9]*$'
        ELSE true
      END
    );

COMMIT;

-- +goose Down
BEGIN;

-- The Up migration's beta progress reset is intentionally irreversible.
DELETE FROM reading_progress;

ALTER TABLE reading_progress
  DROP CONSTRAINT reading_progress_reader_locator_v2_check,
  DROP CONSTRAINT reading_progress_percent_range_check;

ALTER TABLE story_segments ADD COLUMN locator JSONB;

UPDATE story_segments
SET locator = CASE segment_kind
  WHEN 'heading' THEN jsonb_build_object(
    'type', 'heading',
    'h', heading_level,
    'index', ordinal - 1
  )
  WHEN 'paragraph' THEN jsonb_build_object('type', 'para', 'n', ordinal)
  ELSE jsonb_build_object('type', 'block', 'kind', 'reader-2-rollback')
END;

ALTER TABLE story_segments ALTER COLUMN locator SET NOT NULL;

DROP INDEX story_segments_version_chapter_identity_idx;
DROP INDEX story_segments_version_content_identity_idx;

ALTER TABLE story_segments
  DROP CONSTRAINT story_segments_chapter_identity_check,
  DROP CONSTRAINT story_segments_content_occurrence_check,
  DROP CONSTRAINT story_segments_content_key_check,
  DROP CONSTRAINT story_segments_heading_level_check,
  DROP CONSTRAINT story_segments_kind_check,
  DROP CONSTRAINT story_segments_ordinal_positive_check,
  DROP COLUMN chapter_occurrence,
  DROP COLUMN chapter_key,
  DROP COLUMN content_occurrence,
  DROP COLUMN content_key,
  DROP COLUMN heading_level,
  DROP COLUMN segment_kind;

COMMIT;
