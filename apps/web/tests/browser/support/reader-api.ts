import {
  expect,
  fixtureAccessToken,
  fixtureAccountID,
  fixtureProfileID,
  type BrowserProfile,
  test as base,
} from './auth'
import type { Page, Request, Route } from '@playwright/test'

export const READER_SLUG = 'test-only-moonlit-cafe'

export type ReaderEditionKeyFixture =
  | 'classic'
  | 'confident-readers'
  | 'growing-readers'
  | 'story-explorers'
  | 'little-listeners'

const readerEditionOrder: readonly ReaderEditionKeyFixture[] = [
  'classic',
  'confident-readers',
  'growing-readers',
  'story-explorers',
  'little-listeners',
]

export type ReaderSegmentFixture = {
  ordinal: number
  kind: 'heading' | 'paragraph' | 'other'
  headingLevel: number | null
  contentKey: string
  contentOccurrence: number
  chapterKey: string | null
  chapterOccurrence: number | null
  renderedHtml: string
  wordCount: number
}

export type ReaderStoryFixture = {
  slug: string
  title: string
  author: string | null
  language: string
  version: number
  segments: ReaderSegmentFixture[]
}

export type ReaderLocatorFixture = {
  schema: 2
  segment: {
    key: string
    occurrence: number
    ordinal: number
    offset: number
  }
  chapter?: {
    key: string
    occurrence: number
  }
}

export type ProgressFixture = {
  version: number
  locator: ReaderLocatorFixture
  percent: number
}

export type CapturedRequest = {
  method: string
  pathname: string
  search: string
  profileID: string | null
  body: unknown
}

export type MockResponse = {
  status?: number
  body?: unknown
  abort?: string
}

type ReaderLibraryItemFixture = {
  slug: string
  title: string
  author: string | null
}

export type ResponseGate = {
  started: Promise<CapturedRequest>
  fulfill: (body?: unknown, status?: number) => void
  abort: (errorCode?: string) => void
}

type InternalGate = {
  kind: 'gate'
  started: (request: CapturedRequest) => void
  result: Promise<MockResponse>
  publicGate: ResponseGate
}

type QueuedResponse = MockResponse | InternalGate

const chapterOneKey =
  '6f744b440fbf4fa52da46bebf4fd3e5f2de7a1c2fb11f7e9ac2794ccd1956c4e'
const chapterTwoKey =
  '3749b6630ab08c6998fd65117d5265c7e7514e35f02022a4005505d0aba52a73'

function escapeHtml(value: string): string {
  return value
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;')
}

function words(value: string): number {
  return value.trim().split(/\s+/u).length
}

function repeated(value: string, count = 18): string {
  return Array.from({ length: count }, () => value).join(' ')
}

