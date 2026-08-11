import { AxeBuilder } from '@axe-core/playwright'
import { expect, test } from './support/auth'
import type { Page, Route } from '@playwright/test'

const versionOne = '11111111-1111-4111-8111-111111111111'
const versionTwo = '22222222-2222-4222-8222-222222222222'
const versionThree = '33333333-3333-4333-8333-333333333333'
const acquisitionID = '44444444-4444-4444-8444-444444444444'
const timestamp = '2026-07-20T10:00:00Z'
const sourceHash = 'a'.repeat(64)

type QueuedFailure = { status: number; code: string; message: string }

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => {
    resolve = done
  })
  return { promise, resolve }
}

function versionSummary(
  versionId: string,
  version: number,
  options: {
    editionKey?: 'classic' | 'confident-readers' | 'growing-readers' | 'story-explorers' | 'little-listeners'
    isDraft?: boolean
    isPublished?: boolean
    health?: 'ready' | 'repair_required' | 'unavailable'
  } = {},
) {
  return {
    editionKey: options.editionKey ?? 'classic',
    versionId,
    version,
    createdAt: timestamp,
    isDraft: options.isDraft ?? false,
    isPublished: options.isPublished ?? false,
    segmentCount: 3,
    wordCount: 24,
    chapterCount: 1,
    health: options.health ?? 'ready',
  }
}

const editionKeys = [
  'classic',
  'confident-readers',
  'growing-readers',
  'story-explorers',
  'little-listeners',
] as const

type TestReleaseSummary = {
  release: number
  createdAt: string
  editions: Array<{
    editionKey: (typeof editionKeys)[number]
    versionId: string
    version: number
  }>
}

function editionSlots(
  status:
    | 'draft_only'
    | 'published'
    | 'published_with_draft'
    | 'unpublished'
    | 'repair_required',
  publishedVersion: { versionId: string; version: number } | null,
  draftVersion: { versionId: string; version: number } | null,
  versionCount: number,
) {
  return editionKeys.map((editionKey) => ({
    editionKey,
    status: editionKey === 'classic' ? status : 'empty',
    publishedVersion: editionKey === 'classic' ? publishedVersion : null,
    draftVersion: editionKey === 'classic' ? draftVersion : null,
    versionCount: editionKey === 'classic' ? versionCount : 0,
    updatedAt: editionKey === 'classic' ? timestamp : null,
  }))
}

function summary(
  slug: string,
  title: string,
  status:
    | 'draft_only'
    | 'published'
    | 'published_with_draft'
    | 'unpublished'
    | 'repair_required',
  options: { author?: string | null; versionCount?: number } = {},
) {
  const published = status === 'published' || status === 'published_with_draft'
  const draft = status === 'draft_only' || status === 'published_with_draft'
  const currentRelease: TestReleaseSummary | null = published
    ? {
        release: 1,
        createdAt: timestamp,
        editions: [
          { editionKey: 'classic', versionId: versionOne, version: 1 },
        ],
      }
    : null
  return {
    slug,
    title,
    author: options.author === undefined ? 'Panda Author' : options.author,
    language: 'en-GB',
    rights: { label: 'Public domain' },
    sourceUrl: 'https://example.invalid/source',
    status,
    publishedVersion: published ? { versionId: versionOne, version: 1 } : null,
    draftVersion: draft ? { versionId: versionTwo, version: 2 } : null,
    versionCount: options.versionCount ?? (status === 'published_with_draft' ? 2 : 1),
    updatedAt: timestamp,
    source: { status: 'missing', currentVersion: null, versionCount: 0, updatedAt: null },
    editions: editionSlots(
      status,
      published ? { versionId: versionOne, version: 1 } : null,
      draft ? { versionId: versionTwo, version: 2 } : null,
      options.versionCount ?? (status === 'published_with_draft' ? 2 : 1),
    ),
    currentRelease,
    releaseCount: published ? 1 : 0,
  }
}

type TestStorySummary = ReturnType<typeof summary>
type TestVersionSummary = ReturnType<typeof versionSummary>
type TestStoryDetail = Omit<TestStorySummary, 'editions'> & {
  createdAt: string
  versions: TestVersionSummary[]
  editions: Array<TestStorySummary['editions'][number] & { versions: TestVersionSummary[] }>
  releases: Array<NonNullable<TestStorySummary['currentRelease']>>
}

function withDetail(item: TestStorySummary, versions: TestVersionSummary[]): TestStoryDetail {
  return {
    ...item,
    createdAt: timestamp,
    versions,
    editions: item.editions.map((edition) => ({
      ...edition,
      versions: edition.editionKey === 'classic' ? versions : [],
    })),
    releases: item.currentRelease ? [item.currentRelease] : [],
  }
}

function sourceAcquisitionSummary(overrides: Record<string, unknown> = {}) {
  return {
    id: acquisitionID,
    provider: 'project-gutenberg',
    externalId: '11',
    title: "Alice's Adventures in Wonderland",
    contributors: [{ name: 'Lewis Carroll', role: 'author' }],
    languages: ['en'],
    landingUrl: 'https://www.gutenberg.org/ebooks/11',
    providerRights: 'Public domain in the United States.',
    selectedRepresentation: { label: 'Plain text UTF-8', mediaType: 'text/plain; charset=utf-8', providerUrl: 'https://www.gutenberg.org/files/11/11-0.txt', sizeBytes: 1234 },
    normalisationVersion: 'project-gutenberg-plain-text-v1',
    retrievedContentHash: sourceHash,
    normalisedContentHash: sourceHash,
    snapshotHash: sourceHash,
    createdAt: timestamp,
    eligibility: sourceEligibility(),
    sourceQuality: { status: 'pending', note: null as string | null, reviewedAt: null as string | null },
    promotion: null as null | { storySlug: string; storyTitle: string; sourceVersionId: string; sourceVersion: number; promotedAt: string },
    ...overrides,
  }
}

function sourceEvidenceReference(fact: string) {
  return [{ source: 'Catalogue record', fact }]
}

function sourceEligibility(overrides: Record<string, unknown> = {}) {
  return {
    policyVersion: 'panda-pages-copyright-v1',
    evaluationDate: '2026-07-20',
    evaluatedAt: timestamp,
    us: { status: 'eligible', reason: 'us_provider_public_domain_confirmed' },
    uk: { status: 'eligible', reason: 'uk_ordinary_literary_term_expired' },
    overall: 'eligible',
    overallReason: 'overall_eligible',
    opdsRights: 'public_domain',
    rdfRights: 'public_domain',
    headerRights: 'public_domain',
    providerTitle: "Alice's Adventures in Wonderland",
    contributors: [{ name: 'Lewis Carroll', role: 'author', deathYear: 1898 }],
    rdfDigest: sourceHash,
    effectiveUkEvidence: {
      workCategory: 'ordinary_literary',
      workCategoryReferences: sourceEvidenceReference('ordinary literary work'),
      authorship: 'single_known',
      authorshipReferences: sourceEvidenceReference('one author'),
      authorName: 'Lewis Carroll',
      authorDeathYear: 1898,
      authorReferences: sourceEvidenceReference('died in 1898'),
      firstPublicationYear: 1865,
      firstPublicationReferences: sourceEvidenceReference('first published in 1865'),
      translation: { state: 'none_confirmed', references: sourceEvidenceReference('no translation') },
      additionalTextualContribution: { state: 'none_confirmed', references: sourceEvidenceReference('no additional textual contribution') },
      specialCategory: { state: 'none_confirmed', references: sourceEvidenceReference('not a special category') },
      unpublishedAtEnd1988: { state: 'none_confirmed', references: sourceEvidenceReference('published before 1988') },
    },
    ...overrides,
  }
}

function sourceProviderWork(externalId = '11', title = "Alice's Adventures in Wonderland") {
  return {
    provider: 'project-gutenberg',
    externalId,
    title,
    contributors: [{ name: 'Lewis Carroll', role: 'author' }],
    languages: ['en'],
    landingUrl: `https://www.gutenberg.org/ebooks/${externalId}`,
    providerRights: 'Provider metadata only.',
    representations: [{ label: 'Plain text UTF-8', mediaType: 'text/plain; charset=utf-8', url: `https://www.gutenberg.org/files/${externalId}/${externalId}-0.txt`, sizeBytes: 1234 }],
  }
}

