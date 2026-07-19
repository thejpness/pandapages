import { AxeBuilder } from '@axe-core/playwright'
import {
  expect,
  test as base,
  type Page,
  type Route,
} from '@playwright/test'

type LibraryProgressFixture = {
  version: number
  percent: number
  updatedAt: string
  isCurrentVersion: boolean
}

type LibraryStoryFixture = {
  slug: string
  title: string
  author: string | null
  language: string
  publishedVersion: number
  wordCount: number
  chapterCount: number
  progress: LibraryProgressFixture | null
}

type LibraryResponseItemFixture =
  | LibraryStoryFixture
  | Omit<LibraryStoryFixture, 'progress'>

type MockResponse = {
  status?: number
  body: unknown
}

type CapturedRequest = {
  method: string
  pathname: string
}

type ResponseGate = {
  started: Promise<void>
  fulfill: (body?: unknown, status?: number) => void
}

type InternalGate = {
  kind: 'gate'
  signalStarted: () => void
  result: Promise<MockResponse>
  publicGate: ResponseGate
}

type QueuedResponse = MockResponse | InternalGate

const SORT_STORAGE_KEY = 'pp_library_sort_v1'

const CURRENT_STORY: LibraryStoryFixture = {
  slug: 'moonlit-cafe',
  title: 'Moonlit Café',
  author: 'Mara Bell',
  language: 'en-GB',
  publishedVersion: 2,
  wordCount: 1_200,
  chapterCount: 4,
  progress: {
    version: 2,
    percent: 0.42,
    updatedAt: '2026-07-19T12:00:00Z',
    isCurrentVersion: true,
  },
}

const COMPLETED_STORY: LibraryStoryFixture = {
  slug: 'amber-woods',
  title: 'Amber Woods',
  author: 'Traditional',
  language: 'en',
  publishedVersion: 1,
  wordCount: 2_400,
  chapterCount: 6,
  progress: {
    version: 1,
    percent: 0.99,
    updatedAt: '2026-07-19T11:00:00Z',
    isCurrentVersion: true,
  },
}

const UPDATED_STORY: LibraryStoryFixture = {
  slug: 'brave-bamboo',
  title: 'Brave Bamboo',
  author: 'Jun Park',
  language: 'en',
  publishedVersion: 3,
  wordCount: 800,
  chapterCount: 3,
  progress: {
    version: 2,
    percent: 0.61,
    updatedAt: '2026-07-19T13:00:00Z',
    isCurrentVersion: false,
  },
}

const LONG_UNAUTHORED_STORY: LibraryStoryFixture = {
  slug: 'zebra-bamboo-moon',
  title:
    'Zebra and the Astonishingly Long Night-Time Journey Across the Bamboo Moon',
  author: null,
  language: 'en',
  publishedVersion: 1,
  wordCount: 400,
  chapterCount: 0,
  progress: null,
}

const UNAVAILABLE_PROGRESS_STORY: Omit<LibraryStoryFixture, 'progress'> = {
  slug: 'paper-stars',
  title: 'Paper Stars',
  author: 'Nia Rowan',
  language: 'en',
  publishedVersion: 1,
  wordCount: 650,
  chapterCount: 2,
}

const READY_STORIES: LibraryStoryFixture[] = [
  CURRENT_STORY,
  COMPLETED_STORY,
  UPDATED_STORY,
  LONG_UNAUTHORED_STORY,
]

function createGate(defaultBody: unknown): InternalGate {
  let signalStarted: () => void = () => undefined
  let settle!: (response: MockResponse) => void
  let settled = false

  const started = new Promise<void>((resolve) => {
    signalStarted = resolve
  })
  const result = new Promise<MockResponse>((resolve) => {
    settle = (response) => {
      if (settled) return
      settled = true
      resolve(response)
    }
  })

  const internal: InternalGate = {
    kind: 'gate',
    signalStarted,
    result,
    publicGate: {
      started,
      fulfill: (body = defaultBody, status = 200) => {
        settle({ body, status })
      },
    },
  }
  return internal
}

async function fulfillJson(
  route: Route,
  response: MockResponse,
): Promise<void> {
  await route.fulfill({
    status: response.status ?? 200,
    contentType: 'application/json; charset=utf-8',
    headers: { 'Cache-Control': 'no-store' },
    body: JSON.stringify(response.body),
  })
}

class LibraryApiMock {
  readonly requests: CapturedRequest[] = []
  readonly unhandledRequests: CapturedRequest[] = []

  items: LibraryResponseItemFixture[] = READY_STORIES.map((story) => ({
    ...story,
  }))
  unavailableItemCount = 0
  authUnlocked = true

  private readonly authResponses: QueuedResponse[] = []
  private readonly libraryResponses: QueuedResponse[] = []
  private readonly logoutResponses: QueuedResponse[] = []
  private readonly page: Page

  constructor(page: Page) {
    this.page = page
  }

  async install(): Promise<void> {
    await this.page.route('**/api/v1/**', async (route) => {
      await this.handle(route)
    })
  }

  enqueueLibrary(response: MockResponse): void {
    this.libraryResponses.push(response)
  }

  enqueueLogout(response: MockResponse): void {
    this.logoutResponses.push(response)
  }

  deferAuthStatus(): ResponseGate {
    const gate = createGate({ unlocked: true })
    this.authResponses.push(gate)
    return gate.publicGate
  }

  deferLibrary(): ResponseGate {
    const gate = createGate({
      items: this.items,
      unavailableItemCount: this.unavailableItemCount,
    })
    this.libraryResponses.push(gate)
    return gate.publicGate
  }