export function makeReaderStory(
  overrides: Partial<Pick<ReaderStoryFixture, 'slug' | 'title' | 'author' | 'language' | 'version'>> = {},
): ReaderStoryFixture {
  const title = overrides.title ?? 'TEST ONLY — Moonlit Café'
  const opening = repeated(
    'Pöndá carried a lantern past the café window while the quiet harbour waited.',
  )
  const firstChapter = repeated(
    '“Ready?” asked Pöndá. The moon replied, “Oui — allons-y!” and the lantern glowed.',
  )
  const secondChapter = repeated(
    '星の光 shimmered over the quiet water while a sleepy panda watched. 🐼',
  )

  return {
    slug: overrides.slug ?? READER_SLUG,
    title,
    author: overrides.author ?? 'Panda Pages Test Fixture',
    language: overrides.language ?? 'en-GB',
    version: overrides.version ?? 1,
    segments: [
      {
        ordinal: 1,
        kind: 'heading',
        headingLevel: 1,
        contentKey:
          'd31878cf2371f991a595a486444819b429166c113ee33c598822396243a5c3bc',
        contentOccurrence: 1,
        chapterKey: null,
        chapterOccurrence: null,
        renderedHtml: `<h1>${escapeHtml(title)}</h1>`,
        wordCount: words(title),
      },
      {
        ordinal: 2,
        kind: 'paragraph',
        headingLevel: null,
        contentKey:
          '29b24293f72cc951a07c8b554caa723bb4bb1aced83257bb1c6325d0fc087798',
        contentOccurrence: 1,
        chapterKey: null,
        chapterOccurrence: null,
        renderedHtml: `<p>${opening}</p>`,
        wordCount: words(opening),
      },
      {
        ordinal: 3,
        kind: 'heading',
        headingLevel: 2,
        contentKey: chapterOneKey,
        contentOccurrence: 1,
        chapterKey: chapterOneKey,
        chapterOccurrence: 1,
        renderedHtml: '<h2>Chapter One — Lanterns</h2>',
        wordCount: 4,
      },
      {
        ordinal: 4,
        kind: 'paragraph',
        headingLevel: null,
        contentKey:
          'aae1f4bebb97b03ea9d0cfd5431675a250ab6a14be567445b1bb250874574e19',
        contentOccurrence: 1,
        chapterKey: chapterOneKey,
        chapterOccurrence: 1,
        renderedHtml: `<p>${firstChapter}</p>`,
        wordCount: words(firstChapter),
      },
      {
        ordinal: 5,
        kind: 'heading',
        headingLevel: 2,
        contentKey: chapterTwoKey,
        contentOccurrence: 1,
        chapterKey: chapterTwoKey,
        chapterOccurrence: 1,
        renderedHtml: '<h2>Chapter Two — 世界</h2>',
        wordCount: 4,
      },
      {
        ordinal: 6,
        kind: 'paragraph',
        headingLevel: null,
        contentKey:
          'fcbc17ea710ed18987f528decf4c035647b6721c8123e983c88a437aa5ac81db',
        contentOccurrence: 1,
        chapterKey: chapterTwoKey,
        chapterOccurrence: 1,
        renderedHtml: `<p>${secondChapter}</p>`,
        wordCount: words(secondChapter),
      },
    ],
  }
}

export function locatorFor(
  story: ReaderStoryFixture,
  ordinal: number,
  offset = 0.35,
): ReaderLocatorFixture {
  const segment = story.segments.find((candidate) => candidate.ordinal === ordinal)
  if (!segment) throw new Error(`missing Reader fixture segment ${ordinal}`)

  const locator: ReaderLocatorFixture = {
    schema: 2,
    segment: {
      key: segment.contentKey,
      occurrence: segment.contentOccurrence,
      ordinal: segment.ordinal,
      offset,
    },
  }
  if (segment.chapterKey !== null && segment.chapterOccurrence !== null) {
    locator.chapter = {
      key: segment.chapterKey,
      occurrence: segment.chapterOccurrence,
    }
  }
  return locator
}

export function progressFor(
  story: ReaderStoryFixture,
  ordinal = 5,
  offset = 0.35,
  percent = 0.72,
  version = story.version,
): ProgressFixture {
  return {
    version,
    locator: locatorFor(story, ordinal, offset),
    percent,
  }
}

function createGate(defaultBody: unknown): InternalGate {
  let resolveStarted: (request: CapturedRequest) => void = () => undefined
  let resolveResult: (response: MockResponse) => void = () => undefined
  let settled = false
  const started = new Promise<CapturedRequest>((resolve) => {
    resolveStarted = resolve
  })
  const result = new Promise<MockResponse>((resolve) => {
    resolveResult = resolve
  })
  const settle = (response: MockResponse) => {
    if (settled) return
    settled = true
    resolveResult(response)
  }

  const internal: InternalGate = {
    kind: 'gate',
    started: resolveStarted,
    result,
    publicGate: {
      started,
      fulfill: (body = defaultBody, status = 200) => settle({ status, body }),
      abort: (errorCode = 'failed') => settle({ abort: errorCode }),
    },
  }
  return internal
}

function queueFor(
  queues: Map<string, QueuedResponse[]>,
  slug: string,
): QueuedResponse[] {
  const existing = queues.get(slug)
  if (existing) return existing
  const created: QueuedResponse[] = []
  queues.set(slug, created)
  return created
}

function bodyOf(request: Request): unknown {
  const raw = request.postData()
  if (raw === null) return null
  try {
    return JSON.parse(raw) as unknown
  } catch {
    return raw
  }
}

function safeDecode(value: string): string {
  try {
    return decodeURIComponent(value)
  } catch {
    return value
  }
}

function jsonError(code: string, message: string) {
  return { error: { code, message } }
}

