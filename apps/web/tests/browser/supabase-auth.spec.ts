import { AxeBuilder } from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'

const accessToken = 'local-access-token-without-private-claims'
const refreshToken = 'local-refresh-token-owned-by-official-client'

const identityResponse = {
  authenticated: true,
  principal: {
    id: '123e4567-e89b-12d3-a456-426614174100',
    displayName: 'Panda Pages Adult',
  },
  memberships: [
    {
      accountId: '123e4567-e89b-12d3-a456-426614174200',
      accountName: 'My Panda Pages',
      role: 'owner',
    },
  ],
}

function tokenResponse(provider: 'google' | 'facebook') {
  const now = new Date().toISOString()
  return {
    access_token: accessToken,
    token_type: 'bearer',
    expires_in: 3600,
    expires_at: Math.floor(Date.now() / 1000) + 3600,
    refresh_token: refreshToken,
    user: {
      id: '123e4567-e89b-12d3-a456-426614174000',
      aud: 'authenticated',
      role: 'authenticated',
      email: 'adult@example.test',
      email_confirmed_at: now,
      confirmed_at: now,
      last_sign_in_at: now,
      app_metadata: { provider, providers: [provider] },
      user_metadata: {},
      identities: [],
      created_at: now,
      updated_at: now,
      is_anonymous: false,
    },
  }
}

async function expectNoSeriousOrCriticalViolations(page: Page): Promise<void> {
  await page.evaluate(async () => {
    await document.fonts.ready
  })
  const violations = (await new AxeBuilder({ page }).analyze()).violations
    .filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
    .map((violation) => ({
      id: violation.id,
      impact: violation.impact,
      targets: violation.nodes.map((node) => node.target),
    }))
  expect(violations).toEqual([])
}

for (const provider of ['google', 'facebook'] as const) {
  test(`official ${provider} PKCE callback onboards with bearer only, restores identity, and logs out`, async ({ page }) => {
  let authorizeURL: URL | undefined
  let tokenExchange = false
  let logoutCalled = false
  const authRequests: Array<{ method: string; authorization: string; cookie: string }> = []

  await page.route('https://auth.invalid/**', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    if (url.pathname === '/auth/v1/authorize') {
      authorizeURL = url
      await route.fulfill({ status: 200, contentType: 'text/html', body: '<!doctype html><title>Local provider fixture</title>' })
      return
    }
    if (url.pathname === '/auth/v1/token') {
      expect(request.method()).toBe('POST')
      expect(url.searchParams.get('grant_type')).toBe('pkce')
      const body = request.postData() || ''
      expect(body).toContain('auth-code-fixture')
      expect(body).toContain('code_verifier')
      tokenExchange = true
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(tokenResponse(provider)) })
      return
    }
    if (url.pathname === '/auth/v1/logout') {
      expect(request.method()).toBe('POST')
      expect(request.headers().authorization).toBe(`Bearer ${accessToken}`)
      logoutCalled = true
      await route.fulfill({ status: 204 })
      return
    }
    await route.abort('blockedbyclient')
  })

  await page.route('**/api/auth/**', async (route) => {
    const request = route.request()
    authRequests.push({
      method: request.method(),
      authorization: request.headers().authorization || '',
      cookie: request.headers().cookie || '',
    })
    if (new URL(request.url()).pathname === '/api/auth/onboard') {
      expect(request.method()).toBe('POST')
      expect(request.postData()).toBeNull()
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ ...identityResponse, created: true }),
      })
      return
    }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(identityResponse) })
  })

  await page.goto('/account/login')
  await expect(page.getByRole('heading', { name: 'Sign in to Panda Pages' })).toBeVisible()
  await expect(page.getByText('Use your adult account to continue to Panda Pages.')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Continue with Google' })).toBeVisible()
  const providerButton = page.getByRole('button', { name: `Continue with ${provider[0].toUpperCase()}${provider.slice(1)}` })
  if (provider === 'facebook') {
    await providerButton.focus()
    await page.keyboard.press('Enter')
  } else {
    await providerButton.click()
  }

  await expect.poll(() => authorizeURL?.pathname).toBe('/auth/v1/authorize')
  await page.waitForURL('https://auth.invalid/auth/v1/authorize**')
  expect(authorizeURL?.searchParams.get('provider')).toBe(provider)
  expect(authorizeURL?.searchParams.get('code_challenge_method')).toBe('s256')
  expect(authorizeURL?.searchParams.get('code_challenge')).toMatch(/^[A-Za-z0-9_-]{43}$/)
  expect(authorizeURL?.searchParams.get('redirect_to')).toBe('http://127.0.0.1:4173/auth/callback')

  await page.goto('/auth/callback?code=auth-code-fixture')
  await expect(page).toHaveURL(/\/account$/)
  await expect(page.getByRole('heading', { name: 'Choose an account' })).toBeVisible()
  await expect(page.getByText('My Panda Pages')).toBeVisible()
  await expect(page.getByText('Your account choice is checked against current memberships on every request.')).toBeVisible()
  expect(tokenExchange).toBe(true)
  expect(authRequests).toHaveLength(2)
  expect(authRequests.map((request) => request.method)).toEqual(['POST', 'GET'])
  for (const request of authRequests) {
    expect(request.authorization).toBe(`Bearer ${accessToken}`)
    expect(request.cookie).toBe('')
  }

  await page.getByRole('button', { name: 'Sign out' }).click()
  await expect(page).toHaveURL(/\/account\/login$/)
  expect(logoutCalled).toBe(true)
  const stored = await page.evaluate(() => JSON.stringify(window.localStorage))
  expect(stored).not.toContain(accessToken)
  expect(stored).not.toContain(refreshToken)
  })
}