  deferLogout(): ResponseGate {
    const gate = createGate({ ok: true })
    this.logoutResponses.push(gate)
    return gate.publicGate
  }

  count(method: string, pathname: string): number {
    return this.requests.filter(
      (request) =>
        request.method === method && request.pathname === pathname,
    ).length
  }

  private async resolveResponse(
    queued: QueuedResponse | undefined,
    fallback: MockResponse,
  ): Promise<MockResponse> {
    if (queued === undefined) return fallback
    if ('kind' in queued && queued.kind === 'gate') {
      queued.signalStarted()
      return queued.result
    }
    return queued as MockResponse
  }

  private async handle(route: Route): Promise<void> {
    const request = route.request()
    const url = new URL(request.url())
    const captured = {
      method: request.method(),
      pathname: url.pathname,
    }
    this.requests.push(captured)

    if (
      request.method() === 'GET' &&
      url.pathname === '/api/v1/auth/status'
    ) {
      const response = await this.resolveResponse(
        this.authResponses.shift(),
        { body: { unlocked: this.authUnlocked } },
      )
      await fulfillJson(route, response)
      return
    }

    if (
      request.method() === 'POST' &&
      url.pathname === '/api/v1/auth/unlock'
    ) {
      this.authUnlocked = true
      await fulfillJson(route, { body: { ok: true } })
      return
    }

    if (
      request.method() === 'POST' &&
      url.pathname === '/api/v1/auth/logout'
    ) {
      const response = await this.resolveResponse(
        this.logoutResponses.shift(),
        { body: { ok: true } },
      )
      const status = response.status ?? 200
      if (status >= 200 && status < 300) this.authUnlocked = false
      await fulfillJson(route, response)
      return
    }

    if (
      request.method() === 'GET' &&
      url.pathname === '/api/v1/library'
    ) {
      const response = await this.resolveResponse(
        this.libraryResponses.shift(),
        {
          body: {
            items: this.items,
            unavailableItemCount: this.unavailableItemCount,
          },
        },
      )
      if ((response.status ?? 200) === 401) this.authUnlocked = false
      await fulfillJson(route, response)
      return
    }

    if (
      request.method() === 'GET' &&
      url.pathname === '/api/v1/continue'
    ) {
      await fulfillJson(route, { body: { items: [] } })
      return
    }

    if (
      request.method() === 'GET' &&
      url.pathname === '/api/v1/settings'
    ) {
      await fulfillJson(route, {
        body: {
          child: {
            name: 'Ted',
            ageMonths: 84,
            interests: [],
            sensitivities: [],
          },
          prompt: {
            name: 'Default',
            schemaVersion: 1,
            rules: {},
          },
        },
      })
      return
    }

    if (
      request.method() === 'GET' &&
      url.pathname.startsWith('/api/v1/reader/')
    ) {
      await fulfillJson(route, {
        status: 404,
        body: {
          error: { code: 'not_found', message: 'Test story not mounted' },
        },
      })
      return
    }

    if (
      request.method() === 'GET' &&
      url.pathname.startsWith('/api/v1/progress/')
    ) {
      await fulfillJson(route, { body: { progress: null } })
      return
    }

    if (
      request.method() === 'GET' &&
      url.pathname === '/api/v1/admin/stories'
    ) {
      await fulfillJson(route, { body: { items: [] } })
      return
    }

    this.unhandledRequests.push(captured)
    await fulfillJson(route, {
      status: 501,
      body: {
        error: {
          code: 'unhandled_test_route',
          message: 'Unhandled Library test route',
        },
      },
    })
  }
}

const test = base.extend<{ api: LibraryApiMock }>({
  api: [
    async ({ page }, use) => {
      const api = new LibraryApiMock(page)
      await api.install()
      await use(api)
      expect(
        api.unhandledRequests,
        'Library browser test left API requests unhandled',
      ).toEqual([])
    },
    { auto: true },
  ],
})

function storyCard(page: Page, title: string) {
  return page
    .locator('.bookshelf-card')
    .filter({ has: page.locator('.bookshelf-card__title', { hasText: title }) })
}

async function gotoReadyLibrary(page: Page): Promise<void> {
  await page.goto('/library')
  await expect(
    page.getByRole('heading', { name: 'Choose tonight’s story' }),
  ).toBeVisible()
  await expect(page.locator('.bookshelf-card')).toHaveCount(READY_STORIES.length)
}

async function expectPath(
  page: Page,
  pathname: string,
  next: string | null = null,
): Promise<void> {
  await expect
    .poll(() => {
      const url = new URL(page.url())
      return {
        pathname: url.pathname,
        next: url.searchParams.get('next'),
      }
    })
    .toEqual({ pathname, next })
}

async function expectQuery(page: Page, query: string | null): Promise<void> {
  await expect
    .poll(() => new URL(page.url()).searchParams.get('q'))
    .toBe(query)
}

async function expectNoHorizontalOverflow(page: Page): Promise<void> {
  await expect
    .poll(() =>
      page.evaluate(
        () =>
          document.documentElement.scrollWidth <=
          document.documentElement.clientWidth,
      ),
    )
    .toBe(true)
}

function boxesOverlap(
  first: { x: number; y: number; width: number; height: number },
  second: { x: number; y: number; width: number; height: number },
): boolean {
  return !(
    first.x + first.width <= second.x ||
    second.x + second.width <= first.x ||
    first.y + first.height <= second.y ||
    second.y + second.height <= first.y
  )
}

