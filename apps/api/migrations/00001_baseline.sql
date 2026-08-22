-- +goose Up
BEGIN;

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Account-owned product roots.
CREATE TABLE accounts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE assets (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  sha256 text NOT NULL UNIQUE,
  mime_type text NOT NULL,
  bytes bigint NOT NULL,
  original_name text,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE profiles (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  account_id uuid NOT NULL,
  name text NOT NULL,
  pin_hash text,
  pin_failed_attempts integer NOT NULL DEFAULT 0,
  pin_lock_until timestamptz,
  reading_level text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT profiles_account_id_fkey
    FOREIGN KEY (account_id) REFERENCES accounts(id)
    ON UPDATE NO ACTION ON DELETE RESTRICT,

  CONSTRAINT profiles_id_account_id_key
    UNIQUE (id, account_id),

  CONSTRAINT profiles_pin_failed_attempts_check
    CHECK (pin_failed_attempts >= 0),

  CONSTRAINT profiles_pin_state_requires_hash_check
    CHECK (
      pin_hash IS NOT NULL
      OR (pin_failed_attempts = 0 AND pin_lock_until IS NULL)
    ),

  CONSTRAINT profiles_pin_lock_requires_hash_check
    CHECK (pin_lock_until IS NULL OR pin_hash IS NOT NULL),

  CONSTRAINT profiles_pin_hash_bcrypt_check
    CHECK (
      pin_hash IS NULL
      OR pin_hash ~ '^\$2[aby]\$[0-9]{2}\$[./A-Za-z0-9]{53}$'
    ),

  CONSTRAINT profiles_reading_level_check
    CHECK (
      reading_level IN (
        'classic',
        'confident-readers',
        'growing-readers',
        'story-explorers',
        'little-listeners'
      )
    )
);

CREATE INDEX idx_profiles_account
  ON profiles(account_id);

CREATE UNIQUE INDEX ux_profiles_account_name
  ON profiles(account_id, name);

-- Adult identity and account membership.
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
  ON external_identities(principal_id);

CREATE TABLE account_memberships (
  principal_id uuid NOT NULL,
  account_id uuid NOT NULL,
  role text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT account_memberships_pkey
    PRIMARY KEY (principal_id, account_id),

  CONSTRAINT account_memberships_principal_fkey
    FOREIGN KEY (principal_id) REFERENCES principals(id)
    ON UPDATE NO ACTION ON DELETE RESTRICT,

  CONSTRAINT account_memberships_account_fkey
    FOREIGN KEY (account_id) REFERENCES accounts(id)
    ON UPDATE NO ACTION ON DELETE RESTRICT,

  CONSTRAINT account_memberships_role_check
    CHECK (role IN ('owner', 'adult'))
);

CREATE INDEX account_memberships_account_idx
  ON account_memberships(account_id, role, principal_id);

-- Story identity. Live publication authority is the current immutable release.
CREATE TABLE stories (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  slug text NOT NULL,
  visibility text NOT NULL,
  owner_account_id uuid,
  title text NOT NULL,
  author text,
  cover_asset_id uuid,
  current_release_id uuid,
  language text NOT NULL DEFAULT 'en-GB',
  rights jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT stories_owner_account_id_fkey
    FOREIGN KEY (owner_account_id) REFERENCES accounts(id)
    ON UPDATE NO ACTION ON DELETE CASCADE,

  CONSTRAINT stories_visibility_check
    CHECK (visibility IN ('public', 'private')),

  CONSTRAINT stories_visibility_owner_check
    CHECK (
      (visibility = 'public' AND owner_account_id IS NULL)
      OR
      (visibility = 'private' AND owner_account_id IS NOT NULL)
    ),

  CONSTRAINT stories_slug_key
    UNIQUE (slug)
);

CREATE INDEX stories_public_library_idx
  ON stories(updated_at DESC, slug ASC)
  WHERE visibility = 'public' AND current_release_id IS NOT NULL;

CREATE INDEX stories_private_owner_library_idx
  ON stories(owner_account_id, updated_at DESC, slug ASC)
  WHERE visibility = 'private' AND current_release_id IS NOT NULL;

CREATE TABLE contributors (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL,
  sort_name text
);

CREATE INDEX idx_contributors_name
  ON contributors(name);

CREATE UNIQUE INDEX ux_contributors_name
  ON contributors(name);

CREATE TABLE story_contributors (
  story_id uuid NOT NULL REFERENCES stories(id) ON DELETE CASCADE,
  contributor_id uuid NOT NULL REFERENCES contributors(id) ON DELETE CASCADE,
  role text NOT NULL,
  PRIMARY KEY (story_id, contributor_id, role)
);

CREATE INDEX idx_story_contrib_story
  ON story_contributors(story_id);

CREATE INDEX idx_story_contrib_contributor
  ON story_contributors(contributor_id);

-- Edition lifecycle.
CREATE TABLE story_editions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  story_id uuid NOT NULL,
  edition_key text NOT NULL,
  draft_version_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT story_editions_story_id_fkey
    FOREIGN KEY (story_id) REFERENCES stories(id)
    ON UPDATE NO ACTION ON DELETE CASCADE,

  CONSTRAINT story_editions_edition_key_check
    CHECK (
      edition_key IN (
        'classic',
        'confident-readers',
        'growing-readers',
        'story-explorers',
        'little-listeners'
      )
    ),

  CONSTRAINT story_editions_story_key_key
    UNIQUE (story_id, edition_key),

  CONSTRAINT story_editions_id_story_id_key
    UNIQUE (id, story_id)
);