test('login keeps one provider launch pending at a time', async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(crypto.subtle, 'digest', {
      configurable: true,
      value: () => new Promise(() => {}),
    })
  })

  await page.goto('/account/login')
  const google = page.getByRole('button', { name: 'Continue with Google' })
  const facebook = page.getByRole('button', { name: 'Continue with Facebook' })
  await google.focus()
  await page.keyboard.press('Enter')

  await expect(page.getByRole('button', { name: 'Opening secure sign-in…' })).toBeDisabled()
  await expect(facebook).toBeDisabled()
  await expect(page.locator('.identity-actions')).toHaveAttribute('aria-busy', 'true')
})

test('login initiation failure stays on Panda Pages and presents a safe error', async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(crypto, 'getRandomValues', {
      configurable: true,
      value: () => {
        throw new Error('test-only OAuth random failure')
      },
    })
  })

  await page.goto('/account/login')
  await page.getByRole('button', { name: 'Continue with Facebook' }).click()

  const error = page.getByRole('alert')
  await expect(error).toHaveText('Secure sign-in could not start. Check the local Auth configuration and try again.')
  await expect(error).not.toContainText('test-only')
  await expect(page.getByRole('button', { name: 'Continue with Google' })).toBeEnabled()
  await expect(page.getByRole('button', { name: 'Continue with Facebook' })).toBeEnabled()
  await expect(page).toHaveURL(/\/account\/login$/)
})

test('login provider controls are accessible and fit a narrow viewport', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 568 })
  await page.goto('/account/login')

  const google = page.getByRole('button', { name: 'Continue with Google' })
  const facebook = page.getByRole('button', { name: 'Continue with Facebook' })
  await expect(google).toBeVisible()
  await expect(facebook).toBeVisible()
  for (const mark of [google.locator('img'), facebook.locator('img')]) {
    await expect(mark).toHaveAttribute('alt', '')
    await expect(mark).toHaveAttribute('aria-hidden', 'true')
  }

  const [googleBox, facebookBox, scrollWidth] = await Promise.all([
    google.boundingBox(),
    facebook.boundingBox(),
    page.evaluate(() => document.documentElement.scrollWidth),
  ])
  expect(googleBox?.width).toBe(facebookBox?.width)
  expect(googleBox?.height).toBe(facebookBox?.height)
  expect(scrollWidth).toBeLessThanOrEqual(320)
  await expectNoSeriousOrCriticalViolations(page)
})

test('identity session restoration fails closed without an official session', async ({ page }) => {
  await page.goto('/account')
  await expect(page).toHaveURL(/\/account\/login$/)
})
