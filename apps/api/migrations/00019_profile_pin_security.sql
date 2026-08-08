-- +goose Up
BEGIN;

-- A reader PIN is a local child-mode gate, never an adult authentication
-- credential. Only a bcrypt encoded one-way hash is stored; plaintext PINs
-- never enter the schema. Failed verification state is profile-local and is
-- reset whenever a PIN is set or removed.
ALTER TABLE profiles
  ADD COLUMN pin_hash text,
  ADD COLUMN pin_failed_attempts integer NOT NULL DEFAULT 0,
  ADD COLUMN pin_lock_until timestamptz;

ALTER TABLE profiles
  ADD CONSTRAINT profiles_pin_failed_attempts_check
    CHECK (pin_failed_attempts >= 0),
  ADD CONSTRAINT profiles_pin_state_requires_hash_check
    CHECK (pin_hash IS NOT NULL OR (pin_failed_attempts = 0 AND pin_lock_until IS NULL)),
  ADD CONSTRAINT profiles_pin_lock_requires_hash_check
    CHECK (pin_lock_until IS NULL OR pin_hash IS NOT NULL),
  ADD CONSTRAINT profiles_pin_hash_bcrypt_check
    CHECK (pin_hash IS NULL OR pin_hash ~ '^\$2[aby]\$[0-9]{2}\$[./A-Za-z0-9]{53}$');

COMMIT;

-- +goose Down
-- Removing this state would silently remove PIN protection from profiles.
-- That is not a truthful rollback, so an operator must explicitly decide how
-- to handle the security state instead of manufacturing a weaker schema.
-- +goose StatementBegin
DO $$ BEGIN
  RAISE EXCEPTION 'profile PIN security migration is irreversible';
END $$;
-- +goose StatementEnd