class StudioAPI {
  readonly requests: Array<{ method: string; path: string; body: unknown }> = []
  readonly unhandled: string[] = []
  listFailure: QueuedFailure | null = null
  detailFailure: QueuedFailure | null = null
  draftFailure: QueuedFailure | null = null
  draftOutcome: 'created_story' | 'created_version' | 'reused' = 'created_story'
  abortNextList = false
  previewRenderedHTML: string | null = null
  previewGate: {
    started: ReturnType<typeof deferred<void>>
    release: ReturnType<typeof deferred<void>>
  } | null = null
  detailGates = new Map<string, ReturnType<typeof deferred<void>>>()
  sourceSearchGates = new Map<string, ReturnType<typeof deferred<void>>>()
  sourceWorkGates = new Map<string, ReturnType<typeof deferred<void>>>()
  sourceEligibilityGates = new Map<string, ReturnType<typeof deferred<void>>>()
  sourceEligibilityOverrides = new Map<string, Record<string, unknown>>()
  sourceDetailGates = new Map<string, ReturnType<typeof deferred<void>>>()
  sourceListPlans: Array<{ gate: ReturnType<typeof deferred<void>>; items: ReturnType<typeof sourceAcquisitionSummary>[] }> = []

  stories = [
    summary('panda-tale', 'The Panda Tale', 'published_with_draft'),
    summary('quiet-moon', 'The Quiet Moon', 'draft_only', { author: null }),
    summary('old-oak', 'The Old Oak', 'unpublished'),
    summary('repair-story', 'The Tangled Story', 'repair_required'),
  ]

  details = new Map<string, TestStoryDetail>([
    [
      'panda-tale',
      withDetail(this.stories[0], [
        versionSummary(versionTwo, 2, { isDraft: true }),
        versionSummary(versionOne, 1, { isPublished: true }),
      ]),
    ],
    [
      'quiet-moon',
      withDetail(this.stories[1], [versionSummary(versionTwo, 2, { isDraft: true })]),
    ],
    [
      'old-oak',
      withDetail(this.stories[2], [versionSummary(versionOne, 1)]),
    ],
    [
      'repair-story',
      withDetail(this.stories[3], [
        versionSummary(versionOne, 1, { health: 'repair_required' }),
      ]),
    ],
  ])
  sourceAcquisitions: ReturnType<typeof sourceAcquisitionSummary>[] = []

  async install(page: Page) {
    await page.route('**/api/v1/**', (route) => this.handle(route))
  }

  count(method: string, path: string) {
    return this.requests.filter(
      (request) => request.method === method && request.path === path,
    ).length
  }

  private async fulfill(route: Route, body: unknown, status = 200) {
    await route.fulfill({
      status,
      contentType: 'application/json',
      headers: { 'Cache-Control': 'no-store' },
      body: JSON.stringify(body),
    })
  }

  private async fail(route: Route, failure: QueuedFailure) {
    await this.fulfill(
      route,
      { error: { code: failure.code, message: failure.message } },
      failure.status,
    )
  }

  private source(slug: string, id: string) {
    const detail = this.details.get(slug)
    const versions = detail?.versions ?? []
    const version = versions.find((candidate) => candidate.versionId === id)
    if (!detail || !version) return null
    return {
      slug,
      editionKey: version.editionKey,
      versionId: id,
      version: version.version,
      title: detail.title,
      author: detail.author,
      language: detail.language,
      rights: detail.rights,
      sourceUrl: detail.sourceUrl,
      markdown: `# ${detail.title}\n\n## Chapter I\n\nA calm canonical source for ${slug}.\n`,
      renderedHtml: `<h1>${detail.title}</h1><h2>Chapter I</h2><p>A calm canonical source.</p>`,
      segmentCount: version.segmentCount,
      wordCount: version.wordCount,
      chapterCount: version.chapterCount,
      createdAt: version.createdAt,
      isDraft: version.isDraft,
      isPublished: version.isPublished,
      health: version.health,
    }
  }

