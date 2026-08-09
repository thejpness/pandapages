import type { Page, Route } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'

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

class ProfilesApiMock {
  profiles: Profile[] = []
  rateLimitPINVerification = false
  pinVerificationGate: Promise<void> | null = null
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
        if (this.pinVerificationGate) await this.pinVerificationGate
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
    await expect(page.getByRole('heading', { level: 1, name: 'Who’s reading?' })).toBeVisible(); await expect(page.getByRole('button', { name: 'Start reading as Mina' })).toBeVisible(); await expect(page.getByText('Classic', { exact: true })).toBeVisible(); await expect(page.getByRole('button', { name: 'Add profile' })).toBeVisible(); await expect(page.getByRole('button', { name: 'Manage profiles' })).toBeVisible(); await expect(page.getByLabel('New reader name')).toHaveCount(0); await expect(page.getByRole('button', { name: 'Edit reader' })).toHaveCount(0); await expect.poll(() => page.evaluate(() => window.localStorage.getItem('pandapages.selected-reader-profile-id'))).toBeNull(); await page.setViewportSize({ width: 320, height: 700 }); await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
  });
  test("PIN dialog is named, focus-contained, cancellable, and usable on a narrow viewport", async ({ page }) => {
    const api = new ProfilesApiMock(page)
    api.profiles = [{ id: fixtureProfileID, name: "Ted", pin_enabled: true, reading_level: "classic" }]
    await api.install()
    await page.setViewportSize({ width: 320, height: 700 })
    await page.goto("/profiles")

    const trigger = page.getByRole("button", { name: "Start reading as Ted" })
    await trigger.click()
    const dialog = page.getByRole("dialog", { name: "Enter Ted’s PIN" })
    const pin = dialog.getByLabel("Four-digit PIN")
    await expect(dialog).toBeVisible()
    await expect(dialog).toHaveAccessibleDescription("A four-digit PIN is required to start reading as Ted.")
    await expect(pin).toBeFocused()
    const violations = (await new AxeBuilder({ page }).analyze()).violations
      .filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
      .map((violation) => violation.id)
    expect(violations).toEqual([])
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)

    await page.keyboard.press("Tab")
    await expect(dialog.getByRole("button", { name: "Cancel" })).toBeFocused()
    await page.keyboard.press("Tab")
    await expect(dialog.getByRole("button", { name: "Continue" })).toBeFocused()
    await page.keyboard.press("Tab")
    await expect(pin).toBeFocused()
    await page.keyboard.press("Shift+Tab")
    await expect(dialog.getByRole("button", { name: "Continue" })).toBeFocused()

    await page.keyboard.press("Escape")
    await expect(dialog).toBeHidden()
    await expect(trigger).toBeFocused()
  })

  test('create is dedicated and does not start or select a reader', async ({ page }) => {
    const api = new ProfilesApiMock(page); await api.install(); await page.goto('/profiles/new?from=chooser'); await page.getByLabel('Reader name').fill('Ted'); await page.getByLabel('Reading level').selectOption('little-listeners'); await page.getByRole('button', { name: 'Create profile' }).click(); await expect(page).toHaveURL('/profiles'); await expect.poll(() => page.evaluate(() => window.localStorage.getItem('pandapages.selected-reader-profile-id'))).toBeNull(); await expect(page.getByRole('button', { name: 'Start reading as Ted' })).toBeVisible();
  });
  test('manage lists profiles and preserves parent utilities', async ({ page }) => {
    const api = new ProfilesApiMock(page); api.profiles = [{ id: fixtureProfileID, name: 'Mina', pin_enabled: false, reading_level: 'classic' }]; await api.install(); await page.goto('/profiles/manage'); await expect(page.getByRole('heading', { level: 1, name: 'Manage profiles' })).toBeVisible(); await expect(page.getByRole('button', { name: 'Edit Mina' })).toBeVisible(); await expect(page.getByRole('button', { name: 'Add profile' })).toBeVisible(); await expect(page.getByRole('button', { name: 'Story Studio' })).toBeVisible(); await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible();
  });
  test('edit supports rename, level, PIN controls, and exact deletion cleanup', async ({ page }) => {
    const api = new ProfilesApiMock(page); api.profiles = [{ id: fixtureProfileID, name: 'Mina', pin_enabled: false, reading_level: 'classic' }]; await api.install(); await page.addInitScript((id) => window.localStorage.setItem('pandapages.selected-reader-profile-id', id), fixtureProfileID); await page.goto(`/profiles/${fixtureProfileID}/edit`); await page.getByLabel('Reader name').fill('Theo'); await page.getByLabel('Reading level').selectOption('little-listeners'); await page.getByRole('button', { name: 'Save profile' }).click(); await expect(page).toHaveURL('/profiles/manage'); await page.getByRole('button', { name: 'Edit Theo' }).click(); await page.getByRole('button', { name: 'Set PIN' }).click(); await page.getByLabel('Four-digit PIN').fill('1234'); await page.getByRole('button', { name: 'Save PIN' }).click(); await expect(page.getByRole('button', { name: 'Remove PIN' })).toBeVisible(); await page.getByRole('button', { name: 'Delete profile' }).click(); await page.getByRole('alertdialog').getByRole('button', { name: 'Delete profile' }).click(); await expect(page).toHaveURL('/profiles/manage'); await expect.poll(() => page.evaluate(() => window.localStorage.getItem('pandapages.selected-reader-profile-id'))).toBeNull();
  });
  test("parent management is structured, responsive, and keeps reader entry separate", async ({ page }) => {
    const api = new ProfilesApiMock(page)
    api.profiles = [{ id: fixtureProfileID, name: "Mina", pin_enabled: true, reading_level: "little-listeners" }]
    await api.install()
    await page.setViewportSize({ width: 320, height: 700 })

    await page.goto("/profiles/manage")
    await expect(page.getByRole("heading", { level: 1, name: "Manage profiles" })).toBeVisible()
    await expect(page.getByRole("heading", { level: 2, name: "Reader profiles" })).toBeVisible()
    await expect(page.getByText("Little Listeners", { exact: true })).toBeVisible()
    await expect(page.getByText("PIN protected", { exact: true })).toBeVisible()
    await expect(page.getByRole("heading", { level: 2, name: "Account" })).toBeVisible()
    await expect(page.getByRole("button", { name: "Start reading as Mina" })).toHaveCount(0)
    const violations = (await new AxeBuilder({ page }).analyze()).violations
      .filter((violation) => violation.impact === "serious" || violation.impact === "critical")
      .map((violation) => violation.id)
    expect(violations).toEqual([])
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)

    await page.goto("/profiles/new?from=manage")
    await expect(page.getByRole("heading", { level: 1, name: "Add profile" })).toBeVisible()
    await expect(page.getByRole("group", { name: "Reader details" })).toBeVisible()
    await expect(page.getByLabel("Reader name")).toBeVisible()
    await expect(page.getByLabel("Reading level")).toBeVisible()
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)

    await page.goto("/profiles/" + fixtureProfileID + "/edit")
    await expect(page.getByRole("heading", { level: 1, name: "Edit Mina" })).toBeVisible()
    await expect(page.getByRole("heading", { level: 2, name: "PIN and access" })).toBeVisible()
    await expect(page.getByRole("heading", { level: 2, name: "Delete profile" })).toBeVisible()
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)

    const deleteTrigger = page.getByRole("button", { name: "Delete profile" })
    await deleteTrigger.click()
    const deleteDialog = page.getByRole("alertdialog", { name: "Delete Mina?" })
    await expect(deleteDialog).toBeVisible()
    await expect(deleteDialog.getByRole("button", { name: "Cancel" })).toBeFocused()
    await page.keyboard.press("Escape")
    await expect(deleteDialog).toBeHidden()
    await expect(deleteTrigger).toBeFocused()

    const pinTrigger = page.getByRole("button", { name: "Change PIN" })
    await pinTrigger.click()
    const pinDialog = page.getByRole("dialog", { name: "Change Mina’s PIN" })
    await expect(pinDialog.getByLabel("Four-digit PIN")).toBeFocused()
    await page.keyboard.press("Escape")
    await expect(pinDialog).toBeHidden()
    await expect(pinTrigger).toBeFocused()

    const removeTrigger = page.getByRole("button", { name: "Remove PIN" })
    await removeTrigger.click()
    const removeDialog = page.getByRole("alertdialog", { name: "Remove Mina’s PIN?" })
    await expect(removeDialog).toBeVisible()
    await expect(removeDialog.getByRole("button", { name: "Cancel" })).toBeFocused()
    await page.keyboard.press("Escape")
    await expect(removeDialog).toBeHidden()
    await expect(removeTrigger).toBeFocused()
  })

  test('management routes remain contained in reader mode', async ({ page }) => {
    const api = new ProfilesApiMock(page); api.profiles = [{ id: fixtureProfileID, name: 'Mina', pin_enabled: false, reading_level: 'classic' }]; await api.install(); await page.goto('/profiles'); await page.getByRole('button', { name: 'Start reading as Mina' }).click(); await expect(page).toHaveURL('/library'); for (const path of ['/profiles/new', '/profiles/manage', `/profiles/${fixtureProfileID}/edit`]) { await page.evaluate(async (target) => { const app = (document.querySelector('#app') as HTMLElement & { __vue_app__?: { config: { globalProperties: { $router: { push: (value: string) => Promise<unknown> } } } } }).__vue_app__; if (!app) throw new Error('Vue application was not mounted'); await app.config.globalProperties.$router.push(target); }, path); await expect(page).toHaveURL('/library'); }
  });
  test('reader mode opens the chooser for deliberate switching while hiding management', async ({ page }) => {
    const api = new ProfilesApiMock(page)
    const tedID = fixtureProfileID.slice(0, -1) + '1'
    api.profiles = [
      { id: fixtureProfileID, name: 'Mina', pin_enabled: false, reading_level: 'classic' },
      { id: tedID, name: 'Ted', pin_enabled: false, reading_level: 'classic' },
    ]
    await api.install()
    await page.goto('/profiles')
    await page.getByRole('button', { name: 'Start reading as Mina' }).click()
    await expect(page).toHaveURL('/library')
    await page.evaluate(async () => {
      const app = (document.querySelector('#app') as HTMLElement & { __vue_app__?: { config: { globalProperties: { $router: { push: (value: string) => Promise<unknown> } } } } }).__vue_app__
      if (!app) throw new Error('Vue application was not mounted')
      await app.config.globalProperties.$router.push('/profiles?next=/library')
    })
    await expect(page).toHaveURL(/\/profiles\?next=\/library$/)
    await expect(page.getByRole('button', { name: 'Add profile' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Manage profiles' })).toHaveCount(0)
    await expect(page.getByText('Current reader', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: 'Start reading as Ted' }).click()
    await expect(page).toHaveURL('/library')
    await expect.poll(() => page.evaluate(() => window.localStorage.getItem('pandapages.selected-reader-profile-id'))).toBe(tedID)
  })

  test('a failed or cancelled PIN switch retains the existing reader session', async ({ page }) => {
    const api = new ProfilesApiMock(page)
    const tedID = fixtureProfileID.slice(0, -1) + '1'
    api.profiles = [
      { id: fixtureProfileID, name: 'Mina', pin_enabled: false, reading_level: 'classic' },
      { id: tedID, name: 'Ted', pin_enabled: true, reading_level: 'classic' },
    ]
    await api.install()
    await page.goto('/profiles')
    await page.getByRole('button', { name: 'Start reading as Mina' }).click()
    await page.evaluate(async () => {
      const app = (document.querySelector('#app') as HTMLElement & { __vue_app__?: { config: { globalProperties: { $router: { push: (value: string) => Promise<unknown> } } } } }).__vue_app__
      if (!app) throw new Error('Vue application was not mounted')
      await app.config.globalProperties.$router.push('/profiles?next=/library')
    })
    const tedTrigger = page.getByRole('button', { name: 'Start reading as Ted' })
    await tedTrigger.click()
    await expect(page.getByLabel('Four-digit PIN')).toBeFocused()
    await page.getByLabel('Four-digit PIN').fill('0000')
    await page.getByRole('button', { name: 'Continue' }).click()
    await expect(page.getByRole('alert')).toHaveText('That PIN is not right.')
    await expect(page.getByLabel('Four-digit PIN')).toBeFocused()
    await expect.poll(() => page.evaluate(() => window.localStorage.getItem('pandapages.selected-reader-profile-id'))).toBe(fixtureProfileID)
    await page.getByRole('button', { name: 'Cancel' }).click()
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await expect.poll(() => page.evaluate(() => window.localStorage.getItem('pandapages.selected-reader-profile-id'))).toBe(fixtureProfileID)
    await expect(tedTrigger).toBeFocused()
    await tedTrigger.click()
    await page.getByLabel('Four-digit PIN').fill('1234')
    await page.getByRole('button', { name: 'Continue' }).click()
    await expect(page).toHaveURL('/library')
    await expect.poll(() => page.evaluate(() => window.localStorage.getItem('pandapages.selected-reader-profile-id'))).toBe(tedID)
  })

  test("a pending PIN verification cannot be dismissed into a later reader switch", async ({ page }) => {
    const api = new ProfilesApiMock(page)
    api.profiles = [{ id: fixtureProfileID, name: "Ted", pin_enabled: true, reading_level: "classic" }]
    let releaseVerification: () => void = () => { throw new Error("PIN verification release was not prepared") }
    api.pinVerificationGate = new Promise<void>((resolve) => { releaseVerification = resolve })
    await api.install()
    await page.goto("/profiles")

    await page.getByRole("button", { name: "Start reading as Ted" }).click()
    const dialog = page.getByRole("dialog", { name: "Enter Ted’s PIN" })
    await dialog.getByLabel("Four-digit PIN").fill("1234")
    await dialog.getByRole("button", { name: "Continue" }).click()
    await expect(dialog.getByRole("button", { name: "Continue" })).toBeDisabled()
    await page.keyboard.press("Escape")
    await expect(dialog).toBeVisible()
    await expect.poll(() => page.evaluate(() => window.localStorage.getItem("pandapages.selected-reader-profile-id"))).toBeNull()

    releaseVerification()
    await expect(page).toHaveURL("/library")
    await expect.poll(() => page.evaluate(() => window.localStorage.getItem("pandapages.selected-reader-profile-id"))).toBe(fixtureProfileID)
  })

  test('the active PIN reader continues without a second verification', async ({ page }) => {
    const api = new ProfilesApiMock(page)
    api.profiles = [{ id: fixtureProfileID, name: 'Mina', pin_enabled: true, reading_level: 'classic' }]
    await api.install()
    await page.goto('/profiles')
    await page.getByRole('button', { name: 'Start reading as Mina' }).click()
    await page.getByLabel('Four-digit PIN').fill('1234')
    await page.getByRole('button', { name: 'Continue' }).click()
    await expect(page).toHaveURL('/library')
    await page.evaluate(async () => {
      const app = (document.querySelector('#app') as HTMLElement & { __vue_app__?: { config: { globalProperties: { $router: { push: (value: string) => Promise<unknown> } } } } }).__vue_app__
      if (!app) throw new Error('Vue application was not mounted')
      await app.config.globalProperties.$router.push('/profiles?next=/library')
    })
    await page.getByRole('button', { name: 'Start reading as Mina' }).click()
    await expect(page).toHaveURL('/library')
    await expect(page.getByRole('dialog')).toHaveCount(0)
  })
})
