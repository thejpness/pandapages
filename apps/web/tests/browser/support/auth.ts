import { expect, test as base, type Page, type Route } from '@playwright/test'

export const fixtureAccessToken = 'browser-fixture-access-token'
export const fixtureAccountID = '123e4567-e89b-12d3-a456-426614174200'
export const fixturePrincipalID = '123e4567-e89b-12d3-a456-426614174100'

type Role = 'owner' | 'adult'

export type BrowserAuth = {
  accountID: string
  accessToken: string
  setRole: (role: Role) => void
  setMemberships: (memberships: Array<{ accountId: string; accountName: string; role: Role }>) => void
  requireOnboarding: () => void
  assertApplicationRequest: (route: Route) => void
}

function session() {
  const now = new Date().toISOString()
  return {
    access_token: fixtureAccessToken,
    refresh_token: 'browser-fixture-refresh-token',
    token_type: 'bearer',
    expires_in: 60 * 60,
    expires_at: 4_102_444_800,
    user: {
      id: fixturePrincipalID,
      aud: 'authenticated',
      role: 'authenticated',
      email: 'adult@example.test',
      email_confirmed_at: now,
      confirmed_at: now,
      last_sign_in_at: now,
      app_metadata: { provider: 'google', providers: ['google'] },
      user_metadata: {},
      identities: [],
      created_at: now,
      updated_at: now,
      is_anonymous: false,
    },
  }
}

async function installOfficialSession(page: Page): Promise<void> {
  await page.addInitScript((value) => {
    window.localStorage.setItem('sb-auth-auth-token', JSON.stringify(value))
  }, session())
}

export const test = base.extend<{ auth: BrowserAuth }>({
  auth: [async ({ page }, use) => {
    let memberships = [{ accountId: fixtureAccountID, accountName: 'My Panda Pages', role: 'owner' as Role }]
    let onboardingRequired = false
    await installOfficialSession(page)
    await page.route('**/api/auth/**', async (route) => {
      const request = route.request()
      const url = new URL(request.url())
      expect(request.headers().authorization).toBe(`Bearer ${fixtureAccessToken}`)
      expect(request.headers().cookie).toBeFalsy()
      if (url.pathname === '/api/auth/me' && onboardingRequired) {
        await route.fulfill({ status: 409, contentType: 'application/json', body: JSON.stringify({ error: 'onboarding_required' }) })
        return
      }
      if (url.pathname === '/api/auth/onboard') onboardingRequired = false
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ authenticated: true, principal: { id: fixturePrincipalID, displayName: 'Panda Pages Adult' }, memberships }) })
    })
    await use({
      accountID: fixtureAccountID,
      accessToken: fixtureAccessToken,
      setRole: (role) => { memberships = [{ ...memberships[0], role }] },
      setMemberships: (value) => { memberships = value },
      requireOnboarding: () => { onboardingRequired = true },
      assertApplicationRequest: (route) => {
        const headers = route.request().headers()
        expect(headers.authorization).toBe(`Bearer ${fixtureAccessToken}`)
        expect(headers['x-pp-account-id']).toBe(fixtureAccountID)
        expect(headers.cookie).toBeFalsy()
      },
    })
  }, { auto: true }],
})

export { expect }