export class ReaderApiMock {
  readonly requests: CapturedRequest[] = []
  readonly legacyRequests: CapturedRequest[] = []
  readonly unhandledRequests: CapturedRequest[] = []
  readonly stories = new Map<string, ReaderStoryFixture>()
  readonly progress = new Map<string, ProgressFixture | null>()
  private readonly editionStories = new Map<string, Map<ReaderEditionKeyFixture, ReaderStoryFixture>>()
  private readonly eligibleReaderEditions = new Map<string, ReaderEditionKeyFixture[]>()
  private readonly readerEditionOverrides = new Map<string, ReaderEditionKeyFixture>()
  profiles: BrowserProfile[] = [{
    id: fixtureProfileID,
    name: 'Mina',
    pin_enabled: false,
    reading_level: 'classic',
  }]

  authSignedIn = true
  libraryItems: ReaderLibraryItemFixture[] = []

  private readonly storyResponses = new Map<string, QueuedResponse[]>()
  private readonly progressGetResponses = new Map<string, QueuedResponse[]>()
  private readonly progressPutResponses = new Map<string, QueuedResponse[]>()
  private readonly profileProgress = new Map<string, ProgressFixture | null>()
  private readonly page: Page

  constructor(page: Page) {
    this.page = page
    const story = makeReaderStory()
    this.setStory(story)
    this.libraryItems = [
      { slug: story.slug, title: story.title, author: story.author },
    ]
  }

  async install(): Promise<void> {
    await this.page.route('**/api/v1/**', async (route) => {
      await this.handle(route)
    })
  }

  setStory(story: ReaderStoryFixture): void {
    this.stories.set(story.slug, story)
    const editions = this.editionStories.get(story.slug) ?? new Map()
    editions.set('classic', story)
    this.editionStories.set(story.slug, editions)
    if (!this.eligibleReaderEditions.has(story.slug)) {
      this.eligibleReaderEditions.set(story.slug, ['classic'])
    }
    if (!this.progress.has(story.slug)) this.progress.set(story.slug, null)
    if (!this.profileProgress.has(this.progressKey(fixtureProfileID, story.slug))) {
      this.profileProgress.set(this.progressKey(fixtureProfileID, story.slug), null)
    }
  }

  setEditionStory(
    editionKey: ReaderEditionKeyFixture,
    story: ReaderStoryFixture,
  ): void {
    const editions = this.editionStories.get(story.slug) ?? new Map()
    editions.set(editionKey, story)
    this.editionStories.set(story.slug, editions)
    if (editionKey === 'classic') this.stories.set(story.slug, story)
    const eligible = new Set(this.eligibleReaderEditions.get(story.slug) ?? [])
    eligible.add(editionKey)
    this.eligibleReaderEditions.set(
      story.slug,
      readerEditionOrder.filter((key) => eligible.has(key)),
    )
    if (!this.progress.has(story.slug)) this.progress.set(story.slug, null)
    if (!this.profileProgress.has(this.progressKey(fixtureProfileID, story.slug))) {
      this.profileProgress.set(this.progressKey(fixtureProfileID, story.slug), null)
    }
  }

  setEligibleEditions(
    slug: string,
    editions: readonly ReaderEditionKeyFixture[],
  ): void {
    this.eligibleReaderEditions.set(
      slug,
      readerEditionOrder.filter((key) => editions.includes(key)),
    )
  }

  setReaderEditionOverride(
    slug: string,
    editionKey: ReaderEditionKeyFixture,
    profileID = fixtureProfileID,
  ): void {
    this.readerEditionOverrides.set(this.progressKey(profileID, slug), editionKey)
  }

  setProgress(
    slug: string,
    progress: ProgressFixture | null,
    profileID = fixtureProfileID,
  ): void {
    this.profileProgress.set(this.progressKey(profileID, slug), progress)
    if (profileID === fixtureProfileID) this.progress.set(slug, progress)
  }

  progressForProfile(
    slug: string,
    profileID: string,
  ): ProgressFixture | null {
    return this.profileProgress.get(this.progressKey(profileID, slug)) ?? null
  }

  enqueueStory(slug: string, response: MockResponse): void {
    queueFor(this.storyResponses, slug).push(response)
  }

  enqueueProgressGet(slug: string, response: MockResponse): void {
    queueFor(this.progressGetResponses, slug).push(response)
  }

  enqueueProgressPut(slug: string, response: MockResponse): void {
    queueFor(this.progressPutResponses, slug).push(response)
  }