CREATE TABLE story_versions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  story_id uuid NOT NULL REFERENCES stories(id) ON DELETE CASCADE,
  edition_id uuid NOT NULL,
  version integer NOT NULL,
  frontmatter jsonb NOT NULL DEFAULT '{}'::jsonb,
  markdown text NOT NULL,
  rendered_html text NOT NULL,
  content_hash text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT story_versions_story_id_version_key
    UNIQUE (story_id, version),

  CONSTRAINT story_versions_id_story_id_key
    UNIQUE (id, story_id),

  CONSTRAINT story_versions_id_edition_id_key
    UNIQUE (id, edition_id),

  CONSTRAINT story_versions_edition_id_content_hash_key
    UNIQUE (edition_id, content_hash),

  CONSTRAINT story_versions_edition_story_fkey
    FOREIGN KEY (edition_id, story_id)
    REFERENCES story_editions(id, story_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION
);

ALTER TABLE story_editions
  ADD CONSTRAINT story_editions_draft_version_fkey
    FOREIGN KEY (draft_version_id, id)
    REFERENCES story_versions(id, edition_id)
    ON UPDATE NO ACTION ON DELETE SET NULL (draft_version_id);

CREATE INDEX idx_story_versions_story_created
  ON story_versions(story_id, created_at DESC);

CREATE INDEX story_editions_story_updated_idx
  ON story_editions(story_id, updated_at DESC);

CREATE INDEX story_editions_draft_version_idx
  ON story_editions(draft_version_id)
  WHERE draft_version_id IS NOT NULL;

CREATE INDEX story_versions_edition_version_idx
  ON story_versions(edition_id, version DESC);

-- Reader structure and stable Locator v2 anchors.
CREATE TABLE story_sections (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  story_version_id uuid NOT NULL REFERENCES story_versions(id) ON DELETE CASCADE,
  kind text NOT NULL,
  title text,
  ordinal integer NOT NULL,
  start_offset integer,
  end_offset integer,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (story_version_id, ordinal)
);

CREATE INDEX idx_sections_version_ordinal
  ON story_sections(story_version_id, ordinal);

