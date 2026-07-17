import assert from 'node:assert/strict'
import test from 'node:test'
import { loadTypeScript } from './helpers/typescript-module.mjs'

const keyA = 'a'.repeat(64)
const keyB = 'b'.repeat(64)
const keyC = 'c'.repeat(64)
const chapterKeyA = 'd'.repeat(64)
const chapterKeyB = 'e'.repeat(64)

async function readerModule(relativePath) {
  return (
    await loadTypeScript(relativePath, import.meta.url)
  ).module
}

function segment(overrides = {}) {
  return {
    ordinal: 1,
    kind: 'paragraph',
    headingLevel: null,
    contentKey: keyA,
    contentOccurrence: 1,
    chapterKey: null,
    chapterOccurrence: null,
    renderedHtml: '<p>Reader segment</p>',
    wordCount: 2,
    ...overrides,
  }
}

function locator(overrides = {}) {
  return {
    schema: 2,
    segment: {
      key: keyA,
      occurrence: 1,
      ordinal: 1,
      offset: 0.5,
      ...overrides,
    },
  }
}

test('reading line uses 35% of the viewport remaining below sticky chrome', async () => {
  const scroll = await readerModule('../src/lib/reader-scroll-location.ts')

  assert.equal(
    scroll.readerReadingLine({
      height: 1000,
      headerBottom: 100,
      safeAreaTop: 40,
    }),
    415,
  )
  assert.equal(
    scroll.readerReadingLine({
      height: 1000,
      headerBottom: 80,
      safeAreaTop: 120,
    }),
    428,
  )
  assert.equal(
    scroll.readerReadingLine({ height: 100, headerBottom: 200 }),
    100,
  )
})

test('scroll capture selects the active segment and measures its offset', async () => {
  const scroll = await readerModule('../src/lib/reader-scroll-location.ts')
  const segments = [
    segment({ ordinal: 1, kind: 'heading', headingLevel: 1, wordCount: 0 }),
    segment({
      ordinal: 2,
      contentKey: keyB,
      chapterKey: chapterKeyA,
      chapterOccurrence: 1,
      wordCount: 9,
    }),
  ]

  const captured = scroll.captureReaderScrollPosition(
    segments,
    [
      { ordinal: 1, top: 0, bottom: 400 },
      { ordinal: 2, top: 400, bottom: 500 },
    ],
    { height: 1000, headerBottom: 100 },
  )

  assert.deepEqual(captured.locator, {
    schema: 2,
    segment: {
      key: keyB,
      occurrence: 1,
      ordinal: 2,
      offset: 0.15,
    },
    chapter: { key: chapterKeyA, occurrence: 1 },
  })
  assert.ok(Math.abs(captured.percent - 0.235) < Number.EPSILON)

  assert.equal(
    scroll.captureReaderScrollPosition(
      segments,
      [{ ordinal: 99, top: 0, bottom: 1000 }],
      { height: 1000, headerBottom: 0 },
    ),
    null,
  )
})

test('active segment lookup handles gaps and invalid measurements deterministically', async () => {
  const scroll = await readerModule('../src/lib/reader-scroll-location.ts')
  const layouts = [
    { ordinal: 0, top: 0, bottom: 100 },
    { ordinal: 1, top: 100, bottom: 200 },
    { ordinal: 2, top: 300, bottom: 400 },
    { ordinal: 3, top: Number.NaN, bottom: 500 },
  ]

  assert.equal(scroll.activeReaderSegmentLayout(layouts, 150).ordinal, 1)
  assert.equal(scroll.activeReaderSegmentLayout(layouts, 250).ordinal, 2)
  assert.equal(scroll.activeReaderSegmentLayout(layouts, 500).ordinal, 2)
  assert.equal(scroll.activeReaderSegmentLayout([], 100), null)
  assert.equal(
    scroll.readerSegmentOffset({ ordinal: 1, top: 200, bottom: 100 }, 150),
    0,
  )
})

test('word-weighted percentage gives headings a minimum weight and is monotonic', async () => {
  const scroll = await readerModule('../src/lib/reader-scroll-location.ts')
  const segments = [
    segment({ ordinal: 1, kind: 'heading', headingLevel: 1, wordCount: 0 }),
    segment({ ordinal: 2, contentKey: keyB, wordCount: 9 }),
    segment({
      ordinal: 3,
      kind: 'heading',
      headingLevel: 2,
      contentKey: keyC,
      wordCount: 0,
    }),
  ]

  assert.equal(scroll.readerSegmentWeight(segments[0]), 1)
  assert.equal(scroll.readerSegmentWeight(segment({ wordCount: -4 })), 1)
  assert.equal(
    scroll.readerSegmentWeight(segment({ wordCount: Number.NaN })),
    1,
  )
  assert.equal(scroll.readerWeightedPercent(segments, 2, 0.5), 0.5)
  assert.ok(
    scroll.readerWeightedPercent(segments, 3, 0) >
      scroll.readerWeightedPercent(segments, 2, 0.5),
  )
  assert.equal(scroll.readerWeightedPercent(segments, 3, 2), 1)
  assert.equal(scroll.readerWeightedPercent([], 1, 0), 0)
})

