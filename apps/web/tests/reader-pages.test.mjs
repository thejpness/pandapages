import assert from 'node:assert/strict'
import test from 'node:test'
import { loadTypeScript } from './helpers/typescript-module.mjs'

async function moduleAt(relativePath) {
  return (await loadTypeScript(relativePath, import.meta.url)).module
}

function key(index) {
  return Math.max(1, index).toString(16).padStart(64, '0').slice(-64)
}

function segment(overrides = {}) {
  const ordinal = overrides.ordinal ?? 1
  const kind = overrides.kind ?? 'paragraph'
  return {
    ordinal,
    kind,
    headingLevel:
      overrides.headingLevel === undefined
        ? kind === 'heading'
          ? 2
          : null
        : overrides.headingLevel,
    contentKey: overrides.contentKey ?? key(ordinal),
    contentOccurrence: overrides.contentOccurrence ?? 1,
    chapterKey: overrides.chapterKey ?? null,
    chapterOccurrence: overrides.chapterOccurrence ?? null,
    renderedHtml:
      overrides.renderedHtml ??
      (kind === 'heading'
        ? '<h2>Chapter 🐼</h2>'
        : '<p>UTF-8 café 星 🐼</p>'),
    wordCount: overrides.wordCount ?? 24,
  }
}

function metrics(overrides = {}) {
  return {
    fontSize: overrides.fontSize ?? 20,
    lineHeight: overrides.lineHeight ?? 1.65,
    contentWidth: overrides.contentWidth ?? 720,
    availableHeight: overrides.availableHeight ?? 640,
  }
}

function locatorFor(candidate, offset = 0.35) {
  const locator = {
    schema: 2,
    segment: {
      key: candidate.contentKey,
      occurrence: candidate.contentOccurrence,
      ordinal: candidate.ordinal,
      offset,
    },
  }
  if (candidate.chapterKey !== null && candidate.chapterOccurrence !== null) {
    locator.chapter = {
      key: candidate.chapterKey,
      occurrence: candidate.chapterOccurrence,
    }
  }
  return locator
}

test('capacity covers mobile, landscape, tablet, desktop and preference changes', async () => {
  const pages = await moduleAt('../src/lib/reader-pages.ts')
  const cases = [
    ['narrow mobile', metrics({ contentWidth: 288, availableHeight: 500 }), [27, 4, 15]],
    ['landscape', metrics({ contentWidth: 600, availableHeight: 240 }), [57, 9, 7]],
    ['tablet', metrics({ contentWidth: 640, availableHeight: 700 }), [61, 10, 21]],
    ['desktop', metrics({ contentWidth: 720, availableHeight: 800 }), [69, 11, 24]],
    [
      'large font',
      metrics({
        fontSize: 32,
        lineHeight: 1.65,
        contentWidth: 720,
        availableHeight: 800,
      }),
      [43, 7, 15],
    ],
    ['wide content', metrics({ contentWidth: 900, availableHeight: 800 }), [86, 14, 24]],
    ['compact lines', metrics({ lineHeight: 1.4, availableHeight: 800 }), [69, 11, 28]],
    ['spacious lines', metrics({ lineHeight: 2, availableHeight: 800 }), [69, 11, 20]],
  ]

  for (const [name, layout, expected] of cases) {
    const capacity = pages.readerPageCapacity(layout)
    assert.deepEqual(
      [
        capacity.charactersPerLine,
        capacity.wordsPerLine,
        capacity.capacityLines,
      ],
      expected,
      name,
    )
  }

  const first = pages.readerPageCapacity(cases[3][1])
  const repeated = pages.readerPageCapacity({ ...cases[3][1] })
  assert.deepEqual(repeated, first)
})

