-- +goose Up
ALTER TABLE stories
  DROP CONSTRAINT IF EXISTS stories_slug_key;

-- account-scoped slug uniqueness (fixes security + multi-account future)
CREATE UNIQUE INDEX IF NOT EXISTS stories_account_id_slug_key
  ON stories(account_id, slug);

ALTER TABLE stories
  ADD CONSTRAINT stories_account_id_slug_key
  UNIQUE USING INDEX stories_account_id_slug_key;

-- +goose Down
ALTER TABLE stories
  DROP CONSTRAINT IF EXISTS stories_account_id_slug_key;

DROP INDEX IF EXISTS stories_account_id_slug_key;

ALTER TABLE stories
  ADD CONSTRAINT stories_slug_key UNIQUE (slug);
