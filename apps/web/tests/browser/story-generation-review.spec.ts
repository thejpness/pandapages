import { AxeBuilder } from '@axe-core/playwright'
import { expect, test } from './support/auth'
import type { Page, Route } from '@playwright/test'

const sourceVersionA = '11111111-1111-4111-8111-111111111111'
const sourceVersionB = '22222222-2222-4222-8222-222222222222'
const runA = '33333333-3333-4333-8333-333333333333'
const runB = '44444444-4444-4444-8444-444444444444'
const timestamp = '2026-08-18T12:00:00Z'
const sourceSHA = 'a'.repeat(64)
const analysisSHA = 'b'.repeat(64)
const generatedKeys = ['confident-readers', 'growing-readers', 'story-explorers', 'little-listeners'] as const
const storyEditionKeys = ['classic', ...generatedKeys] as const
type EditorialDecision = 'approved' | 'rejected'
type EditorialReview = { id: string; runId: string; decision: EditorialDecision; createdAt: string }

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

function usage() {
  return { InputTokens: 12, CachedTokens: 3, OutputTokens: 8, ReasoningTokens: 5, TotalTokens: 20 }
}

function storyDetail() {
  return {
    slug: 'panda-tale', title: 'The Panda Tale', author: 'Panda Author', language: 'en-GB', rights: { label: 'Public domain' },
    sourceUrl: 'https://example.invalid/source', status: 'unpublished', publishedVersion: null, draftVersion: null,
    versionCount: 0, updatedAt: timestamp, currentRelease: null, releaseCount: 0,
    source: { status: 'ready', currentVersion: { versionId: sourceVersionA, version: 2 }, versionCount: 2, updatedAt: timestamp },
    editions: storyEditionKeys.map((editionKey) => ({ editionKey, status: 'empty', publishedVersion: null, draftVersion: null, versionCount: 0, updatedAt: null, versions: [] })),
    createdAt: timestamp, releases: [],
  }
}

function sourceDetail() {
  return {
    slug: 'panda-tale', status: 'ready', currentVersion: { versionId: sourceVersionA, version: 2 }, versionCount: 2, updatedAt: timestamp,
    versions: [
      { versionId: sourceVersionA, version: 2, title: 'The Panda Tale', author: 'Panda Author', language: 'en-GB', rights: { label: 'Public domain' }, sourceUrl: 'https://example.invalid/source', createdAt: timestamp, isCurrent: true, provenance: { kind: 'source_acquisition', acquisitionId: '55555555-5555-4555-8555-555555555555', provider: 'project-gutenberg', externalId: '11', assessmentHash: 'f'.repeat(64) } },
      { versionId: sourceVersionB, version: 1, title: 'The Panda Tale', author: 'Panda Author', language: 'en-GB', rights: { label: 'Public domain' }, sourceUrl: 'https://example.invalid/source', createdAt: '2026-08-17T12:00:00Z', isCurrent: false },
    ],
  }
}

function analysisArtifact() {
  return {
    SpecificationVersion: 'panda-pages-adaptation-v2', PromptVersion: 'panda-pages-source-analysis-prompt-v3', RequestedModel: 'gpt-5.6-terra', ReturnedModel: 'gpt-5.6-terra', ReasoningEffort: 'high', SourceSHA256: sourceSHA, AnalysisSHA256: analysisSHA, ResponseID: 'analysis-response', Usage: usage(),
    Analysis: { centralPlot: 'Mina keeps a promise despite a storm.', characters: [{ name: 'Mina', role: 'traveller', explicitMotivations: ['keep a promise'], flawsOrAmbiguities: ['rushes ahead'] }], relationships: [], coreStoryBeats: [{ summary: 'Mina receives a lantern.' }], developmentBeats: [], enrichmentMaterial: [], causalDependencies: [], iconicMaterial: [], intenseMaterial: [], adaptationRisks: [] },
  }
}

