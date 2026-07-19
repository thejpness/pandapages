import {
  expect,
  test as base,
  type Page,
  type Request,
  type Route,
} from '@playwright/test'

export const READER_SLUG = 'test-only-moonlit-cafe'

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
  body: unknown
}

export type MockResponse = {
  status?: number
  body?: unknown
  abort?: string
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

  authUnlocked = true
  libraryItems: Array<{ slug: string; title: string; author: string | null }> = []

  private readonly storyResponses = new Map<string, QueuedResponse[]>()
  private readonly progressGetResponses = new Map<string, QueuedResponse[]>()
  private readonly progressPutResponses = new Map<string, QueuedResponse[]>()
  private readonly page: Page

  constructor(page: Page) {
    this.page = page
    const story = makeReaderStory()
    this.stories.set(story.slug, story)
    this.progress.set(story.slug, null)
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
    if (!this.progress.has(story.slug)) this.progress.set(story.slug, null)
  }

  setProgress(slug: string, progress: ProgressFixture | null): void {
    this.progress.set(slug, progress)
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
    const gate = createGate(this.stories.get(slug))
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

  private take(
    queues: Map<string, QueuedResponse[]>,
    slug: string,
  ): QueuedResponse | undefined {
    return queues.get(slug)?.shift()
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
      body: bodyOf(request),
    }
    this.requests.push(captured)

    if (
      url.pathname === '/api/v1/story' ||
      url.pathname.startsWith('/api/v1/story/')
    ) {
      this.legacyRequests.push(captured)
      await this.respond(route, captured, undefined, jsonError('not_found', 'Not found'))
      return
    }

    if (request.method() === 'GET' && url.pathname === '/api/v1/auth/status') {
      await this.respond(route, captured, undefined, { unlocked: this.authUnlocked })
      return
    }
    if (request.method() === 'POST' && url.pathname === '/api/v1/auth/unlock') {
      await this.respond(route, captured, undefined, { ok: true })
      return
    }

    const readerPrefix = '/api/v1/reader/'
    if (request.method() === 'GET' && url.pathname.startsWith(readerPrefix)) {
      const slug = safeDecode(url.pathname.slice(readerPrefix.length))
      const story = this.stories.get(slug)
      const fallback = story ?? jsonError('not_found', 'Story not found')
      const queued = this.take(this.storyResponses, slug)
      await this.respond(
        route,
        captured,
        queued ?? (story ? undefined : { status: 404, body: fallback }),
        fallback,
      )
      return
    }

    const progressPrefix = '/api/v1/progress/'
    if (url.pathname.startsWith(progressPrefix)) {
      const slug = safeDecode(url.pathname.slice(progressPrefix.length))
      if (request.method() === 'GET') {
        await this.respond(
          route,
          captured,
          this.take(this.progressGetResponses, slug),
          { progress: this.progress.get(slug) ?? null },
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
              this.progress.set(slug, {
                version: candidate.version,
                locator: candidate.locator,
                percent: candidate.percent,
              })
            }
          }
        }
        return
      }
    }

    if (request.method() === 'GET' && url.pathname === '/api/v1/library') {
      await this.respond(route, captured, undefined, { items: this.libraryItems })
      return
    }
    if (request.method() === 'GET' && url.pathname === '/api/v1/continue') {
      await this.respond(route, captured, undefined, { items: [] })
      return
    }
    if (request.method() === 'GET' && url.pathname === '/api/v1/settings') {
      await this.respond(route, captured, undefined, {
        child: {
          name: 'TEST ONLY — Reader child',
          ageMonths: 84,
          interests: [],
          sensitivities: [],
        },
        prompt: {
          name: 'TEST ONLY — Reader prompt',
          schemaVersion: 1,
          rules: {},
        },
      })
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
    await api.install()
    await use(api)
    expect(api.unhandledRequests, 'browser test left API requests unhandled').toEqual([])
    expect(api.legacyRequests, 'Reader requested a removed Reader 1 endpoint').toEqual([])
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