CREATE TABLE story_segments (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  story_version_id uuid NOT NULL REFERENCES story_versions(id) ON DELETE CASCADE,
  section_id uuid REFERENCES story_sections(id) ON DELETE SET NULL,
  ordinal integer NOT NULL,
  segment_kind text NOT NULL,
  heading_level integer,
  content_key text NOT NULL,
  content_occurrence integer NOT NULL,
  chapter_key text,
  chapter_occurrence integer,
  markdown text NOT NULL,
  rendered_html text NOT NULL,
  word_count integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT story_segments_story_version_id_ordinal_key
    UNIQUE (story_version_id, ordinal),

  CONSTRAINT story_segments_ordinal_positive_check
    CHECK (ordinal >= 1),

  CONSTRAINT story_segments_kind_check
    CHECK (segment_kind IN ('heading', 'paragraph', 'other')),

  CONSTRAINT story_segments_heading_level_check
    CHECK (
      (segment_kind = 'heading' AND heading_level BETWEEN 1 AND 6)
      OR (segment_kind <> 'heading' AND heading_level IS NULL)
    ),

  CONSTRAINT story_segments_content_key_check
    CHECK (content_key ~ '^[0-9a-f]{64}$'),

  CONSTRAINT story_segments_content_occurrence_check
    CHECK (content_occurrence >= 1),

  CONSTRAINT story_segments_chapter_identity_check
    CHECK (
      (chapter_key IS NULL AND chapter_occurrence IS NULL)
      OR (
        chapter_key ~ '^[0-9a-f]{64}$'
        AND chapter_occurrence >= 1
      )
    )
);

CREATE INDEX idx_segments_version_ordinal
  ON story_segments(story_version_id, ordinal);

CREATE INDEX idx_segments_section_ordinal
  ON story_segments(section_id, ordinal);

CREATE UNIQUE INDEX story_segments_version_content_identity_idx
  ON story_segments(story_version_id, content_key, content_occurrence);

CREATE INDEX story_segments_version_chapter_identity_idx
  ON story_segments(story_version_id, chapter_key, chapter_occurrence, ordinal)
  WHERE chapter_key IS NOT NULL;

