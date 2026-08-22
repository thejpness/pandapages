-- +goose Up
BEGIN;

-- Human editorial decisions are immutable audit events against one exact
-- completed orchestration run. They intentionally do not alter the retained
-- orchestration evidence or current story/publication state.
CREATE TABLE story_orchestration_run_editorial_reviews (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  run_id uuid NOT NULL,
  decision text NOT NULL,
  reviewer_principal_id uuid NOT NULL,
  reviewer_account_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT story_orchestration_run_editorial_reviews_run_fkey
    FOREIGN KEY (run_id) REFERENCES story_orchestration_runs(id)
    ON UPDATE NO ACTION ON DELETE RESTRICT,

  CONSTRAINT story_orchestration_run_editorial_reviews_reviewer_membership_fkey
    FOREIGN KEY (reviewer_principal_id, reviewer_account_id)
    REFERENCES account_memberships(principal_id, account_id)
    ON UPDATE NO ACTION ON DELETE RESTRICT,

  CONSTRAINT story_orchestration_run_editorial_reviews_decision_check
    CHECK (decision IN ('approved', 'rejected'))
);

CREATE INDEX story_orchestration_run_editorial_reviews_run_created_idx
  ON story_orchestration_run_editorial_reviews(run_id, created_at DESC, id DESC);

COMMIT;

-- +goose Down
BEGIN;

DROP TABLE story_orchestration_run_editorial_reviews;

COMMIT;