  private async handle(route: Route) {
    const request = route.request()
    const url = new URL(request.url())
    const method = request.method()
    const path = url.pathname
    const body = request.postDataJSON() ?? null
    this.requests.push({ method, path, body })

    if (path === '/api/auth/me') {
      await this.fulfill(route, { signedIn: true })
      return
    }
    if (path === '/auth/v1/logout' && method === 'POST') {
      await this.fulfill(route, { ok: true })
      return
    }
    if (path === '/api/v1/library') {
      await this.fulfill(route, { items: [], unavailableItemCount: 0 })
      return
    }
    if (path === '/api/v1/profiles' && method === 'GET') {
      await this.fulfill(route, {
        profiles: [
          {
            id: '123e4567-e89b-42d3-a456-426614174300',
            name: 'Mina',
            pin_enabled: false,
            reading_level: 'classic',
          },
        ],
      })
      return
    }

    if (path === '/api/v1/admin/source-providers/project-gutenberg/search' && method === 'GET') {
      const query = url.searchParams.get('q') ?? ''
      const gate = this.sourceSearchGates.get(query)
      if (gate) {
        this.sourceSearchGates.delete(query)
        await gate.promise
      }
      const work = query === 'rabbit'
        ? sourceProviderWork('12', 'Rabbit source')
        : sourceProviderWork()
      await this.fulfill(route, { provider: 'project-gutenberg', results: [work] })
      return
    }

    const sourceWorkMatch = /^\/api\/v1\/admin\/source-providers\/project-gutenberg\/works\/(\d+)$/.exec(path)
    if (sourceWorkMatch && method === 'GET') {
      const externalId = sourceWorkMatch[1]
      const gate = this.sourceWorkGates.get(externalId)
      if (gate) { this.sourceWorkGates.delete(externalId); await gate.promise }
      await this.fulfill(route, sourceProviderWork(externalId, externalId === '12' ? 'Rabbit source' : "Alice's Adventures in Wonderland"))
      return
    }

    if (path === '/api/v1/admin/source-providers/project-gutenberg/works/11/acquisitions' && method === 'POST') {
      const existing = this.sourceAcquisitions.find((item) => item.externalId === '11')
      if (existing) {
        await this.fulfill(route, { outcome: 'reused', acquisition: existing })
        return
      }
      const acquisition = sourceAcquisitionSummary()
      this.sourceAcquisitions.unshift(acquisition)
      await this.fulfill(route, { outcome: 'created', acquisition }, 201)
      return
    }

    const sourceEligibilityMatch = /^\/api\/v1\/admin\/source-providers\/project-gutenberg\/works\/(\d+)\/copyright-eligibility$/.exec(path)
    if (sourceEligibilityMatch && method === 'POST') {
      const externalId = sourceEligibilityMatch[1]
      const gate = this.sourceEligibilityGates.get(externalId)
      if (gate) { this.sourceEligibilityGates.delete(externalId); await gate.promise }
      await this.fulfill(route, sourceEligibility({ providerTitle: externalId === '12' ? 'Rabbit source' : "Alice's Adventures in Wonderland", ...this.sourceEligibilityOverrides.get(externalId) }))
      return
    }

    if (path === '/api/v1/admin/source-acquisitions' && method === 'GET') {
      const plan = this.sourceListPlans.shift()
      if (plan) {
        await plan.gate.promise
        await this.fulfill(route, { items: plan.items })
        return
      }
      await this.fulfill(route, { items: this.sourceAcquisitions })
      return
    }

    const acquisitionReviewMatch = /^\/api\/v1\/admin\/source-acquisitions\/([^/]+)\/source-quality-review$/.exec(path)
    if (acquisitionReviewMatch && method === 'PUT') {
      const acquisition = this.sourceAcquisitions.find((item) => item.id === acquisitionReviewMatch[1])
      const update = body as { status?: string; note?: string }
      if (!acquisition || (update.status !== 'pending' && update.status !== 'approved' && update.status !== 'rejected')) {
        await this.fail(route, { status: 400, code: 'source_acquisition_review_invalid', message: 'source acquisition review is invalid' })
        return
      }
      acquisition.sourceQuality = update.status === 'pending'
        ? { status: 'pending', note: null, reviewedAt: null }
        : { status: update.status, note: String(update.note ?? ''), reviewedAt: timestamp }
      await this.fulfill(route, acquisition)
      return
    }

    const acquisitionPromotionMatch = /^\/api\/v1\/admin\/source-acquisitions\/([^/]+)\/promote$/.exec(path)
    if (acquisitionPromotionMatch && method === "POST") {
      const acquisition = this.sourceAcquisitions.find((item) => item.id === acquisitionPromotionMatch[1])
      const target = body as { target?: { mode?: string; title?: string; slug?: string; storySlug?: string } }
      if (!acquisition) {
        await this.fail(route, { status: 404, code: "source_acquisition_not_found", message: "source acquisition was not found" })
        return
      }
      if (acquisition.sourceQuality.status !== "approved") {
        await this.fail(route, { status: 409, code: "source_acquisition_not_ready", message: "source acquisition is not ready for promotion" })
        return
      }
      if (acquisition.promotion) {
        await this.fulfill(route, { outcome: "reused", promotion: acquisition.promotion })
        return
      }
      const requestTarget = target.target
      const story = requestTarget?.mode === "new_story"
        ? { slug: requestTarget.slug ?? "", title: requestTarget.title ?? "" }
        : this.stories.find((item) => item.slug === requestTarget?.storySlug)
      if (!story || !story.slug || !story.title) {
        await this.fail(route, { status: 404, code: "source_acquisition_promotion_target_invalid", message: "promotion target was not found" })
        return
      }
      acquisition.promotion = {
        storySlug: story.slug,
        storyTitle: story.title,
        sourceVersionId: versionThree,
        sourceVersion: 1,
        promotedAt: timestamp,
      }
      await this.fulfill(route, { outcome: "created", promotion: acquisition.promotion }, 201)
      return
    }

    const acquisitionDetailMatch = /^\/api\/v1\/admin\/source-acquisitions\/([^/]+)$/.exec(path)
    if (acquisitionDetailMatch && method === 'GET') {
      const acquisition = this.sourceAcquisitions.find((item) => item.id === acquisitionDetailMatch[1])
      const gate = this.sourceDetailGates.get(acquisitionDetailMatch[1])
      if (gate) {
        this.sourceDetailGates.delete(acquisitionDetailMatch[1])
        await gate.promise
      }
      if (!acquisition) {
        await this.fail(route, { status: 404, code: 'source_acquisition_not_found', message: 'source acquisition was not found' })
        return
      }
      await this.fulfill(route, { ...acquisition, sourceText: 'Down the rabbit-hole.\n\nThis is durable provider material.\n' })
      return
    }

    if (path === '/api/v1/admin/stories' && method === 'GET') {
      if (this.abortNextList) {
        this.abortNextList = false
        await route.abort('failed')
        return
      }
      if (this.listFailure) {
        const failure = this.listFailure
        this.listFailure = null
        await this.fail(route, failure)
        return
      }
      await this.fulfill(route, { items: this.stories })
      return
    }

    if (path === '/api/v1/admin/preview' && method === 'POST') {
      const input = body as Record<string, unknown>
      if (this.previewGate) {
        const gate = this.previewGate
        this.previewGate = null
        gate.started.resolve()
        await gate.release.promise
      }
      const title = typeof input.title === 'string' ? input.title : ''
      if (!title.trim()) {
        await this.fulfill(
          route,
          {
            error: {
              code: 'preview_invalid',
              message: 'Story content is invalid',
              issues: [
                { field: 'title', code: 'required', message: 'Enter a title' },
              ],
            },
          },
          400,
        )
        return
      }
      await this.fulfill(route, {
        slug: input.slug,
        title: title.trim(),
        author: input.author,
        language: input.language,
        rights: input.rights,
        sourceUrl: input.sourceUrl,
        renderedHtml:
          this.previewRenderedHTML ??
          `<h1>${title.trim()}</h1><p>Canonical preview content.</p>`,
        segmentCount: 3,
        wordCount: 18,
        chapterCount: 1,
        warnings: [
          {
            field: 'sourceUrl',
            code: 'advisory',
            message: 'Confirm the source reference',
          },
        ],
      })
      return
    }

    if (path === '/api/v1/admin/stories/draft' && method === 'POST') {
      if (this.draftFailure) {
        const failure = this.draftFailure
        this.draftFailure = null
        await this.fail(route, failure)
        return
      }
      const input = body as Record<string, unknown>
      const slug = String(input.slug)
      const title = typeof input.title === 'string' ? input.title : ''
      const author = typeof input.author === 'string' ? input.author : null
      const existing = this.details.get(slug)
      if (this.draftOutcome === 'reused' && existing?.draftVersion) {
        const reused = existing.draftVersion
        await this.fulfill(route, {
          slug,
          editionKey:
          typeof input.editionKey === 'string'
            ? input.editionKey
            : 'classic',
          versionId: reused.versionId,
          version: reused.version,
          segmentCount: 3,
          wordCount: 18,
          chapterCount: 1,
          renderedHtml: '<h1>Reused</h1>',
          outcome: 'reused',
        })
        return
      }
      const resultVersion = existing ? 3 : 1
      const resultId = existing ? versionThree : versionOne
      const item = summary(
        slug,
        title,
        'draft_only',
        { author, versionCount: existing ? 3 : 1 },
      )
      item.draftVersion = { versionId: resultId, version: resultVersion }
      item.publishedVersion = existing
        ? (existing.publishedVersion ?? null)
        : null
      if (existing?.publishedVersion) item.status = 'published_with_draft'
      if (existing) {
        item.currentRelease = existing.currentRelease
        item.releaseCount = existing.releaseCount
      }
      const versions = existing
        ? [
            versionSummary(resultId, resultVersion, { isDraft: true }),
            ...(existing.versions.map((version) => ({
              ...version,
              isDraft: false,
            })) as TestVersionSummary[]),
          ]
        : [versionSummary(resultId, resultVersion, { isDraft: true })]
      const detail = withDetail(item, versions)
      const classicEdition = detail.editions[0]
      classicEdition.status = detail.status
      classicEdition.draftVersion = detail.draftVersion
      classicEdition.publishedVersion = detail.publishedVersion
      classicEdition.versionCount = versions.length
      this.details.set(slug, detail)
      const index = this.stories.findIndex((candidate) => candidate.slug === slug)
      if (index >= 0) this.stories[index] = item
      else this.stories.unshift(item)
      await this.fulfill(route, {
        slug,
        editionKey:
          typeof input.editionKey === 'string'
            ? input.editionKey
            : 'classic',
        versionId: resultId,
        version: resultVersion,
        segmentCount: 3,
        wordCount: 18,
        chapterCount: 1,
        renderedHtml: '<h1>Saved</h1>',
        outcome: this.draftOutcome,
      })
      return
    }

    const versionMatch = /^\/api\/v1\/admin\/stories\/([^/]+)\/editions\/([^/]+)\/versions\/([^/]+)$/.exec(path)
    if (versionMatch && method === 'GET') {
      const source = this.source(decodeURIComponent(versionMatch[1]), versionMatch[3])
      if (source && source.editionKey !== decodeURIComponent(versionMatch[2])) {
        await this.fail(route, { status: 404, code: 'version_not_found', message: 'story version was not found' })
        return
      }
      if (!source || source.health !== 'ready') {
        await this.fail(route, {
          status: source ? 409 : 404,
          code: source ? 'version_repair_required' : 'version_not_found',
          message: source ? 'story version requires repair' : 'story version was not found',
        })
        return
      }
      await this.fulfill(route, source)
      return
    }

    const releaseMatch = /^\/api\/v1\/admin\/stories\/([^/]+)\/releases$/.exec(path)
    if (releaseMatch && method === 'POST') {
      const slug = decodeURIComponent(releaseMatch[1])
      const detail = this.details.get(slug)
      const requested = (body as { editions?: Array<{ editionKey?: string; versionId?: string }> }).editions ?? []
      if (!detail || requested.length < 1 || requested.length > editionKeys.length) {
        await this.fail(route, {
          status: 400,
          code: 'release_invalid',
          message: 'Story release is invalid',
        })
        return
      }

      const members: Array<{ editionKey: typeof editionKeys[number]; versionId: string; version: number }> = []
      for (const item of requested) {
        if (!editionKeys.includes(item.editionKey as typeof editionKeys[number]) || typeof item.versionId !== 'string') {
          await this.fail(route, {
            status: 400,
            code: 'release_invalid',
            message: 'Story release is invalid',
          })
          return
        }
        const editionKey = item.editionKey as typeof editionKeys[number]
        const edition = detail.editions.find((candidate) => candidate.editionKey === editionKey)
        const selected = edition?.versions.find((version) => version.versionId === item.versionId)
        if (!edition || !selected || selected.health !== 'ready') {
          await this.fail(route, {
            status: 409,
            code: 'release_repair_required',
            message: 'stored release state or edition version requires repair',
          })
          return
        }
        members.push({
          editionKey,
          versionId: selected.versionId,
          version: selected.version,
        })
      }

      members.sort((left, right) => editionKeys.indexOf(left.editionKey) - editionKeys.indexOf(right.editionKey))
      detail.releaseCount += 1
      const release = {
        release: detail.releaseCount,
        createdAt: timestamp,
        editions: members,
      }
      detail.currentRelease = release
      detail.releases = [release, ...detail.releases]

      for (const edition of detail.editions) {
        const live = members.find((member) => member.editionKey === edition.editionKey)
        edition.publishedVersion = live
          ? { versionId: live.versionId, version: live.version }
          : null
        for (const version of edition.versions) {
          version.isPublished = version.versionId === live?.versionId
        }
        if (live) {
          edition.status =
            edition.draftVersion && edition.draftVersion.versionId !== live.versionId
              ? 'published_with_draft'
              : 'published'
        } else if (edition.draftVersion) {
          edition.status = 'draft_only'
        } else if (edition.versionCount > 0) {
          edition.status = 'unpublished'
        } else {
          edition.status = 'empty'
        }
      }

      const compatibility = members[0]
      detail.publishedVersion = {
        versionId: compatibility.versionId,
        version: compatibility.version,
      }
      detail.status = detail.editions.some(
        (edition) =>
          edition.draftVersion &&
          edition.draftVersion.versionId !== edition.publishedVersion?.versionId,
      )
        ? 'published_with_draft'
        : 'published'

      const item = this.stories.find((candidate) => candidate.slug === slug)
      if (item) Object.assign(item, detail)
      await this.fulfill(route, {
        slug,
        outcome: 'created',
        release,
      })
      return
    }

    const unpublishMatch = /^\/api\/v1\/admin\/stories\/([^/]+)\/unpublish$/.exec(path)
    if (unpublishMatch && method === 'POST') {
      const slug = decodeURIComponent(unpublishMatch[1])
      const detail = this.details.get(slug)
      if (!detail) {
        await this.fail(route, { status: 404, code: 'unpublish_not_found', message: 'story was not found' })
        return
      }
      for (const edition of detail.editions) {
        edition.publishedVersion = null
        for (const version of edition.versions) version.isPublished = false
        if (edition.draftVersion) edition.status = 'draft_only'
        else if (edition.versionCount > 0) edition.status = 'unpublished'
        else edition.status = 'empty'
      }
      detail.currentRelease = null
      detail.publishedVersion = null
      detail.status = detail.editions.some((edition) => edition.draftVersion)
        ? 'draft_only'
        : 'unpublished'
      const item = this.stories.find((candidate) => candidate.slug === slug)
      if (item) Object.assign(item, detail)
      await this.fulfill(route, {
        slug,
        status: detail.status,
        publishedVersion: null,
        draftVersion: detail.draftVersion,
        versionCount: detail.versionCount,
        updatedAt: timestamp,
        currentRelease: null,
        releaseCount: detail.releaseCount,
      })
      return
    }

    const detailMatch = /^\/api\/v1\/admin\/stories\/([^/]+)$/.exec(path)
    if (detailMatch && method === 'GET') {
      const slug = decodeURIComponent(detailMatch[1])
      const gate = this.detailGates.get(slug)
      if (gate) {
        this.detailGates.delete(slug)
        await gate.promise
      }
      if (this.detailFailure) {
        const failure = this.detailFailure
        this.detailFailure = null
        await this.fail(route, failure)
        return
      }
      const detail = this.details.get(slug)
      if (!detail) {
        await this.fail(route, { status: 404, code: 'story_not_found', message: 'story was not found' })
        return
      }
      await this.fulfill(route, detail)
      return
    }

    this.unhandled.push(`${method} ${path}`)
    await this.fail(route, {
      status: 501,
      code: 'unhandled_test_route',
      message: 'Unhandled test route',
    })
  }
}

