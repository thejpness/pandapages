import { expect, READER_SLUG, test } from './support/reader-api'
import { gotoReader, scrollToSegment } from './support/reader-page'

async function expectSafeSignIn(page: import('@playwright/test').Page) {
  await expect.poll(() => new URL(page.url()).pathname).toBe('/account/login')
  expect(new URL(page.url()).searchParams.get('next')).toBe(
    `/read/${READER_SLUG}`,
  )
}

async function openFailedLibraryGate(
  page: import('@playwright/test').Page,
  api: import('./support/reader-api').ReaderApiMock,
) {
  api.enqueueProgressPut(READER_SLUG, {
    status: 500,
    body: { error: { code: 'internal_error', message: 'Unavailable' } },
  })
  await gotoReader(page, api, READER_SLUG)
  await scrollToSegment(page, 4, 0.4)
  await page.getByRole('button', { name: 'Return to Library' }).click()
  await expect(page.getByRole('alert')).toContainText(
    'Progress could not be saved.',
  )
  expect(new URL(page.url()).pathname).toBe(`/read/${READER_SLUG}`)
}

test.describe('Reader progress lifecycle wiring', () => {
  test('a baseline Retry returning 401 follows the safe signed-session transition', async ({
    page,
    api,
  }) => {
    api.enqueueProgressGet(READER_SLUG, {
      status: 500,
      body: { error: { code: 'internal_error', message: 'Unavailable' } },
    })
    api.enqueueProgressGet(READER_SLUG, {
      status: 401,
      body: { error: { code: 'unauthorized', message: 'Session ended' } },
    })
    await gotoReader(page, api, READER_SLUG)
    await page.locator('.reader-save-status').getByRole('button', { name: 'Retry' }).click()

    await expectSafeSignIn(page)
    expect(api.progressPuts(READER_SLUG)).toHaveLength(0)
  })

  test('a progress PUT returning 401 never claims Saved and routes to safe sign-in', async ({
    page,
    api,
  }) => {
    api.enqueueProgressPut(READER_SLUG, {
      status: 401,
      body: { error: { code: 'unauthorized', message: 'Session ended' } },
    })
    await gotoReader(page, api, READER_SLUG)
    await scrollToSegment(page, 4, 0.4)

    await expectSafeSignIn(page)
    expect(api.progressPuts(READER_SLUG)).toHaveLength(1)
  })

  test('Library Retry drains the retained desired snapshot before navigating', async ({
    page,
    api,
  }) => {
    await openFailedLibraryGate(page, api)
    await page.getByRole('alert').getByRole('button', { name: 'Retry' }).click()

    await expect.poll(() => new URL(page.url()).pathname).toBe('/library')
    expect(api.progressPuts(READER_SLUG)).toHaveLength(2)
  })

  test('Leave anyway navigates without turning a failed save into success', async ({
    page,
    api,
  }) => {
    await openFailedLibraryGate(page, api)
    await page.getByRole('alert').getByRole('button', { name: 'Leave anyway' }).click()

    await expect.poll(() => new URL(page.url()).pathname).toBe('/library')
    expect(api.progressPuts(READER_SLUG)).toHaveLength(1)
  })
})