function edition(key: typeof generatedKeys[number], index: number) {
  const contentSHA = String.fromCharCode(99 + index).repeat(64)
  return {
    SpecificationVersion: 'panda-pages-adaptation-v2', PromptVersion: 'panda-pages-edition-prompt-v4', EditionKey: key, RequestedModel: 'gpt-5.6-terra', ReturnedModel: 'gpt-5.6-terra', ReasoningEffort: 'high', SourceSHA256: sourceSHA, AnalysisSHA256: analysisSHA, ContentSHA256: contentSHA,
    Markdown: `# ${key}\n\n**Mina** carries the lantern through the storm.`, ResponseID: `edition-${key}`, Usage: usage(),
    StructuralValidation: { ContractVersion: 'panda-pages-adaptation-v1', EditionKey: key, ContentSHA256: contentSHA, Findings: [] },
  }
}

function editionAssessment(item: ReturnType<typeof edition>, result: 'pass' | 'needs_review' | 'fail') {
  const findings = item.EditionKey === 'confident-readers'
    ? [
        { code: 'scope_too_thin', severity: 'review', message: 'Review the scope.', evidence: [{ location: 'generated_edition', editionKey: item.EditionKey, excerpt: 'Mina carries the lantern through the storm.', explanation: 'This is the first deterministic resolved evidence.' }, { location: 'story_analysis', editionKey: null, excerpt: 'Mina keeps a promise despite a storm.', explanation: 'This is the second deterministic resolved evidence.' }] },
        { code: 'vocabulary_mismatch', severity: 'review', message: 'Review the vocabulary.', evidence: [{ location: 'generated_edition', editionKey: item.EditionKey, excerpt: 'The lantern shines brightly.', explanation: 'This is the third deterministic resolved evidence.' }] },
      ]
    : []
  const assessment = { validationVersion: 'panda-pages-semantic-validation-v3', specificationVersion: 'panda-pages-adaptation-v2', assessmentScope: 'edition', editionKey: item.EditionKey, result, findings }
  return {
    ValidationVersion: 'panda-pages-semantic-validation-v3', SpecificationVersion: 'panda-pages-adaptation-v2', PromptVersion: 'panda-pages-edition-judgement-prompt-v3', AssessmentScope: 'edition', EditionKey: item.EditionKey, EditionKeys: [], RequestedModel: 'gpt-5.6-luna', ReturnedModel: 'gpt-5.6-luna', ReasoningEffort: 'medium', SourceSHA256: sourceSHA, AnalysisSHA256: analysisSHA, EditionBindings: [{ EditionKey: item.EditionKey, ContentSHA256: item.ContentSHA256 }], AssessmentSHA256: 'd'.repeat(64), Assessment: assessment, ResponseID: `assessment-${item.EditionKey}`, Usage: usage(),
  }
}

function bundleAssessment(editions: ReturnType<typeof edition>[], result: 'pass' | 'needs_review' | 'fail') {
  const keys = editions.map((item) => item.EditionKey)
  const assessment = { validationVersion: 'panda-pages-semantic-validation-v3', specificationVersion: 'panda-pages-adaptation-v2', assessmentScope: 'bundle', editionKey: null, editionKeys: keys, result, findings: [{ code: 'edition_progression_questionable', severity: 'review', message: 'Review the edition progression.', evidence: [{ location: 'story_analysis', editionKey: null, excerpt: 'Mina keeps a promise despite a storm.', explanation: 'This is source-analysis evidence.' }] }] }
  return {
    ValidationVersion: 'panda-pages-semantic-validation-v3', SpecificationVersion: 'panda-pages-adaptation-v2', PromptVersion: 'panda-pages-bundle-judgement-prompt-v3', AssessmentScope: 'bundle', EditionKey: null, EditionKeys: keys, RequestedModel: 'gpt-5.6-luna', ReturnedModel: 'gpt-5.6-luna', ReasoningEffort: 'medium', SourceSHA256: sourceSHA, AnalysisSHA256: analysisSHA, EditionBindings: editions.map((item) => ({ EditionKey: item.EditionKey, ContentSHA256: item.ContentSHA256 })), AssessmentSHA256: 'e'.repeat(64), Assessment: assessment, ResponseID: 'bundle-assessment', Usage: usage(),
  }
}

