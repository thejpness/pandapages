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

function reference(fact) { return [{ source: 'Catalogue record', fact }] }

function eligibility(overrides = {}) {
  return {
    policyVersion: 'panda-pages-copyright-v3', evaluationDate: '2026-08-11', evaluatedAt: timestamp,
    us: { status: 'eligible', reason: 'us_provider_public_domain_confirmed' },
    uk: { status: 'eligible', reason: 'uk_ordinary_literary_term_expired' }, overall: 'eligible', overallReason: 'overall_eligible',
    opdsRights: 'public_domain', rdfRights: 'public_domain', headerRights: 'public_domain',
    providerTitle: "Alice's Adventures in Wonderland", contributors: [{ name: 'Lewis Carroll', role: 'author', deathYear: 1898 }], rdfDigest: hash,
    effectiveUkEvidence: {
      workTitle: "Alice's Adventures in Wonderland", workCategory: 'ordinary_literary', workCategoryReferences: reference('ordinary literary work'), authorship: 'single_known', authorshipReferences: reference('one author'),
      authorName: 'Lewis Carroll', authorDeathYear: 1898, authorReferences: reference('died in 1898'), firstPublicationYear: 1865, firstPublicationReferences: reference('first published in 1865'),
      translation: { state: 'none_confirmed', references: reference('no translation') }, additionalTextualContribution: { state: 'none_confirmed', references: reference('no additional textual contribution') },
      unpublishedAtEnd1988: { state: 'none_confirmed', references: reference('published before 1988') },
    },
    ...overrides,
  }
}

function acquisitionSummary(overrides = {}) {
  return {
    id, provider: 'project-gutenberg', externalId: '11', title: "Alice's Adventures in Wonderland", contributors: [{ name: 'Lewis Carroll', role: 'author' }], languages: ['en'],
    landingUrl: 'https://www.gutenberg.org/ebooks/11', providerRights: 'Public domain in the USA.',
    selectedRepresentation: { label: 'Plain text UTF-8', mediaType: 'text/plain; charset=utf-8', providerUrl: 'https://www.gutenberg.org/files/11/11-0.txt', sizeBytes: 1234 },
    normalisationVersion: 'project-gutenberg-plain-text-v1', retrievedContentHash: hash, normalisedContentHash: hash, snapshotHash: hash, createdAt: timestamp,
    eligibility: eligibility(), sourceQuality: { status: 'pending', note: null, reviewedAt: null }, ...overrides,
  }
}