CREATE TABLE reading_progress (
  account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  profile_id uuid NOT NULL,
  story_id uuid NOT NULL,
  story_version_id uuid NOT NULL,
  locator jsonb NOT NULL,
  percent double precision NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT reading_progress_pkey
    PRIMARY KEY (account_id, profile_id, story_id),

  CONSTRAINT reading_progress_profile_account_fkey
    FOREIGN KEY (profile_id, account_id)
    REFERENCES profiles(id, account_id)
    ON DELETE CASCADE,

  CONSTRAINT reading_progress_story_fkey
    FOREIGN KEY (story_id)
    REFERENCES stories(id)
    ON DELETE CASCADE,

  CONSTRAINT reading_progress_story_version_story_fkey
    FOREIGN KEY (story_version_id, story_id)
    REFERENCES story_versions(id, story_id),

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
  ON reading_progress(account_id, profile_id, updated_at DESC);

-- Explicit per-profile, per-story edition selection. This is a Reader choice,
-- not publication authority: current release membership is still authoritative
-- when deciding whether a stored override can be used.
CREATE TABLE reader_story_edition_overrides (
  account_id uuid NOT NULL,
  profile_id uuid NOT NULL,
  story_id uuid NOT NULL,
  edition_key text NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT reader_story_edition_overrides_pkey
    PRIMARY KEY (account_id, profile_id, story_id),

  CONSTRAINT reader_story_edition_overrides_profile_account_fkey
    FOREIGN KEY (profile_id, account_id)
    REFERENCES profiles(id, account_id)
    ON DELETE CASCADE,

  CONSTRAINT reader_story_edition_overrides_story_fkey
    FOREIGN KEY (story_id)
    REFERENCES stories(id)
    ON DELETE CASCADE,

  CONSTRAINT reader_story_edition_overrides_edition_key_check
    CHECK (
      edition_key IN (
        'classic',
        'confident-readers',
        'growing-readers',
        'story-explorers',
        'little-listeners'
      )
    ),

  CONSTRAINT reader_story_edition_overrides_story_edition_fkey
    FOREIGN KEY (story_id, edition_key)
    REFERENCES story_editions(story_id, edition_key)
    ON DELETE CASCADE
);

-- Durable external-provider acquisition evidence. Acquisitions intentionally
-- exist before any Panda Pages story or canonical source is created.
CREATE TABLE source_acquisitions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  provider text NOT NULL,
  external_id text NOT NULL,
  title text NOT NULL,
  contributors jsonb NOT NULL,
  languages jsonb NOT NULL,
  landing_url text NOT NULL,
  provider_rights text,
  representation_label text,
  representation_media_type text NOT NULL,
  representation_provider_url text NOT NULL,
  representation_size_bytes bigint,
  normalisation_version text NOT NULL,
  retrieved_content_hash text NOT NULL,
  normalised_content_hash text NOT NULL,
  source_text text NOT NULL,
  snapshot_hash text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT source_acquisitions_provider_check
    CHECK (provider ~ '^[a-z][a-z0-9-]{0,63}$'),
  CONSTRAINT source_acquisitions_external_id_check
    CHECK (external_id = btrim(external_id) AND char_length(external_id) BETWEEN 1 AND 128),
  CONSTRAINT source_acquisitions_title_check
    CHECK (title = btrim(title) AND char_length(title) BETWEEN 1 AND 1000),
  CONSTRAINT source_acquisitions_contributors_check
    CHECK (jsonb_typeof(contributors) = 'array'),
  CONSTRAINT source_acquisitions_languages_check
    CHECK (jsonb_typeof(languages) = 'array'),
  CONSTRAINT source_acquisitions_landing_url_check
    CHECK (landing_url = btrim(landing_url) AND char_length(landing_url) BETWEEN 1 AND 2048),
  CONSTRAINT source_acquisitions_provider_rights_check
    CHECK (provider_rights IS NULL OR (provider_rights = btrim(provider_rights) AND char_length(provider_rights) BETWEEN 1 AND 1000)),
  CONSTRAINT source_acquisitions_representation_label_check
    CHECK (representation_label IS NULL OR (representation_label = btrim(representation_label) AND char_length(representation_label) BETWEEN 1 AND 500)),
  CONSTRAINT source_acquisitions_representation_media_type_check
    CHECK (representation_media_type = btrim(representation_media_type) AND char_length(representation_media_type) BETWEEN 1 AND 200),
  CONSTRAINT source_acquisitions_representation_url_check
    CHECK (representation_provider_url = btrim(representation_provider_url) AND char_length(representation_provider_url) BETWEEN 1 AND 2048),
  CONSTRAINT source_acquisitions_representation_size_check
    CHECK (representation_size_bytes IS NULL OR representation_size_bytes > 0),
  CONSTRAINT source_acquisitions_normalisation_version_check
    CHECK (normalisation_version = btrim(normalisation_version) AND char_length(normalisation_version) BETWEEN 1 AND 128),
  CONSTRAINT source_acquisitions_retrieved_hash_check
    CHECK (retrieved_content_hash ~ '^[0-9a-f]{64}$'),
  CONSTRAINT source_acquisitions_normalised_hash_check
    CHECK (normalised_content_hash ~ '^[0-9a-f]{64}$'),
  CONSTRAINT source_acquisitions_source_text_check
    CHECK (btrim(source_text) <> ''),
  CONSTRAINT source_acquisitions_snapshot_hash_check
    CHECK (snapshot_hash ~ '^[0-9a-f]{64}$'),
  CONSTRAINT source_acquisitions_snapshot_hash_key
    UNIQUE (snapshot_hash),
  CONSTRAINT source_acquisitions_id_snapshot_hash_key
    UNIQUE (id, snapshot_hash)
);

CREATE INDEX source_acquisitions_created_idx
  ON source_acquisitions(created_at DESC, id DESC);