  deferStory(slug: string): ResponseGate {
    const gate = createGate(this.readerResolution(slug, fixtureProfileID))
    queueFor(this.storyResponses, slug).push(gate)
    return gate.publicGate
  }

  deferProgressGet(slug: string): ResponseGate {
    const gate = createGate({ progress: this.progress.get(slug) ?? null })
    queueFor(this.progressGetResponses, slug).push(gate)
    return gate.publicGate
  }

  deferProgressPut(slug: string): ResponseGate {
    const gate = createGate({ ok: true })
    queueFor(this.progressPutResponses, slug).push(gate)
    return gate.publicGate
  }

  count(method: string, pathname: string): number {
    return this.requests.filter(
      (request) => request.method === method && request.pathname === pathname,
    ).length
  }

  progressPuts(slug = READER_SLUG): CapturedRequest[] {
    return this.requests.filter(
      (request) =>
        request.method === 'PUT' &&
        request.pathname === `/api/v1/progress/${encodeURIComponent(slug)}`,
    )
  }

  private readerResolution(slug: string, profileID: string) {
    const eligible = this.eligibleReaderEditions.get(slug) ?? []
    const editions = this.editionStories.get(slug)
    if (!editions || eligible.length === 0) {
      return jsonError('not_found', 'Story not found')
    }

    const override = this.readerEditionOverrides.get(this.progressKey(profileID, slug))
    const progress = this.progressForProfile(slug, profileID)
    let selected: ReaderEditionKeyFixture | null = null
    if (override && eligible.includes(override) && editions.has(override)) {
      selected = override
    }
    if (!selected && progress) {
      selected =
        eligible.find((key) => editions.get(key)?.version === progress.version) ?? null
    }
    if (!selected) selected = eligible[0] ?? null
    const story = editions.get(selected)
    if (!story) return jsonError('not_found', 'Story not found')
    return {
      state: 'selected',
      eligibleEditions: eligible,
      story: { ...story, editionKey: selected },
    }
  }

  private libraryReadModelItems(profileID: string) {
    return this.libraryItems.flatMap((item) => {
      const eligible = this.eligibleReaderEditions.get(item.slug) ?? []
      const editions = this.editionStories.get(item.slug)
      if (!editions || eligible.length === 0) return []

      const override = this.readerEditionOverrides.get(this.progressKey(profileID, item.slug))
      const progress = this.progressForProfile(item.slug, profileID)
      let selected: ReaderEditionKeyFixture | null = null
      if (override && eligible.includes(override) && editions.has(override)) selected = override
      if (!selected && progress) {
        selected = eligible.find((key) => editions.get(key)?.version === progress.version) ?? null
      }
      if (!selected) selected = eligible[0] ?? null

      const eligibleEditions = eligible.flatMap((editionKey) => {
        const story = editions.get(editionKey)
        if (!story) return []
        return [{
          editionKey,
          version: story.version,
          wordCount: story.segments.reduce((total, segment) => total + segment.wordCount, 0),
          chapterCount: story.segments.filter(
            (segment) => segment.kind === 'heading' && segment.headingLevel === 2,
          ).length,
        }]
      })
      if (eligibleEditions.length === 0) return []
      const identity = selected ? editions.get(selected) : editions.get(eligible[0] ?? 'classic')
      return [{
        ...item,
        language: identity?.language ?? 'en-GB',
        state: 'selected',
        eligibleEditions,
        selectedEdition: selected,
        progress: null,
      }]
    })
  }

  private take(
    queues: Map<string, QueuedResponse[]>,
    slug: string,
  ): QueuedResponse | undefined {
    return queues.get(slug)?.shift()
  }

  private progressKey(profileID: string, slug: string): string {
    return profileID + ':' + slug
  }

  private async respond(
    route: Route,
    captured: CapturedRequest,
    queued: QueuedResponse | undefined,
    fallbackBody: unknown,
  ): Promise<MockResponse> {
    let response: MockResponse
    let wasGated = false
    if (queued && 'kind' in queued && queued.kind === 'gate') {
      wasGated = true
      queued.started(captured)
      response = await queued.result
    } else {
      response = (queued as MockResponse | undefined) ?? {
        status: 200,
        body: fallbackBody,
      }
    }

    try {
      if (response.abort) {
        await route.abort(response.abort)
      } else {
        await route.fulfill({
          status: response.status ?? 200,
          contentType: 'application/json; charset=utf-8',
          headers: { 'Cache-Control': 'no-store' },
          body: JSON.stringify(response.body ?? null),
        })
      }
    } catch (error) {
      // An AbortController may cancel a deliberately held stale Reader request.
      if (!wasGated) throw error
    }
    return response
  }