test('source review wrappers support empty automatic evidence and keep source text detail-only', async (t) => {
  const originalFetch = globalThis.fetch
  t.after(() => { globalThis.fetch = originalFetch })
  const requests = []
  const work = {
    provider: 'project-gutenberg', externalId: '11', title: "Alice's Adventures in Wonderland", contributors: [{ name: 'Lewis Carroll', role: 'author' }], languages: ['en'],
    landingUrl: 'https://www.gutenberg.org/ebooks/11', providerRights: 'Provider metadata',
    representations: [{ label: 'Plain text', mediaType: 'text/plain', url: 'https://www.gutenberg.org/files/11/11.txt', sizeBytes: 1234 }],
  }
  globalThis.fetch = async (url, init = {}) => {
    const path = String(url)
    requests.push({ path, init })
    if (path.includes('/search?')) return response({ provider: 'project-gutenberg', results: [work] })
    if (path.endsWith('/works/11')) return response(work)
    if (path.endsWith('/copyright-eligibility')) return response(eligibility())
    if (path.endsWith('/acquisitions')) return response({ outcome: 'created', acquisition: acquisitionSummary() }, 201)
    if (path.endsWith('/source-quality-review')) return response(acquisitionSummary({ sourceQuality: { status: 'rejected', note: 'Incomplete text', reviewedAt: timestamp } }))
    if (path.endsWith(`/${id}`)) return response({ ...acquisitionSummary(), sourceText: 'Down the rabbit-hole.\n' })
    return response({ items: [acquisitionSummary()] })
  }
  const api = await loadAPI()
  assert.equal((await api.adminSearchSourceProvider('project-gutenberg', 'alice')).results[0].externalId, '11')
  assert.equal((await api.adminGetSourceProviderWork('project-gutenberg', '11')).title, work.title)
  assert.equal((await api.adminCheckSourceEligibility('project-gutenberg', '11', {})).overall, 'eligible')
  assert.equal((await api.adminPersistSourceAcquisition('project-gutenberg', '11', {})).outcome, 'created')
  assert.equal((await api.adminListSourceAcquisitions()).items[0].title, work.title)
  assert.equal((await api.adminGetSourceAcquisition(id)).sourceText, 'Down the rabbit-hole.\n')
  assert.equal((await api.adminUpdateSourceAcquisitionSourceQualityReview(id, { status: 'rejected', note: 'Incomplete text' })).sourceQuality.status, 'rejected')
  assert.deepEqual(requests.map(({ path }) => path), ['/api/v1/admin/source-providers/project-gutenberg/search?q=alice', '/api/v1/admin/source-providers/project-gutenberg/works/11', '/api/v1/admin/source-providers/project-gutenberg/works/11/copyright-eligibility', '/api/v1/admin/source-providers/project-gutenberg/works/11/acquisitions', '/api/v1/admin/source-acquisitions', `/api/v1/admin/source-acquisitions/${id}`, `/api/v1/admin/source-acquisitions/${id}/source-quality-review`])
  assert.deepEqual(JSON.parse(String(requests[2].init.body)), {})
  assert.deepEqual(JSON.parse(String(requests[3].init.body)), {})
  for (const index of [2, 3]) {
    const body = String(requests[index].init.body)
    assert.ok(!body.includes('sourceText') && !body.includes('providerUrl') && !body.includes('snapshotHash') && !body.includes('"eligible"') && !body.includes('policyVersion'))
  }
  assert.deepEqual(JSON.parse(String(requests[6].init.body)), { status: 'rejected', note: 'Incomplete text' })
  assert.ok(requests.every(({ init }) => init.credentials === 'omit'))
  const resolution = { workCategory: 'established', authorship: 'established', author: 'established', firstPublication: 'established', translation: 'established', additionalTextualContribution: 'established', unpublishedAtEnd1988: 'established' }
  assert.equal(api.parseAdminEligibility(eligibility({ automaticResolution: resolution })).automaticResolution.firstPublication, 'established')
  assert.throws(() => api.parseAdminEligibility(eligibility({ automaticResolution: { ...resolution, translation: 'maybe' } })), /Invalid admin response/)
  assert.equal(api.parseAdminEligibility(eligibility({
    us: { status: 'indeterminate', reason: 'us_provider_rights_missing' }, uk: { status: 'indeterminate', reason: 'uk_work_category_unsupported' }, overall: 'blocked', overallReason: 'overall_blocked',
    effectiveUkEvidence: { workTitle: "Alice's Adventures in Wonderland", workCategory: 'unknown', workCategoryReferences: [], authorship: 'unknown', authorshipReferences: [], authorName: '', authorDeathYear: 0, authorReferences: [], firstPublicationYear: 0, firstPublicationReferences: [], translation: { state: 'unknown', references: [] }, additionalTextualContribution: { state: 'unknown', references: [] }, unpublishedAtEnd1988: { state: 'unknown', references: [] } },
  })).overall, 'blocked')
  assert.throws(() => api.parseAdminEligibility(eligibility({ policyVersion: 'panda-pages-copyright-v2' })), /Invalid admin response/)
  assert.throws(() => api.parseAdminSourceAcquisitionListResponse({ items: [{ ...acquisitionSummary(), sourceText: 'must not leak' }] }), /Invalid admin response/)
})
