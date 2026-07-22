# Account ownership model

Status: current repository contract from Goose migration
`00015_account_ownership_integrity.sql`.

This document describes Panda Pages application ownership. It does not create
or describe an external authentication identity. The active authentication
contract remains the signed shared-passcode session documented in
[current-authentication-contract.md](current-authentication-contract.md).

## Domain boundary

An `account` is a Panda Pages household or tenant. It is the application data
boundary, not a person and not an external authentication identity.

- Every reader `profile` belongs to exactly one account.
- Every `story` belongs to exactly one account. Story versions, sections, and
  segments inherit that ownership through their story.
- Every `child_profile` and `prompt_profile` belongs to exactly one account.
  They are application configuration and children's personas, not login users.
- An account can contain multiple reader profiles, but the current application
  uses only its explicitly marked default profile.
- Reading progress and profile settings cannot connect data from different
  accounts.

The `profiles`, `stories`, `child_profiles`, and `prompt_profiles` account
columns are non-null and reference `accounts(id)` with `ON DELETE RESTRICT`.
These root constraints replace the previous convention-only ownership model.

## Database-enforced relationships

`reading_progress.account_id` is non-null and participates in composite foreign
keys to both its profile and story:

```text
(profile_id, account_id) -> profiles(id, account_id)
(story_id, account_id)   -> stories(id, account_id)
```

The existing one-row-per-profile/story primary key remains unchanged. A third
composite relationship enforces that `story_version_id` belongs to the same
`story_id` recorded by the progress row:

```text
(story_version_id, story_id) -> story_versions(id, story_id)
```

Consequently, direct SQL cannot insert or update progress to use another
account's profile or story, or a version belonging to another story. Store
queries also carry the authenticated account ID explicitly; the constraints
are the final defence if application scoping is bypassed.

`profile_settings.account_id` is non-null and must match its owning profile.
Nullable active child and prompt references use the same account tuple:

```text
(profile_id, account_id)               -> profiles(id, account_id)
(active_child_profile_id, account_id)  -> child_profiles(id, account_id)
(active_prompt_profile_id, account_id) -> prompt_profiles(id, account_id)
```

Deleting a selected child or prompt configuration still clears only that
nullable selection. Deleting a profile retains the existing cascades to its
settings and progress. Deleting a story retains the existing cascades beneath
that story. Migration 00015 does not introduce an account-wide cascade.

## Explicit default profile

`profiles.is_default` is a non-null Boolean marker. The partial unique index
`profiles_one_default_per_account_idx` permits at most one marked profile per
account.

Migration 00015 preserves the legacy effective rule for every account present
at migration time: it marks the account-owned profile named exactly `Default`
when one exists; otherwise it creates a separate account-owned `Default`
profile. It never designates an existing differently named profile because it
is oldest. The v14 per-account name uniqueness rule makes duplicate
`Default` names invalid; the migration's `created_at ASC, id ASC` ordering is
deterministic defensive handling.

At runtime, default-profile resolution:

1. reads an account-owned profile named exactly `Default` whose
   `is_default` marker is true without taking an account-row lock;
2. on a missing or invalid marker, starts a transaction and locks that account
   row;
3. re-reads the marker after the lock, returning it when another request has
   already repaired it;
4. otherwise clears an invalid marker, selects an existing exact `Default`,
   or creates one, then marks that profile before committing.

The account-row lock serializes repair or creation across API processes, and
the partial unique index is the final concurrency guard. The frontend still
uses this one profile; migration 00015 adds no profile-selection UI.

## Account deletion policy

Direct deletion of an account that still owns a profile, story, child profile,
or prompt profile is rejected by PostgreSQL. Panda Pages does not recursively
delete a household. A future explicit account-deletion service must decide how
to archive or delete each owned data set in a controlled transaction.

An account with no owned root rows can be deleted at the relational level.
A database principal with the existing table-level `DELETE` privilege can
therefore delete an empty account, while the root foreign keys reject deletion
of an owned account. There is no current delete-account API; this migration does
not add one.

## Read-only migration preflight

Run the following against a version-14 database only from an approved
read-only operator context. It returns categories and counts, never IDs,
profile names, story content, or other personal data:

