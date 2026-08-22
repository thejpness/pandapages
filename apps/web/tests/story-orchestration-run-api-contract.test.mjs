import assert from 'node:assert/strict'
import test from 'node:test'
import { loadTypeScript } from './helpers/typescript-module.mjs'

const sourceVersionID = '11111111-1111-4111-8111-111111111111'
const runID = '22222222-2222-4222-8222-222222222222'
const timestamp = '2026-08-18T12:00:00Z'
const sourceSHA = 'a'.repeat(64)
const analysisSHA = 'b'.repeat(64)
const generatedKeys = ['confident-readers', 'growing-readers', 'story-explorers', 'little-listeners']
const reviewIDOne = '33333333-3333-4333-8333-333333333333'
const reviewIDTwo = '44444444-4444-4444-8444-444444444444'
const reviewIDThree = '55555555-5555-4555-8555-555555555555'

async function loadAPI() {
  return (await loadTypeScript('../src/lib/api.ts', import.meta.url, (value) => value.replaceAll('import.meta.env.VITE_API_BASE', "''"))).module
}

function response(body, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

function usage() {
  return { InputTokens: 12, CachedTokens: 3, OutputTokens: 8, ReasoningTokens: 5, TotalTokens: 20 }
}

function analysisArtifact() {
  return {
    SpecificationVersion: 'panda-pages-adaptation-v2', PromptVersion: 'panda-pages-source-analysis-prompt-v3',
    RequestedModel: 'gpt-5.6-terra', ReturnedModel: 'gpt-5.6-terra', ReasoningEffort: 'high',
    SourceSHA256: sourceSHA, AnalysisSHA256: analysisSHA, ResponseID: 'analysis-response', Usage: usage(),
    Analysis: {
      centralPlot: 'A traveller keeps a promise.',
      characters: [{ name: 'Mina', role: 'traveller', explicitMotivations: ['keep a promise'], flawsOrAmbiguities: ['rushes ahead'] }],
      relationships: [], coreStoryBeats: [{ summary: 'Mina receives a lantern.' }], developmentBeats: [], enrichmentMaterial: [],
      causalDependencies: [], iconicMaterial: [], intenseMaterial: [], adaptationRisks: [],
    },
  }
}

function edition(key, position) {
  const contentSHA = String.fromCharCode(99 + position).repeat(64)
  return {
    SpecificationVersion: 'panda-pages-adaptation-v2', PromptVersion: 'panda-pages-edition-prompt-v4', EditionKey: key,
    RequestedModel: 'gpt-5.6-terra', ReturnedModel: 'gpt-5.6-terra', ReasoningEffort: 'high',
    SourceSHA256: sourceSHA, AnalysisSHA256: analysisSHA, ContentSHA256: contentSHA,
    Markdown: `# ${key}\n\nA safe generated story.`, ResponseID: `edition-${key}`, Usage: usage(),
    StructuralValidation: { ContractVersion: 'panda-pages-adaptation-v1', EditionKey: key, ContentSHA256: contentSHA, Findings: [] },
  }
}

function assessment(key, contentSHA, result = 'pass') {
  const findings = key === 'confident-readers'
    ? [{ code: 'scope_too_thin', severity: 'review', message: 'Review the scope.', evidence: [{ location: 'generated_edition', editionKey: key, excerpt: 'A safe generated story.', explanation: 'The excerpt is shorter than expected.' }] }]
    : []
  const assessment = {
    validationVersion: 'panda-pages-semantic-validation-v3', specificationVersion: 'panda-pages-adaptation-v2',
    assessmentScope: 'edition', editionKey: key, result, findings,
  }
  return {
    ValidationVersion: 'panda-pages-semantic-validation-v3', SpecificationVersion: 'panda-pages-adaptation-v2',
    PromptVersion: 'panda-pages-edition-judgement-prompt-v3', AssessmentScope: 'edition', EditionKey: key, EditionKeys: [],
    RequestedModel: 'gpt-5.6-luna', ReturnedModel: 'gpt-5.6-luna', ReasoningEffort: 'medium', SourceSHA256: sourceSHA,
    AnalysisSHA256: analysisSHA, EditionBindings: [{ EditionKey: key, ContentSHA256: contentSHA }], AssessmentSHA256: 'd'.repeat(64),
    Assessment: assessment, ResponseID: `assessment-${key}`, Usage: usage(),
  }
}

function bundleAssessment(editions, result = 'fail') {
  const keys = editions.map((item) => item.EditionKey)
  const assessment = {
    validationVersion: 'panda-pages-semantic-validation-v3', specificationVersion: 'panda-pages-adaptation-v2',
    assessmentScope: 'bundle', editionKey: null, editionKeys: keys, result,
    findings: [{ code: 'edition_progression_questionable', severity: 'review', message: 'Review the edition progression.', evidence: [{ location: 'story_analysis', editionKey: null, excerpt: 'A traveller keeps a promise.', explanation: 'This is the source-analysis evidence.' }] }],
  }
  return {
    ValidationVersion: 'panda-pages-semantic-validation-v3', SpecificationVersion: 'panda-pages-adaptation-v2',
    PromptVersion: 'panda-pages-bundle-judgement-prompt-v3', AssessmentScope: 'bundle', EditionKey: null, EditionKeys: keys,
    RequestedModel: 'gpt-5.6-luna', ReturnedModel: 'gpt-5.6-luna', ReasoningEffort: 'medium', SourceSHA256: sourceSHA,
    AnalysisSHA256: analysisSHA, EditionBindings: editions.map((item) => ({ EditionKey: item.EditionKey, ContentSHA256: item.ContentSHA256 })), AssessmentSHA256: 'e'.repeat(64),
    Assessment: assessment, ResponseID: 'bundle-assessment', Usage: usage(),
  }
}

function run(result = 'fail') {
  const editions = generatedKeys.map(edition)
  return {
    id: runID, sourceVersionId: sourceVersionID, sourceSha256: sourceSHA, semanticResult: result, createdAt: timestamp,
    analysisArtifact: analysisArtifact(), editions,
    editionAssessments: editions.map((item) => assessment(item.EditionKey, item.ContentSHA256)),
    bundleAssessment: bundleAssessment(editions, result),
  }
}

function review(id, decision, createdAt = timestamp, runId = runID) {
  return { id, runId, decision, createdAt }
}

test('orchestration wrappers preserve endpoint identity, empty generation body, and actual artifact casing', async (t) => {
  const originalFetch = globalThis.fetch
  t.after(() => { globalThis.fetch = originalFetch })
  const requests = []
  globalThis.fetch = async (url, init = {}) => {
    requests.push({ url: String(url), init })
    if (String(url).endsWith('/generate')) return response({ id: runID, sourceVersionId: sourceVersionID, semanticResult: 'fail', createdAt: timestamp }, 201)
    if (String(url).includes('/source-versions/')) return response({ items: [{ id: runID, sourceVersionId: sourceVersionID, sourceSha256: sourceSHA, semanticResult: 'fail', createdAt: timestamp }] })
    return response(run())
  }
  const api = await loadAPI()
  const generation = await api.adminGenerateSourceVersion(sourceVersionID)
  const history = await api.adminListStoryOrchestrationRuns(sourceVersionID)
  const detail = await api.adminGetStoryOrchestrationRun(runID)

  assert.equal(generation.semanticResult, 'fail')
  assert.deepEqual(history.items.map((item) => item.id), [runID])
  assert.deepEqual(detail.editions.map((item) => item.EditionKey), generatedKeys)
  assert.equal(detail.analysisArtifact.PromptVersion, 'panda-pages-source-analysis-prompt-v3')
  assert.equal(detail.editions[0].Markdown, '# confident-readers\n\nA safe generated story.')
  assert.equal(detail.editionAssessments[0].Assessment.findings[0].evidence[0].excerpt, 'A safe generated story.')
  assert.deepEqual(detail.editionAssessments[0].Assessment.editionKeys, [])
  assert.equal(detail.bundleAssessment.Assessment.assessmentScope, 'bundle')
  assert.deepEqual(requests.map((request) => request.url), [
    `/api/v1/admin/source-versions/${sourceVersionID}/generate`,
    `/api/v1/admin/source-versions/${sourceVersionID}/orchestration-runs`,
    `/api/v1/admin/story-orchestration-runs/${runID}`,
  ])
  assert.equal(requests[0].init.method, 'POST')
  assert.equal(requests[0].init.body, undefined)
  assert.equal(new Headers(requests[0].init.headers).has('X-PP-Admin-Key'), false)
})

test('orchestration response parser rejects conflicting identities and classic as a generated edition', async () => {
  const api = await loadAPI()
  const mismatched = run()
  mismatched.editions[0].SourceSHA256 = 'f'.repeat(64)
  assert.throws(() => api.parseAdminStoryOrchestrationRun(mismatched), /Invalid admin response/)

  const classic = run()
  classic.editions[0].EditionKey = 'classic'
  classic.editions[0].StructuralValidation.EditionKey = 'classic'
  assert.throws(() => api.parseAdminStoryOrchestrationRun(classic), /Invalid admin response/)

  const missingBundleEditionKeys = run()
  delete missingBundleEditionKeys.bundleAssessment.Assessment.editionKeys
  assert.throws(() => api.parseAdminStoryOrchestrationRun(missingBundleEditionKeys), /Invalid admin response/)
})

test('editorial review wrappers preserve exact run identity, decisions, and immutable history order', async (t) => {
  const originalFetch = globalThis.fetch
  t.after(() => { globalThis.fetch = originalFetch })
  const requests = []
  const items = [
    review(reviewIDThree, 'approved', '2026-08-18T12:02:00Z'),
    review(reviewIDTwo, 'rejected', '2026-08-18T12:01:00Z'),
    review(reviewIDOne, 'rejected', '2026-08-18T12:00:00Z'),
  ]
  globalThis.fetch = async (url, init = {}) => {
    requests.push({ url: String(url), init })
    if (init.method === 'POST') return response(review(reviewIDThree, JSON.parse(init.body).decision, '2026-08-18T12:03:00Z'), 201)
    return response({ items })
  }
  const api = await loadAPI()
  const empty = api.parseAdminStoryOrchestrationEditorialReviewsResponse({ items: [] })
  assert.deepEqual(empty.items, [])
  const history = await api.adminListStoryOrchestrationEditorialReviews(runID)
  const approved = await api.adminCreateStoryOrchestrationEditorialReview(runID, 'approved')
  const rejected = await api.adminCreateStoryOrchestrationEditorialReview(runID, 'rejected')

  assert.deepEqual(history.items.map((item) => item.decision), ['approved', 'rejected', 'rejected'])
  assert.equal(approved.decision, 'approved')
  assert.equal(rejected.decision, 'rejected')
  assert.deepEqual(requests.map((request) => request.url), [
    `/api/v1/admin/story-orchestration-runs/${runID}/editorial-reviews`,
    `/api/v1/admin/story-orchestration-runs/${runID}/editorial-reviews`,
    `/api/v1/admin/story-orchestration-runs/${runID}/editorial-reviews`,
  ])
  assert.equal(requests[0].init.method, undefined)
  assert.equal(requests[1].init.method, 'POST')
  assert.equal(requests[1].init.body, '{"decision":"approved"}')
  assert.equal(requests[2].init.body, '{"decision":"rejected"}')
  assert.equal(new Headers(requests[1].init.headers).has('X-PP-Admin-Key'), false)
})

test('editorial review history preserves RFC3339Nano ordering before applying the UUID tie-break', async () => {
  const api = await loadAPI()
  const newest = '2026-08-22T18:00:00.123456789Z'
  const older = '2026-08-22T18:00:00.123456788Z'
  const exactTie = '2026-08-22T18:00:00.123456789Z'

  assert.doesNotThrow(() => api.parseAdminStoryOrchestrationEditorialReviewsResponse({
    items: [review(reviewIDOne, 'approved', newest), review(reviewIDTwo, 'rejected', older)],
  }))
  assert.throws(() => api.parseAdminStoryOrchestrationEditorialReviewsResponse({
    items: [review(reviewIDTwo, 'rejected', older), review(reviewIDOne, 'approved', newest)],
  }), /Invalid admin response/)

  assert.doesNotThrow(() => api.parseAdminStoryOrchestrationEditorialReviewsResponse({
    items: [review(reviewIDTwo, 'approved', exactTie), review(reviewIDOne, 'rejected', exactTie)],
  }))
  assert.throws(() => api.parseAdminStoryOrchestrationEditorialReviewsResponse({
    items: [review(reviewIDOne, 'approved', exactTie), review(reviewIDTwo, 'rejected', exactTie)],
  }), /Invalid admin response/)

  assert.doesNotThrow(() => api.parseAdminStoryOrchestrationEditorialReviewsResponse({
    items: [review(reviewIDOne, 'approved', '2026-08-22T18:00:00.9Z'), review(reviewIDTwo, 'rejected', '2026-08-22T18:00:00.10Z')],
  }))
  assert.doesNotThrow(() => api.parseAdminStoryOrchestrationEditorialReviewsResponse({
    items: [review(reviewIDOne, 'approved', '2026-08-22T18:00:00.000000001Z'), review(reviewIDTwo, 'rejected', '2026-08-22T18:00:00Z')],
  }))
  assert.doesNotThrow(() => api.parseAdminStoryOrchestrationEditorialReviewsResponse({
    items: [review(reviewIDOne, 'approved', '2026-08-22T19:00:00.123456789+01:00'), review(reviewIDTwo, 'rejected', '2026-08-22T18:00:00.123456788Z')],
  }))
})

test('editorial review client rejects malformed identity, decision, binding, timestamp, and ordering data', async (t) => {
  const api = await loadAPI()
  const valid = [review(reviewIDTwo, 'approved', '2026-08-18T12:00:00Z'), review(reviewIDOne, 'rejected', '2026-08-18T12:00:00Z')]
  assert.deepEqual(api.parseAdminStoryOrchestrationEditorialReviewsResponse({ items: valid }).items.map((item) => item.id), [reviewIDTwo, reviewIDOne])
  for (const malformed of [
    { items: [review('not-a-uuid', 'approved')] },
    { items: [review('00000000-0000-0000-0000-000000000000', 'approved')] },
    { items: [review(reviewIDOne, 'approved', timestamp, 'not-a-uuid')] },
    { items: [review(reviewIDOne, 'approved', timestamp, '00000000-0000-0000-0000-000000000000')] },
    { items: [review(reviewIDOne, 'needs_review')] },
    { items: [review(reviewIDOne, 'approved', 'not-a-timestamp')] },
    { items: [review(reviewIDOne, 'approved'), review(reviewIDOne, 'rejected', '2026-08-18T11:00:00Z')] },
    { items: [review(reviewIDOne, 'approved', '2026-08-18T11:00:00Z'), review(reviewIDTwo, 'rejected', '2026-08-18T12:00:00Z')] },
    { items: [review(reviewIDOne, 'approved'), review(reviewIDTwo, 'rejected')] },
    { items: null },
  ]) {
    assert.throws(() => api.parseAdminStoryOrchestrationEditorialReviewsResponse(malformed), /Invalid admin response/)
  }

  const originalFetch = globalThis.fetch
  t.after(() => { globalThis.fetch = originalFetch })
  globalThis.fetch = async (_url, init = {}) => {
    if (init.method === 'POST') return response(review(reviewIDOne, 'rejected'), 201)
    return response({ items: [review(reviewIDOne, 'approved', timestamp, sourceVersionID)] })
  }
  await assert.rejects(() => api.adminListStoryOrchestrationEditorialReviews(runID), /Invalid admin response/)
  await assert.rejects(() => api.adminCreateStoryOrchestrationEditorialReview(runID, 'approved'), /Invalid admin response/)
  await assert.rejects(() => api.adminCreateStoryOrchestrationEditorialReview(runID, 'needs_review'), /Invalid admin response/)
})