  private async handle(route: Route): Promise<void> {
    const request = route.request()
    const url = new URL(request.url())
    const captured: CapturedRequest = {
      method: request.method(),
      pathname: url.pathname,
      search: url.search,
      profileID: request.headers()['x-pp-profile-id'] ?? null,
      body: bodyOf(request),
    }
    this.requests.push(captured)

	if (url.pathname.startsWith('/api/v1/')) {
		expect(request.headers().authorization).toBe(`Bearer ${fixtureAccessToken}`)
		expect(request.headers()['x-pp-account-id']).toBe(fixtureAccountID)
		expect(request.headers().cookie).toBeFalsy()
	}
	if (
		url.pathname.startsWith('/api/v1/progress/') ||
		url.pathname.startsWith('/api/v1/reader-resolution/') ||
		url.pathname.startsWith('/api/v1/reader-edition/') ||
		url.pathname === '/api/v1/continue' ||
		url.pathname === '/api/v1/library'
	) {
		const profileID = request.headers()['x-pp-profile-id']
		expect(profileID).toBeTruthy()
		expect(this.profiles.map((profile) => profile.id)).toContain(profileID)
	} else {
		expect(request.headers()['x-pp-profile-id']).toBeFalsy()
	}

    if (
      url.pathname === '/api/v1/story' ||
      url.pathname.startsWith('/api/v1/story/')
    ) {
      this.legacyRequests.push(captured)
      await this.respond(route, captured, undefined, jsonError('not_found', 'Not found'))
      return
    }

    if (request.method() === 'GET' && url.pathname === '/api/auth/me') {
      await this.respond(route, captured, undefined, { signedIn: this.authSignedIn })
      return
    }
    if (request.method() === 'POST' && url.pathname === '/api/auth/onboard') {
      await this.respond(route, captured, undefined, { ok: true })
      return
    }

    const resolutionPrefix = '/api/v1/reader-resolution/'
    if (request.method() === 'GET' && url.pathname.startsWith(resolutionPrefix)) {
      const slug = safeDecode(url.pathname.slice(resolutionPrefix.length))
      const profileID = request.headers()['x-pp-profile-id'] ?? ''
      const fallback = this.readerResolution(slug, profileID)
      const notFound =
        typeof fallback === 'object' &&
        fallback !== null &&
        'error' in fallback
      await this.respond(
        route,
        captured,
        this.take(this.storyResponses, slug) ??
          (notFound ? { status: 404, body: fallback } : undefined),
        fallback,
      )
      return
    }

    const editionPrefix = '/api/v1/reader-edition/'
    if (url.pathname.startsWith(editionPrefix)) {
      const slug = safeDecode(url.pathname.slice(editionPrefix.length))
      const profileID = request.headers()['x-pp-profile-id'] ?? ''
      if (request.method() === 'PUT') {
        const body = captured.body as { editionKey?: ReaderEditionKeyFixture } | null
        const editionKey = body?.editionKey
        const eligible = this.eligibleReaderEditions.get(slug) ?? []
        const exists =
          editionKey !== undefined &&
          eligible.includes(editionKey) &&
          this.editionStories.get(slug)?.has(editionKey)
        if (!exists || editionKey === undefined) {
          await this.respond(
            route,
            captured,
            { status: 404, body: jsonError('not_found', 'Story edition not found') },
            null,
          )
          return
        }
        this.readerEditionOverrides.set(this.progressKey(profileID, slug), editionKey)
        await this.respond(route, captured, undefined, { ok: true })
        return
      }
      if (request.method() === 'DELETE') {
        this.readerEditionOverrides.delete(this.progressKey(profileID, slug))
        await this.respond(route, captured, undefined, { ok: true })
        return
      }
    }

    const retiredReaderPrefix = '/api/v1/reader/'
    if (url.pathname.startsWith(retiredReaderPrefix)) {
      this.legacyRequests.push(captured)
      await this.respond(route, captured, { status: 404, body: jsonError('not_found', 'Not found') }, null)
      return
    }

    const progressPrefix = '/api/v1/progress/'
    if (url.pathname.startsWith(progressPrefix)) {
      const slug = safeDecode(url.pathname.slice(progressPrefix.length))
      const profileID = request.headers()['x-pp-profile-id'] ?? ''
      if (request.method() === 'GET') {
        await this.respond(
          route,
          captured,
          this.take(this.progressGetResponses, slug),
          { progress: this.progressForProfile(slug, profileID) },
        )
        return
      }
      if (request.method() === 'PUT') {
        const response = await this.respond(
          route,
          captured,
          this.take(this.progressPutResponses, slug),
          { ok: true },
        )
        const status = response.status ?? 200
        if (!response.abort && status >= 200 && status < 300) {
          const body = captured.body
          if (typeof body === 'object' && body !== null) {
            const candidate = body as Partial<ProgressFixture>
            if (
              typeof candidate.version === 'number' &&
              typeof candidate.percent === 'number' &&
              candidate.locator !== undefined
            ) {
              this.setProgress(slug, {
                version: candidate.version,
                locator: candidate.locator,
                percent: candidate.percent,
              }, profileID)
            }
          }
        }
        return
      }
    }

    if (request.method() === 'GET' && url.pathname === '/api/v1/library') {
      const profileID = captured.profileID ?? ''
      if (!profileID) {
        await this.respond(
          route,
          captured,
          { status: 400, body: jsonError('profile_required', 'Profile required') },
          null,
        )
        return
      }
      await this.respond(route, captured, undefined, {
        items: this.libraryReadModelItems(profileID),
      })
      return
    }
    if (request.method() === 'GET' && url.pathname === '/api/v1/profiles') {
      await this.respond(route, captured, undefined, {
        profiles: this.profiles.map((profile) => ({
          ...profile,
          reading_level: profile.reading_level ?? 'classic',
        })),
      })
      return
    }
    if (request.method() === 'GET' && url.pathname === '/api/v1/continue') {
      await this.respond(route, captured, undefined, { items: [] })
      return
    }
    this.unhandledRequests.push(captured)
    await this.respond(
      route,
      captured,
      { status: 501, body: jsonError('unhandled_test_route', 'Unhandled test route') },
      null,
    )
  }
}

