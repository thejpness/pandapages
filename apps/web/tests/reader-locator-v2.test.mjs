import assert from 'node:assert/strict'
import test from 'node:test'
import { loadTypeScript } from './helpers/typescript-module.mjs'

const keyA = 'a'.repeat(64)
const keyB = 'b'.repeat(64)
const chapterKey = 'c'.repeat(64)

async function locatorModule() {
  return (
    await loadTypeScript(
      '../src/lib/reader-locator-v2.ts',
      import.meta.url,
    )
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
    renderedHtml: '<p>One</p>',
    wordCount: 1,
    ...overrides,
  }
}

test('Locator v2 parser accepts only its exact strict schema', async () => {
  const module = await locatorModule()
  const valid = {
    schema: 2,
    segment: { key: keyA, occurrence: 1, ordinal: 4, offset: 0.35 },
    chapter: { key: chapterKey, occurrence: 2 },
  }
  assert.deepEqual(module.parseReaderLocatorV2(valid), valid)

  for (const invalid of [
    { mode: 'scroll', scrollY: 200 },
    { ...valid, schema: 1 },
    { ...valid, extra: true },
    { ...valid, segment: { ...valid.segment, key: keyA.toUpperCase() } },
    { ...valid, segment: { ...valid.segment, occurrence: 0 } },
    { ...valid, segment: { ...valid.segment, ordinal: 0 } },
    { ...valid, segment: { ...valid.segment, offset: -0.01 } },
    { ...valid, segment: { ...valid.segment, offset: 1.01 } },
    { ...valid, chapter: { key: chapterKey } },
  ]) {
    assert.throws(() => module.parseReaderLocatorV2(invalid), /Locator v2/)
  }
})

test('locator creation clamps offsets and carries server chapter identity', async () => {
  const module = await locatorModule()
  const chapterSegment = segment({
    ordinal: 3,
    kind: 'heading',
    headingLevel: 2,
    contentKey: keyB,
    chapterKey,
    chapterOccurrence: 2,
  })
  assert.deepEqual(module.createReaderLocatorV2(chapterSegment, 4), {
    schema: 2,
    segment: { key: keyB, occurrence: 1, ordinal: 3, offset: 1 },
    chapter: { key: chapterKey, occurrence: 2 },
  })
  assert.equal(
    module.createReaderLocatorV2(chapterSegment, Number.NaN).segment.offset,
    0,
  )
  assert.equal(module.createReaderLocatorV2(segment(), -2).segment.offset, 0)
})


test('anchor lookup rejects an identity and ordinal mismatch, with ordinal fallback only when identity is absent', async () => {
  const module = await locatorModule()
  const segments = [
    segment({ ordinal: 1 }),
    segment({ ordinal: 2, contentKey: keyB }),
  ]
  const identityMismatch = {
    schema: 2,
    segment: { key: keyB, occurrence: 1, ordinal: 1, offset: 0 },
  }
  assert.equal(module.findReaderSegment(segments, identityMismatch), null)

  const ordinalFallback = {
    schema: 2,
    segment: { key: 'd'.repeat(64), occurrence: 1, ordinal: 1, offset: 0 },
  }
  assert.equal(module.findReaderSegment(segments, ordinalFallback).ordinal, 1)
  assert.equal(
    module.findReaderSegment(segments, {
      ...ordinalFallback,
      segment: { ...ordinalFallback.segment, ordinal: 99 },
    }),
    null,
  )
})

test('scroll capture chooses the 35% reading line and computes segment offset', async () => {
  const module = await locatorModule()
  const segments = [
    segment({ ordinal: 1 }),
    segment({ ordinal: 2, contentKey: keyB, chapterKey, chapterOccurrence: 1 }),
  ]
  const locator = module.captureScrollReaderLocator(
    segments,
    [
      { ordinal: 1, top: 0, bottom: 300 },
      { ordinal: 2, top: 300, bottom: 500 },
    ],
    1000,
  )
  assert.deepEqual(locator, {
    schema: 2,
    segment: { key: keyB, occurrence: 1, ordinal: 2, offset: 0.25 },
    chapter: { key: chapterKey, occurrence: 1 },
  })
})

test('paged capture persists a canonical segment rather than a page index', async () => {
  const module = await locatorModule()
  const segments = [segment(), segment({ ordinal: 2, contentKey: keyB })]
  const locator = module.capturePagedReaderLocator(segments, 2)
  assert.deepEqual(locator, {
    schema: 2,
    segment: { key: keyB, occurrence: 1, ordinal: 2, offset: 0 },
  })
  assert.equal(Object.hasOwn(locator, 'page'), false)
})

test('programmatic mode restore settles its final scroll before capture resumes', async () => {
  const module = await locatorModule()
  const frames = []
  let pendingScrollEvents = []
  let captureSuppressed = true
  let beginningWrites = 0

  const scheduleFrame = (callback) => {
    frames.push(callback)
  }
  const restore = () => {
    pendingScrollEvents.push(() => {
      if (!captureSuppressed) beginningWrites += 1
    })
  }
  const advanceFrame = async () => {
    const scrollEvents = pendingScrollEvents
    pendingScrollEvents = []
    for (const event of scrollEvents) event()
    const callback = frames.shift()
    assert.ok(callback, 'expected a scheduled animation frame')
    callback()
    await Promise.resolve()
  }

  const transition = module.settleProgrammaticReaderRestore(
    restore,
    scheduleFrame,
  )
  await advanceFrame()
  assert.equal(beginningWrites, 0)
  await advanceFrame()
  await transition
  captureSuppressed = false

  assert.equal(beginningWrites, 0)
  pendingScrollEvents.push(() => {
    if (!captureSuppressed) beginningWrites += 1
  })
  pendingScrollEvents.shift()()
  assert.equal(beginningWrites, 1)
})

test('mode cutover preserves an anchor without manufacturing reader movement', async () => {
  const { module } = await loadTypeScript(
    '../src/lib/reader-mode-transition.ts',
    import.meta.url,
  )
  assert.deepEqual(module.planReaderModeTransition(null), {
    anchor: null,
  })

  const locator = {
    schema: 2,
    segment: { key: keyA, occurrence: 1, ordinal: 1, offset: 0.4 },
  }
  assert.deepEqual(module.planReaderModeTransition(locator), {
    anchor: locator,
  })
})
