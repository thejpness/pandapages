-- +goose Up
BEGIN;

CREATE TABLE IF NOT EXISTS profile_settings (
  profile_id uuid PRIMARY KEY REFERENCES profiles(id) ON DELETE CASCADE,

  active_child_profile_id uuid REFERENCES child_profiles(id) ON DELETE SET NULL,
  active_prompt_profile_id uuid REFERENCES prompt_profiles(id) ON DELETE SET NULL,

  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_profile_settings_child
  ON profile_settings(active_child_profile_id);

CREATE INDEX IF NOT EXISTS idx_profile_settings_prompt
  ON profile_settings(active_prompt_profile_id);

COMMIT;

-- +goose Down
BEGIN;

DROP TABLE IF EXISTS profile_settings;

COMMIT;
