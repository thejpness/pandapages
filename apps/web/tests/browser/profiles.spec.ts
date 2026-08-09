import type { Page, Route } from '@playwright/test'

import {
  expect,
  fixtureAccessToken,
  fixtureAccountID,
  fixtureProfileID,
  test,
} from './support/auth'

type Profile = {
  id: string
  name: string
  pin_enabled: boolean
  reading_level:
    | 'classic'
    | 'confident-readers'
    | 'growing-readers'
    | 'story-explorers'
    | 'little-listeners'
}
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
    expect(headers.cookie).toBeFalsy()

    if (request.method() === 'GET' && url.pathname === '/api/v1/library') {
      const profileID = headers['x-pp-profile-id']
      expect(profileID).toBeTruthy()
      expect(this.profiles.some((candidate) => candidate.id === profileID)).toBe(true)
      await this.respond(route, { items: [], unavailableItemCount: 0 })
      return
    }

    expect(headers['x-pp-profile-id']).toBeFalsy()

    if (request.method() === 'GET' && url.pathname === '/api/v1/profiles') {
      await this.respond(route, { profiles: this.profiles })
      return
    }
    if (request.method() === 'POST' && url.pathname === '/api/v1/profiles') {
      const body = request.postDataJSON() as {
        name: string
        readingLevel: Profile['reading_level']
      }
      const profile: Profile = {
        id: fixtureProfileID.slice(0, -1) + String(this.profiles.length + 1),
        name: body.name.trim(),
        pin_enabled: false,
        reading_level: body.readingLevel,
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
        const body = request.postDataJSON() as {
          name: string
          readingLevel: Profile['reading_level']
        }
        profile.name = body.name.trim()
        profile.reading_level = body.readingLevel
        await this.respond(route, profile)
        return
      }
      if (action === 'pin' && request.method() === 'PUT') {
        profile.pin_enabled = true
        await this.respond(route, { pin_enabled: true, reading_level: 'classic' })
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
        await this.respond(route, { pin_enabled: false, reading_level: 'classic' })
        return
      }
      if (request.method() === 'DELETE') {
        this.profiles = this.profiles.filter((candidate) => candidate.id !== id)
        await this.respond(route, null, 204)
        return
      }
    }
    await this.respond(route, { error: { code: 'not_found' } }, 404)
  }
}

test.describe('reader profile lifecycle', () => {
  test('chooser is focused and one-profile convenience does not persist authority', async ({ page }) => {
    const api = new ProfilesApiMock(page); api.profiles = [{ id: fixtureProfileID, name: 'Mina', pin_enabled: false, reading_level: 'classic' }]; await api.install(); await page.goto('/profiles');
    await expect(page.getByRole('heading', { level: 1, name: 'Who’s reading?' })).toBeVisible(); await expect(page.getByRole('button', { name: 'Start reading as Mina' })).toBeVisible(); await expect(page.getByLabel('New reader name')).toHaveCount(0); await expect(page.getByRole('button', { name: 'Edit reader' })).toHaveCount(0); await expect.poll(() => page.evaluate(() => window.localStorage.getItem('pandapages.selected-reader-profile-id'))).toBeNull();
  });
  test('create is dedicated and does not start or select a reader', async ({ page }) => {
    const api = new ProfilesApiMock(page); await api.install(); await page.goto('/profiles/new?from=chooser'); await page.getByLabel('Reader name').fill('Ted'); await page.getByLabel('Reading level').selectOption('little-listeners'); await page.getByRole('button', { name: 'Create profile' }).click(); await expect(page).toHaveURL('/profiles'); await expect.poll(() => page.evaluate(() => window.localStorage.getItem('pandapages.selected-reader-profile-id'))).toBeNull(); await expect(page.getByRole('button', { name: 'Start reading as Ted' })).toBeVisible();
  });
  test('manage lists profiles and preserves parent utilities', async ({ page }) => {
    const api = new ProfilesApiMock(page); api.profiles = [{ id: fixtureProfileID, name: 'Mina', pin_enabled: false, reading_level: 'classic' }]; await api.install(); await page.goto('/profiles/manage'); await expect(page.getByRole('heading', { level: 1, name: 'Manage profiles' })).toBeVisible(); await expect(page.getByRole('button', { name: 'Edit Mina' })).toBeVisible(); await expect(page.getByRole('button', { name: 'Add profile' })).toBeVisible(); await expect(page.getByRole('button', { name: 'Story Studio' })).toBeVisible(); await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible();
  });
  test('edit supports rename, level, PIN controls, and exact deletion cleanup', async ({ page }) => {
    const api = new ProfilesApiMock(page); api.profiles = [{ id: fixtureProfileID, name: 'Mina', pin_enabled: false, reading_level: 'classic' }]; await api.install(); await page.addInitScript((id) => window.localStorage.setItem('pandapages.selected-reader-profile-id', id), fixtureProfileID); await page.goto(`/profiles/${fixtureProfileID}/edit`); await page.getByLabel('Reader name').fill('Theo'); await page.getByLabel('Reading level').selectOption('little-listeners'); await page.getByRole('button', { name: 'Save profile' }).click(); await expect(page).toHaveURL('/profiles/manage'); await page.getByRole('button', { name: 'Edit Theo' }).click(); await page.getByRole('button', { name: 'Set PIN' }).click(); await page.getByLabel('Four-digit PIN').fill('1234'); await page.getByRole('button', { name: 'Save PIN' }).click(); await expect(page.getByRole('button', { name: 'Remove PIN' })).toBeVisible(); await page.getByRole('button', { name: 'Delete profile' }).click(); await page.getByRole('alertdialog').getByRole('button', { name: 'Delete profile' }).click(); await expect(page).toHaveURL('/profiles/manage'); await expect.poll(() => page.evaluate(() => window.localStorage.getItem('pandapages.selected-reader-profile-id'))).toBeNull();
  });
  test('management routes remain contained in reader mode', async ({ page }) => {
    const api = new ProfilesApiMock(page); api.profiles = [{ id: fixtureProfileID, name: 'Mina', pin_enabled: false, reading_level: 'classic' }]; await api.install(); await page.goto('/profiles'); await page.getByRole('button', { name: 'Start reading as Mina' }).click(); await expect(page).toHaveURL('/library'); for (const path of ['/profiles/new', '/profiles/manage', `/profiles/${fixtureProfileID}/edit`]) { await page.evaluate(async (target) => { const app = (document.querySelector('#app') as HTMLElement & { __vue_app__?: { config: { globalProperties: { $router: { push: (value: string) => Promise<unknown> } } } } }).__vue_app__; if (!app) throw new Error('Vue application was not mounted'); await app.config.globalProperties.$router.push(target); }, path); await expect(page).toHaveURL('/library'); }
  });
})
