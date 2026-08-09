-- +goose Up
BEGIN;

-- These tables belong to the retired Journey/settings and first-generation
-- generation model. Lock the complete legacy graph so the empty-data preflight
-- and destructive DDL are one race-free decision.
LOCK TABLE
  generation_jobs,
  account_settings,
  child_profiles,
  prompt_profiles
IN ACCESS EXCLUSIVE MODE;

-- +goose StatementBegin
DO $$
DECLARE
  generation_job_count bigint;
  account_settings_count bigint;
  child_profile_count bigint;
  prompt_profile_count bigint;
BEGIN
  SELECT count(*) INTO generation_job_count FROM generation_jobs;
  SELECT count(*) INTO account_settings_count FROM account_settings;
  SELECT count(*) INTO child_profile_count FROM child_profiles;
  SELECT count(*) INTO prompt_profile_count FROM prompt_profiles;

  IF generation_job_count <> 0
     OR account_settings_count <> 0
     OR child_profile_count <> 0
     OR prompt_profile_count <> 0 THEN
    RAISE EXCEPTION
      'legacy runtime scaffolding retirement refused: generation_jobs=% account_settings=% child_profiles=% prompt_profiles=%',
      generation_job_count,
      account_settings_count,
      child_profile_count,
      prompt_profile_count
      USING ERRCODE = '23514';
  END IF;
END
$$;
-- +goose StatementEnd

-- No CASCADE and no IF EXISTS are intentional. Migration 23 is valid only for
-- the exact v22 shape audited above; unexpected dependencies or schema drift
-- must stop rather than be silently removed.
DROP TABLE generation_jobs;
DROP TABLE account_settings;
DROP TABLE child_profiles;
DROP TABLE prompt_profiles;
DROP TYPE generation_status;

COMMIT;

-- +goose Down
-- Retired rows cannot be reconstructed after a successful Up migration.
-- Recreating empty lookalike tables would be an untruthful rollback.
-- +goose StatementBegin
DO $$
BEGIN
  RAISE EXCEPTION 'legacy runtime scaffolding retirement is irreversible';
END
$$;
-- +goose StatementEnd