-- Copyright eligibility is immutable evidence for an exact acquisition. It
-- is intentionally distinct from the mutable human source-quality review.
CREATE TABLE source_acquisition_eligibility_assessments (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  acquisition_id uuid NOT NULL,
  acquisition_snapshot_hash text NOT NULL,
  provider text NOT NULL,
  external_id text NOT NULL,
  policy_version text NOT NULL,
  evaluation_date date NOT NULL,
  evaluated_at timestamptz NOT NULL,
  us_status text NOT NULL,
  us_reason text NOT NULL,
  opds_rights text NOT NULL,
  rdf_rights text NOT NULL,
  header_rights text NOT NULL,
  uk_status text NOT NULL,
  uk_reason text NOT NULL,
  effective_uk_evidence jsonb NOT NULL,
  provider_evidence jsonb NOT NULL,
  overall_status text NOT NULL,
  overall_reason text NOT NULL,
  assessment_hash text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT source_acquisition_eligibility_assessments_acquisition_snapshot_fkey
    FOREIGN KEY (acquisition_id, acquisition_snapshot_hash)
    REFERENCES source_acquisitions(id, snapshot_hash)
    ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT source_acquisition_eligibility_assessments_provider_check
    CHECK (provider ~ '^[a-z][a-z0-9-]{0,63}$'),
  CONSTRAINT source_acquisition_eligibility_assessments_external_id_check
    CHECK (external_id = btrim(external_id) AND char_length(external_id) BETWEEN 1 AND 128),
  CONSTRAINT source_acquisition_eligibility_assessments_policy_check
    CHECK (policy_version = btrim(policy_version) AND char_length(policy_version) BETWEEN 1 AND 128),
  CONSTRAINT source_acquisition_eligibility_assessments_us_state_check
    CHECK (us_status IN ('eligible', 'ineligible', 'indeterminate')),
  CONSTRAINT source_acquisition_eligibility_assessments_uk_state_check
    CHECK (uk_status IN ('eligible', 'ineligible', 'indeterminate')),
  CONSTRAINT source_acquisition_eligibility_assessments_overall_state_check
    CHECK (overall_status IN ('eligible', 'blocked')),
  CONSTRAINT source_acquisition_eligibility_assessments_rights_check
    CHECK (
      opds_rights IN ('public_domain', 'restricted', 'unknown')
      AND rdf_rights IN ('public_domain', 'restricted', 'unknown')
      AND header_rights IN ('public_domain', 'restricted', 'no_classification', 'conflicting')
    ),
  CONSTRAINT source_acquisition_eligibility_assessments_evidence_check
    CHECK (jsonb_typeof(effective_uk_evidence) = 'object' AND jsonb_typeof(provider_evidence) = 'object'),
  CONSTRAINT source_acquisition_eligibility_assessments_hash_check
    CHECK (assessment_hash ~ '^[0-9a-f]{64}$'),
  CONSTRAINT source_acquisition_eligibility_assessments_hash_key
    UNIQUE (assessment_hash),
  CONSTRAINT source_acquisition_eligibility_assessments_id_acquisition_key
    UNIQUE (id, acquisition_id),
  CONSTRAINT source_acquisition_eligibility_assessments_success_check
    CHECK (overall_status = 'eligible' AND us_status = 'eligible' AND uk_status = 'eligible')
);

CREATE INDEX source_acquisition_eligibility_assessments_current_idx
  ON source_acquisition_eligibility_assessments(acquisition_id, policy_version, evaluation_date DESC, evaluated_at DESC, id DESC);

-- Source quality is the sole mutable review dimension for provider
-- acquisitions. Copyright eligibility is immutable above.
CREATE TABLE source_acquisition_quality_reviews (
  acquisition_id uuid PRIMARY KEY,
  status text NOT NULL,
  note text,
  reviewed_at timestamptz,

  CONSTRAINT source_acquisition_quality_reviews_acquisition_fkey
    FOREIGN KEY (acquisition_id) REFERENCES source_acquisitions(id)
    ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT source_acquisition_quality_reviews_status_check
    CHECK (status IN ('pending', 'approved', 'rejected')),
  CONSTRAINT source_acquisition_quality_reviews_state_check
    CHECK (
      (status = 'pending' AND note IS NULL AND reviewed_at IS NULL)
      OR
      (status IN ('approved', 'rejected')
        AND note IS NOT NULL
        AND note = btrim(note)
        AND char_length(note) BETWEEN 1 AND 4000
        AND reviewed_at IS NOT NULL)
    )
);

