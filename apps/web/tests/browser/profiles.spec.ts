import type { Page, Route } from '@playwright/test'

import {
  expect,
  fixtureAccessToken,
  fixtureAccountID,
  fixtureProfileID,
  test,
} from './support/auth'

type Profile = { id: string; name: string }
const alternateAccountID = '123e4567-e89b-42d3-a456-426614174400'

class ProfilesApiMock {
  profiles: Profile[] = []
  private readonly page: Page

  constructor(page: Page) {
    this.page = page
  }

  async install() {
    await this.page.route('**/api/v1/**', async (route) => this.handle(route))
  }

  private async respond(route: Route, body: unknown, status = 200) {
    await route.fulfill({
      status,
      contentType: 'application/json',
      headers: { 'Cache-Control': 'no-store' },
      body: status === 204 ? '' : JSON.stringify(body),
    })
  }

  private async handle(route: Route) {
    const request = route.request()
    const url = new URL(request.url())
    const headers = request.headers()
    expect(headers.authorization).toBe('Bearer ' + fixtureAccessToken)
    expect(headers['x-pp-account-id']).toBe(fixtureAccountID)
    expect(headers['x-pp-profile-id']).toBeFalsy()
    expect(headers.cookie).toBeFalsy()

    if (request.method() === 'GET' && url.pathname === '/api/v1/profiles') {
      await this.respond(route, { profiles: this.profiles })
      return
    }
    if (request.method() === 'POST' && url.pathname === '/api/v1/profiles') {
      const body = request.postDataJSON() as { name: string }
      const profile = {
        id: fixtureProfileID.slice(0, -1) + String(this.profiles.length + 1),
        name: body.name.trim(),
      }
      this.profiles.push(profile)
      await this.respond(route, profile)
      return
    }
    if (url.pathname.startsWith('/api/v1/profiles/')) {
      const id = url.pathname.slice('/api/v1/profiles/'.length)
      const profile = this.profiles.find((candidate) => candidate.id === id)
      if (!profile) {
        await this.respond(route, { error: { code: 'profile_forbidden' } }, 403)
        return
      }
      if (request.method() === 'PATCH') {
        profile.name = (request.postDataJSON() as { name: string }).name.trim()
        await this.respond(route, profile)
        return
      }
      if (request.method() === 'DELETE') {
        this.profiles = this.profiles.filter((candidate) => candidate.id !== id)
        await this.respond(route, null, 204)
        return
      }
    }
    if (request.method() === 'GET' && url.pathname === '/api/v1/library') {
      await this.respond(route, { items: [], unavailableItemCount: 0 })
      return
    }
    await this.respond(route, { error: { code: 'not_found' } }, 404)
  }
}

test.describe('reader profile lifecycle', () => {
  test('zero profiles shows the first-reader creation state without inventing a default', async ({ page }) => {
    const api = new ProfilesApiMock(page)
    await api.install()

    await page.goto('/profiles')

    await expect(page.getByText('Add the first reader for this account.')).toBeVisible()
    await expect(page.getByLabel('Readers')).toHaveCount(0)
    await expect(page.locator('body')).not.toContainText('Default')
  })

  test('one profile is selected as a frontend convenience and its ID is not displayed', async ({ page }) => {
    const api = new ProfilesApiMock(page)
    api.profiles = [{ id: fixtureProfileID, name: 'Mina' }]
    await api.install()

    await page.goto('/profiles')

    await expect(page.getByRole('button', { name: 'Mina Selected' })).toHaveAttribute('aria-pressed', 'true')
    await expect(page.locator('body')).not.toContainText(fixtureProfileID)
  })

  test('multiple profiles require a choice, and switching stores only the selected ID', async ({ page }) => {
    const api = new ProfilesApiMock(page)
    api.profiles = [
      { id: fixtureProfileID, name: 'Mina' },
      { id: fixtureProfileID.slice(0, -1) + '1', name: 'Ted' },
    ]
    await api.install()

    await page.goto('/profiles')
    await expect(page.getByRole('button', { name: 'Mina' })).toHaveAttribute('aria-pressed', 'false')
    await page.getByRole('button', { name: 'Ted' }).click()
    await expect(page).toHaveURL('/library')
    await expect.poll(() => page.evaluate(() => window.localStorage.getItem('pandapages.selected-reader-profile-id'))).toBe(api.profiles[1].id)
  })

  test('changing accounts clears a stale reader selection before profile reconciliation', async ({ page, auth }) => {
    auth.setMemberships([
      { accountId: alternateAccountID, accountName: 'Second account', role: 'adult' },
    ])
    await page.addInitScript((values) => {
      window.localStorage.setItem('pandapages.selected-account-id', values.accountID)
      window.localStorage.setItem('pandapages.selected-reader-profile-id', values.profileID)
    }, { accountID: fixtureAccountID, profileID: fixtureProfileID })
    await page.route('**/api/v1/profiles**', async (route) => {
      const headers = route.request().headers()
      expect(headers.authorization).toBe('Bearer ' + fixtureAccessToken)
      expect(headers['x-pp-account-id']).toBe(alternateAccountID)
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({ profiles: [] }),
      })
    })

    await page.goto('/account')
    await page.getByRole('button', { name: 'Choose' }).click()

    await expect(page).toHaveURL(/\/profiles\?next=\/library$/)
    await expect.poll(() => page.evaluate(() => window.localStorage.getItem('pandapages.selected-reader-profile-id'))).toBeNull()
  })

  test('can create, rename, and delete the active reader with confirmation', async ({ page }) => {
    const api = new ProfilesApiMock(page)
    await api.install()

    await page.goto('/profiles')
    await page.getByLabel('New reader name').fill('  Ted  ')
    await page.getByRole('button', { name: 'Add reader' }).click()
    await expect(page).toHaveURL('/library')

    await page.goto('/profiles')
    await page.getByRole('button', { name: 'Rename' }).click()
    await page.getByLabel('Reader name', { exact: true }).fill('Theo')
    await page.getByRole('button', { name: 'Save name' }).click()
    await expect(page.getByRole('button', { name: 'Theo Selected' })).toBeVisible()

    await page.getByRole('button', { name: 'Delete' }).click()
    await expect(page.getByRole('alertdialog')).toBeVisible()
    await page.getByRole('button', { name: 'Delete reader' }).click()
    await expect(page.getByText('Add the first reader for this account.')).toBeVisible()
    await expect.poll(() => page.evaluate(() => window.localStorage.getItem('pandapages.selected-reader-profile-id'))).toBeNull()
  })
})
