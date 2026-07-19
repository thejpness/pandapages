import type { Page } from '@playwright/test'
import { expect, type CapturedRequest, type ReaderApiMock } from './reader-api'

export type ReaderPreferenceOverrides = Partial<{
  mode: 'scroll' | 'paged'
  theme: 'night' | 'warm'
  fontFamily: 'book' | 'clear' | 'system'
  fontSize: number
  lineHeight: number
  contentWidth: number
}>

export async function seedReaderPreferences(
  page: Page,
  overrides: ReaderPreferenceOverrides = {},
): Promise<void> {
  await page.addInitScript((preferenceOverrides) => {
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
        ...preferenceOverrides,
      }),
    )
  }, overrides)
}
export async function forceReaderScrollEndFallback(
  page: Page,
): Promise<void> {
  await page.addInitScript(() => {
    for (const prototype of [
      Window.prototype,
      Document.prototype,
      Element.prototype,
      HTMLElement.prototype,
    ]) {
      const descriptor = Object.getOwnPropertyDescriptor(
        prototype,
        'onscrollend',
      )
      if (descriptor?.configurable) {
        Reflect.deleteProperty(prototype, 'onscrollend')
      }
    }

    const add = Object.getOwnPropertyDescriptor(
      EventTarget.prototype,
      'addEventListener',
    )?.value as (
      this: EventTarget,
      type: string,
      listener: EventListenerOrEventListenerObject,
      options?: boolean | AddEventListenerOptions,
    ) => void
    const remove = Object.getOwnPropertyDescriptor(
      EventTarget.prototype,
      'removeEventListener',
    )?.value as (
      this: EventTarget,
      type: string,
      listener: EventListenerOrEventListenerObject,
      options?: boolean | EventListenerOptions,
    ) => void
    Object.defineProperties(EventTarget.prototype, {
      addEventListener: {
        configurable: true,
        value(
          this: EventTarget,
          type: string,
          listener: EventListenerOrEventListenerObject,
          options?: boolean | AddEventListenerOptions,
        ) {
          if (type !== 'scrollend') add.call(this, type, listener, options)
        },
      },
      removeEventListener: {
        configurable: true,
        value(
          this: EventTarget,
          type: string,
          listener: EventListenerOrEventListenerObject,
          options?: boolean | EventListenerOptions,
        ) {
          if (type !== 'scrollend') remove.call(this, type, listener, options)
        },
      },
    })
  })
}

export async function beginPagedAnnouncementCapture(
  page: Page,
): Promise<void> {
  await page.evaluate(() => {
    const target = window as Window & {
      __readerPagedAnnouncements?: string[]
      __readerPagedAnnouncementObserver?: MutationObserver
    }
    target.__readerPagedAnnouncementObserver?.disconnect()
    target.__readerPagedAnnouncements = []
    const region = document.querySelector(
      '[data-reader-paged-view] > .reader-sr-only[role="status"]',
    )
    if (!region) throw new Error('missing paged Reader announcement region')
    target.__readerPagedAnnouncementObserver = new MutationObserver(() => {
      const value = region.textContent?.trim() ?? ''
      if (value) target.__readerPagedAnnouncements?.push(value)
    })
    target.__readerPagedAnnouncementObserver.observe(region, {
      childList: true,
      characterData: true,
      subtree: true,
    })
  })
}

export async function pagedAnnouncementHistory(
  page: Page,
): Promise<string[]> {
  return page.evaluate(
    () =>
      (
        window as Window & {
          __readerPagedAnnouncements?: string[]
        }
      ).__readerPagedAnnouncements ?? [],
  )
}


export function pagedReader(page: Page) {
  return page.locator('[data-reader-paged-view]')
}

export function pagedViewport(page: Page) {
  return page.locator('.reader-paged-viewport')
}

export async function settleReaderFrames(page: Page): Promise<void> {
  await page.evaluate(
    () =>
      new Promise<void>((resolve) => {
        requestAnimationFrame(() => requestAnimationFrame(() => resolve()))
      }),
  )
}