-- Canonical source lifecycle.
CREATE TABLE story_sources (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  story_id uuid NOT NULL,
  current_version_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT story_sources_story_id_fkey
    FOREIGN KEY (story_id) REFERENCES stories(id)
    ON UPDATE NO ACTION ON DELETE CASCADE,

  CONSTRAINT story_sources_story_id_key
    UNIQUE (story_id),

  CONSTRAINT story_sources_id_story_id_key
    UNIQUE (id, story_id)
);

CREATE TABLE story_source_versions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  source_id uuid NOT NULL,
  story_id uuid NOT NULL,
  version integer NOT NULL,
  title text NOT NULL,
  author text,
  language text NOT NULL,
  rights jsonb NOT NULL DEFAULT '{}'::jsonb,
  source_url text,
  source_text text NOT NULL,
  source_acquisition_id uuid,
  source_eligibility_assessment_id uuid,
  snapshot_hash text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT story_source_versions_source_story_fkey
    FOREIGN KEY (source_id, story_id)
    REFERENCES story_sources(id, story_id)
    ON UPDATE NO ACTION ON DELETE CASCADE,

  CONSTRAINT story_source_versions_version_check
    CHECK (version > 0),

  CONSTRAINT story_source_versions_title_check
    CHECK (title = btrim(title) AND title <> ''),

  CONSTRAINT story_source_versions_author_check
    CHECK (author IS NULL OR (author = btrim(author) AND author <> '')),

  CONSTRAINT story_source_versions_language_check
    CHECK (language = btrim(language) AND language <> ''),

  CONSTRAINT story_source_versions_source_url_check
    CHECK (source_url IS NULL OR (source_url = btrim(source_url) AND source_url <> '')),

  CONSTRAINT story_source_versions_source_text_check
    CHECK (btrim(source_text) <> ''),

  CONSTRAINT story_source_versions_snapshot_hash_check
    CHECK (snapshot_hash ~ '^[0-9a-f]{64}$'),

  CONSTRAINT story_source_versions_provenance_pair_check
    CHECK (
      (source_acquisition_id IS NULL AND source_eligibility_assessment_id IS NULL)
      OR
      (source_acquisition_id IS NOT NULL AND source_eligibility_assessment_id IS NOT NULL)
    ),

  CONSTRAINT story_source_versions_acquisition_fkey
    FOREIGN KEY (source_acquisition_id)
    REFERENCES source_acquisitions(id)
    ON UPDATE NO ACTION ON DELETE RESTRICT,

  CONSTRAINT story_source_versions_assessment_acquisition_fkey
    FOREIGN KEY (source_eligibility_assessment_id, source_acquisition_id)
    REFERENCES source_acquisition_eligibility_assessments(id, acquisition_id)
    ON UPDATE NO ACTION ON DELETE RESTRICT,

  CONSTRAINT story_source_versions_source_version_key
    UNIQUE (source_id, version),

  CONSTRAINT story_source_versions_source_snapshot_hash_key
    UNIQUE (source_id, snapshot_hash),

  CONSTRAINT story_source_versions_source_acquisition_key
    UNIQUE (source_acquisition_id),

  CONSTRAINT story_source_versions_id_source_id_key
    UNIQUE (id, source_id)
);

ALTER TABLE story_sources
  ADD CONSTRAINT story_sources_current_version_fkey
    FOREIGN KEY (current_version_id, id)
    REFERENCES story_source_versions(id, source_id)
    ON UPDATE NO ACTION ON DELETE SET NULL (current_version_id);

CREATE INDEX story_source_versions_source_created_idx
  ON story_source_versions(source_id, version DESC, created_at DESC);

