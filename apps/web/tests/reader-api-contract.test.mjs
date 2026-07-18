import assert from 'node:assert/strict'
import test from 'node:test'
import { loadTypeScript } from './helpers/typescript-module.mjs'

const key = 'a'.repeat(64)

async function apiModule() {
  return (
    await loadTypeScript(
      '../src/lib/api.ts',
      import.meta.url,
      (source) => source.replaceAll('import.meta.env.VITE_API_BASE', "''"),
    )
  ).module
}

function validSegment(overrides = {}) {
  return {
    ordinal: 1,
    kind: 'heading',
    headingLevel: 1,
    contentKey: key,
    contentOccurrence: 1,
    chapterKey: null,
    chapterOccurrence: null,
    renderedHtml: '<h1>Café 世界</h1>',
    wordCount: 2,
    ...overrides,
  }
}

function validStory(overrides = {}) {
  return {
    slug: 'reader-story',
    title: 'Café 世界',
    author: null,
    language: 'en-GB',
    version: 1,
    segments: [validSegment()],
    ...overrides,
  }
}

test('Reader payload boundary accepts one coherent strict response', async () => {
  const api = await apiModule()
  assert.deepEqual(api.parseReaderStoryPayload(validStory()), validStory())
  for (const invalid of [
    { ...validStory(), html: '<h1>duplicate</h1>' },
    { ...validStory(), version: 0 },
    { ...validStory(), segments: [] },
    { ...validStory(), segments: [validSegment({ contentKey: 'BAD' })] },
    { ...validStory(), segments: [validSegment({ headingLevel: null })] },
    {
      ...validStory(),
      segments: [
        validSegment({ ordinal: 2 }),
        validSegment({ ordinal: 1, contentKey: 'b'.repeat(64) }),
      ],
    },
    { ...validStory(), segments: [validSegment({ markdown: '# private' })] },
  ]) {
    assert.throws(() => api.parseReaderStoryPayload(invalid), /Reader/)
  }
})

test('getReaderStory makes one coherent request and rejects malformed success', async (t) => {
  const api = await apiModule()
  const originalFetch = globalThis.fetch
  t.after(() => {
    globalThis.fetch = originalFetch
  })
  const requests = []
  globalThis.fetch = async (url, init) => {
    requests.push({ url, init })
    return new Response(JSON.stringify(validStory()), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  }
  assert.deepEqual(await api.getReaderStory('reader/story'), validStory())
  assert.deepEqual(requests.map(({ url }) => url), [
    '/api/v1/reader/reader%2Fstory',
  ])

  globalThis.fetch = async () =>
    new Response(JSON.stringify({ ...validStory(), segments: null }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  await assert.rejects(api.getReaderStory('reader-story'), /Reader response/)
})

test('progress response boundary distinguishes known empty and strict Locator v2', async () => {
  const api = await apiModule()
  const locator = {
    schema: 2,
    segment: { key, occurrence: 1, ordinal: 1, offset: 0.4 },
  }
  assert.deepEqual(api.parseProgressResponse({ progress: null }), {
    progress: null,
  })
  assert.deepEqual(
    api.parseProgressResponse({
      progress: { version: 1, locator, percent: 0.4 },
    }),
    { progress: { version: 1, locator, percent: 0.4 } },
  )
  for (const invalid of [
    {},
    { progress: { version: 1, locator: { mode: 'paged', page: 2 }, percent: 0.2 } },
    { progress: { version: 1, locator, percent: 2 } },
    { progress: { version: 1, locator, percent: 0.2, extra: true } },
  ]) {
    assert.throws(() => api.parseProgressResponse(invalid), /progress|Locator/)
  }
})
