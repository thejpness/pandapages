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

const profileID = '123e4567-e89b-42d3-a456-426614174300'

function edition(overrides = {}) {
  return {
    editionKey: 'growing-readers',
    version: 2,
    wordCount: 1260,
    chapterCount: 4,
    ...overrides,
  }
}

function progress(overrides = {}) {
  return {
    version: 2,
    percent: 0.42,
    updatedAt: '2026-07-19T12:00:00.123456789Z',
    isResolvedVersion: true,
    ...overrides,
  }
}

function story(overrides = {}) {
  return {
    slug: 'the-three-little-pigs',
    title: 'The Three Little Pigs',
    author: 'Traditional',
    language: 'en',
    state: 'selected',
    eligibleEditions: [
      edition(),
      edition({ editionKey: 'story-explorers', version: 3, wordCount: 900, chapterCount: 3 }),
    ],
    selectedEdition: 'growing-readers',
    progress: progress(),
    ...overrides,
  }
}

function chooser(overrides = {}) {
  return story({
    slug: 'choose-an-edition',
    title: 'Choose an Edition',
    state: 'chooser',
    eligibleEditions: [
      edition(),
      edition({ editionKey: 'little-listeners', version: 4, wordCount: 500, chapterCount: 1 }),
    ],
    selectedEdition: null,
    progress: progress({ version: 1, isResolvedVersion: false }),
    ...overrides,
  })
}

function without(record, key) {
  const result = { ...record }
  delete result[key]
  return result
}

test('strict Library boundary accepts selected, chooser, stale-progress, empty-progress, and missing-author stories', async () => {
  const { module: api } = await apiModule()
  const missingAuthor = without(
    story({ slug: 'author-unknown', title: 'Author Unknown', progress: null }),
    'author',
  )
  const value = { items: [story(), chooser(), missingAuthor] }

  const parsed = api.parseLibraryResponse(value)
  assert.equal(parsed.unavailableItemCount, 0)
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

test('strict Library boundary preserves stories when progress metadata is unavailable', async () => {
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
    story({ progress: progress({ isResolvedVersion: 'yes' }) }),
    story({ progress: progress({ version: 3, isResolvedVersion: true }) }),
    chooser({ progress: progress({ isResolvedVersion: true }) }),
  ]

  for (const unavailable of unavailableProgressStories) {
    const [parsed] = api.parseLibraryResponse({ items: [unavailable] }).items
    assert.equal(parsed.progress, null)
    assert.equal(parsed.progressAvailability, 'unavailable')
  }

  const stale = api.parseLibraryResponse({
    items: [story({ progress: progress({ version: 99, isResolvedVersion: false }) })],
  }).items[0]
  assert.equal(stale.progress.isResolvedVersion, false)
  assert.equal(stale.progress.version, 99)
})

test('strict Library boundary rejects malformed resolution fields and internal keys', async () => {
  const { module: api } = await apiModule()
  const invalidStories = [
    without(story(), 'slug'),
    story({ slug: 'Uppercase' }),
    story({ slug: 'story/escape' }),
    story({ title: '   ' }),
    story({ author: 42 }),
    story({ author: '   ' }),
    story({ language: '' }),
    story({ state: 'automatic' }),
    chooser({ eligibleEditions: [edition()] }),
    chooser({ selectedEdition: 'growing-readers' }),
    story({ selectedEdition: null }),
    story({ selectedEdition: 'classic' }),
    story({ eligibleEditions: [] }),
    story({ eligibleEditions: [edition(), edition()] }),
    story({ eligibleEditions: [edition({ editionKey: 'story-explorers' }), edition()] }),
    story({ eligibleEditions: [edition({ version: 0 })] }),
    story({ eligibleEditions: [edition({ wordCount: -1 })] }),
    story({ eligibleEditions: [edition({ chapterCount: -1 })] }),
    story({ wordCount: 1260 }),
    story({ chapterCount: 4 }),
    story({ publishedVersion: 2 }),
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
    { items: [story(), story()] },
    { items: [], unavailableItemCount: -1 },
    { items: [], unavailableItemCount: 1.5 },
    { items: [], unavailableItemCount: Number.MAX_SAFE_INTEGER + 1 },
  ]) {
    assert.throws(
      () => api.parseLibraryResponse(invalid),
      (error) => api.isInvalidLibraryResponseError(error),
    )
  }
})

test('zero edition aggregate counts remain valid and are not invented client-side', async () => {
  const { module: api } = await apiModule()
  const emptyContent = story({
    slug: 'quiet-page',
    title: 'Quiet Page',
    eligibleEditions: [edition({ wordCount: 0, chapterCount: 0 })],
    progress: null,
  })
  assert.deepEqual(api.parseLibraryResponse({ items: [emptyContent] }), {
    items: [{ ...emptyContent, progressAvailability: 'available' }],
    unavailableItemCount: 0,
  })
})

test('Library boundary ignores harmless additive fields but rejects unsafe data at every level', async () => {
  const { module: api } = await apiModule()
  const additive = {
    items: [
      story({
        editorialBadge: { label: 'bedtime', palette: ['paper', 'bamboo'] },
        eligibleEditions: [edition({ futureMetric: 'safe' })],
        progress: progress({ futureProgressHint: { label: 'harmless', values: [1, 2, 3] } }),
      }),
    ],
    unavailableItemCount: 2,
    futureEnvelope: { revision: 3, metadata: [{ label: 'new' }] },
  }
  const parsed = api.parseLibraryResponse(additive)
  assert.equal(parsed.items[0].eligibleEditions[0].editionKey, 'growing-readers')
  assert.equal(parsed.unavailableItemCount, 2)

  const unsafeValues = [
    { items: [], accountId: 'private' },
    { items: [story({ future: { segments: [] } })] },
    { items: [story({ progress: progress({ locator: {} }) })] },
    { items: [story({ markdown: '# private' })] },
    { items: [story({ rendered_html: '<p>private</p>' })] },
    { items: [story({ eligibleEditions: [edition({ versionId: 'private' })] })] },
  ]
  for (const unsafe of unsafeValues) {
    assert.throws(
      () => api.parseLibraryResponse(unsafe),
      (error) => api.isInvalidLibraryResponseError(error),
    )
  }
})

test('getLibrary uses explicit profile scope and rejects malformed success bodies', async (t) => {
  const originalFetch = globalThis.fetch
  t.after(() => { globalThis.fetch = originalFetch })

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

  const result = await api.getLibrary(profileID)
  assert.equal(result.items[0].selectedEdition, 'growing-readers')
  assert.equal(result.unavailableItemCount, 0)
  assert.equal(requests[0].url, '/api/v1/library')
  assert.equal(requests[0].init.credentials, 'omit')
  assert.equal(new Headers(requests[0].init.headers).get('X-PP-Profile-ID'), profileID)
  assert.match(source, /profileScopedRequest<unknown>\(["']\/api\/v1\/library["'], profileID\)/)
  assert.match(source, /return parseLibraryResponse\(data\)/)

  globalThis.fetch = async () =>
    new Response(JSON.stringify({ items: [{ slug: 'incomplete' }] }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  await assert.rejects(
    api.getLibrary(profileID),
    (error) => api.isInvalidLibraryResponseError(error),
  )
})