export const test = base.extend<{ api: ReaderApiMock }>({
  api: async ({ page }, use) => {
    const api = new ReaderApiMock(page)
    await page.addInitScript((profileID) => {
      if (!window.localStorage.getItem('pandapages.selected-reader-profile-id')) {
        window.localStorage.setItem('pandapages.selected-reader-profile-id', profileID)
      }
    }, fixtureProfileID)
    await api.install()
    await use(api)
    expect(api.unhandledRequests, 'browser test left API requests unhandled').toEqual([])
    expect(api.legacyRequests, 'Reader requested a retired Reader endpoint').toEqual([])
  },
})

export { expect }
function fixtureKey(seed: number): string {
  return Math.max(0, Math.trunc(seed)).toString(16).padStart(64, '0')
}

export function makePagedReaderStory(
  overrides: Partial<Pick<ReaderStoryFixture, 'slug' | 'title' | 'author' | 'language' | 'version'>> = {},
): ReaderStoryFixture {
  const title = overrides.title ?? 'TEST ONLY — Paged Moonlight'
  const repeatedChapterKey = fixtureKey(900)
  const finalChapterKey = fixtureKey(901)
  const paragraph = repeated(
    'Pöndá reads a calm moonlit sentence beside the harbour. 🐼',
    6,
  )

  return {
    slug: overrides.slug ?? READER_SLUG,
    title,
    author: overrides.author ?? 'Panda Pages Test Fixture',
    language: overrides.language ?? 'en-GB',
    version: overrides.version ?? 1,
    segments: [
      {
        ordinal: 1,
        kind: 'heading',
        headingLevel: 1,
        contentKey: fixtureKey(1),
        contentOccurrence: 1,
        chapterKey: null,
        chapterOccurrence: null,
        renderedHtml: '<h1>' + escapeHtml(title) + '</h1>',
        wordCount: words(title),
      },
      {
        ordinal: 2,
        kind: 'paragraph',
        headingLevel: null,
        contentKey: fixtureKey(2),
        contentOccurrence: 1,
        chapterKey: null,
        chapterOccurrence: null,
        renderedHtml: '<p>' + paragraph + '</p>',
        wordCount: words(paragraph),
      },
      {
        ordinal: 3,
        kind: 'heading',
        headingLevel: 2,
        contentKey: repeatedChapterKey,
        contentOccurrence: 1,
        chapterKey: repeatedChapterKey,
        chapterOccurrence: 1,
        renderedHtml: '<h2>Moonlit Return</h2>',
        wordCount: 2,
      },
      {
        ordinal: 4,
        kind: 'paragraph',
        headingLevel: null,
        contentKey: fixtureKey(4),
        contentOccurrence: 1,
        chapterKey: repeatedChapterKey,
        chapterOccurrence: 1,
        renderedHtml: '<p>' + paragraph + ' First occurrence.</p>',
        wordCount: words(paragraph) + 2,
      },
      {
        ordinal: 5,
        kind: 'paragraph',
        headingLevel: null,
        contentKey: fixtureKey(5),
        contentOccurrence: 1,
        chapterKey: repeatedChapterKey,
        chapterOccurrence: 1,
        renderedHtml: '<p>' + paragraph + ' A link remains <a href="/library">keyboard accessible</a>.</p>',
        wordCount: words(paragraph) + 6,
      },
      {
        ordinal: 6,
        kind: 'heading',
        headingLevel: 2,
        contentKey: repeatedChapterKey,
        contentOccurrence: 2,
        chapterKey: repeatedChapterKey,
        chapterOccurrence: 2,
        renderedHtml: '<h2>Moonlit Return</h2>',
        wordCount: 2,
      },
      {
        ordinal: 7,
        kind: 'paragraph',
        headingLevel: null,
        contentKey: fixtureKey(7),
        contentOccurrence: 1,
        chapterKey: repeatedChapterKey,
        chapterOccurrence: 2,
        renderedHtml: '<p>' + paragraph + ' Second occurrence.</p>',
        wordCount: words(paragraph) + 2,
      },
      {
        ordinal: 8,
        kind: 'paragraph',
        headingLevel: null,
        contentKey: fixtureKey(8),
        contentOccurrence: 1,
        chapterKey: repeatedChapterKey,
        chapterOccurrence: 2,
        renderedHtml: '<p>' + paragraph + ' UTF-8 世界 and café.</p>',
        wordCount: words(paragraph) + 4,
      },
      {
        ordinal: 9,
        kind: 'heading',
        headingLevel: 2,
        contentKey: finalChapterKey,
        contentOccurrence: 1,
        chapterKey: finalChapterKey,
        chapterOccurrence: 1,
        renderedHtml: '<h2>Home Again</h2>',
        wordCount: 2,
      },
      {
        ordinal: 10,
        kind: 'paragraph',
        headingLevel: null,
        contentKey: fixtureKey(10),
        contentOccurrence: 1,
        chapterKey: finalChapterKey,
        chapterOccurrence: 1,
        renderedHtml: '<p>' + paragraph + ' The end.</p>',
        wordCount: words(paragraph) + 2,
      },
    ],
  }
}

