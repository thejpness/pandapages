import assert from 'node:assert/strict'
import test from 'node:test'
import { loadTypeScript } from './helpers/typescript-module.mjs'

const acquisitionID = '44444444-4444-4444-8444-444444444444'
const versionID = '66666666-6666-4666-8666-666666666666'
const timestamp = '2026-08-11T10:00:00Z'

async function loadAPI() {
  return (await loadTypeScript('../src/lib/api.ts', import.meta.url, (value) => value.replaceAll('import.meta.env.VITE_API_BASE', "''"))).module
}

test('source promotion submits only a target and parses immutable destination state', async (t) => {
  const originalFetch = globalThis.fetch
  t.after(() => { globalThis.fetch = originalFetch })
  const requests = []
  globalThis.fetch = async (url, init = {}) => {
    requests.push({ url: String(url), init })
    return new Response(JSON.stringify({
      outcome: 'created',
      promotion: {
        storySlug: 'alice', storyTitle: 'Alice',
        sourceVersionId: versionID, sourceVersion: 1, promotedAt: timestamp,
      },
    }), { status: 201, headers: { 'Content-Type': 'application/json' } })
  }
  const api = await loadAPI()
  const result = await api.adminPromoteSourceAcquisition(acquisitionID, {
    mode: 'new_story', title: 'Alice', slug: 'alice',
  })
  assert.equal(result.outcome, 'created')
  assert.equal(result.promotion.storySlug, 'alice')
  assert.equal(requests[0].url, `/api/v1/admin/source-acquisitions/${acquisitionID}/promote`)
  assert.deepEqual(JSON.parse(String(requests[0].init.body)), { target: { mode: 'new_story', title: 'Alice', slug: 'alice' } })
  const body = String(requests[0].init.body)
  for (const forbidden of ['sourceText', 'snapshotHash', 'assessmentHash', 'eligible', 'providerUrl']) assert.ok(!body.includes(forbidden))
})