```sql
WITH integrity_counts(category, row_count) AS (
  SELECT 'orphan_profiles', count(*)
  FROM profiles p LEFT JOIN accounts a ON a.id = p.account_id
  WHERE a.id IS NULL
  UNION ALL
  SELECT 'orphan_stories', count(*)
  FROM stories s LEFT JOIN accounts a ON a.id = s.account_id
  WHERE a.id IS NULL
  UNION ALL
  SELECT 'orphan_child_profiles', count(*)
  FROM child_profiles cp LEFT JOIN accounts a ON a.id = cp.account_id
  WHERE a.id IS NULL
  UNION ALL
  SELECT 'orphan_prompt_profiles', count(*)
  FROM prompt_profiles pp LEFT JOIN accounts a ON a.id = pp.account_id
  WHERE a.id IS NULL
  UNION ALL
  SELECT 'orphan_story_versions', count(*)
  FROM story_versions sv LEFT JOIN stories s ON s.id = sv.story_id
  WHERE s.id IS NULL
  UNION ALL
  SELECT 'progress_missing_profile', count(*)
  FROM reading_progress rp LEFT JOIN profiles p ON p.id = rp.profile_id
  WHERE p.id IS NULL
  UNION ALL
  SELECT 'progress_missing_story', count(*)
  FROM reading_progress rp LEFT JOIN stories s ON s.id = rp.story_id
  WHERE s.id IS NULL
  UNION ALL
  SELECT 'progress_missing_version', count(*)
  FROM reading_progress rp
  LEFT JOIN story_versions sv ON sv.id = rp.story_version_id
  WHERE sv.id IS NULL
  UNION ALL
  SELECT 'cross_account_progress', count(*)
  FROM reading_progress rp
  JOIN profiles p ON p.id = rp.profile_id
  JOIN stories s ON s.id = rp.story_id
  WHERE p.account_id IS DISTINCT FROM s.account_id
  UNION ALL
  SELECT 'cross_story_progress_version', count(*)
  FROM reading_progress rp
  JOIN story_versions sv ON sv.id = rp.story_version_id
  WHERE sv.story_id IS DISTINCT FROM rp.story_id
  UNION ALL
  SELECT 'settings_missing_profile', count(*)
  FROM profile_settings ps LEFT JOIN profiles p ON p.id = ps.profile_id
  WHERE p.id IS NULL
  UNION ALL
  SELECT 'settings_missing_child', count(*)
  FROM profile_settings ps
  LEFT JOIN child_profiles cp ON cp.id = ps.active_child_profile_id
  WHERE ps.active_child_profile_id IS NOT NULL AND cp.id IS NULL
  UNION ALL
  SELECT 'settings_missing_prompt', count(*)
  FROM profile_settings ps
  LEFT JOIN prompt_profiles pp ON pp.id = ps.active_prompt_profile_id
  WHERE ps.active_prompt_profile_id IS NOT NULL AND pp.id IS NULL
  UNION ALL
  SELECT 'cross_account_child_settings', count(*)
  FROM profile_settings ps
  JOIN profiles p ON p.id = ps.profile_id
  JOIN child_profiles cp ON cp.id = ps.active_child_profile_id
  WHERE p.account_id IS DISTINCT FROM cp.account_id
  UNION ALL
  SELECT 'cross_account_prompt_settings', count(*)
  FROM profile_settings ps
  JOIN profiles p ON p.id = ps.profile_id
  JOIN prompt_profiles pp ON pp.id = ps.active_prompt_profile_id
  WHERE p.account_id IS DISTINCT FROM pp.account_id
  UNION ALL
  SELECT 'accounts_without_profiles', count(*)
  FROM accounts a
  WHERE NOT EXISTS (SELECT 1 FROM profiles p WHERE p.account_id = a.id)
  UNION ALL
  SELECT 'multiple_legacy_default_candidates', count(*)
  FROM (
    SELECT account_id
    FROM profiles
    WHERE name = 'Default'
    GROUP BY account_id
    HAVING count(*) > 1
  ) conflicts
  UNION ALL
  SELECT 'duplicate_profile_names_per_account', count(*)
  FROM (
    SELECT account_id, name
    FROM profiles
    GROUP BY account_id, name
    HAVING count(*) > 1
  ) conflicts
  UNION ALL
  SELECT 'duplicate_story_slugs_per_account', count(*)
  FROM (
    SELECT account_id, slug
    FROM stories
    GROUP BY account_id, slug
    HAVING count(*) > 1
  ) conflicts
)
SELECT category, row_count
FROM integrity_counts
ORDER BY category;
```

Every category except `accounts_without_profiles` must be zero. Accounts without
profiles are valid before migration 00015; the migration creates an
account-owned `Default` profile for each such account. Stop on any other
non-zero count; do not delete, merge, or reassign rows automatically. Migration
00015 repeats its required checks while holding write-blocking table locks and
rolls its entire transaction back on failure.

After migration, this count must also be zero:

```sql
SELECT count(*) AS accounts_with_invalid_default_count
FROM accounts a
WHERE EXISTS (SELECT 1 FROM profiles p WHERE p.account_id = a.id)
  AND 1 <> (
    SELECT count(*)
    FROM profiles p
    WHERE p.account_id = a.id AND p.is_default
  );
```

## Migration and rollback characteristics

Migration 00015 takes write-blocking locks on the ownership tables so preflight,
backfill, index construction, and validation are atomic. Reads remain possible,
but application writes must be quiesced for the coordinated rollout. Progress
and settings account backfills rewrite those rows; unique-index construction
and foreign-key validation scan the affected tables. Operators must assess row
counts, lock waiters, WAL capacity, and change-window duration beforehand.

The Down migration removes only version-15 columns, constraints, and indexes and
restores the version-14 simple foreign keys. It does not delete profiles,
stories, progress, settings, or content. In particular, it does not delete
`Default` profiles created by migration 00015: they remain valid ordinary v14
profiles. Rollback is therefore schema-reversible but is not guaranteed to
restore the original profile count. Preserving those profiles is intentional
because they may have acquired progress, settings, or relationships after
migration; operators must not interpret retained additional profiles as a
failed rollback. The Down migration necessarily discards the redundant
progress/settings account columns and the explicit default marker. An older API
may therefore resume its legacy name-based profile choice after rollback. The
[coordinated rollout and rollback order](../operations/postgresql-least-privilege-roles.md#migration-00015-coordinated-forward-rollout)
must be followed; mixed API/schema versions are not a supported steady state.

## Future identity boundary

Migration 00015 adds no identity, membership, role, email, OAuth, JWT, or
Supabase runtime schema. Provider-neutral identity mapping is the next phase.

- A first external login must create or explicitly claim an account through a
  reviewed transaction.
- It must never claim the oldest account merely because that account exists.
- Email is mutable provider metadata and is not an authoritative ownership key.
- Account claiming must be explicit, auditable, and rollback-aware.
- Children's profiles remain application personas and must not become external
  authentication identities.

These rules preserve a provider-neutral household boundary before any future
Supabase integration.
