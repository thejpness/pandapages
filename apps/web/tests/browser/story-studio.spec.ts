import { AxeBuilder } from '@axe-core/playwright'
import { expect, test, type Page, type Route } from '@playwright/test'

const versionOne = '11111111-1111-4111-8111-111111111111'
const versionTwo = '22222222-2222-4222-8222-222222222222'
const versionThree = '33333333-3333-4333-8333-333333333333'
const timestamp = '2026-07-20T10:00:00Z'

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
    isDraft?: boolean
    isPublished?: boolean
    health?: 'ready' | 'repair_required' | 'unavailable'
  } = {},
) {
  return {
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
  }
}

type TestStorySummary = ReturnType<typeof summary>
type TestVersionSummary = ReturnType<typeof versionSummary>
type TestStoryDetail = TestStorySummary & {
  createdAt: string
  versions: TestVersionSummary[]
}

class StudioAPI {
  readonly requests: Array<{ method: string; path: string; body: unknown }> = []
  readonly unhandled: string[] = []
  listFailure: QueuedFailure | null = null
  detailFailure: QueuedFailure | null = null
  draftFailure: QueuedFailure | null = null
  draftOutcome: 'created_story' | 'created_version' | 'reused' = 'created_story'
  abortNextList = false
  previewGate: {
    started: ReturnType<typeof deferred<void>>
    release: ReturnType<typeof deferred<void>>
  } | null = null
  detailGates = new Map<string, ReturnType<typeof deferred<void>>>()

  stories = [
    summary('panda-tale', 'The Panda Tale', 'published_with_draft'),
    summary('quiet-moon', 'The Quiet Moon', 'draft_only', { author: null }),
    summary('old-oak', 'The Old Oak', 'unpublished'),
    summary('repair-story', 'The Tangled Story', 'repair_required'),
  ]

  details = new Map<string, TestStoryDetail>([
    [
      'panda-tale',
      {
        ...this.stories[0],
        createdAt: timestamp,
        versions: [
          versionSummary(versionTwo, 2, { isDraft: true }),
          versionSummary(versionOne, 1, { isPublished: true }),
        ],
      },
    ],
    [
      'quiet-moon',
      {
        ...this.stories[1],
        createdAt: timestamp,
        versions: [versionSummary(versionTwo, 2, { isDraft: true })],
      },
    ],
    [
      'old-oak',
      {
        ...this.stories[2],
        createdAt: timestamp,
        versions: [versionSummary(versionOne, 1)],
      },
    ],
    [
      'repair-story',
      {
        ...this.stories[3],
        createdAt: timestamp,
        versions: [
          versionSummary(versionOne, 1, { health: 'repair_required' }),
        ],
      },
    ],
  ])

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