export async function gotoReader(
  page: Page,
  api: ReaderApiMock,
  slug: string,
): Promise<void> {
  await page.goto(`/read/${encodeURIComponent(slug)}`)
  await expect(page.locator('[data-reader-scroll-view], [data-reader-paged-view][data-reader-paged-ready="true"]')).toBeVisible()
  await expect
    .poll(() => api.count('GET', `/api/v1/progress/${encodeURIComponent(slug)}`))
    .toBe(1)
  await settleReaderFrames(page)
}

export async function scrollToSegment(
  page: Page,
  ordinal: number,
  offset = 0.4,
): Promise<void> {
  await page
    .locator(`[data-reader-scroll-segment][data-reader-segment-ordinal="${ordinal}"]`)
    .evaluate((element, targetOffset) => {
      const headerBottom =
        document
          .querySelector<HTMLElement>('[data-reader-header]')
          ?.getBoundingClientRect().bottom ?? 0
      const readingLine = headerBottom + (window.innerHeight - headerBottom) * 0.35
      const rect = element.getBoundingClientRect()
      const target =
        window.scrollY + rect.top + rect.height * Number(targetOffset) - readingLine
      window.scrollTo({ top: Math.max(0, target), behavior: 'auto' })
      window.dispatchEvent(new Event('scroll'))
    }, offset)
  await settleReaderFrames(page)
}

export async function expectSegmentAtReadingLine(
  page: Page,
  ordinal: number,
  offset: number,
  tolerance = 5,
): Promise<void> {
  const distance = await page
    .locator(`[data-reader-segment-ordinal="${ordinal}"]`)
    .first()
    .evaluate((element, targetOffset) => {
      const headerBottom =
        document
          .querySelector<HTMLElement>('[data-reader-header]')
          ?.getBoundingClientRect().bottom ?? 0
      const readingLine = headerBottom + (window.innerHeight - headerBottom) * 0.35
      const rect = element.getBoundingClientRect()
      return Math.abs(rect.top + rect.height * Number(targetOffset) - readingLine)
    }, offset)
  expect(distance).toBeLessThanOrEqual(tolerance)
}

export function expectLocatorV2Request(
  request: CapturedRequest,
  expected: { version?: number; ordinal?: number } = {},
): void {
  expect(request.method).toBe('PUT')
  expect(request.body).toEqual(
    expect.objectContaining({
      version: expected.version ?? expect.any(Number),
      percent: expect.any(Number),
      locator: expect.objectContaining({
        schema: 2,
        segment: expect.objectContaining({
          key: expect.stringMatching(/^[0-9a-f]{64}$/),
          occurrence: expect.any(Number),
          ordinal: expected.ordinal ?? expect.any(Number),
          offset: expect.any(Number),
        }),
      }),
    }),
  )
  const encoded = JSON.stringify(request.body)
  expect(encoded).not.toMatch(/"(?:mode|page|scrollY|startOrdinal|endOrdinal)"/)
}

export async function expectNoProgressPut(
  page: Page,
  api: ReaderApiMock,
  slug: string,
  durationMs = 650,
): Promise<void> {
  const before = api.progressPuts(slug).length
  // This is deliberately bounded beyond the production 450 ms coordinator
  // debounce. Absence assertions cannot synchronize on a request by definition.
  await page.waitForTimeout(durationMs)
  expect(api.progressPuts(slug)).toHaveLength(before)
}

export async function activeElementIsInside(
  page: Page,
  selector: string,
): Promise<boolean> {
  return page.locator(selector).evaluate((element) => element.contains(document.activeElement))
}

export async function expectFocusTrapped(
  page: Page,
  dialogName: string,
): Promise<void> {
  const dialog = page.getByRole('dialog', { name: dialogName })
  await expect(dialog).toBeVisible()
  await expect
    .poll(() =>
      dialog.evaluate((element) => element.contains(document.activeElement)),
    )
    .toBe(true)

  for (let index = 0; index < 12; index += 1) {
    await page.keyboard.press('Tab')
    expect(await dialog.evaluate((element) => element.contains(document.activeElement))).toBe(true)
  }
}