test('the same canonical anchor has stable progress across layout changes', async () => {
  const scroll = await readerModule('../src/lib/reader-scroll-location.ts')
  const segments = [
    segment({ ordinal: 1, wordCount: 4 }),
    segment({ ordinal: 2, contentKey: keyB, wordCount: 12 }),
  ]
  const viewport = { height: 1000, headerBottom: 0 }

  const compact = scroll.captureReaderScrollPosition(
    segments,
    [
      { ordinal: 1, top: -100, bottom: 300 },
      { ordinal: 2, top: 300, bottom: 400 },
    ],
    viewport,
  )
  const reflowed = scroll.captureReaderScrollPosition(
    segments,
    [
      { ordinal: 1, top: -500, bottom: 100 },
      { ordinal: 2, top: 100, bottom: 600 },
    ],
    viewport,
  )

  assert.deepEqual(reflowed.locator, compact.locator)
  assert.equal(reflowed.percent, compact.percent)
  assert.equal(compact.locator.segment.offset, 0.5)
})

test('restore target clamps offsets and scroll boundaries', async () => {
  const scroll = await readerModule('../src/lib/reader-scroll-location.ts')

  assert.equal(
    scroll.readerRestoreScrollTop({
      elementTop: 100,
      elementHeight: 200,
      currentScrollTop: 400,
      readingLine: 350,
      offset: 0.5,
    }),
    250,
  )
  assert.equal(
    scroll.readerRestoreScrollTop({
      elementTop: 100,
      elementHeight: 200,
      currentScrollTop: 400,
      readingLine: 350,
      offset: 4,
      maximumScrollTop: 300,
    }),
    300,
  )
  assert.equal(
    scroll.readerRestoreScrollTop({
      elementTop: 10,
      elementHeight: -50,
      currentScrollTop: 0,
      readingLine: 100,
      offset: -2,
    }),
    0,
  )
})

test('resume requires identity and ordinal consistency before using identity', async () => {
  const scroll = await readerModule('../src/lib/reader-scroll-location.ts')
  const segments = [
    segment({ ordinal: 1 }),
    segment({ ordinal: 2, contentKey: keyB }),
  ]

  assert.deepEqual(
    scroll.findReaderResumeSegment(
      segments,
      locator({ key: keyB, ordinal: 2 }),
    ),
    { segment: segments[1], matchedBy: 'identity' },
  )
  assert.equal(
    scroll.findReaderResumeSegment(
      segments,
      locator({ key: keyB, ordinal: 1 }),
    ),
    null,
  )
  assert.deepEqual(
    scroll.findReaderResumeSegment(
      segments,
      locator({ key: keyC, ordinal: 2 }),
    ),
    { segment: segments[1], matchedBy: 'ordinal' },
  )
  assert.equal(
    scroll.findReaderResumeSegment(
      segments,
      locator({ key: keyC, ordinal: 99 }),
    ),
    null,
  )
})

test('reduced-motion preference controls restore scrolling behavior', async () => {
  const scroll = await readerModule('../src/lib/reader-scroll-location.ts')

  assert.equal(scroll.readerScrollBehavior(true), 'auto')
  assert.equal(scroll.readerScrollBehavior(false), 'smooth')
})

