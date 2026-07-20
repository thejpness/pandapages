import assert from 'node:assert/strict'
import test from 'node:test'
import { loadTypeScript } from './helpers/typescript-module.mjs'

const versionOne = '11111111-1111-4111-8111-111111111111'
const versionTwo = '22222222-2222-4222-8222-222222222222'
const timestamp = '2026-07-20T09:15:00Z'

async function loadAPI() {
  const { module } = await loadTypeScript(
    '../src/lib/api.ts',
    import.meta.url,
    (value) => value.replaceAll('import.meta.env.VITE_API_BASE', "''"),
  )
  return module
}

function summary(status = 'published_with_draft') {
  return {
    slug: 'contract-story',
    title: 'Contract Story',
    author: 'Panda Author',
    language: 'en-GB',
    rights: { label: 'Public domain' },
    sourceUrl: 'https://example.invalid/source',
    status,
    publishedVersion:
      status === 'draft_only' || status === 'unpublished'
        ? null
        : { versionId: versionOne, version: 1 },
    draftVersion:
      status === 'unpublished'
        ? null
        : {
            versionId:
              status === 'published' ? versionOne : versionTwo,
            version: status === 'published' ? 1 : 2,
          },
    versionCount: status === 'unpublished' ? 1 : 2,
    updatedAt: timestamp,
  }
}

function statusResponse(status = 'published') {
  const item = summary(status)
  return {
    slug: item.slug,
    status: item.status,
    publishedVersion: item.publishedVersion,
    draftVersion: item.draftVersion,
    versionCount: item.versionCount,
    updatedAt: item.updatedAt,
  }
}

