import { AxeBuilder } from '@axe-core/playwright'
import type { Page } from '@playwright/test'
import {
  expect,
  locatorFor,
  makePagedReaderStory,
  makeReaderStory,
  progressFor,
  READER_SLUG,
  test,
  type ProgressFixture,
  type ReaderSegmentFixture,
  type ReaderStoryFixture,
} from './support/reader-api'
import {
  activeElementIsInside,
  expectCurrentPageContainsOrdinal,
  expectFocusTrapped,
  expectLocatorV2Request,
  expectNoProgressPut,
  expectSegmentAtReadingLine,
  gotoReader,
  scrollToSegment,
  seedReaderPreferences,
  waitForPagedReady,
} from './support/reader-page'

const serverError = {
  error: { code: 'internal_error', message: 'Test-only progress failure' },
}

function fixtureKey(seed: number): string {
  return Math.max(0, Math.trunc(seed)).toString(16).padStart(64, '0')
}

function withOrdinal(
  segment: ReaderSegmentFixture,
  ordinal: number,
): ReaderSegmentFixture {
  return { ...segment, ordinal }
}

function highConfidenceStories(
  slug = READER_SLUG,
): { oldStory: ReaderStoryFixture; currentStory: ReaderStoryFixture } {
  const oldStory = makeReaderStory({ slug, version: 1 })
  const target = oldStory.segments[3]
  if (!target) throw new Error('missing high-confidence fixture target')
  const inserted: ReaderSegmentFixture = {
    ...target,
    ordinal: 4,
    contentKey: fixtureKey(700),
    renderedHtml: '<p>A new paragraph was inserted before the saved place.</p>',
    wordCount: 9,
  }
  const currentStory: ReaderStoryFixture = {
    ...oldStory,
    version: 2,
    segments: [
      ...oldStory.segments.slice(0, 3),
      inserted,
      ...oldStory.segments.slice(3),
    ].map((segment, index) => withOrdinal(segment, index + 1)),
  }
  return { oldStory, currentStory }
}

function mediumConfidenceStories(): {
  oldStory: ReaderStoryFixture
  currentStory: ReaderStoryFixture
} {
  const oldStory = makeReaderStory({ version: 1 })
  const currentStory: ReaderStoryFixture = {
    ...oldStory,
    version: 2,
    segments: oldStory.segments
      .filter((segment) => segment.ordinal !== 4)
      .map((segment, index) => withOrdinal(segment, index + 1)),
  }
  return { oldStory, currentStory }
}

function lowConfidenceStories(): {
  oldStory: ReaderStoryFixture
  currentStory: ReaderStoryFixture
} {
  const oldStory = makeReaderStory({ version: 1 })
  const chapter = fixtureKey(802)
  const currentStory: ReaderStoryFixture = {
    ...oldStory,
    version: 2,
    segments: [
      {
        ordinal: 1,
        kind: 'heading',
        headingLevel: 1,
        contentKey: fixtureKey(801),
        contentOccurrence: 1,
        chapterKey: null,
        chapterOccurrence: null,
        renderedHtml: '<h1>A completely revised opening</h1>',
        wordCount: 1,
      },
      {
        ordinal: 2,
        kind: 'heading',
        headingLevel: 2,
        contentKey: chapter,
        contentOccurrence: 1,
        chapterKey: chapter,
        chapterOccurrence: 1,
        renderedHtml: '<h2>A new chapter</h2>',
        wordCount: 3,
      },
      {
        ordinal: 3,
        kind: 'paragraph',
        headingLevel: null,
        contentKey: fixtureKey(803),
        contentOccurrence: 1,
        chapterKey: chapter,
        chapterOccurrence: 1,
        renderedHtml:
          '<p>The revised story has a different semantic identity throughout.</p>',
        wordCount: 6,
      },
      {
        ordinal: 4,
        kind: 'paragraph',
        headingLevel: null,
        contentKey: fixtureKey(804),
        contentOccurrence: 1,
        chapterKey: chapter,
        chapterOccurrence: 1,
        renderedHtml:
          '<p>' +
          'A long final passage keeps the mapped semantic anchor restorable. '.repeat(80) +
          '</p>',
        wordCount: 20,
      },
    ],
  }
  return { oldStory, currentStory }
}

function updatedProgress(
  oldStory: ReaderStoryFixture,
  ordinal: number,
  offset: number,
  percent: number,
  version = oldStory.version,
): ProgressFixture {
  return {
    version,
    locator: locatorFor(oldStory, ordinal, offset),
    percent,
  }
}

