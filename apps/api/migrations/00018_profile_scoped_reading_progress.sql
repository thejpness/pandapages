-- +goose Up
BEGIN;

-- Reading progress is reader-specific. Migration 00017 deliberately removed
-- profile ownership, so existing account-scoped beta progress cannot be
-- truthfully assigned to a reader profile. It is non-authoritative
-- development data and is explicitly discarded here; no profile is selected
-- or created as part of this conversion.
DROP TABLE reading_progress;

CREATE TABLE reading_progress (
  account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  profile_id uuid NOT NULL,
  story_id uuid NOT NULL,
  story_version_id uuid NOT NULL,
  locator jsonb NOT NULL,
  percent double precision NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (account_id, profile_id, story_id),
  CONSTRAINT reading_progress_profile_account_fkey
    FOREIGN KEY (profile_id, account_id) REFERENCES profiles(id, account_id) ON DELETE CASCADE,
  CONSTRAINT reading_progress_story_account_fkey
    FOREIGN KEY (story_id, account_id) REFERENCES stories(id, account_id) ON DELETE CASCADE,
  CONSTRAINT reading_progress_story_version_story_fkey
    FOREIGN KEY (story_version_id, story_id) REFERENCES story_versions(id, story_id),
  CONSTRAINT reading_progress_percent_range_check
    CHECK (percent >= 0 AND percent <= 1),
  CONSTRAINT reading_progress_reader_locator_v2_check
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
    )
);

CREATE INDEX reading_progress_account_profile_updated_idx
  ON reading_progress (account_id, profile_id, updated_at DESC);

COMMIT;

-- +goose Down
-- Profile-specific progress cannot be reduced to one account/story row
-- without silently losing reader data. Do not manufacture that policy.
-- +goose StatementBegin
DO $$ BEGIN
  RAISE EXCEPTION 'profile-scoped reading progress migration is irreversible';
END $$;
-- +goose StatementEnd