export async function waitForPagedReady(page: Page): Promise<void> {
  const reader = pagedReader(page)
  await expect(reader).toBeVisible()
  await expect(reader).toHaveAttribute('data-reader-paged-ready', 'true')
  await expect(reader).toHaveAttribute('data-reader-page-count', /^[1-9]\d*$/)
}

export async function readerPageCount(page: Page): Promise<number> {
  await waitForPagedReady(page)
  return Number(await pagedReader(page).getAttribute('data-reader-page-count'))
}

export async function currentReaderPage(page: Page): Promise<number> {
  await waitForPagedReady(page)
  return Number(await pagedReader(page).getAttribute('data-reader-current-page'))
}

export async function waitForReaderPage(
  page: Page,
  pageNumber: number,
): Promise<void> {
  const reader = pagedReader(page)
  await expect(reader).toHaveAttribute(
    'data-reader-current-page',
    String(pageNumber),
  )
  const current = reader.locator('[data-reader-page-current="true"]')
  await expect(current).toHaveCount(1)
  await expect(current).toHaveAttribute(
    'data-reader-page-index',
    String(pageNumber - 1),
  )
  await expect
    .poll(() =>
      pagedViewport(page).evaluate((viewport) => {
        const width = Math.max(1, viewport.clientWidth)
        const position = viewport.scrollLeft / width
        return Math.abs(position - Math.round(position))
      }),
    )
    .toBeLessThanOrEqual(0.02)
}

export async function currentPagedOrdinalRange(
  page: Page,
): Promise<{ start: number; end: number }> {
  const current = pagedReader(page).locator('[data-reader-page-current="true"]')
  await expect(current).toHaveCount(1)
  return {
    start: Number(await current.getAttribute('data-reader-page-start-ordinal')),
    end: Number(await current.getAttribute('data-reader-page-end-ordinal')),
  }
}

export async function expectCurrentPageContainsOrdinal(
  page: Page,
  ordinal: number,
): Promise<void> {
  await expect
    .poll(async () => {
      const range = await currentPagedOrdinalRange(page)
      return ordinal >= range.start && ordinal <= range.end
    })
    .toBe(true)
}

export async function nextReaderPage(page: Page): Promise<number> {
  const current = await currentReaderPage(page)
  await page.getByRole('button', { name: 'Next page' }).click()
  await waitForReaderPage(page, current + 1)
  return current + 1
}

export async function previousReaderPage(page: Page): Promise<number> {
  const current = await currentReaderPage(page)
  await page.getByRole('button', { name: 'Previous page' }).click()
  await waitForReaderPage(page, current - 1)
  return current - 1
}

export async function scrollPagedViewportTo(
  page: Page,
  pageNumber: number,
): Promise<void> {
  await pagedViewport(page).evaluate((viewport, targetPage) => {
    viewport.scrollTo({
      left: (Number(targetPage) - 1) * viewport.clientWidth,
      behavior: 'auto',
    })
  }, pageNumber)
  await waitForReaderPage(page, pageNumber)
}

export async function wheelPagedViewport(
  page: Page,
  direction: 1 | -1,
): Promise<void> {
  const viewport = pagedViewport(page)
  await viewport.hover()
  const width = await viewport.evaluate((element) => element.clientWidth)
  await page.mouse.wheel(direction * width, 0)
}

export async function scrollOversizedPageTo(
  page: Page,
  offset: number,
): Promise<void> {
  const current = pagedReader(page).locator(
    '[data-reader-page-current="true"][data-reader-page-oversized="true"]',
  )
  await expect(current).toHaveCount(1)
  await current.evaluate((element, targetOffset) => {
    const maximum = Math.max(0, element.scrollHeight - element.clientHeight)
    element.scrollTo({
      top: maximum * Math.max(0, Math.min(1, Number(targetOffset))),
      behavior: 'auto',
    })
  }, offset)
  await expect
    .poll(() =>
      current.evaluate((element) => {
        const maximum = Math.max(0, element.scrollHeight - element.clientHeight)
        return maximum > 0 ? element.scrollTop / maximum : 0
      }),
    )
    .toBeCloseTo(offset, 1)
}
