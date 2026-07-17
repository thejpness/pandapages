# Reader 2 scroll foundation

The Reader 2 scroll foundation is the primary browser reading experience. It
uses the coherent GET /api/v1/reader/{slug} response and strict Locator v2
progress contract introduced by migration 00014; it does not change the API,
database, segment identity, chapter identity, or progress schema.

## Frontend ownership

Reader.vue is the route-level orchestrator. It owns the active route,
coordinates focused composables, and composes the visual components:

- useReaderStory owns one cancellable Reader request per route generation,
  strict ready/not-found/unavailable states, document title, and stale-response
  rejection.
- useReaderProgress is the sole Vue owner of the existing progress-baseline
  controller and progress-save coordinator. The pure controllers still own
  load deduplication, one-in-flight writes, latest-wins serialization, retry,
  truthful confirmation, drain, and keepalive behavior.
- useReaderPreferences owns validated local Reader preferences.
- ReaderScrollView renders canonical ordered segments and schedules bounded
  content-anchor capture.
- ReaderPagedView isolates the existing transitional two-segment paging
  representation. Final pagination and reflow remain a later roadmap PR.
- Header, story states, settings, chapters, progress navigation, and
  resume/changed-story decisions are focused components.

The API's Goldmark safe-mode output remains the reviewed HTML-rendering
boundary. Reader components never render Markdown or arbitrary client HTML.

## Preferences v2

Preferences are stored under pp_reader_prefs_v2 with this exact shape:

    schema: 2
    mode: scroll | paged
    theme: night | warm
    fontFamily: book | clear | system
    fontSize: 17..32
    lineHeight: 1.4..2.0
    contentWidth: 560..900

Every field and key is validated. Finite numeric values are clamped; malformed
JSON, missing fields, unknown fields, invalid enums, and storage failures fall
back safely. Defaults are immutable, saved values are validated, and Reset to
Defaults produces a fresh value.

There is deliberately no migration from beta-only pp_reader_prefs_v1.

Book uses the local Literata variable font, Clear uses the local Atkinson
Hyperlegible Next variable font, and System uses the native system stack. Only
Latin normal and italic weight-axis WOFF2 assets are built. No font CDN or
remote asset request is used.

Approved packages are pinned exactly: Reka UI 2.10.1 and VueUse 14.3.0
(MIT), Literata 5.2.8 and Atkinson Hyperlegible Next 5.2.6 (SIL OFL),
Playwright 1.61.1 (Apache-2.0), and axe-core Playwright 4.12.1
(MPL-2.0). Playwright and axe remain development-only and do not enter the
production bundle.

## Scroll location and percentage

Locator v2 remains authoritative. The scroll Reader uses a reading line at 35%
of the usable viewport below the sticky header. It selects the segment
intersecting that line, calculates the offset within the segment, and carries
the segment's server-provided chapter identity.

Capture is scheduled once per animation frame from passive scroll listeners.
Resize and font-layout observations update visual state but are not interpreted
as intentional reading movement. Programmatic resume, Start Over, chapter
navigation, and representation changes suppress native capture until the
anchor has settled. Representation changes preserve the exact segment anchor
without publishing new movement; the transitional paged view retains that
anchor until the reader deliberately changes pages. A trailing reading runway
lets the final segment reach the reading line and produce 100% progress.

Progress percentage is content based:

    (weights before active segment + active weight × offset) / total weight

Segment word counts are weights, with a minimum weight of one for headings and
zero-word blocks. The result is clamped to 0..1, deterministic for the same
anchor, and independent of document height.

## Resume and version boundaries

A meaningful same-version baseline opens a modal decision:

- Resume restores key + occurrence + ordinal identity and offset;
- Start over moves to the first segment and saves through the normal
  coordinator only when the baseline is ready;
- Dismiss leaves the visible location unchanged and does not save by itself.

Identity must agree with the stored ordinal before exact restore. The existing
ordinal fallback is used only when exact identity cannot be used.

Old-version progress is not mapped. It opens a safe “This story has changed”
boundary with Start this version and Return to Library. No current-version
write is made until the reader explicitly starts or later moves intentionally.

Movement while a slow initial baseline is pending cannot be treated as
server-confirmed. A late same-version saved position retains an explicit resume
decision. Movement during an unavailable baseline remains desired and
unconfirmed after successful retry. No PUT is possible before baseline
readiness.

## Accessibility and browser coverage

The Reader provides semantic main/article structure, one visible content H1,
a labelled progressbar, visible focus, 44px-class controls, explicit story
states, restrained live regions, and local low-glare Night/Warm themes.

Settings, chapters, resume, and changed-story decisions use Reka UI Dialog for
modal semantics, focus trapping, Escape, background isolation, body-scroll
locking, and focus return. Reduced-motion preferences disable animated
restoration and UI motion.

Forty Playwright scenarios run against a loopback Vite server with deterministic intercepted
API responses and Chromium in protected CI. Browser tests cover the coherent
content request, Locator v2 persistence and failures, baseline recovery, progress and story session loss, Library drain and Leave anyway,
missing/service states, dialogs and focus, chapters, resume/version
boundaries, stale requests, reduced motion, and transitional paged mode. Axe
checks representative ready, modal, and error states for serious and critical
violations.

## Rollout boundary

API and web still need to be updated together for the Reader 2 contract.
Existing stale PWA clients may require refresh as documented by the backend
contract. This frontend PR adds no migration, deployment action, offline story
support, cross-version mapping, or final paged-reading behavior.
