import {
  expect,
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
} from './support/reader-page'

async function chooseMode(
  page: import('@playwright/test').Page,
  mode: 'Scroll' | 'Paged',
) {
  await page.getByRole('button', { name: 'Reading settings' }).click()
  const dialog = page.getByRole('dialog', { name: 'Reading settings' })
  await dialog.getByRole('radio', { name: mode }).check()
  await dialog.getByRole('button', { name: 'Close' }).click()
  await expect(dialog).toBeHidden()
}

test.describe('Reader representation-change safety', () => {
  test('scroll to paged and back preserves the exact anchor without a rewind write', async ({
    page,
    api,
  }) => {
    const initialPut = api.deferProgressPut(READER_SLUG)
    await gotoReader(page, api, READER_SLUG)
    await scrollToSegment(page, 4, 0.4)
    const request = await initialPut.started
    expectLocatorV2Request(request, { ordinal: 4 })
    initialPut.fulfill({ ok: true })
    await expect(page.locator('.reader-save-status')).toContainText('Saved')

    await chooseMode(page, 'Paged')
    await expect(page.locator('.reader-paged-view')).toBeVisible()
    await expectNoProgressPut(page, api, READER_SLUG)

    await chooseMode(page, 'Scroll')
    await expect(page.locator('[data-reader-scroll-view]')).toBeVisible()
    await expectSegmentAtReadingLine(page, 4, 0.4)
    await expectNoProgressPut(page, api, READER_SLUG)
  })

  test('a mode change during baseline unavailability does not become user movement after Retry', async ({
    page,
    api,
  }) => {
    api.enqueueProgressGet(READER_SLUG, {
      status: 500,
      body: { error: { code: 'internal_error', message: 'Unavailable' } },
    })
    await gotoReader(page, api, READER_SLUG)
    await expect(page.locator('.reader-save-status')).toContainText(
      'Progress unavailable',
    )

    await chooseMode(page, 'Paged')
    await page.locator('.reader-save-status').getByRole('button', { name: 'Retry' }).click()
    await expect
      .poll(() => api.count('GET', `/api/v1/progress/${READER_SLUG}`))
      .toBe(2)
    await expectNoProgressPut(page, api, READER_SLUG)
  })

  test('paged Resume keeps its segment anchor and emits no programmatic page-start write', async ({
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
    const story = api.stories.get(READER_SLUG)
    expect(story).toBeDefined()
    if (!story) return
    api.setProgress(READER_SLUG, progressFor(story, 4, 0.4, 0.55))

    await gotoReader(page, api, READER_SLUG)
    await page.getByRole('dialog', { name: 'Continue reading?' })
      .getByRole('button', { name: 'Resume' })
      .click()
    await expect(page.getByRole('dialog', { name: 'Continue reading?' })).toBeHidden()
    await expectNoProgressPut(page, api, READER_SLUG)
  })
})