async function expectNoSeriousOrCriticalViolations(
  page: Page,
): Promise<void> {
  await page.evaluate(async () => {
    await document.fonts.ready
  })
  const results = await new AxeBuilder({ page }).analyze()
  const violations = results.violations
    .filter(
      (violation) =>
        violation.impact === 'serious' || violation.impact === 'critical',
    )
    .map((violation) => ({
      id: violation.id,
      impact: violation.impact,
      help: violation.help,
      nodes: violation.nodes.map((node) => ({
        target: node.target,
        summary: node.failureSummary,
      })),
    }))
  expect(violations).toEqual([])
}

test.describe('Library 2 bookshelf', () => {
  test('renders one coherent read model with current, completed, updated, and unread semantics', async ({
    page,
    api,
  }) => {
    await gotoReadyLibrary(page)

    await expect(page.getByRole('banner')).toHaveCount(1)
    await expect(
      page.getByRole('link', { name: 'Panda Pages home' }),
    ).toHaveAttribute('href', '/')
    await expect(
      page.getByRole('searchbox', { name: 'Search the library' }),
    ).toBeVisible()
    await expect(page.getByLabel('Sort')).toHaveValue('recent')
    await expect(page.getByText('4 stories', { exact: true })).toBeVisible()

    const hero = page.locator('.continue-card')
    await expect(hero).toHaveAttribute(
      'aria-label',
      'Continue at 42%: Moonlit Café',
    )
    await expect(hero).toContainText('by Mara Bell')
    await expect(hero).toContainText('42% read')
    await expect(
      hero.getByRole('progressbar', {
        name: `Reading progress for ${CURRENT_STORY.title}`,
      }),
    ).toHaveAttribute('aria-valuenow', '42')

    const current = storyCard(page, CURRENT_STORY.title)
    await expect(current).toContainText('1,200 words')
    await expect(current).toContainText('4 chapters')
    await expect(
      current.getByRole('link', {
        name: `Continue at 42%: ${CURRENT_STORY.title}`,
        exact: true,
      }),
    ).toBeVisible()

    const completed = storyCard(page, COMPLETED_STORY.title)
    await expect(completed).toContainText('Finished')
    await expect(
      completed.getByRole('link', {
        name: `Read again: ${COMPLETED_STORY.title}`,
        exact: true,
      }),
    ).toBeVisible()

    const updated = storyCard(page, UPDATED_STORY.title)
    await expect(updated).toContainText('Story updated since you last read')
    await expect(
      updated.getByRole('link', {
        name: `Open updated story: ${UPDATED_STORY.title}`,
        exact: true,
      }),
    ).toBeVisible()
    await expect(updated.getByRole('progressbar')).toHaveCount(0)

    const unread = storyCard(page, LONG_UNAUTHORED_STORY.title)
    await expect(unread).toContainText('Not started')
    await expect(unread.locator('.bookshelf-card__author')).toHaveCount(0)
    await expect(unread).toContainText('No chapter breaks')

    const mainText = await page.getByRole('main').innerText()
    expect(mainText).not.toContain(CURRENT_STORY.slug)
    expect(mainText).not.toContain(UPDATED_STORY.slug)

    expect(api.count('GET', '/api/v1/library')).toBe(1)
    expect(api.count('GET', '/api/v1/continue')).toBe(0)
    expect(api.count('GET', '/api/v1/settings')).toBe(0)
  })

  test('shows a truthful initial loading state before the single response completes', async ({
    page,
    api,
  }) => {
    const gate = api.deferLibrary()
    const navigation = page.goto('/library')
    await gate.started

    await expect(
      page.getByRole('status', { name: 'Loading library' }),
    ).toBeVisible()
    await expect(page.locator('.bookshelf-card')).toHaveCount(0)

    gate.fulfill()
    await navigation
    await expect(
      page.getByRole('heading', { name: 'Choose tonight’s story' }),
    ).toBeVisible()
    expect(api.count('GET', '/api/v1/library')).toBe(1)
  })

  test('searches title, author, and hidden slug while synchronising and clearing the URL', async ({
    page,
  }) => {
    await page.goto('/library?q=Moonlit')
    const search = page.getByRole('searchbox', { name: 'Search the library' })
    await expect(search).toHaveValue('Moonlit')
    await expect(page.locator('.bookshelf-card')).toHaveCount(1)
    await expect(storyCard(page, CURRENT_STORY.title)).toBeVisible()
    await expect(page.locator('.continue-card')).toHaveCount(0)

    await search.fill('Mara')
    await expectQuery(page, 'Mara')
    await expect(page.locator('.bookshelf-card')).toHaveCount(1)
    await expect(storyCard(page, CURRENT_STORY.title)).toBeVisible()

    await search.fill(UPDATED_STORY.slug)
    await expectQuery(page, UPDATED_STORY.slug)
    await expect(page.locator('.bookshelf-card')).toHaveCount(1)
    const slugMatch = storyCard(page, UPDATED_STORY.title)
    await expect(slugMatch).toBeVisible()
    expect(await slugMatch.innerText()).not.toContain(UPDATED_STORY.slug)

    await search.fill('no such story')
    await expectQuery(page, 'no such story')
    await expect(
      page.getByRole('heading', {
        name: 'Nothing found for “no such story”',
      }),
    ).toBeVisible()
    await expect(page.locator('.surprise-button')).toBeDisabled()

    await page
      .getByRole('main')
      .getByRole('button', { name: 'Clear search' })
      .click()
    await expectQuery(page, null)
    await expect(search).toHaveValue('')
    await expect(page.locator('.bookshelf-card')).toHaveCount(READY_STORIES.length)
  })

  test('a newer same-route history query cancels pending search ownership', async ({
    page,
  }) => {
    await page.clock.install()
    await page.goto('/library?q=Mara')
    const search = page.getByRole('searchbox', { name: 'Search the library' })
    await expect(search).toHaveValue('Mara')

    await page.evaluate(async () => {
      type AppRouter = {
        push: (location: {
          path: string
          query: Record<string, string>
        }) => Promise<unknown>
      }
      type VueAppHost = HTMLElement & {
        __vue_app__?: {
          config: { globalProperties: { $router: AppRouter } }
        }
      }

      const app = document.querySelector<VueAppHost>('#app')?.__vue_app__
      if (!app) throw new Error('Vue application was not mounted')
      await app.config.globalProperties.$router.push({
        path: '/library',
        query: { q: 'Moonlit' },
      })
    })
    await expectQuery(page, 'Moonlit')
    await expect(search).toHaveValue('Moonlit')

    await search.fill('stale search')
    await page.goBack()
    await expectQuery(page, 'Mara')
    await expect(search).toHaveValue('Mara')

    await page.clock.fastForward(220)
    await expectQuery(page, 'Mara')
    await expect(search).toHaveValue('Mara')
    await expect(storyCard(page, CURRENT_STORY.title)).toBeVisible()
  })

  test('a pending search query cannot supersede delayed protected navigation', async ({
    page,
    api,
  }) => {
    await page.clock.install()
    await gotoReadyLibrary(page)
    await page.clock.fastForward(6_000)
    const auth = api.deferAuthStatus()

    await page
      .getByRole('searchbox', { name: 'Search the library' })
      .fill('Moon')
    await storyCard(page, CURRENT_STORY.title)
      .getByRole('link', {
        name: `Continue at 42%: ${CURRENT_STORY.title}`,
      })
      .click()
    await auth.started

    await page.clock.fastForward(220)
    await expectPath(page, '/library')
    expect(new URL(page.url()).searchParams.get('q')).toBeNull()

    auth.fulfill()
    await expectPath(page, `/read/${CURRENT_STORY.slug}`)
  })

  test('pending search ownership is released for Surprise navigation', async ({
    page,
    api,
  }) => {
    await page.clock.install()
    await gotoReadyLibrary(page)
    await page.clock.fastForward(6_000)
    const auth = api.deferAuthStatus()
    await page
      .getByRole('searchbox', { name: 'Search the library' })
      .fill('Moon')
    await page.evaluate(() => {
      Math.random = () => 0
    })
    await page.locator('.surprise-button').click()
    await auth.started
    await page.clock.fastForward(220)
    await expectPath(page, '/library')
    expect(new URL(page.url()).searchParams.get('q')).toBeNull()
    auth.fulfill()
    await expectPath(page, `/read/${CURRENT_STORY.slug}`)
  })

  for (const destination of [
    { action: 'Reading profile', path: '/journey' },
    { action: 'Admin', path: '/admin/upload' },
  ]) {
    test(`pending search ownership is released for ${destination.action} navigation`, async ({
      page,
      api,
    }) => {
      await page.clock.install()
      await gotoReadyLibrary(page)
      await page.clock.fastForward(6_000)
      const auth = api.deferAuthStatus()
      await page
        .getByRole('searchbox', { name: 'Search the library' })
        .fill('Moon')
      await page.getByRole('button', { name: 'Parent options' }).click()
      const action = page
        .getByRole('dialog', { name: 'Parent options' })
        .getByRole('button', { name: destination.action })
      await action.focus()
      await action.press('Enter')
      await auth.started
      await page.clock.fastForward(220)
      await expectPath(page, '/library')
      expect(new URL(page.url()).searchParams.get('q')).toBeNull()
      auth.fulfill()
      await expectPath(page, destination.path)
    })
  }

  test('pending search ownership is released for a public route link', async ({
    page,
  }) => {
    await page.clock.install()
    await gotoReadyLibrary(page)
    await page
      .getByRole('searchbox', { name: 'Search the library' })
      .fill('Moon')
    await page.getByRole('link', { name: 'Panda Pages home' }).click()
    await page.clock.fastForward(220)
    await expectPath(page, '/')
  })

  test('supports all four sort modes and validates the stored preference', async ({
    page,
  }) => {
    await page.addInitScript(({ key }) => {
      if (sessionStorage.getItem('library-sort-seeded') === 'yes') return
      localStorage.setItem(key, 'not-a-real-sort')
      sessionStorage.setItem('library-sort-seeded', 'yes')
    }, { key: SORT_STORAGE_KEY })
    await gotoReadyLibrary(page)

    const titles = page.locator('.bookshelf-card__title')
    const sort = page.getByLabel('Sort')
    const expected = {
      recent: [
        UPDATED_STORY.title,
        CURRENT_STORY.title,
        COMPLETED_STORY.title,
        LONG_UNAUTHORED_STORY.title,
      ],
      title: [
        COMPLETED_STORY.title,
        UPDATED_STORY.title,
        CURRENT_STORY.title,
        LONG_UNAUTHORED_STORY.title,
      ],
      shortest: [
        LONG_UNAUTHORED_STORY.title,
        UPDATED_STORY.title,
        CURRENT_STORY.title,
        COMPLETED_STORY.title,
      ],
      longest: [
        COMPLETED_STORY.title,
        CURRENT_STORY.title,
        UPDATED_STORY.title,
        LONG_UNAUTHORED_STORY.title,
      ],
    } as const

    await expect(sort).toHaveValue('recent')
    await expect(titles).toHaveText([...expected.recent])

    for (const selected of ['title', 'shortest', 'longest', 'recent'] as const) {
      await sort.selectOption(selected)
      await expect(titles).toHaveText([...expected[selected]])
      expect(
        await page.evaluate((key) => localStorage.getItem(key), SORT_STORAGE_KEY),
      ).toBe(selected)
    }

    await sort.selectOption('shortest')
    await page.reload()
    await expect(
      page.getByRole('heading', { name: 'Choose tonight’s story' }),
    ).toBeVisible()
    await expect(sort).toHaveValue('shortest')
    await expect(titles).toHaveText([...expected.shortest])
  })

  test('uses a modal Reka dialog with naming, trapping, isolation, scroll lock, Escape, and focus restoration', async ({
    page,
  }) => {
    await gotoReadyLibrary(page)
    const trigger = storyCard(page, CURRENT_STORY.title).getByRole('button', {
      name: `Details for ${CURRENT_STORY.title}`,
    })
    await trigger.click()

    const dialog = page.getByRole('dialog', { name: CURRENT_STORY.title })
    await expect(dialog).toBeVisible()
    await expect(dialog).toHaveAccessibleDescription(
      'Reading information and progress for this story.',
    )
    await expect(dialog).toContainText('by Mara Bell')
    await expect(dialog).toContainText('1,200 words')
    await expect(dialog).toContainText('4 chapters')
    await expect(dialog).toContainText('42% read')
    expect(await dialog.innerText()).not.toContain(CURRENT_STORY.slug)

    for (const selector of [
      '.library-skip-link',
      '.library-header__topline',
      '.library-header__tools',
      '#library-main',
    ]) {
      await expect(page.locator(selector)).toHaveAttribute('aria-hidden', 'true')
    }
    expect(
      await page.evaluate(() => getComputedStyle(document.body).overflow),
    ).toBe('hidden')

    const close = dialog.getByRole('button', { name: 'Close story details' })
    const action = dialog.getByRole('link', {
      name: `Continue at 42%: ${CURRENT_STORY.title}`,
      exact: true,
    })
    await action.focus()
    await page.keyboard.press('Tab')
    await expect(close).toBeFocused()
    await page.keyboard.press('Shift+Tab')
    await expect(action).toBeFocused()

    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()
    await expect(trigger).toBeFocused()
    for (const selector of [
      '.library-skip-link',
      '.library-header__topline',
      '.library-header__tools',
      '#library-main',
    ]) {
      await expect(page.locator(selector)).not.toHaveAttribute(
        'aria-hidden',
        'true',
      )
    }
    expect(
      await page.evaluate(() => getComputedStyle(document.body).overflow),
    ).not.toBe('hidden')
  })

  test('opening story details dismisses the parent popover before isolating the dialog', async ({
    page,
  }) => {
    await gotoReadyLibrary(page)
    const parent = page.getByRole('button', { name: 'Parent options' })
    await parent.click()
    await expect(page.locator('.parent-menu')).toBeVisible()

    await storyCard(page, CURRENT_STORY.title)
      .getByRole('button', { name: `Details for ${CURRENT_STORY.title}` })
      .click()
    await expect(page.locator('.parent-menu')).toBeHidden()
    const dialog = page.getByRole('dialog', { name: CURRENT_STORY.title })
    await expect(dialog).toBeVisible()

    const dialogBox = await dialog.boundingBox()
    expect(dialogBox).not.toBeNull()
    const topTargets = await page.evaluate(
      ({ dialogPoint }) => {
        const belongsToDialogLayer = (x: number, y: number) => {
          const target = document.elementFromPoint(x, y)
          return Boolean(
            target?.closest(
              '[data-testid="story-details-dialog"], .story-dialog__overlay',
            ),
          )
        }
        return {
          dialog: belongsToDialogLayer(dialogPoint.x, dialogPoint.y),
          viewportCorner: belongsToDialogLayer(8, 8),
        }
      },
      {
        dialogPoint: {
          x: Number(dialogBox?.x) + Number(dialogBox?.width) / 2,
          y: Number(dialogBox?.y) + Number(dialogBox?.height) / 2,
        },
      },
    )
    expect(topTargets).toEqual({
      dialog: true,
      viewportCorner: true,
    })
    await expect(page.locator('.library-skip-link')).toHaveAttribute(
      'aria-hidden',
      'true',
    )
  })

  test('Surprise me chooses only from the visible result set and disables with no eligible story', async ({
    page,
  }) => {
    await gotoReadyLibrary(page)
    const search = page.getByRole('searchbox', { name: 'Search the library' })

    await search.fill('no match at all')
    await expectQuery(page, 'no match at all')
    await expect(page.locator('.surprise-button')).toBeDisabled()

    await search.fill('Mara')
    await expectQuery(page, 'Mara')
    await expect(page.locator('.bookshelf-card')).toHaveCount(1)
    await page.evaluate(() => {
      Math.random = () => 0.999
    })
    await page.locator('.surprise-button').click()
    await expectPath(page, `/read/${CURRENT_STORY.slug}`)
  })

  test('a temporary failure retries in place without a browser reload or false sign-out', async ({
    page,
    api,
  }) => {
    api.enqueueLibrary({
      status: 500,
      body: { error: { code: 'internal_error', message: 'Temporary' } },
    })
    await page.goto('/library')

    await expect(
      page.getByRole('heading', { name: 'The library could not be loaded' }),
    ).toBeVisible()
    await expect(page.getByText('Your session is still active.')).toBeVisible()
    await expectPath(page, '/library')
    await page.evaluate(() => {
      ;(window as Window & { __libraryRetryMarker?: string })
        .__libraryRetryMarker = 'same-document'
    })

    await page.getByRole('button', { name: 'Try again' }).click()
    await expect(
      page.getByRole('heading', { name: 'Choose tonight’s story' }),
    ).toBeVisible()
    expect(
      await page.evaluate(
        () =>
          (window as Window & { __libraryRetryMarker?: string })
            .__libraryRetryMarker,
      ),
    ).toBe('same-document')
    expect(api.count('GET', '/api/v1/library')).toBe(2)
  })

  test('a malformed response is rejected without exposing uncertain or internal data', async ({
    page,
    api,
  }) => {
    api.enqueueLibrary({
      body: {
        items: [
          {
            slug: CURRENT_STORY.slug,
            author: CURRENT_STORY.author,
            language: CURRENT_STORY.language,
            publishedVersion: CURRENT_STORY.publishedVersion,
            wordCount: CURRENT_STORY.wordCount,
            chapterCount: CURRENT_STORY.chapterCount,
            progress: CURRENT_STORY.progress,
          },
        ],
      },
    })
    await page.goto('/library')

    await expect(
      page.getByRole('heading', {
        name: 'The library response could not be read safely',
      }),
    ).toBeVisible()
    await expect(page.getByText('No uncertain progress')).toBeVisible()
    await expectPath(page, '/library')
  })

  test('partial progress metadata remains renderable as explicitly unavailable', async ({
    page,
    api,
  }) => {
    api.items = [UNAVAILABLE_PROGRESS_STORY]
    await page.goto('/library')

    const card = storyCard(page, UNAVAILABLE_PROGRESS_STORY.title)
    await expect(card).toBeVisible()
    await expect(card).toContainText('Progress unavailable')
    await expect(
      card.getByRole('link', {
        name: `Read: ${UNAVAILABLE_PROGRESS_STORY.title}`,
        exact: true,
      }),
    ).toBeVisible()
    await expect(card.getByRole('progressbar')).toHaveCount(0)
    await expect(page.locator('.continue-card')).toHaveCount(0)
  })

  test('corrupt published stories are omitted with a truthful partial-library warning', async ({
    page,
    api,
  }) => {
    api.unavailableItemCount = 1
    await gotoReadyLibrary(page)

    const warning = page.getByRole('status', {
      name: 'Some stories could not be shown safely',
    })
    await expect(warning).toContainText('One story could not be shown safely')
    await expect(page.locator('.bookshelf-card')).toHaveCount(READY_STORIES.length)
    await expect(
      page.getByRole('heading', { name: 'No published stories yet' }),
    ).toHaveCount(0)
  })

  test('an entirely quarantined shelf never claims that no stories were published', async ({
    page,
    api,
  }) => {
    api.items = []
    api.unavailableItemCount = 2
    await page.goto('/library')

    await expect(
      page.getByRole('heading', { name: 'Stories could not be shown safely' }),
    ).toBeVisible()
    await expect(page.getByText('2 published stories could')).toBeVisible()
    await expect(page.getByText('A parent needs to review')).toBeVisible()
    await expect(
      page.getByRole('heading', { name: 'No published stories yet' }),
    ).toHaveCount(0)
  })

  test('displayed progress stays below completion until the exact 0.98 threshold', async ({
    page,
    api,
  }) => {
    api.items = [
      {
        ...CURRENT_STORY,
        progress: { ...CURRENT_STORY.progress!, percent: 0.979 },
      },
      {
        ...COMPLETED_STORY,
        progress: { ...COMPLETED_STORY.progress!, percent: 0.98 },
      },
    ]
    await page.goto('/library')

    await expect(
      storyCard(page, CURRENT_STORY.title).getByRole('link', {
        name: `Continue at 97%: ${CURRENT_STORY.title}`,
      }),
    ).toBeVisible()
    await expect(
      storyCard(page, COMPLETED_STORY.title).getByRole('link', {
        name: `Read again: ${COMPLETED_STORY.title}`,
      }),
    ).toBeVisible()
  })

  test('a definitive library 401 clears the shelf and transitions safely to Unlock', async ({
    page,
    api,
  }) => {
    api.enqueueLibrary({
      status: 401,
      body: { error: { code: 'unauthorized', message: 'Session ended' } },
    })
    await page.goto('/library')

    await expectPath(page, '/unlock', '/library')
    await expect(page.getByText('Enter your secret passcode')).toBeVisible()
    await expect(page.getByText(CURRENT_STORY.title)).toHaveCount(0)
    expect(api.count('GET', '/api/v1/library')).toBe(1)
  })

  test('the parent popover supports WebKit Tab order, reverse traversal, Escape, restoration, and onward focus @webkit-library', async ({
    page,
  }) => {
    await gotoReadyLibrary(page)
    const trigger = page.getByRole('button', { name: 'Parent options' })
    await expect(trigger).toContainText('Parent options')
    await trigger.click()

    const menu = page.getByRole('dialog', { name: 'Parent options' })
    await expect(menu).toBeVisible()
    await expect(
      menu.getByRole('button', { name: 'Reading profile' }),
    ).toBeVisible()
    await expect(menu.getByRole('button', { name: 'Admin' })).toBeVisible()
    await expect(menu.getByRole('button')).toHaveCount(2)
    await expect(page.locator('.surprise-button')).toBeVisible()

    const profile = menu.getByRole('button', { name: 'Reading profile' })
    const admin = menu.getByRole('button', { name: 'Admin' })
    const lock = page.getByRole('button', { name: 'Lock Panda Pages' })

    await page.keyboard.press('Tab')
    await expect(profile).toBeFocused()
    await page.keyboard.press('Tab')
    await expect(admin).toBeFocused()
    await page.keyboard.press('Shift+Tab')
    await expect(profile).toBeFocused()
    await page.keyboard.press('Shift+Tab')
    await expect(menu).toBeHidden()
    await expect(trigger).toBeFocused()

    await trigger.click()
    await page.keyboard.press('Tab')
    await page.keyboard.press('Escape')
    await expect(menu).toBeHidden()
    await expect(trigger).toBeFocused()

    await trigger.click()
    await page.keyboard.press('Tab')
    await page.keyboard.press('Tab')
    await page.keyboard.press('Tab')
    await expect(menu).toBeHidden()
    await expect(lock).toBeFocused()

    await trigger.click()
    await menu.getByRole('button', { name: 'Reading profile' }).click()
    await expectPath(page, '/journey')
    await expect(
      page.getByRole('heading', { name: 'Reading profile' }),
    ).toBeVisible()
    const body = await page.locator('body').innerText()
    expect(body).not.toContain('Personalise stories')
    expect(body).not.toContain('Personalised:')
  })

  test('the public-home route works from representative mobile and desktop navigation', async ({
    page,
  }) => {
    for (const viewport of [
      { name: 'mobile', width: 390, height: 844 },
      { name: 'desktop', width: 1440, height: 900 },
    ]) {
      await test.step(viewport.name, async () => {
        await page.setViewportSize(viewport)
        await page.goto('/library')
        await expect(
          page.getByRole('heading', { name: 'Choose tonight’s story' }),
        ).toBeVisible()
        await page.getByRole('link', { name: 'Panda Pages home' }).click()
        await expectPath(page, '/')
        await expect(
          page.getByRole('heading', {
            level: 1,
            name: 'Stories that never grow old.',
          }),
        ).toBeVisible()
      })
    }
  })

  test('Lock waits for confirmed logout before clearing and navigating', async ({
    page,
    api,
  }) => {
    await page.clock.install()
    const logout = api.deferLogout()
    await gotoReadyLibrary(page)
    await page
      .getByRole('searchbox', { name: 'Search the library' })
      .fill('Moon')
    await page.getByRole('button', { name: 'Lock Panda Pages' }).click()
    await logout.started

    await page.clock.fastForward(220)
    await expectPath(page, '/library')
    expect(new URL(page.url()).searchParams.get('q')).toBeNull()
    await expect(page.getByText(CURRENT_STORY.title).first()).toBeVisible()
    await expect(page.getByRole('button', { name: 'Lock Panda Pages' })).toContainText(
      'Locking…',
    )

    logout.fulfill()
    await expectPath(page, '/unlock')
    await expect(page.getByText(CURRENT_STORY.title)).toHaveCount(0)
    expect(api.count('POST', '/api/v1/auth/logout')).toBe(1)
  })

  test('a failed Lock keeps the confirmed library open and reports the failure', async ({
    page,
    api,
  }) => {
    api.enqueueLogout({
      status: 503,
      body: { error: { code: 'unavailable', message: 'Try later' } },
    })
    await gotoReadyLibrary(page)
    await page.getByRole('button', { name: 'Lock Panda Pages' }).click()

    const alert = page.getByRole('alert')
    await expect(alert).toContainText('Could not lock Panda Pages')
    await expect(alert).toContainText('Your library is still open')
    await expectPath(page, '/library')
    await expect(page.getByText(CURRENT_STORY.title).first()).toBeVisible()
    await expect(page.getByRole('button', { name: 'Lock Panda Pages' })).toBeEnabled()
  })

  test('has no page overflow across required phone, landscape, tablet, desktop, and 200%-equivalent viewports', async ({
    page,
  }) => {
    await gotoReadyLibrary(page)

    const viewports = [
      { name: '320px mobile', width: 320, height: 640 },
      { name: '390px mobile', width: 390, height: 844 },
      { name: 'mobile landscape', width: 667, height: 375 },
      { name: 'tablet portrait', width: 768, height: 1024 },
      { name: 'tablet landscape', width: 1024, height: 768 },
      { name: '1440px desktop', width: 1440, height: 900 },
      {
        name: '1440px desktop at 200%-equivalent constraints',
        width: 720,
        height: 450,
      },
    ]

    for (const viewport of viewports) {
      await test.step(viewport.name, async () => {
        await page.setViewportSize(viewport)
        await page.evaluate(
          () => new Promise<void>((resolve) => requestAnimationFrame(() => resolve())),
        )
        await expectNoHorizontalOverflow(page)
        await expect(
          page.getByRole('link', { name: 'Panda Pages home' }),
        ).toBeVisible()
        await expect(
          page.getByRole('searchbox', { name: 'Search the library' }),
        ).toBeVisible()
        await expect(
          page.getByRole('button', { name: 'Parent options' }),
        ).toBeVisible()
        await expect(page.getByRole('button', { name: 'Lock Panda Pages' })).toBeVisible()
      })
    }

    await expect(page.locator('meta[name="viewport"]')).toHaveAttribute(
      'content',
      /viewport-fit=cover/,
    )
  })

  test('primary Library controls provide 44px-class touch targets', async ({
    page,
  }) => {
    await gotoReadyLibrary(page)
    await page
      .getByRole('searchbox', { name: 'Search the library' })
      .fill('Moon')

    const targets = [
      storyCard(page, CURRENT_STORY.title).getByRole('link', {
        name: `Continue at 42%: ${CURRENT_STORY.title}`,
      }),
      storyCard(page, LONG_UNAUTHORED_STORY.title).getByRole('link', {
        name: `Read: ${LONG_UNAUTHORED_STORY.title}`,
      }),
      storyCard(page, CURRENT_STORY.title).getByRole('button', {
        name: `Details for ${CURRENT_STORY.title}`,
      }),
      page.getByRole('button', { name: 'Clear search' }),
      page.getByRole('button', { name: 'Parent options' }),
      page.getByRole('button', { name: 'Lock Panda Pages' }),
      page.getByRole('button', { name: 'Surprise me' }),
    ]

    for (const target of targets) {
      const box = await target.boundingBox()
      expect(box).not.toBeNull()
      expect(box!.height).toBeGreaterThanOrEqual(44)
      expect(box!.width).toBeGreaterThanOrEqual(44)
    }
  })

  test('long titles, missing authors, large text, and safe-area-aware layout remain contained', async ({
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 375 })
    await gotoReadyLibrary(page)
    await page.addStyleTag({
      content: 'html { font-size: 32px !important; }',
    })

    const header = page.locator('.library-header')
    const brand = page.getByRole('link', { name: 'Panda Pages home' })
    const parent = page.getByRole('button', { name: 'Parent options' })
    const lock = page.getByRole('button', { name: 'Lock Panda Pages' })
    await expect(header).toHaveClass(/library-header--static/)
    expect(await header.evaluate((element) => getComputedStyle(element).position)).not.toBe(
      'sticky',
    )
    await expect(brand).toBeVisible()
    await expect(parent).toBeVisible()
    await expect(lock).toBeVisible()
    const brandBox = await brand.boundingBox()
    const parentBox = await parent.boundingBox()
    const lockBox = await lock.boundingBox()
    expect(brandBox).not.toBeNull()
    expect(parentBox).not.toBeNull()
    expect(lockBox).not.toBeNull()
    expect(boxesOverlap(brandBox!, parentBox!)).toBe(false)
    expect(boxesOverlap(brandBox!, lockBox!)).toBe(false)
    expect(boxesOverlap(parentBox!, lockBox!)).toBe(false)

    await parent.click()
    const menu = page.locator('.parent-menu')
    await expect(menu).toBeVisible()
    const menuBox = await menu.boundingBox()
    expect(menuBox).not.toBeNull()
    expect(menuBox!.x).toBeGreaterThanOrEqual(0)
    expect(menuBox!.x + menuBox!.width).toBeLessThanOrEqual(390)
    expect(menuBox!.y).toBeGreaterThanOrEqual(0)
    expect(menuBox!.y + menuBox!.height).toBeLessThanOrEqual(375)
    await page.keyboard.press('Escape')
    await expect(parent).toBeFocused()

    const card = storyCard(page, LONG_UNAUTHORED_STORY.title)
    await expect(card).toBeVisible()
    await expect(card.locator('.bookshelf-card__title')).toHaveText(
      LONG_UNAUTHORED_STORY.title,
    )
    await expect(card.locator('.bookshelf-card__author')).toHaveCount(0)
    const read = card.getByRole('link', {
      name: `Read: ${LONG_UNAUTHORED_STORY.title}`,
    })
    await read.scrollIntoViewIfNeeded()
    const readBox = await read.boundingBox()
    expect(readBox).not.toBeNull()
    expect(
      await page.evaluate(({ x, y }) => {
        const target = document.elementFromPoint(x, y)
        return Boolean(target?.closest('.bookshelf-card__action'))
      }, {
        x: readBox!.x + readBox!.width / 2,
        y: readBox!.y + readBox!.height / 2,
      }),
    ).toBe(true)
    await expectNoHorizontalOverflow(page)
  })

  test('reduced-motion users receive the static card and dialog treatments', async ({
    page,
  }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await gotoReadyLibrary(page)

    const transitionSeconds = await page
      .locator('.bookshelf-card')
      .first()
      .evaluate((element) =>
        Number.parseFloat(getComputedStyle(element).transitionDuration),
      )
    expect(transitionSeconds).toBeLessThanOrEqual(0.001)
    await storyCard(page, CURRENT_STORY.title)
      .getByRole('button', { name: `Details for ${CURRENT_STORY.title}` })
      .click()
    const dialog = page.getByRole('dialog', { name: CURRENT_STORY.title })
    await expect(dialog).toBeVisible()
    expect(
      await dialog.evaluate((element) => getComputedStyle(element).animationName),
    ).toBe('none')
  })

  test('axe: ready bookshelf has no serious or critical violations', async ({
    page,
  }) => {
    await gotoReadyLibrary(page)
    await expectNoSeriousOrCriticalViolations(page)
  })

  test('axe: story dialog has no serious or critical violations', async ({
    page,
  }) => {
    await gotoReadyLibrary(page)
    await storyCard(page, CURRENT_STORY.title)
      .getByRole('button', { name: `Details for ${CURRENT_STORY.title}` })
      .click()
    await expect(
      page.getByRole('dialog', { name: CURRENT_STORY.title }),
    ).toBeVisible()
    await expectNoSeriousOrCriticalViolations(page)
  })

  test('axe: empty bookshelf has no serious or critical violations', async ({
    page,
    api,
  }) => {
    api.items = []
    await page.goto('/library')
    await expect(
      page.getByRole('heading', { name: 'No published stories yet' }),
    ).toBeVisible()
    await expectNoSeriousOrCriticalViolations(page)
  })

  test('axe: temporary error has no serious or critical violations', async ({
    page,
    api,
  }) => {
    api.enqueueLibrary({
      status: 500,
      body: { error: { code: 'internal_error', message: 'Temporary' } },
    })
    await page.goto('/library')
    await expect(
      page.getByRole('heading', { name: 'The library could not be loaded' }),
    ).toBeVisible()
    await expectNoSeriousOrCriticalViolations(page)
  })

  test('axe: no-results search has no serious or critical violations', async ({
    page,
  }) => {
    await page.goto('/library?q=unfindable')
    await expect(
      page.getByRole('heading', {
        name: 'Nothing found for “unfindable”',
      }),
    ).toBeVisible()
    await expectNoSeriousOrCriticalViolations(page)
  })
})
