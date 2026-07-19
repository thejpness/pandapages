import assert from 'node:assert/strict'
import test from 'node:test'
import { loadTypeScript } from './helpers/typescript-module.mjs'

async function apiModule() {
  return loadTypeScript(
    '../src/lib/api.ts',
    import.meta.url,
    (source) => source.replaceAll('import.meta.env.VITE_API_BASE', "''"),
  )
}

function progress(overrides = {}) {
  return {
    version: 2,
    percent: 0.42,
    updatedAt: '2026-07-19T12:00:00.123456789Z',
    isCurrentVersion: true,
    ...overrides,
  }
}

function story(overrides = {}) {
  return {
    slug: 'the-three-little-pigs',
    title: 'The Three Little Pigs',
    author: 'Traditional',
    language: 'en',
    publishedVersion: 2,
    wordCount: 1260,
    chapterCount: 4,
    progress: progress(),
    ...overrides,
  }
}

function without(record, key) {
  const result = { ...record }
  delete result[key]
  return result
}

test('strict Library boundary accepts current, old-version, empty, and missing-author stories', async () => {
  const { module: api } = await apiModule()
  const missingAuthor = without(
    story({ slug: 'author-unknown', title: 'Author Unknown', progress: null }),
    'author',
  )
  const value = {
    items: [
      story(),
      story({
        slug: 'a-story-updated',
        title: 'A Story Updated',
        publishedVersion: 3,
        progress: progress({
          version: 2,
          updatedAt: '2026-07-19T13:15:10+01:00',
          isCurrentVersion: false,
        }),
      }),
      missingAuthor,
    ],
  }

  const parsed = api.parseLibraryResponse(value)
  assert.deepEqual(parsed.items[0], {
    ...value.items[0],
    progressAvailability: 'available',
  })
  assert.deepEqual(parsed.items[1], {
    ...value.items[1],
    progressAvailability: 'available',
  })
  assert.deepEqual(parsed.items[2], {
    ...missingAuthor,
    author: null,
    progressAvailability: 'available',
  })
  assert.equal(api.isInvalidLibraryResponseError(new TypeError('offline')), false)
})

test('strict Library boundary preserves stories when known progress metadata is unavailable', async () => {
  const { module: api } = await apiModule()
  const unavailableProgressStories = [
    without(story(), 'progress'),
    story({ progress: 'temporarily-unavailable' }),
    story({ progress: without(progress(), 'updatedAt') }),
    story({ progress: progress({ version: 0 }) }),
    story({ progress: progress({ version: Number.MAX_SAFE_INTEGER + 1 }) }),
    story({ progress: progress({ percent: -0.1 }) }),
    story({ progress: progress({ percent: 1.1 }) }),
    story({ progress: progress({ percent: Number.NaN }) }),
    story({ progress: progress({ updatedAt: '2026-07-19' }) }),
    story({ progress: progress({ updatedAt: '2026-02-30T12:00:00Z' }) }),
    story({ progress: progress({ updatedAt: '2026-07-19T25:00:00Z' }) }),
    story({ progress: progress({ isCurrentVersion: false }) }),
    story({
      publishedVersion: 3,
      progress: progress({ version: 2, isCurrentVersion: true }),
    }),
  ]

  for (const unavailable of unavailableProgressStories) {
    const [parsed] = api.parseLibraryResponse({ items: [unavailable] }).items
    assert.equal(parsed.progress, null)
    assert.equal(parsed.progressAvailability, 'unavailable')
  }
})

test('strict Library boundary rejects malformed core fields and internal keys', async () => {
  const { module: api } = await apiModule()
  const invalidStories = [
    without(story(), 'slug'),
    story({ slug: 'Uppercase' }),
    story({ slug: 'story/escape' }),
    story({ title: '   ' }),
    story({ author: 42 }),
    story({ author: '   ' }),
    story({ language: '' }),
    story({ publishedVersion: 0 }),
    story({ publishedVersion: 1.2 }),
    story({ publishedVersion: Number.MAX_SAFE_INTEGER + 1 }),
    story({ wordCount: -1 }),
    story({ wordCount: 1.5 }),
    story({ wordCount: Number.MAX_SAFE_INTEGER + 1 }),
    story({ chapterCount: -1 }),
    story({ storyId: 'internal-id' }),
    story({ progress: { ...progress(), locator: { internal: true } } }),
  ]

  for (const invalid of invalidStories) {
    assert.throws(
      () => api.parseLibraryResponse({ items: [invalid] }),
      (error) => api.isInvalidLibraryResponseError(error),
    )
  }

  for (const invalid of [
    null,
    {},
    { items: null },
    { items: [], internal: true },
    { items: [story(), story()] },
  ]) {
    assert.throws(
      () => api.parseLibraryResponse(invalid),
      (error) => api.isInvalidLibraryResponseError(error),
    )
  }
})

test('zero aggregate counts remain valid and are not invented client-side', async () => {
  const { module: api } = await apiModule()
  const emptyContent = story({
    slug: 'quiet-page',
    title: 'Quiet Page',
    wordCount: 0,
    chapterCount: 0,
    progress: null,
  })
  assert.deepEqual(api.parseLibraryResponse({ items: [emptyContent] }), {
    items: [{ ...emptyContent, progressAvailability: 'available' }],
  })
})

test('getLibrary uses the fixed credentialed route and rejects malformed success bodies', async (t) => {
  const originalFetch = globalThis.fetch
  t.after(() => {
    globalThis.fetch = originalFetch
  })

  const { module: api, source } = await apiModule()
  const requests = []
  const payload = { items: [story()] }
  globalThis.fetch = async (url, init) => {
    requests.push({ url, init })
    return new Response(JSON.stringify(payload), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  }

  assert.deepEqual(await api.getLibrary(), {
    items: [{ ...payload.items[0], progressAvailability: 'available' }],
  })
  assert.deepEqual(
    requests.map(({ url, init }) => [url, init.credentials]),
    [['/api/v1/library', 'include']],
  )
  assert.match(source, /request<unknown>\('\/api\/v1\/library'\)/)
  assert.match(source, /return parseLibraryResponse\(data\)/)

  globalThis.fetch = async () =>
    new Response(JSON.stringify({ items: [{ slug: 'incomplete' }] }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  await assert.rejects(
    api.getLibrary(),
    (error) => api.isInvalidLibraryResponseError(error),
  )
})
