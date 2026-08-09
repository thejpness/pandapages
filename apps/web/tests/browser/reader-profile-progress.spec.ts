import {
  expect,
  progressFor,
  READER_SLUG,
  test,
} from './support/reader-api'
import { fixtureProfileID } from './support/auth'
import { gotoReader } from './support/reader-page'

const secondProfileID = '123e4567-e89b-42d3-a456-426614174301'

test('reader progress is reloaded and isolated when the selected profile changes', async ({
  page,
  api,
}) => {
  const story = api.stories.get(READER_SLUG)
  expect(story).toBeDefined()
  if (!story) return

  api.profiles = [
    { id: fixtureProfileID, name: 'Mina', pin_enabled: false },
    { id: secondProfileID, name: 'Ted', pin_enabled: false },
  ]
  api.setProgress(
    READER_SLUG,
    progressFor(story, 5, 0.4, 0.75),
    fixtureProfileID,
  )
  api.setProgress(
    READER_SLUG,
    progressFor(story, 4, 0.2, 0.2),
    secondProfileID,
  )

  await page.addInitScript((profileID) => {
    if (!window.localStorage.getItem('pandapages.selected-reader-profile-id')) {
      window.localStorage.setItem('pandapages.selected-reader-profile-id', profileID)
    }
  }, fixtureProfileID)
  await api.install()

  await gotoReader(page, api, READER_SLUG)
  await expect(page.getByRole('dialog', { name: 'Continue reading?' })).toContainText('75%')
  await page.getByRole('button', { name: 'Dismiss' }).click()

  await page.goto('/profiles')
  await page.getByRole('button', { name: 'Start reading as Ted', exact: true }).click()
  await expect(page).toHaveURL('/library')
  await expect
    .poll(() =>
      page.evaluate(() =>
        window.localStorage.getItem('pandapages.selected-reader-profile-id'),
      ),
    )
    .toBe(secondProfileID)

  await page.goto(`/read/${READER_SLUG}`)
  await expect
    .poll(() => api.count('GET', `/api/v1/progress/${READER_SLUG}`))
    .toBe(2)
  expect(
    api.requests.filter(
      (request) =>
        request.method === 'GET' &&
        request.pathname === `/api/v1/progress/${READER_SLUG}`,
    )[1]?.profileID,
  ).toBe(secondProfileID)
  await expect(page.getByRole('dialog', { name: 'Continue reading?' })).toContainText('20%')
  expect(api.progressForProfile(READER_SLUG, fixtureProfileID)?.percent).toBe(0.75)
  expect(api.progressForProfile(READER_SLUG, secondProfileID)?.percent).toBe(0.2)
})
