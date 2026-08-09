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

  test('one profile is selected as a frontend convenience while management stays secondary', async ({ page }) => {
    const api = new ProfilesApiMock(page)
    api.profiles = [{ id: fixtureProfileID, name: 'Mina', pin_enabled: false }]
    await api.install()

    await page.goto('/profiles')

    await expect(page.getByRole('heading', { level: 3, name: 'Mina' })).toBeVisible()
    await expect(page.getByText('Selected', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Start reading as Mina' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Manage reader' })).toHaveAttribute('aria-expanded', 'false')
    await expect(page.getByRole('button', { name: 'Rename' })).toHaveCount(0)
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
    await expect(page.getByText('Selected', { exact: true })).toHaveCount(0)
    await page.getByRole('button', { name: 'Start reading as Ted' }).click()
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

    await expect(page).toHaveURL('/profiles')
    await expect.poll(() => page.evaluate(() => window.localStorage.getItem('pandapages.selected-reader-profile-id'))).toBeNull()
  })

  test('can create, rename, and delete the active reader with confirmation', async ({ page }) => {
    const api = new ProfilesApiMock(page)
    await api.install()

    await page.goto('/profiles')
    await page.getByLabel('New reader name').fill('  Ted  ')
    await page.getByRole('button', { name: 'Add reader' }).click()
    await expect(page).toHaveURL('/profiles')
    await expect(page.getByRole('heading', { level: 3, name: 'Ted' })).toBeVisible()
    await expect(page.getByText('Selected', { exact: true })).toBeVisible()

    await page.getByRole('button', { name: 'Manage reader' }).click()
    await page.getByRole('button', { name: 'Rename' }).click()
    await page.getByLabel('Reader name', { exact: true }).fill('Theo')
    await page.getByRole('button', { name: 'Save name' }).click()
    await expect(page.getByRole('heading', { level: 3, name: 'Theo' })).toBeVisible()
    await expect(page.getByText('Selected', { exact: true })).toBeVisible()

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
    await page.getByRole('button', { name: 'Start reading as Mina' }).click()
    await expect(page).toHaveURL('/library')
    await expect(page.getByRole('button', { name: 'Leave reader mode' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Parent options' })).toHaveCount(0)

    await page.getByRole('button', { name: 'Leave reader mode' }).click()
    await expect(page).toHaveURL('/profiles')
    await expect(page.getByRole('button', { name: 'Start reading as Mina' })).toBeVisible()
    await expect(page.getByText('Selected', { exact: true })).toBeVisible()
  })

  test('reader mode contains parent and account SPA routes until it is explicitly left', async ({ page }) => {
    const api = new ProfilesApiMock(page)
    api.profiles = [{ id: fixtureProfileID, name: 'Mina', pin_enabled: false }]
    await api.install()

    await page.goto('/profiles')
    await page.getByRole('button', { name: 'Start reading as Mina' }).click()
    await expect(page).toHaveURL('/library')

    for (const path of ['/profiles', '/admin/stories', '/account']) {
      await page.evaluate(async (target) => {
        type AppRouter = { push: (location: string) => Promise<unknown> }
        type VueAppHost = HTMLElement & {
          __vue_app__?: {
            config: { globalProperties: { $router: AppRouter } }
          }
        }
        const app = document.querySelector<VueAppHost>('#app')?.__vue_app__
        if (!app) throw new Error('Vue application was not mounted')
        await app.config.globalProperties.$router.push(target)
      }, path)

      await expect(page).toHaveURL('/library')
      await expect(page.getByRole('button', { name: 'Leave reader mode' })).toBeVisible()
      await expect(page.getByRole('heading', { level: 1, name: 'Parent Hub' })).toHaveCount(0)
      await expect(page.getByRole('heading', { level: 1, name: 'Choose an account' })).toHaveCount(0)
    }
  })

  test('a no-PIN reader restores reader mode after reload and direct Library navigation', async ({ page }) => {
    const api = new ProfilesApiMock(page)
    api.profiles = [{ id: fixtureProfileID, name: 'Mina', pin_enabled: false }]
    await api.install()

    await page.goto('/profiles')
    await page.getByRole('button', { name: 'Start reading as Mina' }).click()
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

  test('Parent Hub exposes account controls and owner-only Story Studio', async ({ page, auth }) => {
    const api = new ProfilesApiMock(page)
    api.profiles = [{ id: fixtureProfileID, name: 'Mina', pin_enabled: false }]
    await api.install()

    await page.goto('/profiles')
    await expect(page.getByRole('heading', { level: 1, name: 'Parent Hub' })).toBeVisible()
    await expect(page.getByRole('heading', { level: 2, name: 'Start reading' })).toBeVisible()
    await expect(page.getByRole('heading', { level: 2, name: 'My Panda Pages' })).toBeVisible()
    await expect(page.getByText('Signed in as Panda Pages Adult · Owner')).toBeVisible()
    await expect(page.getByRole('button', { name: /Story Studio/ })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible()

    auth.setRole('adult')
    await page.reload()
    await expect(page.getByText('Signed in as Panda Pages Adult · Adult member')).toBeVisible()
    await expect(page.getByRole('button', { name: /Story Studio/ })).toHaveCount(0)
  })

  test('Parent Hub signs out through Supabase and clears reader selection', async ({ page }) => {
    const api = new ProfilesApiMock(page)
    api.profiles = [{ id: fixtureProfileID, name: 'Mina', pin_enabled: false }]
    await api.install()
    let logoutCalls = 0
    await page.route('https://auth.invalid/auth/v1/logout**', async (route) => {
      logoutCalls += 1
      await route.fulfill({ status: 204 })
    })

    await page.goto('/profiles')
    await expect.poll(() =>
      page.evaluate(() => window.localStorage.getItem('pandapages.selected-reader-profile-id')),
    ).toBe(fixtureProfileID)
    await page.getByRole('button', { name: 'Sign out' }).click()

    await expect(page).toHaveURL('/account/login')
    expect(logoutCalls).toBe(1)
    await expect.poll(() =>
      page.evaluate(() => window.localStorage.getItem('pandapages.selected-reader-profile-id')),
    ).toBeNull()
  })

  test('a protected reader requires its PIN and never persists it or its unlock', async ({ page }) => {
    const api = new ProfilesApiMock(page)
    api.profiles = [{ id: fixtureProfileID, name: 'Mina', pin_enabled: true }]
    await api.install()

    await page.goto('/library')
    await expect(page).toHaveURL(/\/profiles\?next=\/library$/)
    await expect(page.getByText('PIN protected', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: 'Start reading as Mina' }).click()
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
    await page.getByRole('button', { name: 'Manage reader' }).click()
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
    await page.getByRole('button', { name: 'Start reading as Mina' }).click()
    await page.getByLabel('Four-digit PIN').fill('1234')
    await page.getByRole('button', { name: 'Continue' }).click()
    await expect(page.getByRole('alert')).toHaveText('Too many tries. Please wait before trying again.')
  })
})
