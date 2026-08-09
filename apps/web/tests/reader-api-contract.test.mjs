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

test('Reader payload boundary rejects unsafe rendered HTML envelopes', async () => {
  const api = await apiModule()
  for (const renderedHtml of [
    '<script>alert(1)</script>',
    '<style>body { display: none }</style>',
    '<link rel="stylesheet" href="https://example.invalid/story.css">',
    '<meta http-equiv="refresh" content="0;url=https://example.invalid/">',
    '<base href="https://example.invalid/">',
    '<title>Hostile story title</title>',
    '<!-- hidden payload --><p>Story</p>',
    '<marquee>Unknown element</marquee>',
    '<p onclick="alert(1)">unsafe</p>',
    '<iframe srcdoc="<script>alert(1)</script>"></iframe>',
    '<svg onload="alert(1)"></svg>',
    '<math><mi>x</mi></math>',
    '<a href="javascript:alert(1)">unsafe link</a>',
    '<a href="//example.invalid/escape">protocol relative</a>',
    '<p style="background:url(javascript:alert(1))">unsafe CSS</p>',
  ]) {
    assert.throws(
      () => api.parseReaderStoryPayload(validStory({ segments: [validSegment({ renderedHtml })] })),
      /canonical story HTML/,
    )
  }

  const safe = validStory({
    segments: [validSegment({ renderedHtml: '<p>Safe <em>UTF-8 世界</em></p>' })],
  })
  assert.deepEqual(api.parseReaderStoryPayload(safe), safe)
})

test('Reader resolution boundary accepts selected and chooser states', async () => {
  const api = await apiModule()
  const selected = {
    state: 'selected',
    eligibleEditions: ['classic', 'little-listeners'],
    story: { ...validStory(), editionKey: 'classic' },
  }
  assert.deepEqual(api.parseReaderResolutionPayload(selected), selected)
  const chooser = {
    state: 'chooser',
    eligibleEditions: ['growing-readers', 'little-listeners'],
    story: null,
  }
  assert.deepEqual(api.parseReaderResolutionPayload(chooser), chooser)

  for (const invalid of [
    { ...selected, state: 'unknown' },
    { ...selected, eligibleEditions: ['little-listeners', 'classic'] },
    { ...selected, story: { ...selected.story, editionKey: 'confident-readers' } },
    { ...chooser, eligibleEditions: ['classic'] },
    { ...chooser, story: selected.story },
  ]) {
    assert.throws(() => api.parseReaderResolutionPayload(invalid), /Reader/)
  }
})

test('getReaderStory is profile scoped and edition mutations use the explicit Reader route', async (t) => {
  const api = await apiModule()
  const originalFetch = globalThis.fetch
  t.after(() => {
    globalThis.fetch = originalFetch
  })
  const requests = []
  globalThis.fetch = async (url, init) => {
    const headers = new Headers(init.headers)
    requests.push({
      url: String(url),
      method: init.method ?? 'GET',
      profile: headers.get('x-pp-profile-id'),
      body: init.body ?? null,
    })
    if ((init.method ?? 'GET') === 'GET') {
      return new Response(JSON.stringify({
        state: 'selected',
        eligibleEditions: ['classic'],
        story: { ...validStory(), editionKey: 'classic' },
      }), { status: 200, headers: { 'Content-Type': 'application/json' } })
    }
    return new Response(JSON.stringify({ ok: true }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  }

  const profileID = '123e4567-e89b-42d3-a456-426614174300'
  const resolution = await api.getReaderStory('reader/story', profileID)
  assert.equal(resolution.state, 'selected')
  await api.setReaderStoryEdition('reader/story', profileID, 'little-listeners')
  await api.clearReaderStoryEdition('reader/story', profileID)

  assert.deepEqual(requests, [
    {
      url: '/api/v1/reader-resolution/reader%2Fstory',
      method: 'GET',
      profile: profileID,
      body: null,
    },
    {
      url: '/api/v1/reader-edition/reader%2Fstory',
      method: 'PUT',
      profile: profileID,
      body: JSON.stringify({ editionKey: 'little-listeners' }),
    },
    {
      url: '/api/v1/reader-edition/reader%2Fstory',
      method: 'DELETE',
      profile: profileID,
      body: null,
    },
  ])
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
