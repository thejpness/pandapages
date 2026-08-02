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
- An account can contain multiple reader profiles. The current runtime still
  has a temporary exact-name `Default` resolver, not profile selection.
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

## Transitional `Default` resolver

Migration 00015 deliberately adds no default-profile marker, partial index, or
profile-selection schema. Its durable concern is account ownership and
referential integrity only.

Until explicit profile selection is implemented, the runtime has a temporary,
account-scoped compatibility resolver:

1. it reads a profile named exactly `Default` for the supplied account;
2. if absent, it inserts that exact name under the existing
   `(account_id, name)` uniqueness constraint with `ON CONFLICT DO NOTHING`;
3. it reads and returns the exact-name profile.

It never repurposes an existing differently named profile and it does not
represent an active selected reader profile. This transitional resolver will be
removed by the later explicit profile-selection work; migration 00015 creates no
new persistent selection contract.

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

## Broader optional read-only operator audit

Before rollout, an approved read-only operator may run the following broader
audit against a version-14 database. It includes the ownership and
reference-integrity shapes that migration `00015` rejects, plus additional
hygiene checks for orphan story versions and duplicate profile names and story
slugs. It returns categories and counts, never IDs, profile names, story
content, or other personal data:

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

Every category in this broader audit should be zero. Accounts without profiles
are valid; migration `00015` does not create a profile or choose one for an
account. Stop on any non-zero count; do not delete, merge, or reassign rows
automatically.

Migration `00015` remains authoritative for its own fail-closed preflight. It
performs the narrower ownership and reference-integrity checks in the migration
transaction while holding write-blocking table locks and rolls the transaction
back on failure. The orphan-story-version and duplicate-name/slug hygiene
checks above are useful operator inspection, not migration `00015` categories.


## Migration and rollback characteristics

Migration 00015 takes write-blocking locks on the ownership tables so preflight,
backfill, index construction, and validation are atomic. Reads remain possible,
but application writes must be quiesced for the coordinated rollout. Progress
and settings account backfills rewrite those rows; unique-index construction
and foreign-key validation scan the affected tables. Operators must assess row
counts, lock waiters, WAL capacity, and change-window duration beforehand.

The Down migration removes only version-15 ownership columns, constraints, and
indexes and restores the version-14 simple foreign keys. It does not delete or
create profiles, stories, progress, settings, or content. Rollback is therefore
schema-reversible and preserves the existing data graph, while necessarily
discarding the redundant progress/settings account columns. An older API resumes
its existing exact-name `Default` resolver after rollback. The
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