test('grouping includes every segment once and keeps a short H2 with following content', async () => {
  const pages = await moduleAt('../src/lib/reader-pages.ts')
  const segments = [
    segment({ ordinal: 1, wordCount: 36 }),
    segment({ ordinal: 2, kind: 'heading', headingLevel: 2, wordCount: 4 }),
    segment({ ordinal: 3, wordCount: 27 }),
    segment({ ordinal: 4, wordCount: 18 }),
  ]
  const result = pages.buildReaderPages(
    segments,
    metrics({
      fontSize: 20,
      lineHeight: 1.5,
      contentWidth: 600,
      availableHeight: 300,
    }),
  )

  assert.deepEqual(
    result.map((page) => page.segments.map(({ ordinal }) => ordinal)),
    [[1], [2, 3], [4]],
  )
  assert.deepEqual(
    result.flatMap((page) => page.segments),
    segments,
  )
  assert.ok(result.every((page) => page.segments.length > 0))
  assert.deepEqual(
    result.map((page) => [
      page.index,
      page.startOrdinal,
      page.endOrdinal,
      page.capacityLines,
      page.oversized,
    ]),
    [
      [0, 1, 1, 10, false],
      [1, 2, 3, 10, false],
      [2, 4, 4, 10, false],
    ],
  )
})

test('duplicate headings and invalid word counts remain stable and non-empty', async () => {
  const pages = await moduleAt('../src/lib/reader-pages.ts')
  const repeatedKey = 'a'.repeat(64)
  const segments = [
    segment({
      ordinal: 1,
      kind: 'heading',
      contentKey: repeatedKey,
      contentOccurrence: 1,
      wordCount: 0,
      renderedHtml: '<h2>Same 🐼</h2>',
    }),
    segment({ ordinal: 2, wordCount: -10 }),
    segment({
      ordinal: 3,
      kind: 'heading',
      contentKey: repeatedKey,
      contentOccurrence: 2,
      wordCount: Number.NaN,
      renderedHtml: '<h2>Same 🐼</h2>',
    }),
    segment({ ordinal: 4, kind: 'heading', headingLevel: 3, wordCount: 0 }),
  ]
  const result = pages.buildReaderPages(segments, metrics())

  assert.deepEqual(
    result.flatMap((page) =>
      page.segments.map((candidate) => [
        candidate.ordinal,
        candidate.contentOccurrence,
      ]),
    ),
    [[1, 1], [2, 1], [3, 2], [4, 1]],
  )
  assert.ok(result.every((page) => page.estimatedLines > 0))
  assert.ok(result.every((page) => Number.isFinite(page.estimatedLines)))
  assert.deepEqual(pages.buildReaderPages([], metrics()), [])
})

test('an oversized segment is isolated while every segment remains readable', async () => {
  const pages = await moduleAt('../src/lib/reader-pages.ts')
  const segments = [
    segment({ ordinal: 1, wordCount: 12 }),
    segment({
      ordinal: 2,
      wordCount: 1_000,
      renderedHtml: '<p>One very long unbroken-content block 🐼</p>',
    }),
    segment({ ordinal: 3, wordCount: 12 }),
  ]
  const result = pages.buildReaderPages(segments, metrics())
  const oversized = result.find((page) => page.oversized)

  assert.ok(oversized)
  assert.deepEqual(oversized.segments.map(({ ordinal }) => ordinal), [2])
  assert.ok(oversized.estimatedLines > oversized.capacityLines)
  assert.deepEqual(
    result.flatMap((page) => page.segments.map(({ ordinal }) => ordinal)),
    [1, 2, 3],
  )
  const single = pages.buildReaderPages(
    [segment({ ordinal: 9, wordCount: 1 })],
    metrics(),
  )
  assert.equal(single.length, 1)
  assert.equal(single[0].oversized, false)
})

test('identical input deterministically builds identical pages without mutation', async () => {
  const pages = await moduleAt('../src/lib/reader-pages.ts')
  const segments = Object.freeze(
    Array.from({ length: 100 }, (_, index) =>
      Object.freeze(
        segment({
          ordinal: index + 1,
          wordCount: (index % 17) + 1,
          renderedHtml: '<p>Repeated UTF-8 café 星 🐼</p>',
        }),
      ),
    ),
  )
  const layout = metrics({
    fontSize: 23,
    lineHeight: 1.7,
    contentWidth: 618,
    availableHeight: 537,
  })

  assert.deepEqual(
    pages.buildReaderPages(segments, layout),
    pages.buildReaderPages(segments, { ...layout }),
  )
  assert.deepEqual(
    segments.map(({ ordinal }) => ordinal),
    Array.from({ length: 100 }, (_, index) => index + 1),
  )
})