async function seriousOrCriticalViolations(page: Page) {
  const results = await new AxeBuilder({ page }).analyze()
  return results.violations.filter(
    (violation) =>
      violation.impact === 'serious' || violation.impact === 'critical',
  )
}

async function openCatalogue(page: Page, api: StudioAPI) {
  await api.install(page)
  await page.goto('/admin')
  await expect(page).toHaveURL(/\/admin\/stories$/)
  await expect(page.getByRole('heading', { level: 1, name: 'Stories' })).toBeVisible()
}

async function expectPandaVisualShell(page: Page) {
  const shell = page.locator('.story-studio-shell')
  await expect(shell).toHaveClass(/panda-print-surface/)
  await expect(page.locator('.studio-brand__panda')).toHaveAttribute('src', '/logo.png')
  expect(
    await shell.evaluate((element) => {
      const style = getComputedStyle(element)
      const texture = getComputedStyle(element, '::before')
      return {
        background: style.backgroundColor,
        color: style.color,
        colorScheme: style.colorScheme,
        font: style.fontFamily,
        texture: texture.backgroundImage,
      }
    }),
  ).toEqual({
    background: 'rgb(244, 241, 233)',
    color: 'rgb(17, 17, 15)',
    colorScheme: 'light',
    font: expect.stringContaining('Atkinson Hyperlegible Next Variable'),
    texture: expect.stringContaining('radial-gradient'),
  })
  await expect(page.locator('.studio-nav__new')).toHaveCSS(
    'background-color',
    'rgb(17, 17, 15)',
  )
  expect(
    await page.locator('.studio-page-heading h1').evaluate(
      (element) => getComputedStyle(element).fontFamily,
    ),
  ).toContain('Literata Variable')
}

async function expectNoHorizontalOverflow(page: Page) {
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    ),
  ).toBeLessThanOrEqual(1)
}

test('catalogue loads human statuses and supports deterministic search and filtering', async ({
  page,
}) => {
  const api = new StudioAPI()
  await openCatalogue(page, api)
  await expectPandaVisualShell(page)

  const catalogue = page.getByLabel('Story catalogue')
  await expect(catalogue.locator('.studio-status').filter({ hasText: /^Published · New draft$/ })).toBeVisible()
  await expect(catalogue.locator('.studio-status').filter({ hasText: /^Draft only$/ })).toBeVisible()
  await expect(catalogue.locator('.studio-status').filter({ hasText: /^Unpublished$/ })).toBeVisible()
  await expect(catalogue.locator('.studio-status').filter({ hasText: /^Needs attention$/ })).toBeVisible()

  await page.getByLabel('Search stories').fill('moon')
  await expect(page.getByRole('heading', { name: 'The Quiet Moon' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'The Panda Tale' })).toBeHidden()
  await page.getByLabel('Search stories').fill('')
  await page.getByLabel('Status').selectOption('repair_required')
  await expect(page.getByRole('heading', { name: 'The Tangled Story' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'The Quiet Moon' })).toBeHidden()
  expect(await seriousOrCriticalViolations(page)).toEqual([])
  expect(api.unhandled).toEqual([])
})