function orchestrationRun(id: string, sourceVersionId: string, result: 'pass' | 'needs_review' | 'fail') {
  const editions = generatedKeys.map(edition)
  return { id, sourceVersionId, sourceSha256: sourceSHA, semanticResult: result, createdAt: timestamp, analysisArtifact: analysisArtifact(), editions, editionAssessments: editions.map((item) => editionAssessment(item, result)), bundleAssessment: bundleAssessment(editions, result) }
}

function summary(run: ReturnType<typeof orchestrationRun>) {
  return { id: run.id, sourceVersionId: run.sourceVersionId, sourceSha256: run.sourceSha256, semanticResult: run.semanticResult, createdAt: run.createdAt }
}

function editorialReview(id: string, runId: string, decision: EditorialDecision, createdAt: string): EditorialReview {
  return { id, runId, decision, createdAt }
}

class ReviewAPI {
  requests: Array<{ method: string; path: string; body: string | null }> = []
  history = new Map<string, Array<ReturnType<typeof summary>>>()
  runs = new Map<string, ReturnType<typeof orchestrationRun>>()
  generationResult: 'pass' | 'needs_review' | 'fail' = 'fail'
  generationGate: ReturnType<typeof deferred<void>> | null = null
  generationFailure: { status: number; code: string } | null = null
  historyGates = new Map<string, ReturnType<typeof deferred<void>>>()
  runGates = new Map<string, ReturnType<typeof deferred<void>>>()
  editorialHistory = new Map<string, EditorialReview[]>()
  editorialHistoryGates = new Map<string, ReturnType<typeof deferred<void>>>()
  editorialHistoryFailure = new Map<string, { status: number; code: string }>()
  editorialPostGate: ReturnType<typeof deferred<void>> | null = null
  editorialPostFailure: { status: number; code: string } | null = null
  editorialSequence = 0

  async install(page: Page) {
    await page.route('**/api/v1/admin/**', (route) => this.handle(route))
  }

  count(method: string, path: string) {
    return this.requests.filter((request) => request.method === method && request.path === path).length
  }

  private async fulfill(route: Route, body: unknown, status = 200) {
    await route.fulfill({ status, contentType: 'application/json', headers: { 'Cache-Control': 'no-store' }, body: JSON.stringify(body) })
  }

