# Reader 2 backend contract

Reader 2 is a coordinated beta clean break across PostgreSQL, the Go API, and
the current Vue Reader. It replaces the two-request Reader 1 content model and
mode-specific progress locators. It is not a compatibility layer.

## Migration 00014

`00014_reader_2_contract.sql` adds these explicit `story_segments` columns:

| Column | Contract |
| --- | --- |
| `segment_kind` | `heading`, `paragraph`, or `other` |
| `heading_level` | 1–6 for a heading; otherwise null |
| `content_key` | Lowercase 64-character SHA-256 hex |
| `content_occurrence` | Positive, version-scoped occurrence of that key |
| `chapter_key` | Nearest preceding H2 key, including the H2 itself; otherwise null |
| `chapter_occurrence` | Positive occurrence of that H2 key, paired with `chapter_key` |

Ordinals remain positive and deterministic. `(story_version_id, content_key,
content_occurrence)` is unique. Chapter lookup has a version-scoped partial
index. The obsolete segment `locator` JSON column is removed because ingestion,
Store queries, API responses, fixtures, and the web Reader now use the explicit
columns.

The migration backfills every existing segment in ordinal order. It runs with
the repository's non-superuser Goose owner role and introduces no new runtime
table or privilege requirement.

### Intentional progress reset

The Up migration deletes every `reading_progress` row before installing the
strict Locator v2 constraint. This is an approved beta reset:

- Reader 1 locators are not parsed, translated, or retained;
- this reset is not a precedent for future production progress policy;
- Down deletes progress again and cannot restore the rows removed by Up;
- a rollback can restore the old schema shape, but not reader position data.

## Canonical content keys

One Go owner, `internal/readercontract`, defines the ingestion algorithm. SQL
backfill, explicit fixtures, and tests use the identical conceptual input:

```text
segment-kind + ASCII 0x1f + heading-level-or-0 + ASCII 0x1f
+ Markdown with CRLF and CR normalised to LF
```

SHA-256 is calculated over the UTF-8 bytes and encoded as lowercase hex.
Markdown is authoritative. The algorithm does not trim, collapse whitespace,
case-fold Unicode, or hash rendered HTML. Kind and heading level are part of
the input, so a heading and paragraph with the same visible words differ.
Duplicate canonical blocks share a key and receive increasing occurrences
within one story version.

Only H2 headings establish Reader chapters. The H2 belongs to its chapter;
following blocks inherit it until the next H2. H1 and H3–H6 do not start or
replace a chapter. Repeated H2 text shares a content/chapter key and receives an
increasing chapter occurrence.

Fixed SQL/Go vectors, CRLF/LF equivalence, UTF-8, duplicate blocks, repeated
chapters, and propagation are exercised by unit and disposable PostgreSQL
tests.

## Reader Locator v2

Progress uses one typed locator independent of reader mode:

```json
{
  "schema": 2,
  "segment": {
    "key": "64-character-lowercase-sha256",
    "occurrence": 1,
    "ordinal": 4,
    "offset": 0.35
  },
  "chapter": {
    "key": "64-character-lowercase-sha256",
    "occurrence": 1
  }
}
```

`schema` is numeric 2. Segment key, positive occurrence, positive ordinal, and
finite offset from 0 through 1 are required. Chapter is omitted before the
first H2; when the selected segment has a chapter, both chapter fields are
required and must match the stored segment. Unknown fields are rejected.

Go validates the typed model before storage. `ProgressPut` also verifies in
one transaction that the account owns a published story, the requested version
is exactly its current `published_version_id`, and
ordinal/key/content occurrence/chapter identity all describe one real segment.
The current story/version selection holds a story-row `FOR SHARE` lock through
locator validation, progress persistence, and commit. `AdminPublish` updates
that same row, so publication changes serialise with progress writes. Draft and
previously published versions are not currently readable and return 404 rather
than `locator_mismatch`. PostgreSQL provides the
`reading_progress_reader_locator_v2_check` defence in depth. Percentage is
rejected outside 0–1 rather than clamped on PUT.

`GET /api/v1/progress/{slug}` returns `{"progress":null}` for a known published
story with no progress. A missing or unpublished story remains 404. A saved
value is wrapped under `progress` and contains a typed Locator v2. After a later
publication, GET may still return progress for the older version so future
version-mapping UX can inspect it, but no new PUT may target that older version.
PUT returns `{"ok":true}` only after PostgreSQL commits. A structurally invalid
locator is 400; a well-formed locator that does not match a segment within the
current published version uses the stable safe code `locator_mismatch`.

## Coherent Reader endpoint

The only public content read is:

```text
GET /api/v1/reader/{slug}
```

It returns story metadata, the exact published version number, and that
version's ordered segment read model. Segments include identity fields,
rendered HTML, and word count. They do not include Markdown, internal IDs, the
old locator JSON, or a duplicate full-story HTML representation.

`Store.ReaderStory` uses one SQL statement, so publication cannot change
between independent metadata and segment queries. Account ownership and the
published pointer are part of that statement. The former
`GET /api/v1/story/{slug}` and `/segments` routes, `StoryLatest`,
`StorySegments`, and their frontend wrappers are removed, not aliased.

## Minimum web cutover

The existing Reader loads one coherent payload and renders its segments in
both scroll and temporary paged modes. Scroll capture selects the segment at a
35% viewport reading line and stores an offset within that segment. Paged
capture stores a representative visible segment, never a page index. Resume
prefers key plus occurrence and uses ordinal only as a same-version fallback.

Mode changes capture a Locator v2 anchor, suppress capture while changing the
representation, restore the same anchor, and issue at most one desired update.
If no anchor can be captured, no beginning locator is manufactured. The
existing baseline and serial save coordinator guarantees remain in force:
unknown progress is never treated as empty, only one PUT is in flight, the
latest desired value wins, and Saved follows server confirmation only.

Reader preferences are now the validated `pp_reader_prefs_v2` display schema. The beta-only v1 key is not migrated. Scroll architecture, accessibility, and browser coverage are documented in `reader-2-scroll-foundation.md`; none of those frontend changes alter migration 00014 or Locator v2.

## Rollout and rollback

API, migration, and web must be released as one coordinated change. Migration
00014 removes schema and HTTP contracts used by an older web bundle. Existing
browser/PWA clients may need a refresh after a future deployment. Do not stage
the new migration with the old API/web or the new web with the old API.

No production rollout is performed by this change. Before a separately
authorised rollout, take and verify a backup and accept that rolling the schema
Down cannot recover reset progress. Restoring pre-migration progress requires a
full pre-migration database restore, which also rolls back every later database
change; normal application rollback should therefore be forward-fix oriented.

The contract intentionally defers final pagination/reflow, version-to-version
mapping, updated-story UX, offline queues, profiles, sharing, and the Reader 2
visual redesign.
