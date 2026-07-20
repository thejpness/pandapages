import assert from 'node:assert/strict'
import test from 'node:test'
import { loadTypeScript } from './helpers/typescript-module.mjs'

const key = (character) => character.repeat(64)
const segmentKey = key('a')
const otherSegmentKey = key('b')
const missingSegmentKey = key('c')
const chapterKey = key('d')
const otherChapterKey = key('e')

async function mappingModule() {
  return loadTypeScript(
    '../src/lib/reader-cross-version-progress.ts',
    import.meta.url,
  )
}

function segment(overrides = {}) {
  return {
    ordinal: 1,
    kind: 'paragraph',
    headingLevel: null,
    contentKey: segmentKey,
    contentOccurrence: 1,
    chapterKey: null,
    chapterOccurrence: null,
    renderedHtml: '<p>Reader segment</p>',
    wordCount: 4,
    ...overrides,
  }
}

function locator(overrides = {}, chapter) {
  const value = {
    schema: 2,
    segment: {
      key: segmentKey,
      occurrence: 1,
      ordinal: 1,
      offset: 0.4,
      ...overrides,
    },
  }
  if (chapter) value.chapter = chapter
  return value
}

function input(overrides = {}) {
  return {
    oldVersion: 1,
    oldLocator: locator(),
    oldPercent: 0.4,
    currentVersion: 2,
    currentSegments: [segment()],
    ...overrides,
  }
}

test('exact segment mapping keeps an unchanged ordinal', async () => {
  const { module } = await mappingModule()
  const result = module.mapReaderProgressAcrossVersions(input())
  assert.equal(result.kind, 'segment')
  assert.equal(result.confidence, 'high')
  assert.equal(result.locator.segment.ordinal, 1)
})

test('exact segment mapping follows identity to a moved ordinal', async () => {
  const { module } = await mappingModule()
  const result = module.mapReaderProgressAcrossVersions(
    input({
      currentSegments: [
        segment({ ordinal: 1, contentKey: otherSegmentKey }),
        segment({ ordinal: 7 }),
      ],
    }),
  )
  assert.equal(result.kind, 'segment')
  assert.equal(result.locator.segment.ordinal, 7)
})

test('exact segment mapping preserves and clamps the old offset', async () => {
  const { module } = await mappingModule()
  const result = module.mapReaderProgressAcrossVersions(
    input({ oldLocator: locator({ offset: 0.875 }) }),
  )
  assert.equal(result.kind, 'segment')
  assert.equal(result.locator.segment.offset, 0.875)
})

test('duplicate segment content is resolved by content occurrence', async () => {
  const { module } = await mappingModule()
  const result = module.mapReaderProgressAcrossVersions(
    input({
      oldLocator: locator({ occurrence: 2, ordinal: 4 }),
      currentSegments: [
        segment({ ordinal: 2, contentOccurrence: 1 }),
        segment({ ordinal: 9, contentOccurrence: 2 }),
      ],
    }),
  )
  assert.equal(result.kind, 'segment')
  assert.equal(result.locator.segment.occurrence, 2)
  assert.equal(result.locator.segment.ordinal, 9)
})

test('exact segment mapping is rejected when chapter context changed', async () => {
  const { module } = await mappingModule()
  const result = module.mapReaderProgressAcrossVersions(
    input({
      oldLocator: locator(
        { ordinal: 4 },
        { key: chapterKey, occurrence: 1 },
      ),
      currentSegments: [
        segment({
          ordinal: 4,
          chapterKey: otherChapterKey,
          chapterOccurrence: 1,
        }),
      ],
    }),
  )
  assert.notEqual(result.kind, 'segment')
  assert.equal(result.kind, 'percentage')
})

test('repeated chapters map by canonical key and occurrence', async () => {
  const { module } = await mappingModule()
  const result = module.mapReaderProgressAcrossVersions(
    input({
      oldLocator: locator(
        { key: missingSegmentKey, ordinal: 8 },
        { key: chapterKey, occurrence: 2 },
      ),
      currentSegments: [
        segment({
          ordinal: 2,
          kind: 'heading',
          headingLevel: 2,
          contentKey: chapterKey,
          contentOccurrence: 1,
          chapterKey,
          chapterOccurrence: 1,
        }),
        segment({
          ordinal: 6,
          kind: 'heading',
          headingLevel: 2,
          contentKey: chapterKey,
          contentOccurrence: 2,
          chapterKey,
          chapterOccurrence: 2,
        }),
      ],
    }),
  )
  assert.equal(result.kind, 'chapter')
  assert.equal(result.confidence, 'medium')
  assert.equal(result.locator.segment.ordinal, 6)
  assert.deepEqual(result.locator.chapter, { key: chapterKey, occurrence: 2 })
})

test('a missing segment falls back to its exact chapter heading at offset zero', async () => {
  const { module } = await mappingModule()
  const result = module.mapReaderProgressAcrossVersions(
    input({
      oldLocator: locator(
        { key: missingSegmentKey, ordinal: 4, offset: 0.9 },
        { key: chapterKey, occurrence: 1 },
      ),
      currentSegments: [
        segment({
          ordinal: 3,
          kind: 'heading',
          headingLevel: 2,
          contentKey: chapterKey,
          chapterKey,
          chapterOccurrence: 1,
        }),
      ],
    }),
  )
  assert.equal(result.kind, 'chapter')
  assert.equal(result.locator.segment.ordinal, 3)
  assert.equal(result.locator.segment.offset, 0)
})

