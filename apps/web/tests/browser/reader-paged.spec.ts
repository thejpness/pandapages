import type { Page } from '@playwright/test'
import {
  expect,
  makeOversizedReaderStory,
  makePagedReaderStory,
  makeReaderStory,
  progressFor,
  READER_SLUG,
  test,
} from './support/reader-api'
import {
  beginPagedAnnouncementCapture,
  currentPagedOrdinalRange,
  currentReaderPage,
  expectCurrentPageContainsOrdinal,
  expectLocatorV2Request,
  expectNoProgressPut,
  forceReaderScrollEndFallback,
  gotoReader,
  nextReaderPage,
  pagedReader,
  pagedViewport,
  pagedAnnouncementHistory,
  previousReaderPage,
  readerPageCount,
  scrollOversizedPageTo,
  seedReaderPreferences,
  waitForPagedReady,
  waitForReaderPage,
  wheelPagedViewport,
} from './support/reader-page'

async function chooseMode(page: Page, mode: 'Scroll' | 'Paged') {
  await page.getByRole('button', { name: 'Reading settings' }).click()
  const dialog = page.getByRole('dialog', { name: 'Reading settings' })
  await dialog.getByRole('radio', { name: mode }).check()
  await dialog.getByRole('button', { name: 'Close' }).click()
  await expect(dialog).toBeHidden()
  await expect(
    page.locator('[data-reader-preference-pending]'),
  ).toHaveAttribute('data-reader-preference-pending', 'false')
  const targetMode = mode === 'Scroll' ? 'scroll' : 'paged'
  await expect(
    page.locator(`[data-reader-view-mode="${targetMode}"]`),
  ).toBeVisible()
}

async function scrollWebKitPagedViewportTo(
  page: Page,
  pageNumber: number,
): Promise<void> {
  const viewport = pagedViewport(page)
  await expect
    .poll(() =>
      viewport.evaluate(
        (element, targetPage) =>
          new Promise<boolean>((resolve) => {
            const reader = element.closest('[data-reader-paged-view]')
            const firstWidth = element.clientWidth
            const firstCount = Number(reader?.getAttribute('data-reader-page-count'))
            requestAnimationFrame(() => {
              const secondWidth = element.clientWidth
              const secondCount = Number(
                reader?.getAttribute('data-reader-page-count'),
              )
              resolve(
                firstWidth > 0 &&
                  firstWidth === secondWidth &&
                  firstCount >= Number(targetPage) &&
                  firstCount === secondCount,
              )
            })
          }),
        pageNumber,
      ),
    )
    .toBe(true)

  const applyExternalScroll = async () => {
    await viewport.evaluate((element, targetPage) => {
      element.scrollLeft = (Number(targetPage) - 1) * element.clientWidth
      element.dispatchEvent(new Event('scroll'))
      if ('onscrollend' in element) {
        element.dispatchEvent(new Event('scrollend'))
      }
    }, pageNumber)
  }
  const settlesOnTarget = async (): Promise<boolean> => {
    await applyExternalScroll()
    try {
      await expect
        .poll(() => currentReaderPage(page), { timeout: 3_000 })
        .toBe(pageNumber)
      return true
    } catch {
      return false
    }
  }

  if (await settlesOnTarget()) return
  await applyExternalScroll()
  await expect
    .poll(() => currentReaderPage(page), { timeout: 3_000 })
    .toBe(pageNumber)
}

