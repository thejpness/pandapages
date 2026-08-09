import {
  expect,
  makeReaderStory,
  READER_SLUG,
  test,
} from './support/reader-api'
import { gotoReader, scrollToSegment } from './support/reader-page'
import { fixtureProfileID } from './support/auth'

test.describe('Reader edition resolution', () => {
  test('multiple eligible editions require an explicit chooser before story content initializes', async ({
    page,
    api,
  }) => {
    const classic = makeReaderStory({ version: 1 })
    const listeners = makeReaderStory({
      version: 2,
      title: 'TEST ONLY — Moonlit Café for Little Listeners',
    })
    api.setEditionStory('classic', classic)
    api.setEditionStory('little-listeners', listeners)
    api.setEligibleEditions(READER_SLUG, ['classic', 'little-listeners'])
    api.setProgress(READER_SLUG, null)

    await page.goto(`/read/${READER_SLUG}`)

    const chooser = page.getByRole('dialog', { name: 'Choose a story edition' })
    await expect(chooser).toBeVisible()
    await expect(page.locator('[data-reader-scroll-view], [data-reader-paged-view]')).toHaveCount(0)

    await chooser.getByRole('button', { name: /Little Listeners/ }).click()

    await expect(page.getByRole('heading', { level: 1, name: listeners.title })).toBeVisible()
    await expect(chooser).toBeHidden()
    expect(api.count('GET', `/api/v1/reader-resolution/${READER_SLUG}`)).toBe(2)
    expect(api.count('PUT', `/api/v1/reader-edition/${READER_SLUG}`)).toBe(1)
    const editionWrite = api.requests.find(
      (request) =>
        request.method === 'PUT' &&
        request.pathname === `/api/v1/reader-edition/${READER_SLUG}`,
    )
    expect(editionWrite?.profileID).toBe(fixtureProfileID)
    expect(editionWrite?.body).toEqual({ editionKey: 'little-listeners' })
    expect(api.legacyRequests).toEqual([])
  })

  test('changing edition in settings flushes the old place then reuses the existing cross-version decision', async ({
    page,
    api,
  }) => {
    const classic = makeReaderStory({ version: 1 })
    const listeners = makeReaderStory({
      version: 2,
      title: 'TEST ONLY — Moonlit Café for Little Listeners',
    })
    api.setEditionStory('classic', classic)
    api.setEditionStory('little-listeners', listeners)
    api.setEligibleEditions(READER_SLUG, ['classic', 'little-listeners'])
    api.setReaderEditionOverride(READER_SLUG, 'classic')

    await gotoReader(page, api, READER_SLUG)
    await scrollToSegment(page, 4, 0.4)
    await page.getByRole('button', { name: 'Reading settings' }).click()

    const settings = page.getByRole('dialog', { name: 'Reading settings' })
    await expect(settings).toContainText('This story is currently using Classic')
    await settings.getByRole('button', { name: /Little Listeners/ }).click()

    await expect(page.getByRole('dialog', { name: 'Story updated' })).toBeVisible()
    expect(api.count('PUT', `/api/v1/progress/${READER_SLUG}`)).toBeGreaterThanOrEqual(1)
    expect(api.count('PUT', `/api/v1/reader-edition/${READER_SLUG}`)).toBe(1)
    expect(api.count('GET', `/api/v1/reader-resolution/${READER_SLUG}`)).toBe(2)

    const progressWriteIndex = api.requests.findIndex(
      (request) =>
        request.method === 'PUT' &&
        request.pathname === `/api/v1/progress/${READER_SLUG}`,
    )
    const editionWriteIndex = api.requests.findIndex(
      (request) =>
        request.method === 'PUT' &&
        request.pathname === `/api/v1/reader-edition/${READER_SLUG}`,
    )
    expect(progressWriteIndex).toBeGreaterThanOrEqual(0)
    expect(editionWriteIndex).toBeGreaterThan(progressWriteIndex)

    const resolutionRequests = api.requests.filter(
      (request) =>
        request.method === 'GET' &&
        request.pathname === `/api/v1/reader-resolution/${READER_SLUG}`,
    )
    expect(resolutionRequests.every((request) => request.profileID === fixtureProfileID)).toBe(true)
    expect(api.legacyRequests).toEqual([])
  })
})
