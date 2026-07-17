import type { Page } from '@playwright/test'
import { expect, type CapturedRequest, type ReaderApiMock } from './reader-api'

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
  await expect(page.locator('[data-reader-scroll-view], .reader-paged-story')).toBeVisible()
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
