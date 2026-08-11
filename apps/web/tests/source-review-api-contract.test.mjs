import assert from 'node:assert/strict'
import test from 'node:test'
import { loadTypeScript } from './helpers/typescript-module.mjs'

const id = '44444444-4444-4444-8444-444444444444'
const timestamp = '2026-08-11T10:00:00Z'
const hash = 'a'.repeat(64)

async function loadAPI() {
  return (await loadTypeScript('../src/lib/api.ts', import.meta.url, (value) => value.replaceAll('import.meta.env.VITE_API_BASE', "''"))).module
}

function response(body, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

function acquisitionSummary(overrides = {}) {
  return {
    id,
    provider: 'project-gutenberg',
    externalId: '11',
    title: "Alice's Adventures in Wonderland",
    contributors: [{ name: 'Lewis Carroll', role: 'author' }],
    languages: ['en'],
    landingUrl: 'https://www.gutenberg.org/ebooks/11',
    providerRights: 'Public domain in the USA.',
    selectedRepresentation: {
      label: 'Plain text UTF-8',
      mediaType: 'text/plain; charset=utf-8',
      providerUrl: 'https://www.gutenberg.org/files/11/11-0.txt',
      sizeBytes: 1234,
    },
    normalisationVersion: 'project-gutenberg-plain-text-v1',
    retrievedContentHash: hash,
    normalisedContentHash: hash,
    snapshotHash: hash,
    createdAt: timestamp,
    review: { rights: { status: 'pending' }, editorial: { status: 'pending' } },
    ...overrides,
  }
}

test('source review wrappers use only provider/work identifiers and keep source text detail-only', async (t) => {
  const originalFetch = globalThis.fetch
  t.after(() => { globalThis.fetch = originalFetch })
  const requests = []
  const work = {
    provider: 'project-gutenberg', externalId: '11', title: "Alice's Adventures in Wonderland",
    contributors: [{ name: 'Lewis Carroll', role: 'author' }], languages: ['en'],
    landingUrl: 'https://www.gutenberg.org/ebooks/11', providerRights: 'Provider metadata',
    representations: [{ label: 'Plain text', mediaType: 'text/plain', url: 'https://www.gutenberg.org/files/11/11.txt', sizeBytes: 1234 }],
  }
  globalThis.fetch = async (url, init = {}) => {
    const path = String(url)
    requests.push({ path, init })
    if (path.includes('/search?')) return response({ provider: 'project-gutenberg', results: [work] })
    if (path.endsWith('/works/11')) return response(work)
    if (path.endsWith('/acquisitions')) return response({ outcome: 'created', acquisition: acquisitionSummary() }, 201)
    if (path.endsWith('/rights-review')) return response(acquisitionSummary({ review: { rights: { status: 'approved', note: 'Human rationale', reviewedAt: timestamp }, editorial: { status: 'pending' } } }))
    if (path.endsWith('/editorial-review')) return response(acquisitionSummary({ review: { rights: { status: 'approved', note: 'Human rationale', reviewedAt: timestamp }, editorial: { status: 'rejected', note: 'Incomplete text', reviewedAt: timestamp } } }))
    if (path.endsWith(`/${id}`)) return response({ ...acquisitionSummary(), sourceText: 'Down the rabbit-hole.\n' })
    return response({ items: [acquisitionSummary()] })
  }
  const api = await loadAPI()
  assert.equal((await api.adminSearchSourceProvider('project-gutenberg', 'alice')).results[0].externalId, '11')
  assert.equal((await api.adminGetSourceProviderWork('project-gutenberg', '11')).title, work.title)
  assert.equal((await api.adminPersistSourceAcquisition('project-gutenberg', '11')).outcome, 'created')
  assert.equal((await api.adminListSourceAcquisitions()).items[0].title, work.title)
  assert.equal((await api.adminGetSourceAcquisition(id)).sourceText, 'Down the rabbit-hole.\n')
  assert.equal((await api.adminUpdateSourceAcquisitionRightsReview(id, { status: 'approved', note: 'Human rationale' })).review.rights.status, 'approved')
  assert.equal((await api.adminUpdateSourceAcquisitionEditorialReview(id, { status: 'rejected', note: 'Incomplete text' })).review.editorial.status, 'rejected')
  assert.deepEqual(requests.map(({ path }) => path), ['/api/v1/admin/source-providers/project-gutenberg/search?q=alice', '/api/v1/admin/source-providers/project-gutenberg/works/11', '/api/v1/admin/source-providers/project-gutenberg/works/11/acquisitions', '/api/v1/admin/source-acquisitions', `/api/v1/admin/source-acquisitions/${id}`, `/api/v1/admin/source-acquisitions/${id}/rights-review`, `/api/v1/admin/source-acquisitions/${id}/editorial-review`])
  assert.equal(requests[2].init.body, undefined)
  assert.deepEqual(JSON.parse(String(requests[5].init.body)), { status: 'approved', note: 'Human rationale' })
  assert.deepEqual(JSON.parse(String(requests[6].init.body)), { status: 'rejected', note: 'Incomplete text' })
  assert.ok(requests.every(({ init }) => init.credentials === 'omit'))
  assert.throws(() => api.parseAdminSourceAcquisitionListResponse({ items: [{ ...acquisitionSummary(), sourceText: 'must not leak' }] }), /Invalid admin response/)
})