function response(body, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

test('catalogue parser accepts every finite status and projects additive fields', async () => {
  const api = await loadAPI()
  const statuses = [
    'draft_only',
    'published',
    'published_with_draft',
    'unpublished',
    'repair_required',
  ]
  const parsed = api.parseAdminStoriesListResponse({
    items: statuses.map((status, index) => ({
      ...summary(status),
      slug: `contract-story-${index + 1}`,
      additive: { harmless: true },
    })),
    harmlessTopLevel: 'ignored',
  })

  assert.deepEqual(
    parsed.items.map((item) => item.status),
    statuses,
  )
  assert.equal('additive' in parsed.items[0], false)
  assert.equal('harmlessTopLevel' in parsed, false)
})

test('detail parser validates deterministic versions and every health state', async () => {
  const api = await loadAPI()
  const parsed = api.parseAdminStoryDetail({
    ...summary(),
    createdAt: timestamp,
    versions: [
      {
        versionId: versionTwo,
        version: 2,
        createdAt: timestamp,
        isDraft: true,
        isPublished: false,
        segmentCount: 3,
        wordCount: 21,
        chapterCount: 1,
        health: 'ready',
      },
      {
        versionId: versionOne,
        version: 1,
        createdAt: timestamp,
        isDraft: false,
        isPublished: true,
        segmentCount: 2,
        wordCount: 13,
        chapterCount: 0,
        health: 'repair_required',
      },
    ],
  })
  assert.deepEqual(
    parsed.versions.map((version) => version.health),
    ['ready', 'repair_required'],
  )

  const unavailable = structuredClone(parsed)
  unavailable.versions[1].health = 'unavailable'
  assert.equal(
    api.parseAdminStoryDetail(unavailable).versions[1].health,
    'unavailable',
  )
})

test('protected version source parser returns canonical source without Reader envelopes', async () => {
  const api = await loadAPI()
  const source = api.parseAdminVersionSource({
    slug: 'contract-story',
    versionId: versionTwo,
    version: 2,
    title: 'Contract Story',
    author: null,
    language: 'cy',
    rights: {},
    sourceUrl: null,
    markdown: '# Contract Story\n\nReadable source.\n',
    renderedHtml: '<h1>Contract Story</h1>',
    segmentCount: 2,
    wordCount: 4,
    chapterCount: 0,
    createdAt: timestamp,
    isDraft: true,
    isPublished: false,
    health: 'ready',
  })
  assert.match(source.markdown, /Readable source/)
  assert.equal(source.author, null)
})

test('preview and draft parsers expose structured counts and created/reused outcomes', async () => {
  const api = await loadAPI()
  const preview = api.parseAdminPreviewResponse({
    slug: 'contract-story',
    title: 'Contract Story',
    author: null,
    language: 'en-GB',
    rights: {},
    sourceUrl: null,
    renderedHtml: '<h1>Contract Story</h1>',
    segmentCount: 2,
    wordCount: 5,
    chapterCount: 0,
    warnings: [
      { field: 'sourceUrl', code: 'advisory', message: 'Check the source' },
    ],
  })
  assert.equal(preview.warnings[0].field, 'sourceUrl')

  for (const outcome of ['created_story', 'created_version', 'reused']) {
    const draft = api.parseAdminDraftUpsertResponse({
      slug: 'contract-story',
      versionId: versionTwo,
      version: 2,
      segmentCount: 2,
      wordCount: 5,
      chapterCount: 0,
      renderedHtml: '<h1>Contract Story</h1>',
      outcome,
    })
    assert.equal(draft.outcome, outcome)
  }
})

test('admin parsers reject sensitive, Locator, and Reader-content envelopes', async () => {
  const api = await loadAPI()
  for (const injected of [
    { accountId: 'secret' },
    { profile: { name: 'private' } },
    { session: { token: 'private' } },
    { storyId: 'internal' },
    { locator: { schema: 2 } },
    { segments: [] },
    { nested: { contentKey: 'reader-key' } },
    { markdown: '# misplaced' },
  ]) {
    assert.throws(
      () =>
        api.parseAdminStoriesListResponse({
          items: [{ ...summary(), ...injected }],
        }),
      /Invalid admin response/,
    )
  }
})

test('validation issues stay typed while 401, 403, 409, and 500 remain distinct', async (t) => {
  const originalFetch = globalThis.fetch
  t.after(() => {
    globalThis.fetch = originalFetch
  })
  const api = await loadAPI()
  const cases = [
    [401, 'unauthorized'],
    [403, 'forbidden'],
    [409, 'draft_repair_required'],
    [500, 'draft_failed'],
  ]
  for (const [status, code] of cases) {
    globalThis.fetch = async () =>
      response(
        {
          error: {
            code,
            message: `status ${status}`,
            issues:
              status === 409
                ? [{ field: 'markdown', code: 'invalid', message: 'Fix it' }]
                : undefined,
          },
        },
        status,
      )
    await assert.rejects(
      api.adminDraftUpsertStory({
        slug: 'contract-story',
        title: 'Contract Story',
        markdown: '# Contract Story',
      }),
      (error) => error.status === status && error.code === code,
    )
  }

  globalThis.fetch = async () =>
    response(
      {
        error: {
          code: 'preview_invalid',
          message: 'Story content is invalid',
          issues: [{ field: 'title', code: 'required', message: 'Enter a title' }],
        },
      },
      400,
    )
  let validationError
  try {
    await api.adminPreview({
      slug: 'contract-story',
      title: '',
      markdown: '# Contract Story',
    })
  } catch (error) {
    validationError = error
  }
  assert.deepEqual(api.getAdminValidationIssues(validationError), [
    { field: 'title', code: 'required', message: 'Enter a title' },
  ])
})

test('connectivity failures are not converted into API success or an HTTP status', async (t) => {
  const originalFetch = globalThis.fetch
  t.after(() => {
    globalThis.fetch = originalFetch
  })
  globalThis.fetch = async () => {
    throw new TypeError('network offline')
  }
  const api = await loadAPI()
  await assert.rejects(api.adminListStories(), (error) => {
    assert.equal(error instanceof TypeError, true)
    assert.equal(error.status, undefined)
    return true
  })
})

test('detail, source, publish, and unpublish wrappers use fixed credentialed routes', async (t) => {
  const originalFetch = globalThis.fetch
  t.after(() => {
    globalThis.fetch = originalFetch
  })
  const requests = []
  globalThis.fetch = async (url, init) => {
    requests.push({ url, init })
    if (String(url).includes('/versions/')) {
      return response({
        slug: 'contract-story',
        versionId: versionOne,
        version: 1,
        title: 'Contract Story',
        author: null,
        language: 'en-GB',
        rights: {},
        sourceUrl: null,
        markdown: '# Contract Story',
        renderedHtml: '<h1>Contract Story</h1>',
        segmentCount: 1,
        wordCount: 2,
        chapterCount: 0,
        createdAt: timestamp,
        isDraft: true,
        isPublished: false,
        health: 'ready',
      })
    }
    if (String(url).endsWith('/publish')) return response(statusResponse())
    if (String(url).endsWith('/unpublish')) {
      return response(statusResponse('draft_only'))
    }
    return response({
      ...summary(),
      createdAt: timestamp,
      versions: [
        {
          versionId: versionTwo,
          version: 2,
          createdAt: timestamp,
          isDraft: true,
          isPublished: false,
          segmentCount: 2,
          wordCount: 4,
          chapterCount: 0,
          health: 'ready',
        },
        {
          versionId: versionOne,
          version: 1,
          createdAt: timestamp,
          isDraft: false,
          isPublished: true,
          segmentCount: 2,
          wordCount: 4,
          chapterCount: 0,
          health: 'ready',
        },
      ],
    })
  }

  const api = await loadAPI()
  await api.adminGetStory('contract-story')
  await api.adminGetVersionSource('contract-story', versionOne)
  await api.adminPublishStory('contract-story', versionOne)
  await api.adminUnpublishStory('contract-story')

  assert.deepEqual(
    requests.map(({ url }) => url),
    [
      '/api/v1/admin/stories/contract-story',
      `/api/v1/admin/stories/contract-story/versions/${versionOne}`,
      '/api/v1/admin/stories/contract-story/publish',
      '/api/v1/admin/stories/contract-story/unpublish',
    ],
  )
  assert.ok(requests.every(({ init }) => init.credentials === 'include'))
  assert.deepEqual(JSON.parse(String(requests[2].init.body)), {
    versionId: versionOne,
  })
  assert.equal(requests[3].init.method, 'POST')
})