test('global source review validates factual evidence, saves eligible work, and keeps source-quality review independent', async ({ page }) => {
  const api = new StudioAPI()
  await api.install(page)
  await page.goto('/admin/source-review')

  await expect(page.getByRole('heading', { level: 1, name: 'Source review' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Source review', exact: true })).toHaveAttribute('aria-current', 'page')
  await expect(page.getByText('No sources saved for review yet')).toHaveCount(0)
  const findTab = page.getByRole('tab', { name: 'Find a source' })
  const savedTab = page.getByRole('tab', { name: 'Saved sources' })
  await findTab.focus()
  await page.keyboard.press('ArrowRight')
  await expect(savedTab).toHaveAttribute('aria-selected', 'true')
  await expect(savedTab).toBeFocused()
  await page.keyboard.press('ArrowLeft')
  await expect(findTab).toHaveAttribute('aria-selected', 'true')
  await expect(findTab).toBeFocused()
  await page.getByRole('searchbox', { name: 'Search Project Gutenberg' }).fill('alice')
  await page.getByRole('button', { name: 'Search', exact: true }).click()
  await expect(page.getByRole('heading', { name: "Alice's Adventures in Wonderland" })).toBeVisible()
  await page.getByRole('button', { name: 'Select work' }).click()
  await expect(page.getByRole('heading', { name: "Alice's Adventures in Wonderland" })).toHaveCount(2)
  await expect(page.getByText('United States:')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Validate & save for source review' })).toBeVisible()
  await expect(page.getByRole('button', { name: /Preview source|Approve preview|Accept candidate/ })).toHaveCount(0)
  await page.getByLabel('Work type evidence source').fill('Title-page record')
  await page.getByLabel('Work type observed fact').fill('The title page identifies this as an ordinary literary work.')
  await page.getByLabel('First publication year').fill('1865')
  await page.getByLabel('First publication evidence source').fill('Library catalogue')
  await page.getByLabel('First publication observed fact').fill('The catalogue records first publication in 1865.')
  await page.getByLabel('First publication evidence locator / reference (optional)').fill('Shelfmark C.123, title page')
  await page.getByLabel('Translation', { exact: true }).selectOption('none_confirmed')
  await page.getByLabel('Translation evidence source').fill('Edition inspection')
  await page.getByLabel('Translation observed fact').fill('No translator is identified in this acquired text.')
  await page.getByLabel('Additional textual contribution', { exact: true }).selectOption('none_confirmed')
  await page.getByLabel('Additional textual contribution evidence source').fill('Edition inspection')
  await page.getByLabel('Additional textual contribution observed fact').fill('No additional textual contribution appears in this acquired text.')
  await page.getByLabel('Special category', { exact: true }).selectOption('none_confirmed')
  await page.getByLabel('Special category evidence source').fill('Catalogue record')
  await page.getByLabel('Special category observed fact').fill('The catalogue identifies no Crown or special category.')
  await page.getByLabel('Unpublished at end of 1988', { exact: true }).selectOption('none_confirmed')
  await page.getByLabel('Unpublished-at-end-of-1988 evidence source').fill('Publication record')
  await page.getByLabel('Unpublished-at-end-of-1988 observed fact').fill('The work was published before the end of 1988.')
  await page.getByRole('button', { name: 'Validate & save for source review' }).click()

  await expect(page.getByText('Saved for source review.')).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Saved source text' })).toBeVisible()
  await expect(page.locator('.source-review__text pre')).toContainText('durable provider material')
  await expect(page.getByRole('heading', { name: 'Copyright eligibility' })).toBeVisible()
  expect(api.count('POST', '/api/v1/admin/source-providers/project-gutenberg/works/11/acquisitions')).toBe(1)
  const saveBody = api.requests.find((request) => request.path.endsWith('/acquisitions'))?.body as Record<string, unknown>
  expect(saveBody).toMatchObject({ firstPublicationYear: 1865, workCategoryReferences: [{ source: 'Title-page record', fact: 'The title page identifies this as an ordinary literary work.' }], firstPublicationReferences: [{ source: 'Library catalogue', fact: 'The catalogue records first publication in 1865.', locator: 'Shelfmark C.123, title page' }], translation: { references: [{ source: 'Edition inspection', fact: 'No translator is identified in this acquired text.' }] } })
  expect(JSON.stringify(saveBody)).not.toMatch(/sourceText|providerUrl|snapshotHash|policyVersion|"eligible"/)
  await expect(page.getByLabel('Rights status')).toHaveCount(0)

  await page.getByLabel('Source quality status').selectOption('rejected')
  await page.getByLabel('Rationale').fill('Complete, readable text for the intended work.')
  await page.getByRole('button', { name: 'Save source quality review' }).click()
  await expect(page.getByText('Source quality review updated.')).toBeVisible()

  await page.getByLabel('Source quality status').selectOption('approved')
  await page.getByLabel('Rationale').fill('Complete, readable text for the intended work.')
  await page.getByRole('button', { name: 'Save source quality review' }).click()
  await expect(page.getByRole('heading', { name: 'Ready for canonical-source promotion' })).toBeVisible()
  await page.getByRole('button', { name: 'Promote to canonical source' }).click()
  await expect(page.getByLabel('Story title')).toHaveValue("Alice's Adventures in Wonderland")
  await expect(page.getByLabel('Story slug')).toHaveValue('alice-s-adventures-in-wonderland')
  await page.getByRole('button', { name: 'Create story & promote source' }).click()
  await expect(page.getByRole('heading', { name: 'Promoted to canonical source' })).toBeVisible()
  await expect(page.getByText('Source quality was locked when this acquisition was promoted.', { exact: true })).toBeVisible()
  await expect(page.getByText('Status', { exact: true })).toBeVisible()
  await expect(page.getByText('approved', { exact: true })).toBeVisible()
  await expect(page.getByText('Complete, readable text for the intended work.', { exact: true })).toBeVisible()
  await expect(page.getByLabel('Source quality status')).toHaveCount(0)
  await expect(page.getByLabel('Rationale')).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Save source quality review' })).toHaveCount(0)
  const promotionBody = api.requests.find((request) => request.path.endsWith('/promote'))?.body as Record<string, unknown>
  expect(promotionBody).toEqual({ target: { mode: 'new_story', title: "Alice's Adventures in Wonderland", slug: 'alice-s-adventures-in-wonderland' } })
  expect(JSON.stringify(promotionBody)).not.toMatch(/sourceText|snapshotHash|assessmentHash|eligible|providerUrl/)
  await expect(page.getByRole('button', { name: /Generate editions|Publish|Create release/ })).toHaveCount(0)
  expect(api.count('PUT', `/api/v1/admin/source-acquisitions/${acquisitionID}/source-quality-review`)).toBe(2)

  await page.getByRole('tab', { name: 'Find a source' }).click()
  await page.getByRole('searchbox', { name: 'Search Project Gutenberg' }).fill('alice')
  await page.getByRole('button', { name: 'Search', exact: true }).click()
  await page.getByRole('button', { name: 'Select work' }).click()
  await page.getByRole('button', { name: 'Validate & save for source review' }).click()
  await expect(page.getByText('This exact saved source already exists. Opening it for review.')).toBeVisible()
  expect(api.count('POST', '/api/v1/admin/source-providers/project-gutenberg/works/11/acquisitions')).toBe(2)
  expect(api.unhandled).toEqual([])
  expect(await seriousOrCriticalViolations(page)).toEqual([])
})

test('global source review preserves provider facts through a stale factual form', async ({ page }) => {
  const api = new StudioAPI()
  api.sourceEligibilityOverrides.set('11', { contributors: [{ name: 'Lewis Carroll', role: 'author', deathYear: 1898 }, { name: 'Translator', role: 'translator' }] })
  await api.install(page)
  await page.goto('/admin/source-review')
  await page.getByRole('searchbox', { name: 'Search Project Gutenberg' }).fill('alice')
  await page.getByRole('button', { name: 'Search', exact: true }).click()
  await page.getByRole('button', { name: 'Select work' }).click()
  await expect(page.getByText('Translator — translator')).toBeVisible()
  await expect(page.getByText('Provider author death year: 1898')).toBeVisible()
  await expect(page.getByLabel('Translation', { exact: true })).toHaveCount(0)
  await expect(page.getByLabel('Author death year', { exact: true })).toHaveCount(0)
  await page.getByLabel('First publication year').fill('1865')
  await expect(page.locator('.source-review__stale')).toHaveAttribute('role', 'status')
  await expect(page.getByText('Eligibility conclusion needs revalidation after factual evidence changed.')).toBeVisible()
  await expect(page.getByText('Translator — translator')).toBeVisible()
  expect(api.unhandled).toEqual([])
})


test('global source review isolates stale search, saved-list, and detail requests', async ({ page }) => {
  const api = new StudioAPI()
  const searchA = deferred<void>()
  const searchB = deferred<void>()
  const savedA = deferred<void>()
  const savedB = deferred<void>()
  const detailA = deferred<void>()
  const detailB = deferred<void>()
  const secondID = '55555555-5555-4555-8555-555555555555'
  const first = sourceAcquisitionSummary({ title: 'First saved source' })
  const second = sourceAcquisitionSummary({ id: secondID, externalId: '12', title: 'Second saved source' })
  api.sourceAcquisitions = []
  await api.install(page)
  await page.goto('/admin/source-review')
  await expect.poll(() => api.count('GET', '/api/v1/admin/source-acquisitions')).toBe(1)
  api.sourceAcquisitions = [first, second]

  api.sourceSearchGates.set('alice', searchA)
  api.sourceSearchGates.set('rabbit', searchB)
  await page.getByRole('searchbox', { name: 'Search Project Gutenberg' }).fill('alice')
  await page.getByRole('button', { name: 'Search', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Searching Project Gutenberg' })).toBeVisible()
  await page.locator('#source-provider-search').evaluate((element) => {
    const input = element as HTMLInputElement
    input.value = 'rabbit'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    input.form?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  })
  await expect.poll(() => api.count('GET', '/api/v1/admin/source-providers/project-gutenberg/search')).toBe(2)
  searchA.resolve()
  await expect(page.getByRole('heading', { name: 'Searching Project Gutenberg' })).toBeVisible()
  await expect(page.getByRole('alert')).toHaveCount(0)
  await expect(page.getByRole('heading', { name: 'Rabbit source' })).toHaveCount(0)
  searchB.resolve()
  await expect(page.getByRole('heading', { name: 'Rabbit source' })).toBeVisible()

  const delayedWorkA = deferred<void>()
  api.sourceWorkGates.set('11', delayedWorkA)
  await page.getByRole('searchbox', { name: 'Search Project Gutenberg' }).fill('alice')
  await page.getByRole('button', { name: 'Search', exact: true }).click()
  await page.getByRole('button', { name: 'Select work' }).click()
  await expect.poll(() => api.count('GET', '/api/v1/admin/source-providers/project-gutenberg/works/11')).toBe(1)
  await page.getByRole('searchbox', { name: 'Search Project Gutenberg' }).fill('rabbit')
  await page.getByRole('button', { name: 'Search', exact: true }).click()
  await page.getByRole('button', { name: 'Select work' }).click()
  await expect(page.getByText('Project Gutenberg #12').last()).toBeVisible()
  delayedWorkA.resolve()
  await expect(page.getByText('Project Gutenberg #12').last()).toBeVisible()
  await expect(page.getByRole('alert')).toHaveCount(0)

  api.sourceListPlans.push({ gate: savedA, items: [first] }, { gate: savedB, items: [second] })
  await page.getByRole('tab', { name: 'Saved sources' }).click()
  await expect.poll(() => api.count('GET', '/api/v1/admin/source-acquisitions')).toBe(2)
  await page.getByRole('tab', { name: 'Find a source' }).click()
  await page.getByRole('tab', { name: 'Saved sources' }).click()
  await expect.poll(() => api.count('GET', '/api/v1/admin/source-acquisitions')).toBe(3)
  savedA.resolve()
  await expect(page.getByRole('heading', { name: 'Loading saved sources' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'First saved source' })).toHaveCount(0)
  await expect(page.getByRole('alert')).toHaveCount(0)
  savedB.resolve()
  await expect(page.getByRole('button', { name: 'Second saved source' })).toBeVisible()
  await page.getByRole('tab', { name: 'Find a source' }).click()
  await page.getByRole('tab', { name: 'Saved sources' }).click()
  await expect(page.getByRole('button', { name: 'First saved source' })).toBeVisible()

  api.sourceDetailGates.set(acquisitionID, detailA)
  api.sourceDetailGates.set(secondID, detailB)
  await page.getByRole('button', { name: 'First saved source' }).click()
  await expect.poll(() => api.count('GET', `/api/v1/admin/source-acquisitions/${acquisitionID}`)).toBe(1)
  await page.getByRole('button', { name: 'Second saved source' }).click()
  await expect.poll(() => api.count('GET', `/api/v1/admin/source-acquisitions/${secondID}`)).toBe(1)
  detailA.resolve()
  await expect(page.getByRole('heading', { name: 'Opening saved source' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'First saved source' })).toHaveCount(0)
  await expect(page.getByRole('alert')).toHaveCount(0)
  detailB.resolve()
  await expect(page.getByRole('heading', { name: 'Second saved source' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'First saved source' })).toHaveCount(0)
  expect(api.unhandled).toEqual([])
})

test('owner can round-trip Manage profiles and Story Studio without entering reader mode', async ({
  page,
}) => {
  const api = new StudioAPI()
  await api.install(page)

  await page.goto('/profiles/manage')
  await expect(
    page.getByRole('heading', { level: 1, name: 'Manage profiles' }),
  ).toBeVisible()
  await expect(
    page.getByRole('button', { name: /Story Studio/ }),
  ).toBeVisible()
  await expect(
    page.getByRole('button', { name: 'Parent controls' }),
  ).toHaveCount(0)

  await page.getByRole('button', { name: /Story Studio/ }).click()

  await expect(page).toHaveURL('/admin/stories')
  await expect(
    page.getByRole('heading', { level: 1, name: 'Stories' }),
  ).toBeVisible()
  await expect(
    page.getByRole('button', { name: 'Manage profiles' }),
  ).toBeVisible()

  await page.getByRole('button', { name: 'Manage profiles' }).click()

  await expect(page).toHaveURL('/profiles/manage')
  await expect(
    page.getByRole('heading', { level: 1, name: 'Manage profiles' }),
  ).toBeVisible()
  await expect(
    page.getByRole('button', { name: 'Edit Mina' }),
  ).toBeVisible()
  await expect(
    page.getByRole('button', { name: 'Parent controls' }),
  ).toHaveCount(0)
  expect(api.unhandled).toEqual([])
})

test('new story preview shows structured validation, canonical output and outdated state', async ({
  page,
}) => {
  const api = new StudioAPI()
  await api.install(page)
  await page.goto('/admin/stories/new')

  await page.getByRole('button', { name: 'Preview', exact: true }).first().click()
  await expect(page.locator('#story-title-error')).toHaveText('Enter a title')
  await page.getByRole('button', { name: 'Enter a title' }).click()
  await expect(page.getByLabel('Title')).toBeFocused()

  await page.getByLabel('Title').fill('A Calm Panda')
  await expect(page.getByLabel('Slug')).toHaveValue('a-calm-panda')
  await page.getByLabel('Markdown').fill('# A Calm Panda\n\nA gentle story.\n')
  await page.getByRole('button', { name: 'Preview', exact: true }).first().click()
  await expect(page.getByRole('heading', { name: 'Reader result' })).toBeVisible()
  await expect(page.getByText('Canonical preview content.')).toBeVisible()
  await expect(page.getByText('18')).toBeVisible()
  await page.getByLabel('Markdown').fill('# A Calm Panda\n\nA changed story.\n')
  await expect(page.getByText('Preview out of date')).toBeVisible()
  expect(api.count('POST', '/api/v1/admin/preview')).toBe(2)
  expect(await seriousOrCriticalViolations(page)).toEqual([])
})

test('rejects hostile preview HTML before it reaches the Story Studio v-html sink', async ({
  page,
}) => {
  const api = new StudioAPI()
  api.previewRenderedHTML = [
    '<script src="https://story-xss.invalid/script.js"></script>',
    '<iframe srcdoc="<script>window.__storyXSS = true</script>"></iframe>',
    '<form><input name="secret"><button>Submit</button></form>',
    '<object data="https://story-xss.invalid/object"></object>',
    '<embed src="https://story-xss.invalid/embed">',
    '<p onclick="window.__storyXSS = true">Hostile preview</p>',
    '<a href="javascript:window.__storyXSS = true">Hostile link</a>',
  ].join('')
  const dialogs: string[] = []
  const unexpectedRequests: string[] = []
  page.on('dialog', async (dialog) => {
    dialogs.push(dialog.message())
    await dialog.dismiss()
  })
  page.on('request', (request) => {
    if (new URL(request.url()).hostname === 'story-xss.invalid') {
      unexpectedRequests.push(request.url())
    }
  })
  await api.install(page)
  await page.goto('/admin/stories/new')
  await page.getByLabel('Title').fill('Hostile Preview')
  await page.getByLabel('Markdown').fill('# Hostile Preview\n\nA story.\n')
  await page.getByRole('button', { name: 'Preview', exact: true }).first().click()

  await expect(page.getByText('Canonical preview content.')).toBeHidden()
  await expect(
    page.locator(
      '.preview-pane__story script, .preview-pane__story iframe, .preview-pane__story form, .preview-pane__story input, .preview-pane__story button, .preview-pane__story object, .preview-pane__story embed, .preview-pane__story [onload], .preview-pane__story [onerror], .preview-pane__story [onclick], .preview-pane__story a[href^="javascript:" i], .preview-pane__story a[href^="data:" i]',
    ),
  ).toHaveCount(0)
  expect(dialogs).toEqual([])
  expect(unexpectedRequests).toEqual([])
  expect(
    await page.evaluate(
      () => (window as Window & { __storyXSS?: boolean }).__storyXSS,
    ),
  ).toBeUndefined()
})

test('editing during a deferred preview prevents the stale response from replacing current input', async ({
  page,
}) => {
  const api = new StudioAPI()
  const gate = {
    started: deferred<void>(),
    release: deferred<void>(),
  }
  api.previewGate = gate
  await api.install(page)
  await page.goto('/admin/stories/new')
  await page.getByLabel('Title').fill('First title')
  await page.getByLabel('Markdown').fill('# First title\n\nFirst version.\n')
  await page.getByRole('button', { name: 'Preview', exact: true }).first().click()
  await gate.started.promise
  await page.getByLabel('Title').fill('Newer title')
  gate.release.resolve()
  await expect(page.getByText('Preparing preview…')).toBeHidden()
  await expect(page.getByText('Canonical preview content.')).toBeHidden()

  await page.getByRole('button', { name: 'Preview', exact: true }).first().click()
  await expect(page.getByText('Canonical preview content.')).toBeVisible()
  await expect(page.locator('.preview-pane__story h1')).toHaveText('Newer title')
})

test('saving creates an initial immutable draft without publishing or opening Reader', async ({
  page,
}) => {
  const api = new StudioAPI()
  await api.install(page)
  await page.goto('/admin/stories/new')
  await page.getByLabel('Title').fill('New Panda Story')
  await page.getByLabel('Markdown').fill('# New Panda Story\n\nA new beginning.\n')
  await page.getByRole('button', { name: 'Save Classic draft', exact: true }).first().click()

  await expect(page).toHaveURL(/\/admin\/stories\/new-panda-story\?saved=created_story&version=1&edition=classic$/)
  await expect(page.getByText('Story created with Classic draft version 1.')).toBeVisible()
  expect(api.count('POST', '/api/v1/admin/stories/draft')).toBe(1)
  expect(api.requests.some((request) => request.path.endsWith('/publish'))).toBe(false)
  expect(api.requests.some((request) => request.path.startsWith('/api/v1/reader/'))).toBe(false)
})

test('existing version opens read-only as a source and reports created versus reused outcomes', async ({
  page,
}) => {
  const api = new StudioAPI()
  api.draftOutcome = 'created_version'
  await api.install(page)
  await page.goto(`/admin/stories/panda-tale/edit?fromVersion=${versionTwo}`)
  await expect(page.getByRole('heading', { level: 1, name: 'Edit The Panda Tale' })).toBeVisible()
  await expect(page.getByLabel('Slug')).toHaveAttribute('readonly', '')
  await expect(page.getByText('Starting from Classic version 2.')).toBeVisible()
  await page.getByLabel('Markdown').fill('# The Panda Tale\n\nA genuinely new version.\n')
  await page.getByRole('button', { name: 'Save Classic draft', exact: true }).first().click()
  await expect(page.getByText('Classic draft version 3 created.')).toBeVisible()

  api.draftOutcome = 'reused'
  await page.goto(`/admin/stories/panda-tale/edit?fromVersion=${versionThree}`)
  await page.getByRole('button', { name: 'Save Classic draft', exact: true }).first().click()
  await expect(page.getByText('Existing healthy Classic version 3 reused.')).toBeVisible()
})

test('repair-required save conflict and repair summaries disable unsafe actions', async ({
  page,
}) => {
  const api = new StudioAPI()
  api.draftFailure = {
    status: 409,
    code: 'draft_repair_required',
    message: 'stored story version requires repair',
  }
  await api.install(page)
  await page.goto('/admin/stories/new')
  await page.getByLabel('Title').fill('Repair Candidate')
  await page.getByLabel('Markdown').fill('# Repair Candidate\n\nText.\n')
  await page.getByRole('button', { name: 'Save Classic draft', exact: true }).first().click()
  await expect(page.getByRole('alert').getByText('Needs attention')).toBeVisible()
  await expect(page.getByText('Unsaved changes')).toBeVisible()

  await page.getByRole('button', { name: 'Stories', exact: true }).click()
  const leave = page.getByRole('dialog', { name: 'Leave with unsaved changes?' })
  await leave.getByRole('button', { name: 'Discard changes and leave' }).click()
  await page.getByRole('heading', { name: 'The Tangled Story' }).locator('xpath=ancestor::article').getByRole('button', { name: 'Review story' }).click()
  await expect(page.getByRole('heading', { level: 2, name: 'Needs attention' })).toBeVisible()
  await expect(page.locator('.repair-banner')).toHaveCSS(
    'background-color',
    'rgb(255, 242, 216)',
  )
  await expect(page.getByRole('button', { name: 'Review release' })).toBeDisabled()
  await expect(page.getByText('This stored version cannot safely be reused or included in a release.')).toBeVisible()
  expect(await seriousOrCriticalViolations(page)).toEqual([])
})

test('release publication is deliberate, immutable and exposes Reader only after success', async ({
  page,
}) => {
  const api = new StudioAPI()
  await api.install(page)
  await page.goto('/admin/stories/panda-tale')
  await expect(page.locator('.detail-overview')).toBeVisible()
  await expect(page.locator('.edition-card')).toHaveCount(5)
  await expect(page.getByRole('heading', { name: 'Five-edition workspace' })).toBeVisible()
  await expect(page.locator('.version-row')).toHaveCount(2)
  await expect(page.getByLabel('Include Classic in release')).toBeChecked()
  await expect(page.getByLabel('Classic release version')).toHaveValue(versionTwo)
  expect(await seriousOrCriticalViolations(page)).toEqual([])

  await page.getByRole('button', { name: 'Review release' }).click()
  const dialog = page.getByRole('dialog', { name: 'Publish release?' })
  await expect(dialog.getByText(/Release 2 will replace the story's current live edition set atomically/)).toBeVisible()
  await expect(dialog.getByText('Classic')).toBeVisible()
  await expect(dialog.getByText('v2')).toBeVisible()
  expect(await seriousOrCriticalViolations(page)).toEqual([])

  await dialog.getByRole('button', { name: 'Publish release' }).click()
  await expect(page.getByText('Release 2 published with 1 edition.')).toBeVisible()
  await expect(page.getByRole('link', { name: 'Open published story' })).toBeVisible()
  expect(api.count('POST', '/api/v1/admin/stories/panda-tale/releases')).toBe(1)
  expect(api.count('POST', '/api/v1/admin/stories/panda-tale/publish')).toBe(0)
})

test('unpublish withdraws the current release while retaining release and version history', async ({
  page,
}) => {
  const api = new StudioAPI()
  await api.install(page)
  await page.goto('/admin/stories/panda-tale')
  await expect(page.locator('.version-row')).toHaveCount(2)
  const initialRows = await page.locator('.version-row').count()
  await page.getByRole('button', { name: 'Unpublish story' }).click()
  const dialog = page.getByRole('dialog', { name: 'Unpublish this story?' })
  await expect(dialog.getByText(/Drafts, immutable versions and historical reading progress remain/)).toBeVisible()
  await expect(dialog).toHaveCSS('background-color', 'rgb(255, 254, 250)')
  expect(await seriousOrCriticalViolations(page)).toEqual([])
  await dialog.getByRole('button', { name: 'Unpublish story' }).click()
  await expect(page.getByText(/Story unpublished/)).toBeVisible()
  await expect(page.getByRole('link', { name: 'Open published story' })).toBeHidden()
  await expect(page.getByText('Release history · 1')).toBeVisible()
  await expect(page.locator('.version-row__number').filter({ hasText: /^v2$/ })).toBeVisible()
  expect(await page.locator('.version-row').count()).toBe(initialRows)
  expect(api.count('POST', '/api/v1/admin/stories/panda-tale/unpublish')).toBe(1)
})

test('dirty navigation requires an accessible decision while clean navigation does not', async ({
  page,
}) => {
  const api = new StudioAPI()
  await api.install(page)
  await page.setViewportSize({ width: 844, height: 390 })
  await page.goto('/admin/stories/new')
  await page.getByLabel('Title').fill('Unsaved Panda')
  await page.getByRole('button', { name: 'Stories', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Leave with unsaved changes?' })
  await expect(dialog).toBeVisible()
  const dialogBox = await dialog.boundingBox()
  expect(dialogBox).not.toBeNull()
  expect(dialogBox!.y).toBeGreaterThanOrEqual(0)
  expect(dialogBox!.y + dialogBox!.height).toBeLessThanOrEqual(390)
  expect(await seriousOrCriticalViolations(page)).toEqual([])
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  await expect(page).toHaveURL(/\/admin\/stories\/new$/)

  await page.getByRole('button', { name: 'Stories', exact: true }).click()
  await dialog.getByRole('button', { name: 'Discard changes and leave' }).click()
  await expect(page).toHaveURL(/\/admin\/stories$/)

  await page.getByRole('button', { name: 'New story' }).first().click()
  await page.getByRole('button', { name: 'Stories', exact: true }).click()
  await expect(page).toHaveURL(/\/admin\/stories$/)
  await expect(dialog).toBeHidden()
})

test('401 goes to sign-in with a safe next while 403 and retryable failures stay truthful', async ({
  page,
}) => {
  const api = new StudioAPI()
  api.listFailure = { status: 403, code: 'forbidden', message: 'admin key required' }
  await api.install(page)
  await page.goto('/admin/stories')
  await expect(page.getByText('Administrator access is not available for this request.')).toBeVisible()
  await expect(page.getByText('admin key required')).toBeHidden()

  api.listFailure = { status: 500, code: 'list_failed', message: 'story catalogue unavailable' }
  await page.reload()
  await expect(page.getByRole('button', { name: 'Try again' })).toBeVisible()
  const errorState = page.locator('.studio-state[data-kind="error"]')
  await expect(errorState).toHaveCSS(
    'background-color',
    'rgb(251, 249, 243)',
  )
  await expect(errorState.locator('.studio-state__mark')).toHaveCSS(
    'background-color',
    'rgb(255, 240, 236)',
  )
  await expect(errorState.locator('.studio-state__mark')).toHaveCSS(
    'color',
    'rgb(123, 48, 40)',
  )
  expect(await seriousOrCriticalViolations(page)).toEqual([])
  await page.getByRole('button', { name: 'Try again' }).click()
  await expect(page.getByRole('heading', { name: 'The Panda Tale' })).toBeVisible()

  api.abortNextList = true
  await page.reload()
  await expect(page.getByRole('button', { name: 'Try again' })).toBeVisible()
  await page.getByRole('button', { name: 'Try again' }).click()
  await expect(page.getByRole('heading', { name: 'The Panda Tale' })).toBeVisible()

  api.listFailure = { status: 401, code: 'unauthorized', message: 'sign-in required' }
  await page.reload()
  await expect(page).toHaveURL(/\/account\/login\?next=\/admin\/stories$/)
})

test('stale detail response cannot replace a newer route', async ({ page }) => {
  const api = new StudioAPI()
  const gate = deferred<void>()
  api.detailGates.set('panda-tale', gate)
  await openCatalogue(page, api)
  await page.getByRole('heading', { name: 'The Panda Tale' }).locator('xpath=ancestor::article').getByRole('button', { name: 'Review story' }).click()
  await expect(page.getByText('Opening story')).toBeVisible()
  await page.getByRole('button', { name: 'Stories', exact: true }).click()
  await page.getByRole('heading', { name: 'The Tangled Story' }).locator('xpath=ancestor::article').getByRole('button', { name: 'Review story' }).click()
  await expect(page.getByRole('heading', { level: 1, name: 'The Tangled Story' })).toBeVisible()
  gate.resolve()
  await expect(page.getByRole('heading', { level: 1, name: 'The Tangled Story' })).toBeVisible()
  await expect(page.getByRole('heading', { level: 1, name: 'The Panda Tale' })).toBeHidden()
})

test('mobile and desktop editor layouts do not overflow', async ({ page }) => {
  const api = new StudioAPI()
  await api.install(page)
  for (const viewport of [
    { width: 320, height: 640 },
    { width: 390, height: 844 },
    { width: 844, height: 390 },
    { width: 768, height: 1024 },
    { width: 1024, height: 768 },
    { width: 1440, height: 900 },
  ]) {
    await page.setViewportSize(viewport)
    await page.goto('/admin/stories/new')
    await expectNoHorizontalOverflow(page)
    await expect(page.getByLabel('Markdown')).toBeVisible()
  }
  await page.setViewportSize({ width: 720, height: 450 })
  await page.goto('/admin/stories/new')
  await page.addStyleTag({
    content: 'html { font-size: 32px !important; }',
  })
  await expectNoHorizontalOverflow(page)
  await expect(page.getByRole('button', { name: 'Save Classic draft', exact: true }).last()).toBeVisible()
  expect(await seriousOrCriticalViolations(page)).toEqual([])
})

test('@webkit-library editor keyboard flow and confirmation dialog restore focus', async ({
  page,
}) => {
  const api = new StudioAPI()
  await api.install(page)
  await page.goto('/admin/stories/new')
  await page.getByLabel('Title').focus()
  await page.keyboard.type('Keyboard Panda')
  await page.keyboard.press('Tab')
  await expect(page.getByLabel('Author')).toBeFocused()
  await page.getByRole('button', { name: 'Stories', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Leave with unsaved changes?' })
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('button', { name: 'Stories', exact: true })).toBeFocused()
})

test('@webkit-library local file import is editable and never saves automatically', async ({
  page,
}) => {
  const api = new StudioAPI()
  await api.install(page)
  await page.goto('/admin/stories/new')
  const chooser = page.waitForEvent('filechooser')
  await page.getByRole('button', { name: 'Import file' }).click()
  const fileChooser = await chooser
  await fileChooser.setFiles({
    name: 'A Gentle Panda - Rowan.txt',
    mimeType: 'text/plain',
    buffer: Buffer.from(
      '*** START OF THE PROJECT GUTENBERG EBOOK SAMPLE ***\nCHAPTER I\nA quiet walk.\n*** END OF THE PROJECT GUTENBERG EBOOK SAMPLE ***',
    ),
  })
  await expect(page.getByLabel('Title')).toHaveValue('A Gentle Panda')
  await expect(page.getByLabel('Author')).toHaveValue('Rowan')
  await expect(page.getByLabel('Markdown')).toHaveValue(/## CHAPTER I/)
  await expect(page.getByText('Imported from A Gentle Panda - Rowan.txt')).toBeVisible()
  expect(api.count('POST', '/api/v1/admin/stories/draft')).toBe(0)
  expect(api.count('POST', '/api/v1/admin/preview')).toBe(0)
})