    if (path === '/api/v1/auth/status') {
      await this.fulfill(route, { unlocked: true })
      return
    }
    if (path === '/api/v1/auth/logout' && method === 'POST') {
      await this.fulfill(route, { ok: true })
      return
    }
    if (path === '/api/v1/library') {
      await this.fulfill(route, { items: [], unavailableItemCount: 0 })
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
        renderedHtml: `<h1>${title.trim()}</h1><p>Canonical preview content.</p>`,
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
      const versions = existing
        ? [
            versionSummary(resultId, resultVersion, { isDraft: true }),
            ...(existing.versions.map((version) => ({
              ...version,
              isDraft: false,
            })) as TestVersionSummary[]),
          ]
        : [versionSummary(resultId, resultVersion, { isDraft: true })]
      const detail = { ...item, createdAt: timestamp, versions }
      this.details.set(slug, detail)
      const index = this.stories.findIndex((candidate) => candidate.slug === slug)
      if (index >= 0) this.stories[index] = item
      else this.stories.unshift(item)
      await this.fulfill(route, {
        slug,
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

    const versionMatch = /^\/api\/v1\/admin\/stories\/([^/]+)\/versions\/([^/]+)$/.exec(path)
    if (versionMatch && method === 'GET') {
      const source = this.source(decodeURIComponent(versionMatch[1]), versionMatch[2])
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

    const publishMatch = /^\/api\/v1\/admin\/stories\/([^/]+)\/publish$/.exec(path)
    if (publishMatch && method === 'POST') {
      const slug = decodeURIComponent(publishMatch[1])
      const detail = this.details.get(slug)
      const rawVersionId = (body as Record<string, unknown>).versionId
      const id = typeof rawVersionId === 'string' ? rawVersionId : ''
      const versions = detail?.versions ?? []
      const selected = versions.find((version) => version.versionId === id)
      if (!detail || !selected || selected.health !== 'ready') {
        await this.fail(route, {
          status: 409,
          code: 'publish_repair_required',
          message: 'story version is unavailable or unreadable',
        })
        return
      }
      for (const version of versions) version.isPublished = version.versionId === id
      detail.publishedVersion = { versionId: id, version: selected.version }
      detail.status = detail.draftVersion?.versionId === id ? 'published' : 'published_with_draft'
      const item = this.stories.find((candidate) => candidate.slug === slug)
      if (item) Object.assign(item, detail)
      await this.fulfill(route, {
        slug,
        status: detail.status,
        publishedVersion: detail.publishedVersion,
        draftVersion: detail.draftVersion,
        versionCount: detail.versionCount,
        updatedAt: timestamp,
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
      for (const version of detail.versions) version.isPublished = false
      detail.publishedVersion = null
      detail.status = detail.draftVersion ? 'draft_only' : 'unpublished'
      const item = this.stories.find((candidate) => candidate.slug === slug)
      if (item) Object.assign(item, detail)
      await this.fulfill(route, {
        slug,
        status: detail.status,
        publishedVersion: null,
        draftVersion: detail.draftVersion,
        versionCount: detail.versionCount,
        updatedAt: timestamp,
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
  await expect(catalogue.getByText('Published · New draft')).toBeVisible()
  await expect(catalogue.getByText('Draft only')).toBeVisible()
  await expect(catalogue.getByText('Unpublished')).toBeVisible()
  await expect(catalogue.getByText('Needs attention')).toBeVisible()

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
  await page.getByRole('button', { name: 'Save draft', exact: true }).first().click()

  await expect(page).toHaveURL(/\/admin\/stories\/new-panda-story\?saved=created_story&version=1$/)
  await expect(page.getByText('Story created as draft version 1.')).toBeVisible()
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
  await expect(page.getByText('Starting from version 2.')).toBeVisible()
  await page.getByLabel('Markdown').fill('# The Panda Tale\n\nA genuinely new version.\n')
  await page.getByRole('button', { name: 'Save draft', exact: true }).first().click()
  await expect(page.getByText('Draft version 3 created.')).toBeVisible()

  api.draftOutcome = 'reused'
  await page.goto(`/admin/stories/panda-tale/edit?fromVersion=${versionThree}`)
  await page.getByRole('button', { name: 'Save draft', exact: true }).first().click()
  await expect(page.getByText('Existing healthy version 3 reused.')).toBeVisible()
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
  await page.getByRole('button', { name: 'Save draft', exact: true }).first().click()
  await expect(page.getByRole('alert').getByText('Needs attention')).toBeVisible()
  await expect(page.getByText('Unsaved changes')).toBeVisible()

  await page.getByRole('button', { name: 'Stories', exact: true }).click()
  const leave = page.getByRole('dialog', { name: 'Leave with unsaved changes?' })
  await leave.getByRole('button', { name: 'Discard changes and leave' }).click()
  await page.getByRole('heading', { name: 'The Tangled Story' }).locator('..').getByRole('button', { name: 'Review story' }).click()
  await expect(page.getByRole('heading', { level: 2, name: 'Needs attention' })).toBeVisible()
  await expect(page.locator('.repair-banner')).toHaveCSS(
    'background-color',
    'rgb(255, 242, 216)',
  )
  await expect(page.getByRole('button', { name: 'Publish selected version' })).toBeDisabled()
  await expect(page.getByText('This stored version cannot safely be reused or published.')).toBeVisible()
  expect(await seriousOrCriticalViolations(page)).toEqual([])
})

test('publish is deliberate, retains history and exposes Reader only after success', async ({
  page,
}) => {
  const api = new StudioAPI()
  await api.install(page)
  await page.goto('/admin/stories/panda-tale')
  await expect(page.locator('.detail-overview')).toBeVisible()
  await expect(page.locator('.version-row')).toHaveCount(2)
  expect(await seriousOrCriticalViolations(page)).toEqual([])
  await page.getByLabel('Select version 2 for publication').check()
  await page.getByRole('button', { name: 'Publish selected version' }).click()
  const dialog = page.getByRole('dialog', { name: 'Publish this version?' })
  await expect(dialog.getByText('Version 1 is currently published.')).toBeVisible()
  await expect(dialog.getByText(/Existing historical versions and reading progress are retained/)).toBeVisible()
  expect(await seriousOrCriticalViolations(page)).toEqual([])
  await dialog.getByRole('button', { name: 'Publish version' }).click()
  await expect(page.getByText('Version 2 published. Readers can now open it.')).toBeVisible()
  await expect(page.getByRole('link', { name: 'Open published story' })).toBeVisible()
  expect(api.count('POST', '/api/v1/admin/stories/panda-tale/publish')).toBe(1)
})

test('unpublish removes Reader availability while retaining drafts and version history', async ({
  page,
}) => {
  const api = new StudioAPI()
  await api.install(page)
  await page.goto('/admin/stories/panda-tale')
  await expect(page.locator('.version-row')).toHaveCount(2)
  const initialRows = await page.locator('.version-row').count()
  await page.getByRole('button', { name: 'Unpublish' }).click()
  const dialog = page.getByRole('dialog', { name: 'Unpublish this story?' })
  await expect(dialog.getByText(/Drafts, immutable versions and historical reading progress remain/)).toBeVisible()
  await expect(dialog).toHaveCSS('background-color', 'rgb(255, 254, 250)')
  expect(await seriousOrCriticalViolations(page)).toEqual([])
  await dialog.getByRole('button', { name: 'Unpublish story' }).click()
  await expect(page.getByText(/Story unpublished/)).toBeVisible()
  await expect(page.getByRole('link', { name: 'Open published story' })).toBeHidden()
  await expect(page.getByText('Version 2', { exact: true })).toBeVisible()
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

test('401 goes to Unlock with a safe next while 403 and retryable failures stay truthful', async ({
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

  api.listFailure = { status: 401, code: 'unauthorized', message: 'unlock required' }
  await page.reload()
  await expect(page).toHaveURL(/\/unlock\?next=\/admin\/stories$/)
})

test('stale detail response cannot replace a newer route', async ({ page }) => {
  const api = new StudioAPI()
  const gate = deferred<void>()
  api.detailGates.set('panda-tale', gate)
  await openCatalogue(page, api)
  await page.getByRole('heading', { name: 'The Panda Tale' }).locator('..').getByRole('button', { name: 'Manage story' }).click()
  await expect(page.getByText('Opening story')).toBeVisible()
  await page.getByRole('button', { name: 'Stories', exact: true }).click()
  await page.getByRole('heading', { name: 'The Tangled Story' }).locator('..').getByRole('button', { name: 'Review story' }).click()
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
  await expect(page.getByRole('button', { name: 'Save draft', exact: true }).last()).toBeVisible()
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
