-- +goose Up
BEGIN;

-- A reader's reading level is persistent reader-profile configuration.
-- Before Lifecycle 7 every Reader resolved through the Classic compatibility
-- projection, so Classic is the truthful upgrade value for existing profiles.
-- New profile creation must supply an explicit value; there is deliberately no
-- database default after this migration.
LOCK TABLE profiles IN SHARE ROW EXCLUSIVE MODE;

ALTER TABLE profiles
  ADD COLUMN reading_level text;

UPDATE profiles
SET reading_level = 'classic'
WHERE reading_level IS NULL;

ALTER TABLE profiles
  ADD CONSTRAINT profiles_reading_level_check
  CHECK (
    reading_level IN (
      'classic',
      'confident-readers',
      'growing-readers',
      'story-explorers',
      'little-listeners'
    )
  );

ALTER TABLE profiles
  ALTER COLUMN reading_level SET NOT NULL;

COMMIT;

-- +goose Down
-- Removing this column would discard a reader's explicit persistent reading
-- level. That cannot be truthfully reconstructed from any other state.
-- +goose StatementBegin
DO $$
BEGIN
  RAISE EXCEPTION
    'profile reading level migration is irreversible'
    USING ERRCODE = '0A000';
END
$$;
-- +goose StatementEnd