  private async handle(route: Route) {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    const method = request.method()
    this.requests.push({ method, path, body: request.postData() })

    if (path === '/api/v1/admin/stories/panda-tale' && method === 'GET') return this.fulfill(route, storyDetail())
    if (path === '/api/v1/admin/stories/panda-tale/source' && method === 'GET') return this.fulfill(route, sourceDetail())

    const generate = /^\/api\/v1\/admin\/source-versions\/([^/]+)\/generate$/.exec(path)
    if (generate && method === 'POST') {
      if (this.generationGate) {
        const gate = this.generationGate
        this.generationGate = null
        await gate.promise
      }
      if (this.generationFailure) {
        const failure = this.generationFailure
        this.generationFailure = null
        return this.fulfill(route, { error: { code: failure.code, message: 'internal message must never be displayed' } }, failure.status)
      }
      const created = orchestrationRun(runA, generate[1], this.generationResult)
      this.runs.set(created.id, created)
      this.history.set(generate[1], [summary(created), ...(this.history.get(generate[1]) ?? [])])
      return this.fulfill(route, { id: created.id, sourceVersionId: created.sourceVersionId, semanticResult: created.semanticResult, createdAt: created.createdAt }, 201)
    }

    const history = /^\/api\/v1\/admin\/source-versions\/([^/]+)\/orchestration-runs$/.exec(path)
    if (history && method === 'GET') {
      const gate = this.historyGates.get(history[1])
      if (gate) await gate.promise
      return this.fulfill(route, { items: this.history.get(history[1]) ?? [] })
    }

    const run = /^\/api\/v1\/admin\/story-orchestration-runs\/([^/]+)$/.exec(path)
    if (run && method === 'GET') {
      const gate = this.runGates.get(run[1])
      if (gate) await gate.promise
      const found = this.runs.get(run[1])
      if (!found) return this.fulfill(route, { error: { code: 'not_found' } }, 404)
      return this.fulfill(route, found)
    }

    const editorial = /^\/api\/v1\/admin\/story-orchestration-runs\/([^/]+)\/editorial-reviews$/.exec(path)
    if (editorial && method === 'GET') {
      const gate = this.editorialHistoryGates.get(editorial[1])
      if (gate) await gate.promise
      const failure = this.editorialHistoryFailure.get(editorial[1])
      if (failure) {
        this.editorialHistoryFailure.delete(editorial[1])
        return this.fulfill(route, { error: { code: failure.code, message: 'internal message must never be displayed' } }, failure.status)
      }
      return this.fulfill(route, { items: this.editorialHistory.get(editorial[1]) ?? [] })
    }

    if (editorial && method === 'POST') {
      if (this.editorialPostGate) {
        const gate = this.editorialPostGate
        this.editorialPostGate = null
        await gate.promise
      }
      if (this.editorialPostFailure) {
        const failure = this.editorialPostFailure
        this.editorialPostFailure = null
        return this.fulfill(route, { error: { code: failure.code, message: 'internal message must never be displayed' } }, failure.status)
      }
      const decision = JSON.parse(request.postData() ?? '{}').decision as EditorialDecision
      this.editorialSequence += 1
      const created: EditorialReview = {
        id: `66666666-6666-4666-8666-${String(this.editorialSequence).padStart(12, '0')}`,
        runId: editorial[1],
        decision,
        createdAt: `2026-08-18T13:00:${String(this.editorialSequence).padStart(2, '0')}Z`,
      }
      this.editorialHistory.set(editorial[1], [created, ...(this.editorialHistory.get(editorial[1]) ?? [])])
      return this.fulfill(route, created, 201)
    }

    return this.fulfill(route, { error: { code: 'unhandled' } }, 501)
  }
}

async function openReview(page: Page, api: ReviewAPI) {
  await api.install(page)
  await page.goto('/admin/stories/panda-tale')
  await expect(page.getByRole('heading', { level: 2, name: 'Generate and review adaptations' })).toBeVisible()
  await expect(page.getByLabel('Canonical source revision')).toHaveValue(sourceVersionA)
}

