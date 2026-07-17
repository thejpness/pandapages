# Test fixture lifecycle

Normal Panda Pages migrations leave an empty application catalogue. They create
the schema plus the shared Default account/profile bootstrap records, but no
test stories, story versions, segments, child or prompt profiles, reading
progress, profile settings, or generation jobs.

## Historical migration decision

Migration `00008_seed_test_data.sql` shipped in the original migration history
and is recorded as applied in the PostgreSQL storage audit. It cannot be
deleted, renumbered, or rewritten safely. Its complete logical inventory is:

| Relation | Historical fixture rows |
| --- | --- |
| `profiles` | Ensures `Default`; normally creates no row because `00007` already inserted it. |
| `child_profiles` | `Ted`, 34 months, interests `space/animals/trains`, sensitivity `no_spiders`. |
| `prompt_profiles` | `Bedtime calm v1` with the exact calm/UK-English rules payload and schema version 1. |
| `contributors` | `Aesop` and `Mary Shelley`. |
| `works` | `The Fox and the Grapes`, `The Little Star and the Moon`, and `Frankenstein; or, The Modern Prometheus`. |
| `stories` | Published slugs `the-fox-and-the-grapes`, `the-little-star-and-the-moon`, and `frankenstein-excerpt-ch1`. |
| `story_versions` | Version 1 for each of the three stories. |
| `story_sections` | One `Letter I` chapter for the Frankenstein excerpt. |
| `story_segments` | Three ordered segments for each story, nine total. |
| `story_contributors` | Fox → Aesop and Frankenstein → Mary Shelley. |
| `reading_progress` | Default profile → Fox version 1, paragraph 1, 33 percent. |
| `generation_jobs` | One succeeded Little Star job using `seed-model`/`v1`, Ted, and Bedtime calm v1. |
| Other relations | No account, profile-settings, asset, or schema-meta row. |

It also attempted to ensure a `Default` profile, but migration `00007` had
already created that runtime bootstrap profile. Migration `00011` subsequently
created the shared default account and assigned all existing profiles and
stories to it. That account may contain manually uploaded stories and is not a
fixture account.

Forward migration `00013_remove_historical_test_fixtures.sql` removes the
historical fixtures after `00008` has run. Because `00008` generated UUIDs,
`00013` cannot identify a story by an immutable fixture ID. It instead requires
an exact lifecycle fingerprint before deleting a story root: oldest-account
ownership; story, work, and publication metadata; exactly one version numbered
1; the historical frontmatter, Markdown, rendered HTML, and content hash; both
draft and published pointers targeting that version; exact section and segment
rows, counts, ordinals, locators, ownership, content, and word counts; and the
historical contributor links. Cascades start only at story rows satisfying
every part of that fingerprint. The migration separately deletes the exact
seeded child, prompt, and generation-job payloads, and deletes historical work
and contributor rows only when no remaining story references them.

The shared account and Default profile are always preserved. A work or
contributor reused by unrelated content is preserved. The cleanup Down path
does not restore test data; use the explicit command below instead.

The historical migration did not provide immutable IDs. A content or hash
change, additional version, new draft, republication, pointer change, structural
change, metadata change, ownership ambiguity, or any other fingerprint mismatch
preserves the entire story with its versions, sections, segments, contributor
links, and progress for manual review. Names such as `Ted`, `Aesop`, and
`Mary Shelley`, the classic work titles, and even the three public-looking
slugs can plausibly collide with intentional content. For that reason story
metadata alone is never treated as a complete fixture fingerprint. Deletion
order is the exact fixture generation job, exact unchanged historical story
lifecycles and their dependants, exact child/prompt payloads, then only
unreferenced work/contributor rows.

## Local workflow

Start the normal development stack and let the one-shot migration service
finish. The resulting database has no story content. Test data is never seeded
automatically by Compose.

Install the deterministic reader fixture explicitly:

```bash
PP_ALLOW_TEST_SEED=1 \
PP_TEST_SEED_DATABASE=pandapages \
scripts/dev/seed-test-data.sh
```

The fixture contains one published UTF-8 story, two chapters, six ordered
segments, fixed test-only UUIDs, a test work/contributor, a child/prompt pair,
and one test generation job. The segments mirror ingestion's top-level block
model: H1, opening paragraph, Chapter One H2, its paragraph, Chapter Two H2,
and its paragraph are six independent rows with deterministic Reader 2
content keys, occurrences, kinds, heading levels, and H2 chapter propagation.
It does not include progress by default.

Add the Default-profile progress row only for a test that needs it:

```bash
PP_ALLOW_TEST_SEED=1 \
PP_TEST_SEED_DATABASE=pandapages \
scripts/dev/seed-test-data.sh --with-progress
```

Rerunning either seed form is idempotent. Remove only the explicit fixed-ID
fixtures, then recreate them if needed:

```bash
PP_ALLOW_TEST_SEED=1 \
PP_TEST_SEED_DATABASE=pandapages \
scripts/dev/seed-test-data.sh --remove
```

Run `scripts/dev/seed-test-data.sh --help` for the command contract.

## Safety boundary

The seed command accepts no database URL or password. It:

- requires the exact acknowledgement `PP_ALLOW_TEST_SEED=1`;
- requires the exact database name `pandapages`;
- refuses `DOCKER_HOST`, unknown Docker contexts, and non-Unix Docker endpoints;
- targets either the single running development PostgreSQL container labelled
  `com.pandapages.test-seed-target=local-development`, or an explicitly named
  disposable integration container with the dedicated test label;
- requires the target to be running with migration `00014` applied;
- uses container-local `psql`, prints no database credential, performs no
  network request, and creates no temporary credential file.

Production Compose has no test-seed label or seed service. The seed command is
not invoked by either Compose migration service or by the API.

## Automated coverage

`scripts/tests/postgresql-fixtures-integration.sh` creates generated local
PostgreSQL resources and proves:

- a fresh full migration has the required schema and no fixtures;
- the real `up-to 12` history contains the complete historical inventory;
- `00013` removes an unchanged historical lifecycle and all fixture-owned
  dependants;
- content-only edits, additional drafts, and republished versions preserve the
  complete ambiguous story lifecycle, including segments, pointers, contributor
  links, and progress;
- the account, Default profile, unrelated same-account
  story/version/segment/progress/job, unrelated profile/settings, and shared
  catalogue records remain;
- cleanup rollback/reapplication is non-restoring and idempotent;
- seed refusal cases fail closed;
- explicit six-block seed shape, optional progress, rerun, removal, and
  recreation are safe;
- migration `00014` resets beta progress and removes the obsolete segment
  locator while preserving the explicit fixture boundary;
- the signed-session coherent Reader endpoint reads all six UTF-8 segments;
- optional progress is a valid strict Reader Locator v2;
- every generated container, network, volume, credential, and artifact is
  removed.

The suite runs in the protected `Configuration` CI job. It uses only generated
non-production data and local Docker resources.

`scripts/tests/reader-store-integration.sh`, in the protected `Backend` job,
separately proves SQL/Go key parity, migration 13→14 and Down/Up behaviour,
direct locator constraints, coherent reads during republish, account isolation,
strict progress validation, removed Reader 1 endpoints, and no false HTTP
success after a database write failure.