test('chapter derivation preserves H2 identity, duplicates, UTF-8, and labels', async () => {
  const chaptersModule = await readerModule('../src/lib/reader-chapters.ts')
  const segments = [
    segment({
      ordinal: 1,
      kind: 'heading',
      headingLevel: 1,
      renderedHtml: '<h1>Story title</h1>',
    }),
    segment({ ordinal: 2, contentKey: keyB }),
    segment({
      ordinal: 3,
      kind: 'heading',
      headingLevel: 2,
      contentKey: keyC,
      chapterKey: chapterKeyA,
      chapterOccurrence: 1,
      renderedHtml: '<h2> Café&nbsp; &amp; 月 &#x1f319; </h2>',
    }),
    segment({
      ordinal: 4,
      chapterKey: chapterKeyA,
      chapterOccurrence: 1,
    }),
    segment({
      ordinal: 5,
      kind: 'heading',
      headingLevel: 3,
      chapterKey: chapterKeyA,
      chapterOccurrence: 1,
      renderedHtml: '<h3>Not a Reader chapter</h3>',
    }),
    segment({
      ordinal: 6,
      kind: 'heading',
      headingLevel: 2,
      contentKey: keyB,
      chapterKey: chapterKeyA,
      chapterOccurrence: 2,
      renderedHtml: '<h2>Café &amp; 月 &#127769;</h2>',
    }),
    segment({
      ordinal: 7,
      kind: 'heading',
      headingLevel: 2,
      contentKey: keyA,
      chapterKey: chapterKeyB,
      chapterOccurrence: 1,
      renderedHtml: '<h2><span> </span></h2>',
    }),
  ]

  const chapters = chaptersModule.buildReaderChapters(segments)
  assert.deepEqual(chapters, [
    {
      key: chapterKeyA,
      occurrence: 1,
      ordinal: 3,
      title: 'Café & 月 🌙',
    },
    {
      key: chapterKeyA,
      occurrence: 2,
      ordinal: 6,
      title: 'Café & 月 🌙',
    },
    {
      key: chapterKeyB,
      occurrence: 1,
      ordinal: 7,
      title: 'Chapter',
    },
  ])
  assert.equal(
    chaptersModule.readerChapterAccessibleLabel(chapters, chapters[0]),
    'Café & 月 🌙, 1 of 2',
  )
  assert.equal(
    chaptersModule.readerChapterAccessibleLabel(chapters, chapters[1]),
    'Café & 月 🌙, 2 of 2',
  )
  assert.equal(
    chaptersModule.readerChapterAccessibleLabel(chapters, chapters[2]),
    'Chapter',
  )
})

test('current chapter prefers server identity and falls back by ordinal', async () => {
  const chaptersModule = await readerModule('../src/lib/reader-chapters.ts')
  const chapters = [
    { key: chapterKeyA, occurrence: 1, ordinal: 3, title: 'Repeated' },
    { key: chapterKeyA, occurrence: 2, ordinal: 6, title: 'Repeated' },
  ]

  assert.equal(
    chaptersModule.currentReaderChapter(
      chapters,
      segment({
        ordinal: 7,
        chapterKey: chapterKeyA,
        chapterOccurrence: 2,
      }),
    ),
    chapters[1],
  )
  assert.equal(
    chaptersModule.currentReaderChapter(
      chapters,
      segment({
        ordinal: 7,
        chapterKey: chapterKeyB,
        chapterOccurrence: 9,
      }),
    ),
    chapters[1],
  )
  assert.equal(
    chaptersModule.currentReaderChapter(chapters, segment({ ordinal: 2 })),
    null,
  )
  assert.equal(chaptersModule.currentReaderChapter(chapters, null), null)
})

test('transitional pages retain ordered two-segment grouping', async () => {
  const pagesModule = await readerModule('../src/lib/reader-pages.ts')
  const segments = Array.from({ length: 7 }, (_, index) =>
    segment({
      ordinal: index + 1,
      contentKey: String(index + 1).repeat(64),
    }),
  )

  const pages = pagesModule.buildTransitionalReaderPages(segments)
  assert.deepEqual(
    pages.map(({ index, startOrdinal, endOrdinal, segments: pageSegments }) => ({
      index,
      startOrdinal,
      endOrdinal,
      ordinals: pageSegments.map(({ ordinal }) => ordinal),
    })),
    [
      { index: 0, startOrdinal: 1, endOrdinal: 2, ordinals: [1, 2] },
      { index: 1, startOrdinal: 3, endOrdinal: 4, ordinals: [3, 4] },
      { index: 2, startOrdinal: 5, endOrdinal: 6, ordinals: [5, 6] },
      { index: 3, startOrdinal: 7, endOrdinal: 7, ordinals: [7] },
    ],
  )
  assert.equal(pagesModule.readerPageForOrdinal(pages, 4), 1)
  assert.equal(pagesModule.readerPageForOrdinal(pages, 7), 3)
  assert.equal(pagesModule.readerPageForOrdinal(pages, 8), -1)
  assert.deepEqual(pagesModule.buildTransitionalReaderPages([]), [])
})

test('content failures distinguish a missing story from service unavailability', async () => {
  const contentState = await readerModule(
    '../src/lib/reader-content-state.ts',
  )

  assert.deepEqual(contentState.readerContentFailure(404), {
    status: 'not-found',
  })
  for (const status of [undefined, 0, 401, 500, 503]) {
    assert.deepEqual(contentState.readerContentFailure(status), {
      status: 'unavailable',
    })
  }
})