CREATE INDEX story_sources_current_version_idx
  ON story_sources(current_version_id)
  WHERE current_version_id IS NOT NULL;

-- Completed orchestration evidence is immutable and retained against the exact
-- canonical source version used to produce it. It does not represent draft or
-- publication state.
CREATE TABLE story_orchestration_runs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  source_version_id uuid NOT NULL,
  source_sha256 text NOT NULL,
  semantic_result text NOT NULL,
  artifacts jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT story_orchestration_runs_source_version_fkey
    FOREIGN KEY (source_version_id) REFERENCES story_source_versions(id)
    ON UPDATE NO ACTION ON DELETE RESTRICT,

  CONSTRAINT story_orchestration_runs_source_sha256_check
    CHECK (source_sha256 ~ '^[0-9a-f]{64}$'),

  CONSTRAINT story_orchestration_runs_semantic_result_check
    CHECK (semantic_result IN ('pass', 'needs_review', 'fail')),

  CONSTRAINT story_orchestration_runs_artifacts_shape_check
    CHECK (
      jsonb_typeof(artifacts) = 'object'
      AND COALESCE(jsonb_typeof(artifacts -> 'analysisArtifact') = 'object', false)
      AND CASE
        WHEN jsonb_typeof(artifacts -> 'editions') = 'array'
          THEN jsonb_array_length(artifacts -> 'editions') = 4
        ELSE false
      END
      AND CASE
        WHEN jsonb_typeof(artifacts -> 'editionAssessments') = 'array'
          THEN jsonb_array_length(artifacts -> 'editionAssessments') = 4
        ELSE false
      END
      AND COALESCE(jsonb_typeof(artifacts -> 'bundleAssessment') = 'object', false)
    )
);

CREATE INDEX story_orchestration_runs_source_created_idx
  ON story_orchestration_runs(source_version_id, created_at DESC, id DESC);

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
    CHECK (decision IN ('approved', 'rejected')),

  CONSTRAINT story_orchestration_run_editorial_reviews_id_run_key
    UNIQUE (id, run_id)
);

CREATE INDEX story_orchestration_run_editorial_reviews_run_created_idx
  ON story_orchestration_run_editorial_reviews(run_id, created_at DESC, id DESC);

-- Immutable provenance for deterministic copies from an exact currently
-- approved orchestration run into the existing editable Story Studio drafts.
CREATE TABLE story_orchestration_run_draft_ingests (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  run_id uuid NOT NULL,
  editorial_review_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT story_orchestration_run_draft_ingests_run_fkey
    FOREIGN KEY (run_id) REFERENCES story_orchestration_runs(id)
    ON UPDATE NO ACTION ON DELETE RESTRICT,

  CONSTRAINT story_orchestration_run_draft_ingests_review_run_fkey
    FOREIGN KEY (editorial_review_id, run_id)
    REFERENCES story_orchestration_run_editorial_reviews(id, run_id)
    ON UPDATE NO ACTION ON DELETE RESTRICT,

  CONSTRAINT story_orchestration_run_draft_ingests_editorial_review_key
    UNIQUE (editorial_review_id)
);

-- Each ingest retains the exact initial mutable version created for each
-- generated edition. Later editorial edits create new story versions and do
-- not mutate this provenance mapping.
CREATE TABLE story_orchestration_run_draft_ingest_editions (
  draft_ingest_id uuid NOT NULL,
  edition_id uuid NOT NULL,
  story_version_id uuid NOT NULL,

  CONSTRAINT story_orchestration_run_draft_ingest_editions_pkey
    PRIMARY KEY (draft_ingest_id, edition_id),

  CONSTRAINT story_orchestration_run_draft_ingest_editions_ingest_fkey
    FOREIGN KEY (draft_ingest_id)
    REFERENCES story_orchestration_run_draft_ingests(id)
    ON UPDATE NO ACTION ON DELETE RESTRICT,

  CONSTRAINT story_orchestration_run_draft_ingest_editions_edition_fkey
    FOREIGN KEY (edition_id) REFERENCES story_editions(id)
    ON UPDATE NO ACTION ON DELETE RESTRICT,

  CONSTRAINT story_orchestration_run_draft_ingest_editions_version_edition_fkey
    FOREIGN KEY (story_version_id, edition_id)
    REFERENCES story_versions(id, edition_id)
    ON UPDATE NO ACTION ON DELETE RESTRICT,

  CONSTRAINT story_orchestration_run_draft_ingest_editions_story_version_key
    UNIQUE (story_version_id)
);