test.describe('Reader paged reading', () => {
  test(
    'initial paged preference builds a deterministic complete page model',
    { tag: '@paged-core' },
    async ({ page, api }) => {
      await page.setViewportSize({ width: 900, height: 720 })
      await seedReaderPreferences(page)
      const story = makePagedReaderStory()
      api.setStory(story)

      await gotoReader(page, api, READER_SLUG)
      await waitForPagedReady(page)

      expect(await readerPageCount(page)).toBe(10)
      await expect(pagedReader(page)).toHaveAttribute('data-reader-current-page', '1')
      const segments = pagedReader(page).locator('[data-reader-paged-segment]')
      await expect(segments).toHaveCount(story.segments.length)
      expect(
        await segments.evaluateAll((elements) =>
          elements.map((element) =>
            Number(element.getAttribute('data-reader-segment-ordinal')),
          ),
        ),
      ).toEqual(story.segments.map(({ ordinal }) => ordinal))

      const pages = pagedReader(page).locator('[data-reader-page-index]')
      await expect(pages).toHaveCount(10)
      await expect(
        pagedReader(page).locator(
          '[data-reader-page-index][data-reader-page-current="true"]',
        ),
      ).toHaveCount(1)
      await expect(
        page.getByRole('navigation', { name: 'Page navigation' }),
      ).toBeVisible()
      await expect(page.getByRole('button', { name: 'Previous page' })).toBeDisabled()
      await expect(page.getByRole('button', { name: 'Next page' })).toBeEnabled()
      await expect(page.getByText('Page 1 of 10', { exact: true })).toBeVisible()
      expect(api.legacyRequests).toEqual([])
    },
  )

  test(
    'Previous and Next settle once and persist semantic Locator v2 progress',
    { tag: '@paged-core' },
    async ({ page, api }) => {
      await page.setViewportSize({ width: 900, height: 720 })
      await seedReaderPreferences(page)
      const story = makeReaderStory()
      api.setStory(story)
      const firstPut = api.deferProgressPut(READER_SLUG)

      await gotoReader(page, api, READER_SLUG)
      const count = await readerPageCount(page)
      expect(count).toBeGreaterThan(2)
      await nextReaderPage(page)
      const range = await currentPagedOrdinalRange(page)
      const request = await firstPut.started
      expectLocatorV2Request(request, { ordinal: range.start })
      const body = request.body as { percent: number }
      expect(Math.abs(body.percent - 1 / count)).toBeGreaterThan(0.05)
      expect(api.progressPuts()).toHaveLength(1)
      firstPut.fulfill({ ok: true })
      await expect(page.locator('.reader-save-status')).toContainText('Saved')

      await previousReaderPage(page)
      await expect(page.getByRole('button', { name: 'Previous page' })).toBeDisabled()
      await expect(page.getByRole('button', { name: 'Next page' })).toBeEnabled()
      await expect.poll(() => api.progressPuts().length).toBe(2)
    },
  )

  test('keyboard paging uses one bounded transition path', async ({ page, api }) => {
    await seedReaderPreferences(page)
    api.setStory(makePagedReaderStory())
    await gotoReader(page, api, READER_SLUG)
    const finalPage = await readerPageCount(page)

    await page.keyboard.press('ArrowRight')
    await waitForReaderPage(page, 2)
    await page.keyboard.press('ArrowLeft')
    await waitForReaderPage(page, 1)
    await page.keyboard.press('PageDown')
    await waitForReaderPage(page, 2)
    await page.keyboard.press('PageUp')
    await waitForReaderPage(page, 1)
    await page.keyboard.press('End')
    await waitForReaderPage(page, finalPage)
    await expect(page.getByRole('button', { name: 'Next page' })).toBeDisabled()
    await page.keyboard.press('End')
    await waitForReaderPage(page, finalPage)
    await page.keyboard.press('Home')
    await waitForReaderPage(page, 1)
    await page.keyboard.press('Home')
    await waitForReaderPage(page, 1)
  })

  test('keyboard navigation is ignored inside Reading settings controls', async ({
    page,
    api,
  }) => {
    await seedReaderPreferences(page)
    api.setStory(makePagedReaderStory())
    await gotoReader(page, api, READER_SLUG)
    await page.getByRole('button', { name: 'Reading settings' }).click()
    const dialog = page.getByRole('dialog', { name: 'Reading settings' })
    const textSize = dialog.getByRole('slider', { name: 'Text size' })
    await textSize.focus()
    await page.keyboard.press('End')
    await expect(textSize).toHaveValue('32')
    expect(await currentReaderPage(page)).toBe(1)
  })

  test(
    'horizontal page input settles to one page and one progress update',
    { tag: '@paged-core' },
    async ({ page, api, browserName }) => {
      await seedReaderPreferences(page)
      api.setStory(makeReaderStory())
      const put = api.deferProgressPut(READER_SLUG)
      await gotoReader(page, api, READER_SLUG)

      if (browserName === 'webkit') {
        // Playwright mobile WebKit exposes tap but neither a swipe API nor
        // mouse.wheel. Exercise WebKit's native overflow/snap settling with an
        // external scroll instead; Chromium has the trusted CDP touch case.
        await scrollWebKitPagedViewportTo(page, 2)
      } else {
        await wheelPagedViewport(page, 1)
      }
      await waitForReaderPage(page, 2)
      const request = await put.started
      const range = await currentPagedOrdinalRange(page)
      expectLocatorV2Request(request, { ordinal: range.start })
      expect(api.progressPuts()).toHaveLength(1)
      put.fulfill({ ok: true })
    },
  )

  test('Chromium native touch swipe settles through horizontal scroll snapping', async ({
    page,
    api,
    context,
    browserName,
  }) => {
    test.skip(browserName !== 'chromium', 'CDP trusted touch input is Chromium-only')
    await seedReaderPreferences(page)
    api.setStory(makeReaderStory())
    const put = api.deferProgressPut(READER_SLUG)
    await gotoReader(page, api, READER_SLUG)
    await beginPagedAnnouncementCapture(page)

    const bounds = await pagedViewport(page).boundingBox()
    expect(bounds).not.toBeNull()
    if (!bounds) return
    const cdp = await context.newCDPSession(page)
    await cdp.send('Emulation.setTouchEmulationEnabled', {
      enabled: true,
      maxTouchPoints: 1,
    })
    const y = bounds.y + bounds.height / 2
    const startX = bounds.x + bounds.width * 0.82
    const endX = bounds.x + bounds.width * 0.18
    await cdp.send('Input.dispatchTouchEvent', {
      type: 'touchStart',
      touchPoints: [{ x: startX, y }],
    })
    for (let step = 1; step <= 8; step += 1) {
      const x = startX + ((endX - startX) * step) / 8
      await cdp.send('Input.dispatchTouchEvent', {
        type: 'touchMove',
        touchPoints: [{ x, y }],
      })
      await page.evaluate(
        () => new Promise<void>((resolve) => requestAnimationFrame(() => resolve())),
      )
    }
    await cdp.send('Input.dispatchTouchEvent', {
      type: 'touchEnd',
      touchPoints: [],
    })

    await waitForReaderPage(page, 2)
    const request = await put.started
    expectLocatorV2Request(request, {
      ordinal: (await currentPagedOrdinalRange(page)).start,
    })
    expect(api.progressPuts()).toHaveLength(1)
    await expect
      .poll(async () =>
        (await pagedAnnouncementHistory(page)).filter((value) =>
          value.startsWith('Page 2 of '),
        ),
      )
      .toHaveLength(1)
    put.fulfill({ ok: true })
  })

  test('trusted touch can cross the midpoint, hold, and return without publishing', async ({
    page,
    api,
    context,
    browserName,
  }) => {
    test.skip(browserName !== 'chromium', 'CDP trusted touch input is Chromium-only')
    await seedReaderPreferences(page)
    api.setStory(makeReaderStory())
    await gotoReader(page, api, READER_SLUG)
    await beginPagedAnnouncementCapture(page)

    const viewport = pagedViewport(page)
    const bounds = await viewport.boundingBox()
    expect(bounds).not.toBeNull()
    if (!bounds) return
    const cdp = await context.newCDPSession(page)
    await cdp.send('Emulation.setTouchEmulationEnabled', {
      enabled: true,
      maxTouchPoints: 1,
    })
    const y = bounds.y + bounds.height / 2
    const startX = bounds.x + bounds.width * 0.82
    const crossedX = bounds.x + bounds.width * 0.18
    await cdp.send('Input.dispatchTouchEvent', {
      type: 'touchStart',
      touchPoints: [{ x: startX, y }],
    })
    for (let step = 1; step <= 8; step += 1) {
      const x = startX + ((crossedX - startX) * step) / 8
      await cdp.send('Input.dispatchTouchEvent', {
        type: 'touchMove',
        touchPoints: [{ x, y }],
      })
      await page.evaluate(
        () => new Promise<void>((resolve) => requestAnimationFrame(() => resolve())),
      )
    }
    await expect
      .poll(() =>
        viewport.evaluate(
          (element) => element.scrollLeft / Math.max(1, element.clientWidth),
        ),
      )
      .toBeGreaterThan(0.5)

    await page.waitForTimeout(220)
    expect(await currentReaderPage(page)).toBe(1)
    expect(api.progressPuts()).toHaveLength(0)
    expect(
      (await pagedAnnouncementHistory(page)).some((value) =>
        value.startsWith('Page 2 of '),
      ),
    ).toBe(false)

    for (let step = 1; step <= 8; step += 1) {
      const x = crossedX + ((startX - crossedX) * step) / 8
      await cdp.send('Input.dispatchTouchEvent', {
        type: 'touchMove',
        touchPoints: [{ x, y }],
      })
      await page.evaluate(
        () => new Promise<void>((resolve) => requestAnimationFrame(() => resolve())),
      )
    }
    await expect
      .poll(() =>
        viewport.evaluate(
          (element) => element.scrollLeft / Math.max(1, element.clientWidth),
        ),
      )
      .toBeLessThan(0.2)
    await cdp.send('Input.dispatchTouchEvent', {
      type: 'touchEnd',
      touchPoints: [],
    })

    await waitForReaderPage(page, 1)
    await expectNoProgressPut(page, api, READER_SLUG)
    expect(
      (await pagedAnnouncementHistory(page)).some((value) =>
        value.startsWith('Page 2 of '),
      ),
    ).toBe(false)
  })

  test('quiet fallback settles wheel input when scrollend is unavailable', async ({
    page,
    api,
  }) => {
    await forceReaderScrollEndFallback(page)
    await seedReaderPreferences(page)
    api.setStory(makeReaderStory())
    const put = api.deferProgressPut(READER_SLUG)
    await gotoReader(page, api, READER_SLUG)

    const viewport = pagedViewport(page)
    expect(await viewport.evaluate((element) => 'onscrollend' in element)).toBe(false)
    await wheelPagedViewport(page, 1)
    await waitForReaderPage(page, 2)

    const request = await put.started
    expectLocatorV2Request(request, {
      ordinal: (await currentPagedOrdinalRange(page)).start,
    })
    expect(api.progressPuts()).toHaveLength(1)
    put.fulfill({ ok: true })
  })

  test('quiet fallback defers active pointer scroll until cancellation', async ({
    page,
    api,
  }) => {
    await forceReaderScrollEndFallback(page)
    await seedReaderPreferences(page)
    api.setStory(makeReaderStory())
    const put = api.deferProgressPut(READER_SLUG)
    await gotoReader(page, api, READER_SLUG)
    await beginPagedAnnouncementCapture(page)

    const viewport = pagedViewport(page)
    await viewport.evaluate((element) => {
      element.dispatchEvent(
        new PointerEvent('pointerdown', {
          bubbles: true,
          isPrimary: true,
          pointerId: 47,
          pointerType: 'touch',
        }),
      )
      element.scrollLeft = element.clientWidth
      element.dispatchEvent(new Event('scroll'))
    })
    await page.waitForTimeout(220)
    expect(await currentReaderPage(page)).toBe(1)
    expect(api.progressPuts()).toHaveLength(0)
    expect(await pagedAnnouncementHistory(page)).toEqual([])

    await viewport.evaluate((element) => {
      element.dispatchEvent(
        new PointerEvent('pointercancel', {
          bubbles: true,
          isPrimary: true,
          pointerId: 47,
          pointerType: 'touch',
        }),
      )
    })
    await waitForReaderPage(page, 2)

    const request = await put.started
    expectLocatorV2Request(request, {
      ordinal: (await currentPagedOrdinalRange(page)).start,
    })
    expect(api.progressPuts()).toHaveLength(1)
    await expect
      .poll(async () =>
        (await pagedAnnouncementHistory(page)).filter((value) =>
          value.startsWith('Page 2 of '),
        ),
      )
      .toHaveLength(1)
    put.fulfill({ ok: true })
  })

  test('a partial horizontal gesture returning to the page does not save', async ({
    page,
    api,
  }) => {
    await seedReaderPreferences(page)
    api.setStory(makeReaderStory())
    await gotoReader(page, api, READER_SLUG)
    const viewport = pagedViewport(page)
    await viewport.hover()
    const width = await viewport.evaluate((element) => element.clientWidth)
    await page.mouse.wheel(width * 0.28, 0)
    await page.mouse.wheel(-width * 0.28, 0)

    await expectNoProgressPut(page, api, READER_SLUG)
    await waitForReaderPage(page, 1)
  })

  test(
    'same-version paged Resume restores identity and offset without saving',
    { tag: '@paged-core' },
    async ({ page, api }) => {
      await seedReaderPreferences(page)
      const story = makeReaderStory()
      api.setStory(story)
      api.setProgress(READER_SLUG, progressFor(story, 4, 0.4, 0.55))

      await gotoReader(page, api, READER_SLUG)
      await page.getByRole('dialog', { name: 'Continue reading?' })
        .getByRole('button', { name: 'Resume' })
        .click()

      await expectCurrentPageContainsOrdinal(page, 4)
      await expect(page.getByRole('dialog', { name: 'Continue reading?' })).toBeHidden()
      await expectNoProgressPut(page, api, READER_SLUG)
      await expect(page.locator('.reader-sr-only[role="status"]').last()).toContainText(
        'Reading place restored.',
      )
    },
  )

  test('paged Start over saves only the canonical beginning locator', async ({
    page,
    api,
  }) => {
    await seedReaderPreferences(page)
    const story = makePagedReaderStory()
    api.setStory(story)
    api.setProgress(READER_SLUG, progressFor(story, 7, 0.3, 0.7))
    const put = api.deferProgressPut(READER_SLUG)

    await gotoReader(page, api, READER_SLUG)
    await page.getByRole('dialog', { name: 'Continue reading?' })
      .getByRole('button', { name: 'Start over' })
      .click()

    await waitForReaderPage(page, 1)
    const request = await put.started
    expectLocatorV2Request(request, { ordinal: 1 })
    expect(api.progressPuts()).toHaveLength(1)
    put.fulfill({ ok: true })
  })

  test('paged Dismiss leaves the visible opening page unchanged', async ({
    page,
    api,
  }) => {
    await seedReaderPreferences(page)
    const story = makePagedReaderStory()
    api.setStory(story)
    api.setProgress(READER_SLUG, progressFor(story, 7, 0.3, 0.7))

    await gotoReader(page, api, READER_SLUG)
    await page.getByRole('dialog', { name: 'Continue reading?' })
      .getByRole('button', { name: 'Dismiss' })
      .click()

    await waitForReaderPage(page, 1)
    await expectNoProgressPut(page, api, READER_SLUG)
  })

  test('paged chapter selection distinguishes duplicate names and saves its H2', async ({
    page,
    api,
  }) => {
    await seedReaderPreferences(page)
    api.setStory(makePagedReaderStory())
    const put = api.deferProgressPut(READER_SLUG)
    await gotoReader(page, api, READER_SLUG)

    const trigger = page.getByRole('button', { name: 'Chapters' })
    await trigger.click()
    const dialog = page.getByRole('dialog', { name: 'Chapters' })
    await expect(
      dialog.getByRole('button', { name: 'Moonlit Return, 1 of 2' }),
    ).toBeVisible()
    await dialog.getByRole('button', { name: 'Moonlit Return, 2 of 2' }).click()

    await expect(dialog).toBeHidden()
    await expect(trigger).toBeFocused()
    await expectCurrentPageContainsOrdinal(page, 6)
    const request = await put.started
    expectLocatorV2Request(request, { ordinal: 6 })
    expect(request.body).toEqual(
      expect.objectContaining({
        locator: expect.objectContaining({
          chapter: expect.objectContaining({ occurrence: 2 }),
        }),
      }),
    )
    put.fulfill({ ok: true })
  })

  test(
    'portrait-to-landscape reflow preserves the resumed content anchor',
    { tag: '@paged-core' },
    async ({ page, api }) => {
      await page.setViewportSize({ width: 390, height: 720 })
      await seedReaderPreferences(page)
      const story = makeReaderStory()
      api.setStory(story)
      api.setProgress(READER_SLUG, progressFor(story, 4, 0.4, 0.55))
      await gotoReader(page, api, READER_SLUG)
      await page.getByRole('dialog', { name: 'Continue reading?' })
        .getByRole('button', { name: 'Resume' })
        .click()
      await expectCurrentPageContainsOrdinal(page, 4)

      await page.setViewportSize({ width: 720, height: 390 })
      await expect
        .poll(() => pagedViewport(page).evaluate((element) => element.clientWidth))
        .toBeGreaterThan(500)
      await expectNoProgressPut(page, api, READER_SLUG)
      await expectCurrentPageContainsOrdinal(page, 4)
    },
  )

  test('typography and width reflow preserve chapter identity without extra writes', async ({
    page,
    api,
  }) => {
    await seedReaderPreferences(page)
    api.setStory(makePagedReaderStory())
    const chapterPut = api.deferProgressPut(READER_SLUG)
    await gotoReader(page, api, READER_SLUG)
    await page.getByRole('button', { name: 'Chapters' }).click()
    await page.getByRole('dialog', { name: 'Chapters' })
      .getByRole('button', { name: 'Moonlit Return, 2 of 2' })
      .click()
    const request = await chapterPut.started
    expectLocatorV2Request(request, { ordinal: 6 })
    chapterPut.fulfill({ ok: true })
    await expect(page.locator('.reader-save-status')).toContainText('Saved')

    const writes = api.progressPuts().length
    await page.getByRole('button', { name: 'Reading settings' }).click()
    const settings = page.getByRole('dialog', { name: 'Reading settings' })
    await settings.getByRole('radio', { name: 'Clear' }).check()
    await settings.getByRole('slider', { name: 'Text size' }).fill('30')
    await settings.getByRole('slider', { name: 'Line height' }).fill('2')
    await settings.getByRole('slider', { name: 'Content width' }).fill('900')
    await settings.getByRole('button', { name: 'Close' }).click()

    await expectCurrentPageContainsOrdinal(page, 6)
    await expectNoProgressPut(page, api, READER_SLUG)
    expect(api.progressPuts()).toHaveLength(writes)
  })

  test('oversized content scrolls, saves an offset, and restores it', async ({
    page,
    api,
  }) => {
    await page.setViewportSize({ width: 390, height: 720 })
    await seedReaderPreferences(page)
    const story = makeOversizedReaderStory()
    api.setStory(story)
    await gotoReader(page, api, READER_SLUG)

    await nextReaderPage(page)
    const oversized = pagedReader(page).locator(
      '[data-reader-page-current="true"][data-reader-page-oversized="true"]',
    )
    await expect(oversized).toHaveCount(1)
    expect(
      await oversized.evaluate(
        (element) => element.scrollHeight > element.clientHeight,
      ),
    ).toBe(true)
    await expect.poll(() => api.progressPuts().length).toBe(1)

    const verticalPut = api.deferProgressPut(READER_SLUG)
    await scrollOversizedPageTo(page, 0.55)
    const request = await verticalPut.started
    expectLocatorV2Request(request, { ordinal: 2 })
    const body = request.body as {
      locator: { segment: { offset: number } }
      percent: number
    }
    expect(body.locator.segment.offset).toBeCloseTo(0.55, 1)
    expect(body.percent).toBeGreaterThan(0.1)
    verticalPut.fulfill({ ok: true })
    await expect(page.locator('.reader-save-status')).toContainText('Saved')

    const writes = api.progressPuts().length
    await page.reload()
    await waitForPagedReady(page)
    await page.getByRole('dialog', { name: 'Continue reading?' })
      .getByRole('button', { name: 'Resume' })
      .click()
    await expectCurrentPageContainsOrdinal(page, 2)
    await expect
      .poll(() =>
        pagedReader(page)
          .locator('[data-reader-page-current="true"]')
          .evaluate((element) => {
            const maximum = Math.max(0, element.scrollHeight - element.clientHeight)
            return maximum > 0 ? element.scrollTop / maximum : 0
          }),
      )
      .toBeCloseTo(0.55, 1)
    await expectNoProgressPut(page, api, READER_SLUG)
    expect(api.progressPuts()).toHaveLength(writes)
  })

  test('baseline unavailability permits paging and recovery saves no beginning locator', async ({
    page,
    api,
  }) => {
    await seedReaderPreferences(page)
    api.setStory(makeReaderStory())
    api.enqueueProgressGet(READER_SLUG, {
      status: 500,
      body: { error: { code: 'internal_error', message: 'Unavailable' } },
    })
    const recovered = api.deferProgressGet(READER_SLUG)
    const put = api.deferProgressPut(READER_SLUG)
    await gotoReader(page, api, READER_SLUG)

    await nextReaderPage(page)
    const range = await currentPagedOrdinalRange(page)
    await expectNoProgressPut(page, api, READER_SLUG)
    await page.locator('.reader-save-status').getByRole('button', { name: 'Retry' }).click()
    await recovered.started
    expect(api.progressPuts()).toHaveLength(0)
    recovered.fulfill({ progress: null })

    const request = await put.started
    expectLocatorV2Request(request, { ordinal: range.start })
    expect(range.start).not.toBe(1)
    put.fulfill({ ok: true })
  })

  test('reduced motion uses immediate page restoration', async ({ page, api }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.addInitScript(() => {
      const target = window as Window & { __readerElementScrollBehaviors?: string[] }
      const originalScrollTo = Object.getOwnPropertyDescriptor(
        Element.prototype,
        'scrollTo',
      )?.value as
        | ((this: Element, options?: ScrollToOptions | number, y?: number) => void)
        | undefined
      if (!originalScrollTo) return
      target.__readerElementScrollBehaviors = []
      HTMLElement.prototype.scrollTo = function (
        optionsOrX?: ScrollToOptions | number,
        y?: number,
      ) {
        if (typeof optionsOrX !== 'number' && optionsOrX?.behavior) {
          target.__readerElementScrollBehaviors?.push(optionsOrX.behavior)
        }
        if (typeof optionsOrX === 'number') {
          originalScrollTo.call(this, optionsOrX, y ?? 0)
        } else {
          originalScrollTo.call(this, optionsOrX)
        }
      }
    })
    await seedReaderPreferences(page)
    api.setStory(makeReaderStory())
    await gotoReader(page, api, READER_SLUG)
    await nextReaderPage(page)

    const behaviors = await page.evaluate(
      () =>
        (window as Window & { __readerElementScrollBehaviors?: string[] })
          .__readerElementScrollBehaviors ?? [],
    )
    expect(behaviors).toContain('auto')
    expect(behaviors).not.toContain('smooth')
  })

  test('a new paged route resets presentation state without stale content', async ({
    page,
    api,
  }) => {
    await seedReaderPreferences(page)
    const storyA = makePagedReaderStory({
      slug: 'paged-story-a',
      title: 'TEST ONLY — Paged Story A',
    })
    const storyB = makePagedReaderStory({
      slug: 'paged-story-b',
      title: 'TEST ONLY — Paged Story B',
    })
    api.setStory(storyA)
    api.setStory(storyB)

    await gotoReader(page, api, storyA.slug)
    await nextReaderPage(page)
    await page.goto('/read/' + storyB.slug)
    await waitForPagedReady(page)

    await waitForReaderPage(page, 1)
    await expect(page.getByRole('heading', { level: 1, name: storyB.title })).toBeVisible()
    await expect(page.getByText(storyA.title, { exact: true })).toHaveCount(0)
  })

  test(
    'paged to scroll and back preserves the same segment without a beginning write',
    { tag: '@paged-core' },
    async ({ page, api }) => {
      await seedReaderPreferences(page)
      const story = makeReaderStory()
      api.setStory(story)
      api.setProgress(READER_SLUG, progressFor(story, 4, 0.4, 0.55))
      await gotoReader(page, api, READER_SLUG)
      await page.getByRole('dialog', { name: 'Continue reading?' })
        .getByRole('button', { name: 'Resume' })
        .click()
      await expectCurrentPageContainsOrdinal(page, 4)

      await chooseMode(page, 'Scroll')
      await expect(page.locator(
        '[data-reader-scroll-segment][data-reader-segment-ordinal="4"]',
      )).toBeVisible()
      const canonicalScrollAnchor = await page.locator('[data-reader-scroll-view]').evaluate((view) => {
        const headerBottom = document.querySelector<HTMLElement>('[data-reader-header]')?.getBoundingClientRect().bottom ?? 0
        const readingLine = headerBottom + (window.innerHeight - headerBottom) * 0.35
        const segment = [...view.querySelectorAll<HTMLElement>('[data-reader-scroll-segment]')].find((candidate) => {
          const rect = candidate.getBoundingClientRect()
          return rect.top <= readingLine && rect.bottom >= readingLine
        })
        if (!segment) throw new Error('No canonical scroll anchor')
        const rect = segment.getBoundingClientRect()
        return { ordinal: Number(segment.dataset.readerSegmentOrdinal), offset: (readingLine - rect.top) / Math.max(1, rect.height) }
      })
      await chooseMode(page, 'Paged')
      await expectCurrentPageContainsOrdinal(page, canonicalScrollAnchor.ordinal)
      await expectNoProgressPut(page, api, READER_SLUG)
    },
  )

  test('a delayed meaningful baseline defers Resume until Settings closes', async ({
    page,
    api,
  }) => {
    await seedReaderPreferences(page)
    const story = makePagedReaderStory()
    api.setStory(story)
    const baseline = api.deferProgressGet(READER_SLUG)

    await gotoReader(page, api, READER_SLUG)
    await baseline.started
    await page.getByRole('button', { name: 'Reading settings' }).click()
    const settings = page.getByRole('dialog', { name: 'Reading settings' })
    const resume = page.getByRole('dialog', { name: 'Continue reading?' })
    await expect(settings).toBeVisible()
    await expect(page.locator('[role="dialog"]:visible')).toHaveCount(1)

    baseline.fulfill({
      progress: progressFor(story, 7, 0.35, 0.72),
    })
    await expect(settings).toBeVisible()
    await expect(resume).toBeHidden()
    await expect(page.locator('[role="dialog"]:visible')).toHaveCount(1)

    await settings.getByRole('button', { name: 'Close' }).click()
    await expect(settings).toBeHidden()
    await expect(resume).toBeVisible()
    await expect(page.locator('[role="dialog"]:visible')).toHaveCount(1)
    await expectNoProgressPut(page, api, READER_SLUG)
  })

  test('paged Start over keeps one canonical beginning save across a mode change', async ({
    page,
    api,
  }) => {
    await seedReaderPreferences(page)
    const story = makePagedReaderStory()
    api.setStory(story)
    api.setProgress(READER_SLUG, progressFor(story, 7, 0.35, 0.72))
    const put = api.deferProgressPut(READER_SLUG)

    await gotoReader(page, api, READER_SLUG)
    await page.getByRole('dialog', { name: 'Continue reading?' })
      .getByRole('button', { name: 'Start over' })
      .click()
    const request = await put.started
    expectLocatorV2Request(request, { ordinal: 1 })
    expect(request.body).toEqual(
      expect.objectContaining({
        percent: 0,
        locator: expect.objectContaining({
          segment: expect.objectContaining({ ordinal: 1, offset: 0 }),
        }),
      }),
    )
    expect(api.progressPuts()).toHaveLength(1)

    await chooseMode(page, 'Scroll')
    await expect(page.locator('[data-reader-scroll-view]')).toBeVisible()
    expect(api.progressPuts()).toHaveLength(1)
    await expect(page.locator('.reader-save-status')).not.toContainText('Saved')

    put.fulfill({ ok: true })
    await expect(page.locator('.reader-save-status')).toContainText('Saved')
    await expectNoProgressPut(page, api, READER_SLUG)
    expect(api.progressPuts()).toHaveLength(1)
    expect(api.progress.get(READER_SLUG)).toEqual(
      expect.objectContaining({
        version: story.version,
        percent: 0,
        locator: expect.objectContaining({
          segment: expect.objectContaining({ ordinal: 1, offset: 0 }),
        }),
      }),
    )
  })

  test('Start over clears a dormant oversized page offset before later navigation', async ({
    page,
    api,
  }) => {
    await page.setViewportSize({ width: 390, height: 720 })
    await seedReaderPreferences(page)
    const story = makeOversizedReaderStory()
    api.setStory(story)
    api.setProgress(READER_SLUG, progressFor(story, 4, 0.35, 0.9))
    const startPut = api.deferProgressPut(READER_SLUG)
    const oversizedPut = api.deferProgressPut(READER_SLUG)

    await gotoReader(page, api, READER_SLUG)
    const decision = page.getByRole('dialog', { name: 'Continue reading?' })
    await expect(decision).toBeVisible()
    const oversized = pagedReader(page).locator(
      '[data-reader-page-oversized="true"]',
    )
    await expect(oversized).toHaveCount(1)
    await expect(oversized).toHaveAttribute('data-reader-page-current', 'false')
    const seededOffset = await oversized.evaluate((element) => {
      const maximum = Math.max(0, element.scrollHeight - element.clientHeight)
      element.scrollTop = maximum * 0.7
      return { maximum, scrollTop: element.scrollTop }
    })
    expect(seededOffset.maximum).toBeGreaterThan(0)
    expect(seededOffset.scrollTop).toBeGreaterThan(0)
    await expect(decision).toBeVisible()

    await decision.getByRole('button', { name: 'Start over' }).click()
    const startRequest = await startPut.started
    expectLocatorV2Request(startRequest, { ordinal: 1 })
    expect(startRequest.body).toEqual(
      expect.objectContaining({
        percent: 0,
        locator: expect.objectContaining({
          segment: expect.objectContaining({ ordinal: 1, offset: 0 }),
        }),
      }),
    )
    startPut.fulfill({ ok: true })
    await expect(page.locator('.reader-save-status')).toContainText('Saved')

    await nextReaderPage(page)
    await expect(oversized).toHaveAttribute('data-reader-page-current', 'true')
    await expect
      .poll(() => oversized.evaluate((element) => element.scrollTop))
      .toBeLessThanOrEqual(1)
    const oversizedRequest = await oversizedPut.started
    expectLocatorV2Request(oversizedRequest, { ordinal: 2 })
    const expectedCanonicalOffset = await oversized.evaluate((element) => {
      const segment = element.querySelector<HTMLElement>('[data-reader-segment-ordinal="2"]')
      if (!segment) throw new Error('Missing oversized segment')
      const pageRect = element.getBoundingClientRect()
      const segmentRect = segment.getBoundingClientRect()
      const segmentTop = segmentRect.top - pageRect.top + element.scrollTop
      return (element.scrollTop + element.clientHeight * 0.35 - segmentTop) / Math.max(1, segmentRect.height)
    })
    const oversizedBody = oversizedRequest.body as { locator: { segment: { offset: number } }; percent: number }
    expect(oversizedBody.locator.segment.offset).toBeCloseTo(expectedCanonicalOffset, 2)
    expect(oversizedBody.percent).toBeGreaterThan(0)
    oversizedPut.fulfill({ ok: true })
    await expect(page.locator('.reader-save-status')).toContainText('Saved')
    await expectNoProgressPut(page, api, READER_SLUG)
    expect(api.progressPuts()).toHaveLength(2)
    expect(api.progress.get(READER_SLUG)?.locator.segment.offset).toBeCloseTo(expectedCanonicalOffset, 2)
  })

  test('rapid Paged to Scroll to Paged toggles retain one anchor and no stale listener', async ({
    page,
    api,
  }) => {
    await page.addInitScript(() => {
      type InstrumentedWindow = Window & {
        __readerActiveWindowScrollListeners?: Set<
          EventListenerOrEventListenerObject
        >
      }
      const target = window as InstrumentedWindow
      const active = new Set<EventListenerOrEventListenerObject>()
      target.__readerActiveWindowScrollListeners = active
      const add = window.addEventListener.bind(window) as (
        type: string,
        listener: EventListenerOrEventListenerObject,
        options?: boolean | AddEventListenerOptions,
      ) => void
      const remove = window.removeEventListener.bind(window) as (
        type: string,
        listener: EventListenerOrEventListenerObject,
        options?: boolean | EventListenerOptions,
      ) => void
      Object.defineProperty(window, 'addEventListener', {
        configurable: true,
        value(
          type: string,
          listener: EventListenerOrEventListenerObject,
          options?: boolean | AddEventListenerOptions,
        ) {
          if (type === 'scroll') active.add(listener)
          add(type, listener, options)
        },
      })
      Object.defineProperty(window, 'removeEventListener', {
        configurable: true,
        value(
          type: string,
          listener: EventListenerOrEventListenerObject,
          options?: boolean | EventListenerOptions,
        ) {
          if (type === 'scroll') active.delete(listener)
          remove(type, listener, options)
        },
      })
    })
    await seedReaderPreferences(page)
    const story = makeReaderStory()
    api.setStory(story)
    api.setProgress(READER_SLUG, progressFor(story, 4, 0.4, 0.55))
    const pageErrors: string[] = []
    page.on('pageerror', (error) => pageErrors.push(error.message))
    const activeScrollListenerCount = () =>
      page.evaluate(
        () =>
          (
            window as Window & {
              __readerActiveWindowScrollListeners?: Set<unknown>
            }
          ).__readerActiveWindowScrollListeners?.size ?? -1,
      )

    await gotoReader(page, api, READER_SLUG)
    await page.getByRole('dialog', { name: 'Continue reading?' })
      .getByRole('button', { name: 'Resume' })
      .click()
    await expectCurrentPageContainsOrdinal(page, 4)
    const initialRange = await currentPagedOrdinalRange(page)
    const initialListenerCount = await activeScrollListenerCount()

    await page.getByRole('button', { name: 'Reading settings' }).click()
    const settings = page.getByRole('dialog', { name: 'Reading settings' })
    await settings.getByRole('radio', { name: 'Scroll' }).check()
    await expect(page.locator('[data-reader-scroll-view]')).toBeAttached()
    await expect
      .poll(activeScrollListenerCount)
      .toBeGreaterThan(initialListenerCount)
    await settings.getByRole('radio', { name: 'Paged' }).check()
    await settings.getByRole('button', { name: 'Close' }).click()

    await waitForPagedReady(page)
    await expectCurrentPageContainsOrdinal(page, 4)
    await expect.poll(activeScrollListenerCount).toBe(initialListenerCount)
    await page.evaluate(() => window.dispatchEvent(new Event('scroll')))
    await expectNoProgressPut(page, api, READER_SLUG)
    expect(await currentPagedOrdinalRange(page)).toEqual(initialRange)
    expect(api.progressPuts()).toHaveLength(0)
    expect(api.progress.get(READER_SLUG)?.locator.segment).toEqual(
      expect.objectContaining({ ordinal: 4, offset: 0.4 }),
    )
    expect(pageErrors).toEqual([])
  })

  test('page navigation controls keep their widths from page 9 to page 10', async ({
    page,
    api,
  }) => {
    await page.setViewportSize({ width: 900, height: 720 })
    await seedReaderPreferences(page)
    api.setStory(makePagedReaderStory())
    await gotoReader(page, api, READER_SLUG)
    expect(await readerPageCount(page)).toBe(10)

    const navigation = page.getByRole('navigation', { name: 'Page navigation' })
    await page.keyboard.press('End')
    await waitForReaderPage(page, 10)
    await previousReaderPage(page)
    await expect(navigation.getByText('Page 9 of 10', { exact: true })).toBeVisible()
    const widths = () =>
      navigation.evaluate((element) => ({
        navigation: element.getBoundingClientRect().width,
        controls: Array.from(element.children, (child) =>
          child.getBoundingClientRect().width,
        ),
      }))
    const pageNineWidths = await widths()

    await nextReaderPage(page)
    await expect(
      navigation.getByText('Page 10 of 10', { exact: true }),
    ).toBeVisible()
    const pageTenWidths = await widths()
    expect(pageTenWidths.navigation).toBeCloseTo(pageNineWidths.navigation, 2)
    expect(pageTenWidths.controls).toHaveLength(pageNineWidths.controls.length)
    for (let index = 0; index < pageNineWidths.controls.length; index += 1) {
      expect(pageTenWidths.controls[index]).toBeCloseTo(
        pageNineWidths.controls[index] ?? 0,
        2,
      )
    }
  })
})