test('locator mapping distinguishes occurrences and limits ordinal fallback', async () => {
  const pages = await moduleAt('../src/lib/reader-pages.ts')
  const duplicateKey = 'b'.repeat(64)
  const segments = [
    segment({
      ordinal: 1,
      contentKey: duplicateKey,
      contentOccurrence: 1,
      wordCount: 30,
    }),
    segment({
      ordinal: 2,
      contentKey: duplicateKey,
      contentOccurrence: 2,
      wordCount: 30,
    }),
    segment({ ordinal: 3, contentKey: 'c'.repeat(64), wordCount: 30 }),
  ]
  const result = pages.buildReaderPages(
    segments,
    metrics({
      fontSize: 20,
      lineHeight: 2,
      contentWidth: 280,
      availableHeight: 200,
    }),
  )

  assert.equal(
    pages.readerPageForLocator(result, segments, locatorFor(segments[0])),
    0,
  )
  assert.equal(
    pages.readerPageForLocator(result, segments, locatorFor(segments[1])),
    1,
  )
  assert.equal(pages.readerPageForOrdinal(result, 3), 2)
  assert.equal(pages.readerPageForOrdinal(result, 99), -1)

  const fallback = locatorFor(segments[2])
  fallback.segment.key = 'f'.repeat(64)
  assert.equal(pages.readerPageForLocator(result, segments, fallback), 2)

  const mismatch = locatorFor(segments[0])
  mismatch.segment.ordinal = 2
  assert.equal(pages.readerPageForLocator(result, segments, mismatch), -1)
})

test('representative Locator v2 carries chapter identity and oversized offset', async () => {
  const pages = await moduleAt('../src/lib/reader-pages.ts')
  const chapterKey = 'd'.repeat(64)
  const chapter = segment({
    ordinal: 7,
    kind: 'heading',
    contentKey: chapterKey,
    chapterKey,
    chapterOccurrence: 3,
    wordCount: 3,
  })
  const ordinary = pages.buildReaderPages([chapter], metrics())[0]
  assert.deepEqual(pages.readerPageRepresentativeLocator(ordinary, 0.8), {
    schema: 2,
    segment: {
      key: chapterKey,
      occurrence: 1,
      ordinal: 7,
      offset: 0,
    },
    chapter: { key: chapterKey, occurrence: 3 },
  })

  const oversized = pages.buildReaderPages(
    [segment({ ordinal: 8, wordCount: 2_000 })],
    metrics(),
  )[0]
  assert.equal(oversized.oversized, true)
  assert.equal(
    pages.readerPageRepresentativeLocator(oversized, 0.4).segment.offset,
    0.4,
  )

  const geometry = { scrollHeight: 1_000, clientHeight: 400 }
  assert.equal(pages.readerOversizedOffset({ ...geometry, scrollTop: 240 }), 0.4)
  assert.equal(pages.readerOversizedScrollTop({ ...geometry, offset: 0.4 }), 240)
  assert.equal(
    pages.readerOversizedOffset({ ...geometry, scrollTop: 10_000 }),
    1,
  )
  assert.equal(pages.readerOversizedScrollTop({ ...geometry, offset: -2 }), 0)
  assert.equal(
    pages.readerOversizedOffset({
      scrollTop: 50,
      scrollHeight: 300,
      clientHeight: 400,
    }),
    0,
  )
  assert.equal(pages.readerPageRepresentativeLocator(null), null)
})

test('reflow changes page index while leaving the canonical anchor unchanged', async () => {
  const pages = await moduleAt('../src/lib/reader-pages.ts')
  const segments = Array.from({ length: 12 }, (_, index) =>
    segment({ ordinal: index + 1, wordCount: 25 }),
  )
  const anchor = locatorFor(segments[8], 0.37)
  const before = structuredClone(anchor)
  const wide = pages.buildReaderPages(
    segments,
    metrics({
      fontSize: 18,
      lineHeight: 1.5,
      contentWidth: 900,
      availableHeight: 800,
    }),
  )
  const narrow = pages.buildReaderPages(
    segments,
    metrics({
      fontSize: 20,
      lineHeight: 1.65,
      contentWidth: 300,
      availableHeight: 500,
    }),
  )

  const wideIndex = pages.readerPageForLocator(wide, segments, anchor)
  const narrowIndex = pages.readerPageForLocator(narrow, segments, anchor)
  assert.notEqual(wideIndex, narrowIndex)
  assert.ok(wideIndex >= 0)
  assert.ok(narrowIndex >= 0)
  assert.deepEqual(anchor, before)
  assert.equal(Object.hasOwn(anchor, 'page'), false)
  assert.equal(Object.hasOwn(anchor.segment, 'scrollTop'), false)
})