test('missing segment and chapter fall back to weighted percentage', async () => {
  const { module } = await mappingModule()
  const result = module.mapReaderProgressAcrossVersions(
    input({
      oldLocator: locator(
        { key: missingSegmentKey },
        { key: otherChapterKey, occurrence: 4 },
      ),
      oldPercent: 0.2,
      currentSegments: [
        segment({ ordinal: 1, contentKey: otherSegmentKey, wordCount: 1 }),
        segment({ ordinal: 2, contentKey: key('f'), wordCount: 4 }),
      ],
    }),
  )
  assert.equal(result.kind, 'percentage')
  assert.equal(result.confidence, 'low')
  assert.equal(result.locator.segment.ordinal, 2)
  assert.equal(result.locator.segment.offset, 0)
})

test('percentage inversion uses Reader word weights', async () => {
  const { module } = await mappingModule()
  const result = module.mapReaderProgressAcrossVersions(
    input({
      oldLocator: locator({ key: missingSegmentKey }),
      oldPercent: 0.2,
      currentSegments: [
        segment({ ordinal: 1, contentKey: otherSegmentKey, wordCount: 1 }),
        segment({ ordinal: 2, contentKey: key('f'), wordCount: 3 }),
        segment({ ordinal: 3, contentKey: key('0'), wordCount: 6 }),
      ],
    }),
  )
  assert.equal(result.kind, 'percentage')
  assert.equal(result.locator.segment.ordinal, 2)
  assert.ok(Math.abs(result.locator.segment.offset - 1 / 3) < Number.EPSILON)
  assert.ok(Math.abs(result.percent - 0.2) < Number.EPSILON)
})

test('zero percent maps to the beginning', async () => {
  const { module } = await mappingModule()
  const result = module.mapReaderProgressAcrossVersions(
    input({
      oldLocator: locator({ key: missingSegmentKey }),
      oldPercent: 0,
      currentSegments: [
        segment({ ordinal: 3, contentKey: otherSegmentKey }),
        segment({ ordinal: 8, contentKey: key('f') }),
      ],
    }),
  )
  assert.equal(result.kind, 'percentage')
  assert.equal(result.locator.segment.ordinal, 3)
  assert.equal(result.locator.segment.offset, 0)
  assert.equal(result.percent, 0)
})

test('100 percent maps to the end of the final readable segment', async () => {
  const { module } = await mappingModule()
  const result = module.mapReaderProgressAcrossVersions(
    input({
      oldLocator: locator({ key: missingSegmentKey }),
      oldPercent: 1,
      currentSegments: [
        segment({ ordinal: 3, contentKey: otherSegmentKey }),
        segment({ ordinal: 8, contentKey: key('f') }),
      ],
    }),
  )
  assert.equal(result.kind, 'percentage')
  assert.equal(result.locator.segment.ordinal, 8)
  assert.equal(result.locator.segment.offset, 1)
  assert.equal(result.percent, 1)
})

test('zero-word segments retain the minimum weight of one', async () => {
  const { module } = await mappingModule()
  const result = module.mapReaderProgressAcrossVersions(
    input({
      oldLocator: locator({ key: missingSegmentKey }),
      oldPercent: 0.25,
      currentSegments: [
        segment({ ordinal: 1, contentKey: otherSegmentKey, wordCount: 0 }),
        segment({ ordinal: 2, contentKey: key('f'), wordCount: 0 }),
      ],
    }),
  )
  assert.equal(result.kind, 'percentage')
  assert.equal(result.locator.segment.ordinal, 1)
  assert.equal(result.locator.segment.offset, 0.5)
})

test('an unusable old percentage yields no mapping after exact fallbacks fail', async () => {
  const { module } = await mappingModule()
  for (const oldPercent of [Number.NaN, Number.POSITIVE_INFINITY, -0.1, 1.1]) {
    assert.deepEqual(
      module.mapReaderProgressAcrossVersions(
        input({
          oldLocator: locator({ key: missingSegmentKey }),
          oldPercent,
          currentSegments: [segment({ contentKey: otherSegmentKey })],
        }),
      ),
      { kind: 'none', confidence: 'none' },
    )
  }
})

test('an invalid old locator yields no mapping', async () => {
  const { module } = await mappingModule()
  assert.deepEqual(
    module.mapReaderProgressAcrossVersions(
      input({ oldLocator: { schema: 1 }, oldPercent: 0.5 }),
    ),
    { kind: 'none', confidence: 'none' },
  )
})

test('no current segments yields no mapping', async () => {
  const { module } = await mappingModule()
  assert.deepEqual(
    module.mapReaderProgressAcrossVersions(input({ currentSegments: [] })),
    { kind: 'none', confidence: 'none' },
  )
})

test('mapping is deterministic, does not mutate inputs, and returns no presentation data', async () => {
  const { module, source } = await mappingModule()
  const value = input({
    oldLocator: locator({ key: missingSegmentKey }),
    oldPercent: 0.65,
    currentSegments: [
      segment({ ordinal: 1, contentKey: otherSegmentKey, wordCount: 2 }),
      segment({ ordinal: 2, contentKey: key('f'), wordCount: 8 }),
    ],
  })
  const before = structuredClone(value)
  const first = module.mapReaderProgressAcrossVersions(value)
  const second = module.mapReaderProgressAcrossVersions(value)

  assert.deepEqual(first, second)
  assert.deepEqual(value, before)
  assert.deepEqual(Object.keys(first).sort(), [
    'confidence',
    'kind',
    'locator',
    'percent',
  ])
  assert.doesNotMatch(JSON.stringify(first), /page|viewport|scroll/i)
  assert.doesNotMatch(source, /page(?:Index|Count)|viewport|scrollRatio/i)
})
