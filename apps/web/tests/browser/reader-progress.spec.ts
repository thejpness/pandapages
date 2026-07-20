import {
  expect,
  makeReaderStory,
  progressFor,
  READER_SLUG,
  test,
} from './support/reader-api'
import {
  expectLocatorV2Request,
  expectNoProgressPut,
  expectSegmentAtReadingLine,
  gotoReader,
  scrollToSegment,
  scrollPagedViewportTo,
} from './support/reader-page'

const serverError = {
  error: { code: 'internal_error', message: 'Test-only failure' },
}

test.describe('Reader progress decisions and persistence', () => {
  test('scenario 5: same-version Resume restores the exact segment without an intermediate write', async ({
    page,
    api,
  }) => {
    const story = api.stories.get(READER_SLUG)
    expect(story).toBeDefined()
    if (!story) return
    api.setProgress(READER_SLUG, progressFor(story, 5, 0.4, 0.74))

    await gotoReader(page, api, READER_SLUG)
    const dialog = page.getByRole('dialog', { name: 'Continue reading?' })
    await expect(dialog).toBeVisible()
    await dialog.getByRole('button', { name: 'Resume' }).click()
    await expect(dialog).toBeHidden()
    await expectSegmentAtReadingLine(page, 5, 0.4)
    await expectNoProgressPut(page, api, READER_SLUG)
    await expect(page.locator('.reader-sr-only[role="status"]')).toContainText('Reading place restored.')
  })

  test('scenario 6: Start over writes a beginning Locator v2 and is Saved only after success', async ({
    page,
    api,
  }) => {
    const story = api.stories.get(READER_SLUG)
    expect(story).toBeDefined()
    if (!story) return
    api.setProgress(READER_SLUG, progressFor(story, 5, 0.3, 0.72))
    const put = api.deferProgressPut(READER_SLUG)

    await gotoReader(page, api, READER_SLUG)
    await page.getByRole('dialog', { name: 'Continue reading?' })
      .getByRole('button', { name: 'Start over' })
      .click()
    const request = await put.started
    expectLocatorV2Request(request, { version: story.version, ordinal: 1 })
    await expect(page.locator('.reader-save-status')).toContainText('Saving…')
    await expect(page.locator('.reader-save-status')).not.toContainText('Saved')

    put.fulfill()
    await expect(page.locator('.reader-save-status')).toContainText('Saved')
    expect(api.progressPuts()).toHaveLength(1)
  })

  test('scenario 7: Dismiss leaves the visible location unchanged and does not write', async ({
    page,
    api,
  }) => {
    const story = api.stories.get(READER_SLUG)
    expect(story).toBeDefined()
    if (!story) return
    api.setProgress(READER_SLUG, progressFor(story, 5, 0.3, 0.72))

    await gotoReader(page, api, READER_SLUG)
    const before = await page.evaluate(() => window.scrollY)
    await page.getByRole('dialog', { name: 'Continue reading?' })
      .getByRole('button', { name: 'Dismiss' })
      .click()
    await expect(page.getByRole('dialog', { name: 'Continue reading?' })).toBeHidden()
    expect(await page.evaluate(() => window.scrollY)).toBe(before)
    await expectNoProgressPut(page, api, READER_SLUG)
  })

  test('scenarios 8, 9 and 26: scroll emits Locator v2 and Saved waits for the PUT', async ({
    page,
    api,
  }) => {
    const put = api.deferProgressPut(READER_SLUG)
    await gotoReader(page, api, READER_SLUG)

    await scrollToSegment(page, 4, 0.45)
    const request = await put.started
    expectLocatorV2Request(request, { version: 1, ordinal: 4 })
    await expect(page.locator('.reader-save-status')).toContainText('Saving…')
    await expect(page.locator('.reader-save-status')).not.toContainText('Saved')
    const progressBefore = Number(
      await page.getByRole('progressbar', { name: 'Reading progress' }).getAttribute('aria-valuenow'),
    )
    expect(progressBefore).toBeGreaterThan(0)

    put.fulfill({ ok: true })
    await expect(page.locator('.reader-save-status')).toContainText('Saved')
    expect(api.progressPuts()).toHaveLength(1)
  })

  test('scenario 10: failed persistence stays retryable and cannot claim Saved', async ({
    page,
    api,
  }) => {
    api.enqueueProgressPut(READER_SLUG, { status: 500, body: serverError })
    const retry = api.deferProgressPut(READER_SLUG)
    await gotoReader(page, api, READER_SLUG)

    await scrollToSegment(page, 4, 0.5)
    await expect(page.locator('.reader-save-status')).toContainText('Save failed')
    await expect(page.locator('.reader-save-status')).not.toContainText('Saved')
    await page.locator('.reader-save-status').getByRole('button', { name: 'Retry' }).click()
    const request = await retry.started
    expectLocatorV2Request(request, { ordinal: 4 })
    await expect(page.locator('.reader-save-status')).not.toContainText('Saved')
    retry.fulfill({ ok: true })
    await expect(page.locator('.reader-save-status')).toContainText('Saved')
    expect(api.progressPuts()).toHaveLength(2)
  })

  test('scenario 11: a 500 progress baseline leaves the story readable and never writes', async ({
    page,
    api,
  }) => {
    api.enqueueProgressGet(READER_SLUG, { status: 500, body: serverError })
    await gotoReader(page, api, READER_SLUG)
    await expect(page.locator('[data-reader-scroll-view]')).toBeVisible()
    await expect(page.locator('.reader-save-status')).toContainText('Progress unavailable')

    await scrollToSegment(page, 4, 0.5)
    await page.evaluate(() => window.dispatchEvent(new Event('pagehide')))
    await expectNoProgressPut(page, api, READER_SLUG)
  })

  test('scenario 12: baseline Retry enables exactly one current-position save only after GET succeeds', async ({
    page,
    api,
  }) => {
    api.enqueueProgressGet(READER_SLUG, { status: 500, body: serverError })
    const baselineRetry = api.deferProgressGet(READER_SLUG)
    const put = api.deferProgressPut(READER_SLUG)
    await gotoReader(page, api, READER_SLUG)
    await expect(page.locator('.reader-save-status')).toContainText('Progress unavailable')

    await scrollToSegment(page, 4, 0.45)
    await expectNoProgressPut(page, api, READER_SLUG)
    await page.locator('.reader-save-status').getByRole('button', { name: 'Retry' }).click()
    await baselineRetry.started
    await expect(page.locator('.reader-save-status')).toContainText('Checking progress…')
    expect(api.progressPuts()).toHaveLength(0)

    baselineRetry.fulfill({ progress: null })
    const request = await put.started
    expectLocatorV2Request(request, { ordinal: 4 })
    await expect(page.locator('.reader-save-status')).not.toContainText('Saved')
    put.fulfill({ ok: true })
    await expect(page.locator('.reader-save-status')).toContainText('Saved')
    expect(api.progressPuts()).toHaveLength(1)
  })

  test('slow first baseline preserves an explicit resume decision and does not overwrite it', async ({
    page,
    api,
  }) => {
    const story = api.stories.get(READER_SLUG)
    expect(story).toBeDefined()
    if (!story) return
    const baseline = api.deferProgressGet(READER_SLUG)

    await page.goto(`/read/${READER_SLUG}`)
    await expect(page.locator('[data-reader-scroll-view]')).toBeVisible()
    await baseline.started
    await scrollToSegment(page, 4, 0.45)
    await expectNoProgressPut(page, api, READER_SLUG)

    baseline.fulfill({ progress: progressFor(story, 5, 0.3, 0.72) })
    await expect(page.getByRole('dialog', { name: 'Continue reading?' })).toBeVisible()
    await expectNoProgressPut(page, api, READER_SLUG)
  })

  test('scenario 23: old-version progress shows the changed-story boundary and cannot write until started', async ({
    page,
    api,
  }) => {
    const story = makeReaderStory({ version: 2 })
    api.setStory(story)
    api.setProgress(READER_SLUG, progressFor(story, 5, 0.3, 0.72, 1))
    const put = api.deferProgressPut(READER_SLUG)

    await gotoReader(page, api, READER_SLUG)
    const dialog = page.getByRole('dialog', { name: 'Story updated' })
    await expect(dialog).toBeVisible()
    await expect(dialog.getByRole('button', { name: 'Resume' })).toHaveCount(0)
    await expectNoProgressPut(page, api, READER_SLUG)

    await dialog.getByRole('button', { name: 'Start this version' }).click()
    const request = await put.started
    expectLocatorV2Request(request, { version: 2, ordinal: 1 })
    put.fulfill({ ok: true })
    await expect(page.locator('.reader-save-status')).toContainText('Saved')
  })

  test('scenario 25: reduced-motion resume never requests smooth scrolling', async ({
    page,
    api,
  }) => {
    const story = api.stories.get(READER_SLUG)
    expect(story).toBeDefined()
    if (!story) return
    api.setProgress(READER_SLUG, progressFor(story, 5, 0.25, 0.7))
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.addInitScript(() => {
      const target = window as Window & { __readerScrollBehaviors?: string[] }
      const original = window.scrollTo.bind(window)
      target.__readerScrollBehaviors = []
      window.scrollTo = (
        optionsOrX?: ScrollToOptions | number,
        y?: number,
      ) => {
        if (typeof optionsOrX === 'number') {
          original(optionsOrX, y ?? 0)
          return
        }
        if (optionsOrX?.behavior) {
          target.__readerScrollBehaviors?.push(optionsOrX.behavior)
        }
        original(optionsOrX)
      }
    })

    await gotoReader(page, api, READER_SLUG)
    await page.getByRole('dialog', { name: 'Continue reading?' })
      .getByRole('button', { name: 'Resume' })
      .click()
    await expect(page.getByRole('dialog', { name: 'Continue reading?' })).toBeHidden()
    const behaviors = await page.evaluate(
      () => (window as Window & { __readerScrollBehaviors?: string[] }).__readerScrollBehaviors ?? [],
    )
    expect(behaviors).not.toContain('smooth')
    expect(behaviors).toContain('auto')
  })

  test('scenario 27: paged mode loads coherently and emits no Reader 1 locator', async ({
    page,
    api,
  }) => {
    await page.addInitScript(() => {
      localStorage.setItem(
        'pp_reader_prefs_v2',
        JSON.stringify({
          schema: 2,
          mode: 'paged',
          theme: 'night',
          fontFamily: 'book',
          fontSize: 20,
          lineHeight: 1.65,
          contentWidth: 720,
        }),
      )
    })
    const put = api.deferProgressPut(READER_SLUG)
    await gotoReader(page, api, READER_SLUG)
    const paged = page.locator('.reader-paged-view')
    await expect(paged).toBeVisible()
    const expectedOrdinal = Number(
      await page.locator('[data-reader-page-index="1"]')
        .getAttribute('data-reader-page-start-ordinal'),
    )

    await scrollPagedViewportTo(page, 2)
    const request = await put.started
    expectLocatorV2Request(request, { ordinal: expectedOrdinal })
    put.fulfill({ ok: true })
    expect(api.count('GET', `/api/v1/reader/${READER_SLUG}`)).toBe(1)
  })
})