test('keyboard keys map to one bounded canonical page target', async () => {
  const navigation = await moduleAt('../src/lib/reader-page-navigation.ts')
  const base = { currentPage: 2, pageCount: 5 }
  for (const [keyName, expected] of [
    ['ArrowRight', 3],
    ['PageDown', 3],
    ['ArrowLeft', 1],
    ['PageUp', 1],
    ['Home', 0],
    ['End', 4],
  ]) {
    assert.equal(
      navigation.readerPageNavigationTarget({ ...base, key: keyName }),
      expected,
    )
  }
})

test('keyboard ignores interactive focus, modal state, modifiers and shortcuts', async () => {
  const navigation = await moduleAt('../src/lib/reader-page-navigation.ts')
  const base = { key: 'ArrowRight', currentPage: 2, pageCount: 5 }

  for (const targetTagName of [
    'input',
    'TEXTAREA',
    'Select',
    'button',
    'a',
    'summary',
  ]) {
    assert.equal(
      navigation.readerPageNavigationTarget({ ...base, targetTagName }),
      null,
    )
  }
  for (const ignored of [
    { targetIsContentEditable: true },
    { targetIsInteractive: true },
    { modalOpen: true },
    { defaultPrevented: true },
    { altKey: true },
    { ctrlKey: true },
    { metaKey: true },
    { shiftKey: true },
  ]) {
    assert.equal(
      navigation.readerPageNavigationTarget({ ...base, ...ignored }),
      null,
    )
  }
  assert.equal(
    navigation.readerPageNavigationTarget({ ...base, key: 'Space' }),
    null,
  )
})

test('keyboard commands do nothing at first, final, empty and single-page boundaries', async () => {
  const navigation = await moduleAt('../src/lib/reader-page-navigation.ts')

  for (const keyName of ['ArrowLeft', 'PageUp', 'Home']) {
    assert.equal(
      navigation.readerPageNavigationTarget({
        key: keyName,
        currentPage: 0,
        pageCount: 5,
      }),
      null,
    )
  }
  for (const keyName of ['ArrowRight', 'PageDown', 'End']) {
    assert.equal(
      navigation.readerPageNavigationTarget({
        key: keyName,
        currentPage: 4,
        pageCount: 5,
      }),
      null,
    )
  }
  for (const keyName of [
    'ArrowRight',
    'ArrowLeft',
    'PageDown',
    'PageUp',
    'Home',
    'End',
  ]) {
    assert.equal(
      navigation.readerPageNavigationTarget({
        key: keyName,
        currentPage: 0,
        pageCount: 1,
      }),
      null,
    )
  }
  assert.equal(
    navigation.readerPageNavigationTarget({
      key: 'End',
      currentPage: 0,
      pageCount: 0,
    }),
    null,
  )
})

test('a safely redistributable final page is not needlessly tiny', async () => {
  const pages = await moduleAt('../src/lib/reader-pages.ts')
  const segments = Array.from({ length: 11 }, (_, index) =>
    segment({ ordinal: index + 1, wordCount: 1 }),
  )
  const result = pages.buildReaderPages(
    segments,
    metrics({
      fontSize: 20,
      lineHeight: 1.5,
      contentWidth: 600,
      availableHeight: 600,
    }),
  )
  const final = result.at(-1)

  assert.equal(result.length, 2)
  assert.ok(final)
  assert.ok(final.estimatedLines >= Math.ceil(final.capacityLines * 0.35))
  assert.deepEqual(
    result.map((page) => page.segments.length),
    [7, 4],
  )
  assert.deepEqual(
    result.flatMap((page) => page.segments.map(({ ordinal }) => ordinal)),
    Array.from({ length: 11 }, (_, index) => index + 1),
  )
})
