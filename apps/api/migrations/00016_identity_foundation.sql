-- +goose Up
BEGIN;

CREATE TABLE principals (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  display_name text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT principals_display_name_check CHECK (
    display_name = btrim(display_name)
    AND char_length(display_name) BETWEEN 1 AND 120
  )
);

CREATE TABLE external_identities (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  principal_id uuid NOT NULL,
  provider text NOT NULL,
  issuer text NOT NULL,
  subject text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT external_identities_principal_fkey
    FOREIGN KEY (principal_id) REFERENCES principals(id)
    ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT external_identities_provider_check CHECK (
    provider ~ '^[a-z][a-z0-9_-]{0,31}$'
  ),
  CONSTRAINT external_identities_issuer_check CHECK (
    issuer = btrim(issuer)
    AND char_length(issuer) BETWEEN 1 AND 512
  ),
  CONSTRAINT external_identities_subject_check CHECK (
    subject = btrim(subject)
    AND char_length(subject) BETWEEN 1 AND 255
  ),
  CONSTRAINT external_identities_provider_issuer_subject_key
    UNIQUE (provider, issuer, subject)
);

CREATE INDEX external_identities_principal_idx
  ON external_identities (principal_id);

CREATE TABLE account_memberships (
  principal_id uuid NOT NULL,
  account_id uuid NOT NULL,
  role text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT account_memberships_pkey PRIMARY KEY (principal_id, account_id),
  CONSTRAINT account_memberships_principal_fkey
    FOREIGN KEY (principal_id) REFERENCES principals(id)
    ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT account_memberships_account_fkey
    FOREIGN KEY (account_id) REFERENCES accounts(id)
    ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT account_memberships_role_check CHECK (role IN ('owner', 'adult'))
);

CREATE INDEX account_memberships_account_idx
  ON account_memberships (account_id, role, principal_id);

COMMIT;

-- +goose Down
BEGIN;

DROP TABLE account_memberships;
DROP TABLE external_identities;
DROP TABLE principals;

COMMIT;