export function makeLongUnbrokenReaderStory(
  overrides: Partial<Pick<ReaderStoryFixture, 'slug' | 'title' | 'author' | 'language' | 'version'>> = {},
): ReaderStoryFixture {
  const title = overrides.title ?? 'TEST ONLY — Long Unbroken Page'
  const ascii = 'PandaPagesReadingToken'.repeat(220)
  const cjk = '月夜熊猫物語'.repeat(360)

  return {
    slug: overrides.slug ?? READER_SLUG,
    title,
    author: overrides.author ?? 'Panda Pages Test Fixture',
    language: overrides.language ?? 'en-GB',
    version: overrides.version ?? 1,
    segments: [
      {
        ordinal: 1,
        kind: 'heading',
        headingLevel: 1,
        contentKey: fixtureKey(960),
        contentOccurrence: 1,
        chapterKey: null,
        chapterOccurrence: null,
        renderedHtml: '<h1>' + escapeHtml(title) + '</h1>',
        wordCount: words(title),
      },
      {
        ordinal: 2,
        kind: 'paragraph',
        headingLevel: null,
        contentKey: fixtureKey(961),
        contentOccurrence: 1,
        chapterKey: null,
        chapterOccurrence: null,
        renderedHtml: '<p>' + ascii + '</p>',
        wordCount: 1,
      },
      {
        ordinal: 3,
        kind: 'paragraph',
        headingLevel: null,
        contentKey: fixtureKey(962),
        contentOccurrence: 1,
        chapterKey: null,
        chapterOccurrence: null,
        renderedHtml: '<p>' + cjk + '</p>',
        wordCount: 1,
      },
      {
        ordinal: 4,
        kind: 'paragraph',
        headingLevel: null,
        contentKey: fixtureKey(963),
        contentOccurrence: 1,
        chapterKey: null,
        chapterOccurrence: null,
        renderedHtml: '<p>Every pathological segment remains present exactly once.</p>',
        wordCount: 8,
      },
    ],
  }
}

