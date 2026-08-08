import type { Page, Route } from '@playwright/test'

import {
  expect,
  fixtureAccessToken,
  fixtureAccountID,
  fixtureProfileID,
  test,
} from './support/auth'

type Profile = { id: string; name: string; pin_enabled: boolean }
const alternateAccountID = '123e4567-e89b-42d3-a456-426614174400'

class ProfilesApiMock {
  profiles: Profile[] = []
  rateLimitPINVerification = false
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
        name: body.name.trim(), pin_enabled: false,
      }
      this.profiles.push(profile)
      await this.respond(route, profile)
      return
    }
    if (url.pathname.startsWith('/api/v1/profiles/')) {
      const remainder = url.pathname.slice('/api/v1/profiles/'.length)
      const [id, action] = remainder.split('/')
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
      if (action === 'pin' && request.method() === 'PUT') {
        profile.pin_enabled = true
        await this.respond(route, { pin_enabled: true })
        return
      }
      if (action === 'pin' && request.method() === 'POST') {
        if (this.rateLimitPINVerification) {
          await this.respond(route, { error: { code: 'pin_rate_limited' } }, 429)
          return
        }
        const pin = (request.postDataJSON() as { pin: string }).pin
        if (pin !== '1234') {
          await this.respond(route, { error: { code: 'pin_invalid' } }, 403)
          return
        }
        await this.respond(route, { verified: true })
        return
      }
      if (action === 'pin' && request.method() === 'DELETE') {
        profile.pin_enabled = false
        await this.respond(route, { pin_enabled: false })
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
    api.profiles = [{ id: fixtureProfileID, name: 'Mina', pin_enabled: false }]
    await api.install()

    await page.goto('/profiles')

    await expect(page.getByRole('button', { name: 'Mina Selected' })).toHaveAttribute('aria-pressed', 'true')
    await expect(page.locator('body')).not.toContainText(fixtureProfileID)
  })

  test('multiple profiles require a choice, and switching stores only the selected ID', async ({ page }) => {
    const api = new ProfilesApiMock(page)
    api.profiles = [
      { id: fixtureProfileID, name: 'Mina', pin_enabled: false },
      { id: fixtureProfileID.slice(0, -1) + '1', name: 'Ted', pin_enabled: false },
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

  test('a no-PIN reader enters reader mode and can explicitly return to account mode', async ({ page }) => {
    const api = new ProfilesApiMock(page)
    api.profiles = [{ id: fixtureProfileID, name: 'Mina', pin_enabled: false }]
    await api.install()

    await page.goto('/profiles')
    await page.getByRole('button', { name: 'Mina Selected' }).click()
    await expect(page).toHaveURL('/library')
    await expect(page.getByRole('button', { name: 'Leave reader mode' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Parent options' })).toHaveCount(0)

    await page.getByRole('button', { name: 'Leave reader mode' }).click()
    await expect(page).toHaveURL('/profiles')
    await expect(page.getByRole('button', { name: 'Mina Selected' })).toBeVisible()
  })

  test('a no-PIN reader restores reader mode after reload and direct Library navigation', async ({ page }) => {
    const api = new ProfilesApiMock(page)
    api.profiles = [{ id: fixtureProfileID, name: 'Mina', pin_enabled: false }]
    await api.install()

    await page.goto('/profiles')
    await page.getByRole('button', { name: 'Mina Selected' }).click()
    await expect(page).toHaveURL('/library')
    await expect(page.getByRole('button', { name: 'Leave reader mode' })).toBeVisible()

    await page.reload()
    await expect(page).toHaveURL('/library')
    await expect(page.getByRole('button', { name: 'Leave reader mode' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Parent options' })).toHaveCount(0)

    await page.getByRole('button', { name: 'Leave reader mode' }).click()
    await expect(page).toHaveURL('/profiles')
    await page.goto('/library')
    await expect(page).toHaveURL('/library')
    await expect(page.getByRole('button', { name: 'Leave reader mode' })).toBeVisible()
  })

  test('Story Studio is discoverable to account owners only', async ({ page, auth }) => {
    const api = new ProfilesApiMock(page)
    api.profiles = [{ id: fixtureProfileID, name: 'Mina', pin_enabled: false }]
    await api.install()

    await page.goto('/profiles')
    await expect(page.getByRole('button', { name: 'Story Studio' })).toBeVisible()

    auth.setRole('adult')
    await page.reload()
    await expect(page.getByRole('button', { name: 'Story Studio' })).toHaveCount(0)
  })

  test('a protected reader requires its PIN and never persists it or its unlock', async ({ page }) => {
    const api = new ProfilesApiMock(page)
    api.profiles = [{ id: fixtureProfileID, name: 'Mina', pin_enabled: true }]
    await api.install()

    await page.goto('/library')
    await expect(page).toHaveURL(/\/profiles\?next=\/library$/)
    await page.getByRole('button', { name: 'Mina PIN protected' }).click()
    await expect(page.getByRole('dialog')).toBeVisible()
    await page.getByLabel('Four-digit PIN').fill('0000')
    await page.getByRole('button', { name: 'Continue' }).click()
    await expect(page.getByRole('alert')).toHaveText('That PIN is not right.')
    await expect(page).toHaveURL(/\/profiles\?next=\/library$/)

    await page.getByLabel('Four-digit PIN').fill('1234')
    await page.getByRole('button', { name: 'Continue' }).click()
    await expect(page).toHaveURL('/library')
    await expect.poll(() => page.evaluate(() => JSON.stringify(window.localStorage))).not.toContain('1234')

    await page.reload()
    await expect(page).toHaveURL(/\/profiles/)
    await expect(page.getByRole('dialog')).toHaveCount(0)
  })

  test('PIN management supports set, remove, and a finite rate-limit message', async ({ page }) => {
    const api = new ProfilesApiMock(page)
    api.profiles = [{ id: fixtureProfileID, name: 'Mina', pin_enabled: false }]
    await api.install()

    await page.goto('/profiles')
    await page.getByRole('button', { name: 'Set PIN' }).click()
    await page.getByLabel('Four-digit PIN').fill('1234')
    await page.getByRole('button', { name: 'Save PIN' }).click()
    await expect(page.getByRole('button', { name: 'Change PIN' })).toBeVisible()

    await page.getByRole('button', { name: 'Remove PIN' }).click()
    await expect(page.getByRole('alertdialog')).toContainText('Remove Mina’s PIN?')
    await page.getByRole('button', { name: 'Remove PIN' }).last().click()
    await expect(page.getByRole('button', { name: 'Set PIN' })).toBeVisible()

    api.profiles[0].pin_enabled = true
    api.rateLimitPINVerification = true
    await page.reload()
    await page.getByRole('button', { name: 'Mina PIN protected' }).click()
    await page.getByLabel('Four-digit PIN').fill('1234')
    await page.getByRole('button', { name: 'Continue' }).click()
    await expect(page.getByRole('alert')).toHaveText('Too many tries. Please wait before trying again.')
  })
})