async function storyUpdatedDialog(page: Page) {
  const dialog = page.getByRole('dialog', { name: 'Story updated' })
  await expect(dialog).toBeVisible()
  return dialog
}

async function seriousOrCriticalViolations(page: Page) {
  await page.evaluate(async () => {
    await document.fonts.ready
  })
  const results = await new AxeBuilder({ page }).analyze()
  return results.violations
    .filter(
      (violation) =>
        violation.impact === 'serious' || violation.impact === 'critical',
    )
    .map((violation) => violation.id)
}

test.describe('Reader cross-version progress decisions', () => {
  test('high-confidence scroll mapping follows a moved segment and saves only after intentional movement', async ({
    page,
    api,
  }) => {
    const { oldStory, currentStory } = highConfidenceStories()
    api.setStory(currentStory)
    api.setProgress(
      READER_SLUG,
      updatedProgress(oldStory, 4, 0.4, 0.48),
    )
    const put = api.deferProgressPut(READER_SLUG)

    await gotoReader(page, api, READER_SLUG)
    const dialog = await storyUpdatedDialog(page)
    await expect(dialog).toContainText(
      'We found the same reading place in this updated version.',
    )
    await expectNoProgressPut(page, api, READER_SLUG)

    await dialog
      .getByRole('button', { name: 'Continue in the updated story' })
      .click()
    await expect(dialog).toBeHidden()
    await expectSegmentAtReadingLine(page, 5, 0.4)
    await expectNoProgressPut(page, api, READER_SLUG)
    expect(
      await activeElementIsInside(page, '[data-reader-scroll-view]'),
    ).toBe(true)

    await scrollToSegment(page, 6, 0)
    const request = await put.started
    expectLocatorV2Request(request, { version: 2, ordinal: 6 })
    expect(request.body).toEqual(
      expect.objectContaining({
        locator: expect.objectContaining({
          segment: expect.objectContaining({ ordinal: 6 }),
        }),
      }),
    )
    put.fulfill()
    await expect(page.locator('.reader-save-status')).toContainText('Saved')
    expect(api.progressPuts()).toHaveLength(1)
  })

  test('high-confidence paged mapping restores the semantic target without an immediate PUT', async ({
    page,
    api,
  }) => {
    await seedReaderPreferences(page)
    const oldStory = makePagedReaderStory({ version: 1 })
    const currentStory = makePagedReaderStory({ version: 2 })
    api.setStory(currentStory)
    api.setProgress(
      READER_SLUG,
      updatedProgress(oldStory, 7, 0, 0.68),
    )

    await gotoReader(page, api, READER_SLUG)
    const dialog = await storyUpdatedDialog(page)
    await dialog
      .getByRole('button', { name: 'Continue in the updated story' })
      .click()
    await expect(dialog).toBeHidden()
    await expectCurrentPageContainsOrdinal(page, 7)
    await expectNoProgressPut(page, api, READER_SLUG)
  })

  test('medium confidence restores the exact chapter heading, not the removed segment offset', async ({
    page,
    api,
  }) => {
    const { oldStory, currentStory } = mediumConfidenceStories()
    api.setStory(currentStory)
    api.setProgress(
      READER_SLUG,
      updatedProgress(oldStory, 4, 0.85, 0.46),
    )

    await gotoReader(page, api, READER_SLUG)
    const dialog = await storyUpdatedDialog(page)
    await expect(dialog).toContainText(
      'We found the same chapter in this updated version.',
    )
    await dialog
      .getByRole('button', { name: 'Continue in the updated story' })
      .click()
    await expectSegmentAtReadingLine(page, 3, 0)
    await expectNoProgressPut(page, api, READER_SLUG)
  })

  test('low confidence inverts the saved weighted percentage against current segments', async ({
    page,
    api,
  }) => {
    const { oldStory, currentStory } = lowConfidenceStories()
    api.setStory(currentStory)
    api.setProgress(
      READER_SLUG,
      updatedProgress(oldStory, 4, 0.6, 0.2),
    )

    await gotoReader(page, api, READER_SLUG)
    const dialog = await storyUpdatedDialog(page)
    await expect(dialog).toContainText(
      'We found an approximate place based on your reading progress.',
    )
    await dialog
      .getByRole('button', { name: 'Continue in the updated story' })
      .click()
    await expectSegmentAtReadingLine(page, 3, 1 / 3)
    await expectNoProgressPut(page, api, READER_SLUG)
  })

  test('a newer saved version produces an honest no-safe-mapping decision without Continue', async ({
    page,
    api,
  }) => {
    const currentStory = makeReaderStory({ version: 2 })
    api.setStory(currentStory)
    api.setProgress(
      READER_SLUG,
      progressFor(currentStory, 4, 0.4, 0.5, 3),
    )

    await gotoReader(page, api, READER_SLUG)
    const dialog = await storyUpdatedDialog(page)
    await expect(dialog).toContainText(
      'We could not safely find your previous reading place',
    )
    await expect(
      dialog.getByRole('button', {
        name: 'Continue in the updated story',
      }),
    ).toHaveCount(0)
    await expectNoProgressPut(page, api, READER_SLUG)
  })

  test('duplicate segment content maps by occurrence rather than old ordinal', async ({
    page,
    api,
  }) => {
    const repeatedKey = fixtureKey(850)
    const oldStory = makeReaderStory({ version: 1 })
    const oldLocator = {
      schema: 2 as const,
      segment: {
        key: repeatedKey,
        occurrence: 2,
        ordinal: 2,
        offset: 0.5,
      },
    }
    const currentStory: ReaderStoryFixture = {
      ...oldStory,
      version: 2,
      segments: [
        withOrdinal(oldStory.segments[0], 1),
        {
          ...oldStory.segments[1],
          ordinal: 2,
          contentKey: repeatedKey,
          contentOccurrence: 1,
        },
        {
          ...oldStory.segments[1],
          ordinal: 3,
          contentKey: repeatedKey,
          contentOccurrence: 2,
          renderedHtml: '<p>The second repeated segment is the target.</p>',
        },
      ],
    }
    api.setStory(currentStory)
    api.setProgress(READER_SLUG, {
      version: 1,
      locator: oldLocator,
      percent: 0.7,
    })

    await gotoReader(page, api, READER_SLUG)
    const dialog = await storyUpdatedDialog(page)
    await dialog
      .getByRole('button', { name: 'Continue in the updated story' })
      .click()
    await expectSegmentAtReadingLine(page, 3, 0.5)
    await expectNoProgressPut(page, api, READER_SLUG)
  })

  test('repeated chapter labels map by canonical key and occurrence', async ({
    page,
    api,
  }) => {
    const oldStory = makePagedReaderStory({ version: 1 })
    const currentStory: ReaderStoryFixture = {
      ...oldStory,
      version: 2,
      segments: oldStory.segments
        .filter((segment) => segment.ordinal !== 7)
        .map((segment, index) => withOrdinal(segment, index + 1)),
    }
    api.setStory(currentStory)
    api.setProgress(
      READER_SLUG,
      updatedProgress(oldStory, 7, 0.7, 0.7),
    )

    await gotoReader(page, api, READER_SLUG)
    const dialog = await storyUpdatedDialog(page)
    await expect(dialog).toContainText('same chapter')
    await dialog
      .getByRole('button', { name: 'Continue in the updated story' })
      .click()
    await expectSegmentAtReadingLine(page, 6, 0)
    await expectNoProgressPut(page, api, READER_SLUG)
  })

  test('Start this version forces exactly one beginning snapshot through the coordinator', async ({
    page,
    api,
  }) => {
    const { oldStory, currentStory } = highConfidenceStories()
    api.setStory(currentStory)
    api.setProgress(
      READER_SLUG,
      updatedProgress(oldStory, 4, 0.4, 0.48),
    )
    const put = api.deferProgressPut(READER_SLUG)

    await gotoReader(page, api, READER_SLUG)
    const dialog = await storyUpdatedDialog(page)
    await expect(dialog).toContainText(
      'Starting this version moves your saved place to its beginning.',
    )
    await dialog.getByRole('button', { name: 'Start this version' }).click()
    const request = await put.started
    expectLocatorV2Request(request, { version: 2, ordinal: 1 })
    expect(request.body).toEqual(
      expect.objectContaining({
        percent: 0,
        locator: expect.objectContaining({
          segment: expect.objectContaining({ ordinal: 1, offset: 0 }),
        }),
      }),
    )
    put.fulfill()
    await expect(page.locator('.reader-save-status')).toContainText('Saved')
    expect(api.progressPuts()).toHaveLength(1)
  })

  test('a failed Start save stays truthful and retryable', async ({
    page,
    api,
  }) => {
    const { oldStory, currentStory } = highConfidenceStories()
    api.setStory(currentStory)
    api.setProgress(
      READER_SLUG,
      updatedProgress(oldStory, 4, 0.4, 0.48),
    )
    api.enqueueProgressPut(READER_SLUG, {
      status: 500,
      body: serverError,
    })
    const retry = api.deferProgressPut(READER_SLUG)

    await gotoReader(page, api, READER_SLUG)
    await (await storyUpdatedDialog(page))
      .getByRole('button', { name: 'Start this version' })
      .click()
    await expect(page.locator('.reader-save-status')).toContainText(
      'Save failed',
    )
    await expect(page.locator('.reader-save-status')).not.toContainText('Saved')
    await page
      .locator('.reader-save-status')
      .getByRole('button', { name: 'Retry' })
      .click()
    const request = await retry.started
    expectLocatorV2Request(request, { version: 2, ordinal: 1 })
    retry.fulfill()
    await expect(page.locator('.reader-save-status')).toContainText('Saved')
    expect(api.progressPuts()).toHaveLength(2)
  })

  test('Return to Library leaves old-version progress untouched', async ({
    page,
    api,
  }) => {
    const { oldStory, currentStory } = highConfidenceStories()
    api.setStory(currentStory)
    api.setProgress(
      READER_SLUG,
      updatedProgress(oldStory, 4, 0.4, 0.48),
    )

    await gotoReader(page, api, READER_SLUG)
    await (await storyUpdatedDialog(page))
      .getByRole('button', { name: 'Return to Library' })
      .click()
    await expect(page).toHaveURL(/\/library$/)
    expect(api.progressPuts()).toHaveLength(0)
  })

  test('Escape follows safe cancellation and returns to Library without writing @paged-core', async ({
    page,
    api,
  }) => {
    await seedReaderPreferences(page)
    const oldStory = makePagedReaderStory({ version: 1 })
    const currentStory = makePagedReaderStory({ version: 2 })
    api.setStory(currentStory)
    api.setProgress(
      READER_SLUG,
      updatedProgress(oldStory, 7, 0, 0.68),
    )

    await gotoReader(page, api, READER_SLUG)
    await expectFocusTrapped(page, 'Story updated')
    await page.keyboard.press('Escape')
    await expect(page).toHaveURL(/\/library$/)
    expect(api.progressPuts()).toHaveLength(0)
  })

  test('a slow old-version baseline opens one decision only after it becomes known', async ({
    page,
    api,
  }) => {
    const { oldStory, currentStory } = highConfidenceStories()
    api.setStory(currentStory)
    api.setProgress(
      READER_SLUG,
      updatedProgress(oldStory, 4, 0.4, 0.48),
    )
    const baseline = api.deferProgressGet(READER_SLUG)

    await page.goto(`/read/${READER_SLUG}`)
    await baseline.started
    await expect(page.locator('[data-reader-scroll-view]')).toBeVisible()
    await expect(page.getByRole('dialog', { name: 'Story updated' })).toBeHidden()
    await expectNoProgressPut(page, api, READER_SLUG)

    baseline.fulfill({ progress: api.progress.get(READER_SLUG) })
    await storyUpdatedDialog(page)
    await expect(page.locator('[role="dialog"]:visible')).toHaveCount(1)
    await expectNoProgressPut(page, api, READER_SLUG)
  })

  test('a progress baseline that arrives before the story waits for the coherent Reader view', async ({
    page,
    api,
  }) => {
    const { oldStory, currentStory } = highConfidenceStories()
    api.setStory(currentStory)
    api.setProgress(
      READER_SLUG,
      updatedProgress(oldStory, 4, 0.4, 0.48),
    )
    const storyGate = api.deferStory(READER_SLUG)

    const navigation = page.goto(`/read/${READER_SLUG}`)
    await storyGate.started
    await expect
      .poll(() => api.count('GET', `/api/v1/progress/${READER_SLUG}`))
      .toBe(1)
    await expect(page.getByRole('dialog', { name: 'Story updated' })).toBeHidden()

    storyGate.fulfill(currentStory)
    await navigation
    await storyUpdatedDialog(page)
    await expectNoProgressPut(page, api, READER_SLUG)
  })

  test('stale old-version baseline and mapped restoration cannot affect a replacement route', async ({
    page,
    api,
  }) => {
    const storyA = highConfidenceStories('updated-story-a')
    const storyB = makeReaderStory({
      slug: 'updated-story-b',
      title: 'TEST ONLY — Replacement story',
      version: 1,
    })
    api.setStory(storyA.currentStory)
    api.setProgress(
      storyA.currentStory.slug,
      updatedProgress(storyA.oldStory, 4, 0.4, 0.48),
    )
    api.setStory(storyB)
    api.setProgress(storyB.slug, null)
    const staleBaseline = api.deferProgressGet(storyA.currentStory.slug)

    await page.goto(`/read/${storyA.currentStory.slug}`)
    await staleBaseline.started
    await page.goto(`/read/${storyB.slug}`)
    staleBaseline.fulfill({
      progress: api.progress.get(storyA.currentStory.slug),
    })
    await expect(page.getByRole('heading', { name: storyB.title })).toBeVisible()
    await expect(page.getByRole('dialog', { name: 'Story updated' })).toBeHidden()
    expect(api.progressPuts()).toHaveLength(0)

    await page.goto(`/read/${storyA.currentStory.slug}`)
    const dialog = await storyUpdatedDialog(page)
    await dialog
      .getByRole('button', { name: 'Continue in the updated story' })
      .dispatchEvent('click')
    await page.goto(`/read/${storyB.slug}`)
    await expect(page.getByRole('heading', { name: storyB.title })).toBeVisible()
    await expect(page.getByRole('dialog', { name: 'Story updated' })).toBeHidden()
    expect(api.progressPuts()).toHaveLength(0)
  })

  test('rapid preference changes while the decision is unresolved cannot write or create two dialogs', async ({
    page,
    api,
  }) => {
    const oldStory = makePagedReaderStory({ version: 1 })
    const currentStory = makePagedReaderStory({ version: 2 })
    api.setStory(currentStory)
    api.setProgress(
      READER_SLUG,
      updatedProgress(oldStory, 7, 0, 0.68),
    )
    const baseline = api.deferProgressGet(READER_SLUG)

    await gotoReader(page, api, READER_SLUG)
    await page.getByRole('button', { name: 'Reading settings' }).click()
    const settings = page.getByRole('dialog', { name: 'Reading settings' })
    await settings.locator('input[name="reader-mode"]').evaluateAll((inputs) => {
      const radios = inputs as HTMLInputElement[]
      for (const value of ['paged', 'scroll', 'paged']) {
        const radio = radios.find((candidate) => candidate.value === value)
        radio?.click()
      }
    })
    baseline.fulfill({ progress: api.progress.get(READER_SLUG) })

    await expect(settings).toBeHidden()
    await storyUpdatedDialog(page)
    await expect(page.locator('[role="dialog"]:visible')).toHaveCount(1)
    await expectNoProgressPut(page, api, READER_SLUG)
  })

  test('presentation-only mode change after Continue does not persist', async ({
    page,
    api,
  }) => {
    const { oldStory, currentStory } = highConfidenceStories()
    api.setStory(currentStory)
    api.setProgress(
      READER_SLUG,
      updatedProgress(oldStory, 4, 0.4, 0.48),
    )

    await gotoReader(page, api, READER_SLUG)
    await (await storyUpdatedDialog(page))
      .getByRole('button', { name: 'Continue in the updated story' })
      .click()
    await page.getByRole('button', { name: 'Reading settings' }).click()
    const settings = page.getByRole('dialog', { name: 'Reading settings' })
    await settings.getByRole('radio', { name: 'Paged' }).check()
    await waitForPagedReady(page)
    await settings.getByRole('button', { name: 'Close' }).click()
    await expectNoProgressPut(page, api, READER_SLUG)
  })

  test('progress 500 and connectivity failures remain unavailable without guessing or writing', async ({
    page,
    api,
  }) => {
    api.enqueueProgressGet(READER_SLUG, {
      status: 500,
      body: serverError,
    })
    await gotoReader(page, api, READER_SLUG)
    await expect(page.locator('.reader-save-status')).toContainText(
      'Progress unavailable',
    )
    await expect(page.getByRole('dialog', { name: 'Story updated' })).toBeHidden()
    await expectNoProgressPut(page, api, READER_SLUG)

    const offlineStory = makeReaderStory({
      slug: 'offline-progress-story',
      title: 'TEST ONLY — Offline progress',
    })
    api.setStory(offlineStory)
    api.enqueueProgressGet(offlineStory.slug, { abort: 'failed' })
    await gotoReader(page, api, offlineStory.slug)
    await expect(page.locator('.reader-save-status')).toContainText(
      'Progress unavailable',
    )
    await expectNoProgressPut(page, api, offlineStory.slug)
  })

  test('progress 401 performs the existing session transition without a decision or write', async ({
    page,
    api,
  }) => {
    api.enqueueProgressGet(READER_SLUG, {
      status: 401,
      body: { error: { code: 'unauthorized', message: 'unlock required' } },
    })

    await page.goto(`/read/${READER_SLUG}`)
    await expect(page).toHaveURL(/\/unlock\?next=/)
    await expect(page.getByRole('dialog', { name: 'Story updated' })).toBeHidden()
    expect(api.progressPuts()).toHaveLength(0)
  })

  test('Story Updated stays centered with visible actions on desktop', async ({
    page,
    api,
  }) => {
    await page.setViewportSize({ width: 1280, height: 720 })
    const { oldStory, currentStory } = highConfidenceStories()
    api.setStory(currentStory)
    api.setProgress(
      READER_SLUG,
      updatedProgress(oldStory, 4, 0.4, 0.48),
    )

    await gotoReader(page, api, READER_SLUG)
    const dialog = await storyUpdatedDialog(page)
    await dialog.evaluate(async (element) => {
      await Promise.allSettled(
        element.getAnimations().map((animation) => animation.finished),
      )
    })
    const geometry = await dialog.evaluate((element) => {
      const rect = element.getBoundingClientRect()
      return {
        top: rect.top,
        left: rect.left,
        right: rect.right,
        bottom: rect.bottom,
        viewportWidth: window.innerWidth,
        viewportHeight: window.innerHeight,
      }
    })
    expect(geometry.top).toBeGreaterThan(0)
    expect(geometry.left).toBeGreaterThan(0)
    expect(geometry.right).toBeLessThan(geometry.viewportWidth)
    expect(geometry.bottom).toBeLessThan(geometry.viewportHeight)
    await expect(dialog.getByRole('button')).toHaveCount(3)
    for (const button of await dialog.getByRole('button').all()) {
      await expect(button).toBeVisible()
    }
  })


  test('Story Updated is accessible and remains inside a short large-text mobile viewport', async ({
    page,
    api,
  }) => {
    await page.setViewportSize({ width: 390, height: 600 })
    const { oldStory, currentStory } = highConfidenceStories()
    api.setStory(currentStory)
    api.setProgress(
      READER_SLUG,
      updatedProgress(oldStory, 4, 0.4, 0.48),
    )

    await gotoReader(page, api, READER_SLUG)
    await page.addStyleTag({ content: 'html { font-size: 150%; }' })
    const dialog = await storyUpdatedDialog(page)
    await dialog.evaluate(async (element) => {
      await Promise.allSettled(
        element.getAnimations().map((animation) => animation.finished),
      )
    })
    const geometry = await dialog.evaluate((element) => {
      const rect = element.getBoundingClientRect()
      return {
        top: rect.top,
        left: rect.left,
        right: rect.right,
        bottom: rect.bottom,
        viewportWidth: window.innerWidth,
        viewportHeight: window.innerHeight,
        scrollable: element.scrollHeight <= element.clientHeight || element.clientHeight > 0,
      }
    })
    expect(geometry.top).toBeGreaterThanOrEqual(0)
    expect(geometry.left).toBeGreaterThanOrEqual(0)
    expect(geometry.right).toBeLessThanOrEqual(geometry.viewportWidth)
    expect(geometry.bottom).toBeLessThanOrEqual(geometry.viewportHeight + 1)
    expect(geometry.scrollable).toBe(true)
    await expectFocusTrapped(page, 'Story updated')
    expect(await seriousOrCriticalViolations(page)).toEqual([])
  })

  test('same-version Resume remains unchanged', async ({ page, api }) => {
    const story = makeReaderStory({ version: 2 })
    api.setStory(story)
    api.setProgress(READER_SLUG, progressFor(story, 5, 0.35, 0.72))

    await gotoReader(page, api, READER_SLUG)
    const resume = page.getByRole('dialog', { name: 'Continue reading?' })
    await expect(resume).toBeVisible()
    await expect(page.getByRole('dialog', { name: 'Story updated' })).toBeHidden()
    await resume.getByRole('button', { name: 'Resume' }).click()
    await expect(resume).toBeHidden()
    await expectSegmentAtReadingLine(page, 5, 0.35)
    await expectNoProgressPut(page, api, READER_SLUG)
  })
})
