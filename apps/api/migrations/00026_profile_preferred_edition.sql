-- +goose Up
BEGIN;

-- A reader's preferred edition is persistent reader-profile configuration.
-- Before Lifecycle 7 every Reader resolved through the Classic compatibility
-- projection, so Classic is the truthful upgrade value for existing profiles.
-- New profile creation must supply an explicit value; there is deliberately no
-- database default after this migration.
LOCK TABLE profiles IN SHARE ROW EXCLUSIVE MODE;

ALTER TABLE profiles
  ADD COLUMN preferred_edition text;

UPDATE profiles
SET preferred_edition = 'classic'
WHERE preferred_edition IS NULL;

ALTER TABLE profiles
  ADD CONSTRAINT profiles_preferred_edition_check
  CHECK (
    preferred_edition IN (
      'classic',
      'confident-readers',
      'growing-readers',
      'story-explorers',
      'little-listeners'
    )
  );

ALTER TABLE profiles
  ALTER COLUMN preferred_edition SET NOT NULL;

COMMIT;

-- +goose Down
-- Removing this column would discard a reader's explicit persistent reading
-- level. That cannot be truthfully reconstructed from any other state.
-- +goose StatementBegin
DO $$
BEGIN
  RAISE EXCEPTION
    'profile preferred edition migration is irreversible'
    USING ERRCODE = '0A000';
END
$$;
-- +goose StatementEnd
