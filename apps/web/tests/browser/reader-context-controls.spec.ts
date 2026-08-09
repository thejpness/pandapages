import { fixtureProfileID } from './support/auth'
import { expect, READER_SLUG, test } from './support/reader-api'
import { gotoReader } from './support/reader-page'

async function expectProfileChooser(
  page: import('@playwright/test').Page,
  next: string | null,
) {
  await expect.poll(() => {
    const url = new URL(page.url())
    return { pathname: url.pathname, next: url.searchParams.get('next') }
  }).toEqual({ pathname: '/profiles', next })
}

test.describe('Reader context controls', () => {
  test('Switch reader preserves the active reader while opening the chooser', async ({
    page,
    api,
  }) => {
    await gotoReader(page, api, READER_SLUG)
    await page.getByRole('button', { name: 'Switch reader' }).click()

    await expectProfileChooser(page, `/read/${READER_SLUG}`)
    await expect(page.getByText('Current reader', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Add profile' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Manage profiles' })).toHaveCount(0)
    await expect.poll(() => page.evaluate(() => window.localStorage.getItem('pandapages.selected-reader-profile-id'))).toBe(fixtureProfileID)
  })

  test('Parent controls leaves reader mode for the parent profile surface', async ({
    page,
    api,
  }) => {
    await gotoReader(page, api, READER_SLUG)
    await page.getByRole('button', { name: 'Parent controls' }).click()

    await expectProfileChooser(page, null)
    await expect(page.getByText('Selected reader', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Add profile' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Manage profiles' })).toBeVisible()
    await expect.poll(() => page.evaluate(() => window.localStorage.getItem('pandapages.selected-reader-profile-id'))).toBe(fixtureProfileID)
  })
})