test('generation uses the exact source version, keeps the request in view, then opens a reviewable semantic fail', async ({ page }) => {
  const api = new ReviewAPI()
  const gate = deferred<void>()
  api.generationGate = gate
  await openReview(page, api)

  const panel = page.locator('.generation-review')
  const generate = panel.getByRole('button', { name: 'Generate adaptations' })
  await generate.click()
  await expect(panel.getByText('Generating four adaptations. This can take several minutes.')).toBeVisible()
  await expect(panel.getByRole('button', { name: 'Generating adaptations…' })).toBeDisabled()
  await expect(page.getByRole('heading', { name: 'Leave while generation is running?' })).toHaveCount(0)
  await page.getByRole('button', { name: 'Source review' }).click()
  await expect(page.getByRole('heading', { name: 'Leave while generation is running?' })).toBeVisible()
  await page.getByRole('button', { name: 'Cancel' }).click()
  expect(api.count('POST', `/api/v1/admin/source-versions/${sourceVersionA}/generate`)).toBe(1)
  expect(api.requests.find((request) => request.path.endsWith('/generate'))?.body).toBeNull()

  gate.resolve()
  await expect(page.getByRole('heading', { name: 'Generated adaptations' })).toBeVisible()
  await expect(panel.getByText('Fail — machine assessment').first()).toBeVisible()
  expect(api.count('GET', `/api/v1/admin/source-versions/${sourceVersionA}/orchestration-runs`)).toBeGreaterThanOrEqual(2)
  expect(api.count('GET', `/api/v1/admin/story-orchestration-runs/${runA}`)).toBe(1)

  await expect(page.getByRole('tab')).toHaveText(['Confident Readers', 'Growing Readers', 'Story Explorers', 'Little Listeners'])
  await expect(page.getByRole('tab', { name: 'Classic' })).toHaveCount(0)
  const findings = panel.locator('.generation-assessment .generation-findings > li')
  await expect(findings).toHaveCount(2)
  await expect(findings.nth(0)).toContainText('Review the scope.')
  await expect(findings.nth(1)).toContainText('Review the vocabulary.')
  await findings.nth(0).locator('summary').click()
  const evidence = findings.nth(0).locator('.generation-evidence > li')
  await expect(evidence).toHaveCount(2)
  await expect(evidence.nth(0)).toContainText('Mina carries the lantern through the storm.')
  await expect(evidence.nth(1)).toContainText('Mina keeps a promise despite a storm.')
  await page.getByRole('tab', { name: 'Confident Readers' }).focus()
  await page.keyboard.press('ArrowRight')
  await expect(page.getByRole('tab', { name: 'Growing Readers' })).toHaveAttribute('aria-selected', 'true')
  await expect(panel.getByText('Bundle assessment')).toBeVisible()
  await page.locator('.generation-disclosure > summary').filter({ hasText: 'Source analysis' }).click()
  await expect(panel.getByText('Mina keeps a promise despite a storm.').last()).toBeVisible()
  await page.locator('.generation-disclosure > summary').filter({ hasText: 'Technical provenance' }).click()
  await expect(panel.getByText('gpt-5.6-terra / gpt-5.6-terra').first()).toBeVisible()
  await expect(panel.getByRole('heading', { name: 'Human editorial decision' })).toBeVisible()
  await expect(panel.getByText('Not reviewed')).toBeVisible()
  await expect(panel.getByRole('button', { name: 'Approve this run' })).toBeEnabled()
  await expect(panel.getByRole('button', { name: 'Reject this run' })).toBeEnabled()
  await expect(panel.getByRole('button', { name: /publish|ingest|create draft|save as story|request changes/i })).toHaveCount(0)
  expect((await new AxeBuilder({ page }).analyze()).violations.filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')).toEqual([])
})

for (const result of ['pass', 'needs_review'] as const) {
  test(`a completed ${result} generation refreshes history and opens its immutable run`, async ({ page }) => {
    const api = new ReviewAPI()
    api.generationResult = result
    await openReview(page, api)
    await page.getByRole('button', { name: 'Generate adaptations' }).click()
    await expect(page.getByRole('heading', { name: 'Generated adaptations' })).toBeVisible()
    await expect(page.getByText(result === 'pass' ? 'Pass — machine assessment' : 'Needs review — machine assessment').first()).toBeVisible()
    expect(api.count('GET', `/api/v1/admin/source-versions/${sourceVersionA}/orchestration-runs`)).toBeGreaterThanOrEqual(2)
    expect(api.count('GET', `/api/v1/admin/story-orchestration-runs/${runA}`)).toBe(1)
  })
}

test('source switches and run selections ignore stale history and detail responses', async ({ page }) => {
  const api = new ReviewAPI()
  const staleHistory = deferred<void>()
  api.historyGates.set(sourceVersionA, staleHistory)
  await openReview(page, api)
  await page.getByLabel('Canonical source revision').selectOption(sourceVersionB)
  await expect(page.getByText('No generations for this source revision yet.')).toBeVisible()
  staleHistory.resolve()
  await expect(page.getByText('No generations for this source revision yet.')).toBeVisible()

  const first = orchestrationRun(runA, sourceVersionB, 'pass')
  const second = orchestrationRun(runB, sourceVersionB, 'needs_review')
  api.runs.set(runA, first)
  api.runs.set(runB, second)
  api.history.set(sourceVersionB, [summary(first), summary(second)])
  await page.getByRole('button', { name: 'Refresh' }).click()
  await expect(page.getByRole('button', { name: /Pass — machine assessment/ })).toBeVisible()
  const staleDetail = deferred<void>()
  api.runGates.set(runA, staleDetail)
  await page.getByRole('button', { name: /Pass — machine assessment/ }).click()
  await page.getByRole('button', { name: /Needs review — machine assessment/ }).click()
  await expect(page.getByText('Needs review — machine assessment').last()).toBeVisible()
  staleDetail.resolve()
  await expect(page.getByText('Needs review — machine assessment').last()).toBeVisible()
})

test('editorial review preserves repeated history, confirms append-only actions, and refetches the authoritative decision', async ({ page }) => {
  const api = new ReviewAPI()
  const selected = orchestrationRun(runA, sourceVersionA, 'pass')
  api.runs.set(runA, selected)
  api.history.set(sourceVersionA, [summary(selected)])
  api.editorialHistory.set(runA, [
    editorialReview('55555555-5555-4555-8555-555555555555', runA, 'rejected', '2026-08-18T12:02:00Z'),
    editorialReview('44444444-4444-4444-8444-444444444444', runA, 'approved', '2026-08-18T12:01:00Z'),
    editorialReview('33333333-3333-4333-8333-333333333333', runA, 'approved', '2026-08-18T12:00:00Z'),
  ])
  await openReview(page, api)
  await page.getByRole('button', { name: /Pass — machine assessment/ }).click()
  const editorial = page.locator('.editorial-review')
  await expect(editorial.getByText('Rejected').first()).toBeVisible()
  await expect(editorial.getByRole('button', { name: 'Reject this run' })).toBeDisabled()
  await expect(editorial.getByRole('button', { name: 'Approve this run' })).toBeEnabled()
  await editorial.locator('summary').click()
  const rows = editorial.locator('.editorial-review__history-list > li')
  await expect(rows).toHaveCount(3)
  await expect(rows.nth(0)).toContainText('Rejected')
  await expect(rows.nth(1)).toContainText('Approved')
  await expect(rows.nth(2)).toContainText('Approved')

  await editorial.getByRole('button', { name: 'Approve this run' }).click()
  await expect(page.getByRole('heading', { name: 'Approve this generated run?' })).toBeVisible()
  await page.getByRole('button', { name: 'Cancel' }).click()
  expect(api.count('POST', `/api/v1/admin/story-orchestration-runs/${runA}/editorial-reviews`)).toBe(0)

  const postGate = deferred<void>()
  api.editorialPostGate = postGate
  await editorial.getByRole('button', { name: 'Approve this run' }).click()
  await page.getByRole('button', { name: 'Record approval' }).click()
  await expect(editorial.locator('button').filter({ hasText: 'Approve this run' })).toBeDisabled()
  await expect(editorial.locator('button').filter({ hasText: 'Reject this run' })).toBeDisabled()
  postGate.resolve()
  await expect(editorial.getByText('Editorial decision recorded. Editorial history is up to date.')).toBeVisible()
  await expect(editorial.getByText('Approved').first()).toBeVisible()
  await expect(editorial.getByRole('button', { name: 'Approve this run' })).toBeDisabled()
  await expect(editorial.getByRole('button', { name: 'Reject this run' })).toBeEnabled()
  expect(api.count('POST', `/api/v1/admin/story-orchestration-runs/${runA}/editorial-reviews`)).toBe(1)
  expect(api.requests.find((request) => request.method === 'POST' && request.path.endsWith('/editorial-reviews'))?.body).toBe('{"decision":"approved"}')
  expect(api.count('GET', `/api/v1/admin/story-orchestration-runs/${runA}/editorial-reviews`)).toBe(2)
})

test('machine and human decisions remain separate, including failed machine assessment with human approval', async ({ page }) => {
  const api = new ReviewAPI()
  const selected = orchestrationRun(runA, sourceVersionA, 'fail')
  api.runs.set(runA, selected)
  api.history.set(sourceVersionA, [summary(selected)])
  api.editorialHistory.set(runA, [editorialReview('33333333-3333-4333-8333-333333333333', runA, 'approved', '2026-08-18T12:00:00Z')])
  await openReview(page, api)
  await page.getByRole('button', { name: /Fail — machine assessment/ }).click()
  await expect(page.getByText('Fail — machine assessment').last()).toBeVisible()
  const editorial = page.locator('.editorial-review')
  await expect(editorial.getByRole('heading', { name: 'Human editorial decision' })).toBeVisible()
  await expect(editorial.getByText('Approved').first()).toBeVisible()
  await expect(editorial.getByRole('button', { name: 'Approve this run' })).toBeDisabled()
  await expect(editorial.getByRole('button', { name: 'Reject this run' })).toBeEnabled()
})

test('recorded decision with refresh failure preserves the prior history without claiming a new current decision', async ({ page }) => {
  const api = new ReviewAPI()
  const selected = orchestrationRun(runA, sourceVersionA, 'pass')
  api.runs.set(runA, selected)
  api.history.set(sourceVersionA, [summary(selected)])
  api.editorialHistory.set(runA, [editorialReview('33333333-3333-4333-8333-333333333333', runA, 'approved', '2026-08-18T12:00:00Z')])
  await openReview(page, api)
  await page.getByRole('button', { name: /Pass — machine assessment/ }).click()
  const editorial = page.locator('.editorial-review')
  await expect(editorial.getByText('Approved').first()).toBeVisible()
  api.editorialHistoryFailure.set(runA, { status: 503, code: 'editorial_reviews_unavailable' })
  await editorial.getByRole('button', { name: 'Reject this run' }).click()
  await page.getByRole('button', { name: 'Record rejection' }).click()
  await expect(editorial.getByText('Decision recorded, but editorial history could not be refreshed.')).toBeVisible()
  await expect(editorial.getByText('Current decision unavailable')).toBeVisible()
  await expect(editorial.getByText('Previously loaded history is shown below. Refresh it before relying on the current decision.')).toBeHidden()
  await editorial.locator('summary').click()
  await expect(editorial.getByText('Previously loaded history is shown below. Refresh it before relying on the current decision.')).toBeVisible()
  await editorial.getByRole('button', { name: 'Try again' }).click()
  await expect(editorial.getByText('Rejected').first()).toBeVisible()
})

test('editorial requests from an unmounted run cannot contaminate the next selected run', async ({ page }) => {
  const api = new ReviewAPI()
  const first = orchestrationRun(runA, sourceVersionA, 'pass')
  const second = orchestrationRun(runB, sourceVersionA, 'needs_review')
  api.runs.set(runA, first)
  api.runs.set(runB, second)
  api.history.set(sourceVersionA, [summary(first), summary(second)])
  api.editorialHistory.set(runA, [editorialReview('33333333-3333-4333-8333-333333333333', runA, 'approved', '2026-08-18T12:00:00Z')])
  api.editorialHistory.set(runB, [editorialReview('44444444-4444-4444-8444-444444444444', runB, 'rejected', '2026-08-18T12:01:00Z')])
  const staleHistory = deferred<void>()
  api.editorialHistoryGates.set(runA, staleHistory)
  await openReview(page, api)
  await page.getByRole('button', { name: /Pass — machine assessment/ }).click()
  await page.getByRole('button', { name: /Needs review — machine assessment/ }).click()
  const editorial = page.locator('.editorial-review')
  await expect(editorial.getByText('Rejected').first()).toBeVisible()
  staleHistory.resolve()
  await expect(editorial.getByText('Rejected').first()).toBeVisible()
  await expect(editorial.getByText('Approved').first()).toHaveCount(0)
})

test('an in-flight editorial decision for an unmounted run cannot refresh another selected run', async ({ page }) => {
  const api = new ReviewAPI()
  const first = orchestrationRun(runA, sourceVersionA, 'pass')
  const second = orchestrationRun(runB, sourceVersionA, 'needs_review')
  api.runs.set(runA, first)
  api.runs.set(runB, second)
  api.history.set(sourceVersionA, [summary(first), summary(second)])
  api.editorialHistory.set(runA, [editorialReview('33333333-3333-4333-8333-333333333333', runA, 'approved', '2026-08-18T12:00:00Z')])
  api.editorialHistory.set(runB, [editorialReview('44444444-4444-4444-8444-444444444444', runB, 'rejected', '2026-08-18T12:01:00Z')])
  await openReview(page, api)
  await page.getByRole('button', { name: /Pass — machine assessment/ }).click()
  const editorial = page.locator('.editorial-review')
  await expect(editorial.getByText('Approved').first()).toBeVisible()

  const postGate = deferred<void>()
  api.editorialPostGate = postGate
  await editorial.getByRole('button', { name: 'Reject this run' }).click()
  await page.getByRole('button', { name: 'Record rejection' }).click()
  await expect(page.getByRole('button', { name: 'Working…' })).toBeVisible()

  await page.locator('.generation-history-list__item').filter({ hasText: 'Needs review' }).evaluate((button: HTMLButtonElement) => button.click())
  await expect(page.getByText('Needs review — machine assessment').last()).toBeVisible()
  await expect(editorial.getByText('Rejected').first()).toBeVisible()

  postGate.resolve()
  await expect.poll(() => api.count('GET', `/api/v1/admin/story-orchestration-runs/${runA}/editorial-reviews`)).toBe(1)
  await expect(editorial.getByText('Rejected').first()).toBeVisible()
  await expect(editorial.getByText('Editorial decision recorded. Editorial history is up to date.')).toHaveCount(0)
})

test('editorial integrity errors stay local to review controls and do not hide immutable generated content', async ({ page }) => {
  const api = new ReviewAPI()
  const selected = orchestrationRun(runA, sourceVersionA, 'pass')
  api.runs.set(runA, selected)
  api.history.set(sourceVersionA, [summary(selected)])
  await openReview(page, api)
  await page.getByRole('button', { name: /Pass — machine assessment/ }).click()
  const editorial = page.locator('.editorial-review')
  api.editorialPostFailure = { status: 409, code: 'story_orchestration_run_repair_required' }
  await editorial.getByRole('button', { name: 'Approve this run' }).click()
  await page.getByRole('button', { name: 'Record approval' }).click()
  await expect(editorial.getByText('Generation run needs repair')).toBeVisible()
  await expect(editorial.getByText(/concurrent/i)).toHaveCount(0)
  await expect(page.getByRole('tab', { name: 'Confident Readers' })).toBeVisible()
  await expect(page.getByText('Generated story')).toBeVisible()
})

test('a generation timeout remains an operational error and the review layout stays usable at a narrow width', async ({ page }) => {
  const api = new ReviewAPI()
  api.generationFailure = { status: 504, code: 'generation_timeout' }
  await openReview(page, api)
  await page.getByRole('button', { name: 'Generate adaptations' }).click()
  await expect(page.getByText(/Refresh recent generations before retrying/)).toBeVisible()
  await expect(page.getByText('Fail — machine assessment')).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Try again' })).toBeVisible()

  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(1)
})