export function makeMeasuredOverflowReaderStory(
  overrides: Partial<Pick<ReaderStoryFixture, 'slug' | 'title' | 'author' | 'language' | 'version'>> = {},
): ReaderStoryFixture {
  const title = overrides.title ?? 'TEST ONLY — Measured Overflow'
  const sparseCode = Array.from({ length: 90 }, (_, index) =>
    index % 15 === 0 ? 'panda' : '',
  ).join('\n')

  return {
    slug: overrides.slug ?? READER_SLUG,
    title,
    author: overrides.author ?? 'Panda Pages Test Fixture',
    language: overrides.language ?? 'en-GB',
    version: overrides.version ?? 1,
    segments: [
      {
        ordinal: 1,
        kind: 'heading',
        headingLevel: 1,
        contentKey: fixtureKey(970),
        contentOccurrence: 1,
        chapterKey: null,
        chapterOccurrence: null,
        renderedHtml: '<h1>' + escapeHtml(title) + '</h1>',
        wordCount: words(title),
      },
      {
        ordinal: 2,
        kind: 'other',
        headingLevel: null,
        contentKey: fixtureKey(971),
        contentOccurrence: 1,
        chapterKey: null,
        chapterOccurrence: null,
        renderedHtml: '<pre><code>' + sparseCode + '</code></pre>',
        wordCount: words(sparseCode),
      },
      {
        ordinal: 3,
        kind: 'paragraph',
        headingLevel: null,
        contentKey: fixtureKey(972),
        contentOccurrence: 1,
        chapterKey: null,
        chapterOccurrence: null,
        renderedHtml: '<p>Measured correction keeps this following block separate.</p>',
        wordCount: 8,
      },
    ],
  }
}

export function makeOversizedReaderStory(
  overrides: Partial<Pick<ReaderStoryFixture, 'slug' | 'title' | 'author' | 'language' | 'version'>> = {},
): ReaderStoryFixture {
  const title = overrides.title ?? 'TEST ONLY — Oversized Page'
  const chapterKey = fixtureKey(950)
  const longParagraph = repeated(
    'A very long moonlit paragraph remains readable without clipping or splitting.',
    140,
  )
  const ending = repeated('The harbour settles after the long reading passage.', 4)

  return {
    slug: overrides.slug ?? READER_SLUG,
    title,
    author: overrides.author ?? 'Panda Pages Test Fixture',
    language: overrides.language ?? 'en-GB',
    version: overrides.version ?? 1,
    segments: [
      {
        ordinal: 1,
        kind: 'heading',
        headingLevel: 1,
        contentKey: fixtureKey(951),
        contentOccurrence: 1,
        chapterKey: null,
        chapterOccurrence: null,
        renderedHtml: '<h1>' + escapeHtml(title) + '</h1>',
        wordCount: words(title),
      },
      {
        ordinal: 2,
        kind: 'paragraph',
        headingLevel: null,
        contentKey: fixtureKey(952),
        contentOccurrence: 1,
        chapterKey: null,
        chapterOccurrence: null,
        renderedHtml: '<p>' + longParagraph + '</p>',
        wordCount: words(longParagraph),
      },
      {
        ordinal: 3,
        kind: 'heading',
        headingLevel: 2,
        contentKey: chapterKey,
        contentOccurrence: 1,
        chapterKey,
        chapterOccurrence: 1,
        renderedHtml: '<h2>After the Long Page</h2>',
        wordCount: 4,
      },
      {
        ordinal: 4,
        kind: 'paragraph',
        headingLevel: null,
        contentKey: fixtureKey(954),
        contentOccurrence: 1,
        chapterKey,
        chapterOccurrence: 1,
        renderedHtml: '<p>' + ending + '</p>',
        wordCount: words(ending),
      },
    ],
  }
}