-- Immutable release manifests. current_release_id is the modern authority.
CREATE TABLE story_releases (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  story_id uuid NOT NULL,
  release_number integer NOT NULL CHECK (release_number > 0),
  created_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT story_releases_story_fkey
    FOREIGN KEY (story_id) REFERENCES stories(id)
    ON DELETE CASCADE,

  CONSTRAINT story_releases_story_number_key
    UNIQUE (story_id, release_number),

  CONSTRAINT story_releases_id_story_key
    UNIQUE (id, story_id)
);

CREATE TABLE story_release_editions (
  release_id uuid NOT NULL,
  story_id uuid NOT NULL,
  edition_id uuid NOT NULL,
  story_version_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT story_release_editions_pkey
    PRIMARY KEY (release_id, edition_id),

  CONSTRAINT story_release_editions_release_version_key
    UNIQUE (release_id, story_version_id),

  CONSTRAINT story_release_editions_release_story_fkey
    FOREIGN KEY (release_id, story_id)
    REFERENCES story_releases(id, story_id)
    ON DELETE CASCADE,

  CONSTRAINT story_release_editions_edition_story_fkey
    FOREIGN KEY (edition_id, story_id)
    REFERENCES story_editions(id, story_id)
    DEFERRABLE INITIALLY DEFERRED,

  CONSTRAINT story_release_editions_version_edition_fkey
    FOREIGN KEY (story_version_id, edition_id)
    REFERENCES story_versions(id, edition_id)
    DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX story_releases_story_created_idx
  ON story_releases(story_id, release_number DESC);

CREATE INDEX story_release_editions_story_version_idx
  ON story_release_editions(story_id, story_version_id);

ALTER TABLE stories
  ADD CONSTRAINT stories_current_release_story_fkey
    FOREIGN KEY (current_release_id, id)
    REFERENCES story_releases(id, story_id)
    ON DELETE SET NULL (current_release_id);

CREATE INDEX stories_current_release_idx
  ON stories(current_release_id)
  WHERE current_release_id IS NOT NULL;

COMMIT;

-- +goose Down
BEGIN;

-- Break the intentional lifecycle cycles before dropping tables.
ALTER TABLE stories
  DROP CONSTRAINT stories_current_release_story_fkey;

ALTER TABLE story_editions
  DROP CONSTRAINT story_editions_draft_version_fkey;

ALTER TABLE story_sources
  DROP CONSTRAINT story_sources_current_version_fkey;

DROP TABLE story_release_editions;
DROP TABLE story_releases;
DROP TABLE story_orchestration_run_draft_ingest_editions;
DROP TABLE story_orchestration_run_draft_ingests;
DROP TABLE story_orchestration_run_editorial_reviews;
DROP TABLE story_orchestration_runs;
DROP TABLE story_source_versions;
DROP TABLE story_sources;
DROP TABLE source_acquisition_quality_reviews;
DROP TABLE source_acquisition_eligibility_assessments;
DROP TABLE source_acquisitions;
DROP TABLE reader_story_edition_overrides;
DROP TABLE reading_progress;
DROP TABLE story_segments;
DROP TABLE story_sections;
DROP TABLE story_versions;
DROP TABLE story_editions;
DROP TABLE story_contributors;
DROP TABLE contributors;
DROP TABLE stories;
DROP TABLE account_memberships;
DROP TABLE external_identities;
DROP TABLE principals;
DROP TABLE profiles;
DROP TABLE assets;
DROP TABLE accounts;

COMMIT;
