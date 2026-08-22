import assert from 'node:assert/strict'
import test from 'node:test'
import { loadTypeScript } from './helpers/typescript-module.mjs'

const sourceVersionID = '11111111-1111-4111-8111-111111111111'
const runID = '22222222-2222-4222-8222-222222222222'
const timestamp = '2026-08-18T12:00:00Z'
const sourceSHA = 'a'.repeat(64)
const analysisSHA = 'b'.repeat(64)
const generatedKeys = ['confident-readers', 'growing-readers', 'story-explorers', 'little-listeners']

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
