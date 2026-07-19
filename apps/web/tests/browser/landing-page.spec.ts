import { AxeBuilder } from '@axe-core/playwright'
import { expect, test, type Page, type Route } from '@playwright/test'

const FEATURED_SLUG = 'the-three-little-pigs'

async function gotoLanding(page: Page): Promise<void> {
  await page.goto('/')
  await expect(
    page.getByRole('heading', {
      level: 1,
      name: 'Stories that never grow old.',
    }),
  ).toBeVisible()
}

async function fulfillJson(
  route: Route,
  body: unknown,
  status = 200,
): Promise<void> {
  await route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(body),
  })
}

async function installSessionRoutes(
  page: Page,
  initiallyUnlocked = false,
): Promise<void> {
  let unlocked = initiallyUnlocked
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const url = new URL(request.url())

    if (
      request.method() === 'GET' &&
      url.pathname === '/api/v1/auth/status'
    ) {
      await fulfillJson(route, { unlocked })
      return
    }

    if (
      request.method() === 'POST' &&
      url.pathname === '/api/v1/auth/unlock'
    ) {
      unlocked = true
      await fulfillJson(route, { ok: true })
      return
    }

    if (
      request.method() === 'GET' &&
      url.pathname === '/api/v1/continue'
    ) {
      await fulfillJson(route, { items: [] })
      return
    }

    if (
      request.method() === 'GET' &&
      url.pathname === '/api/v1/library'
    ) {
      await fulfillJson(route, { items: [] })
      return
    }

    if (
      request.method() === 'GET' &&
      url.pathname === '/api/v1/settings'
    ) {
      await fulfillJson(route, {
        child: {
          name: '',
          ageMonths: 0,
          interests: [],
          sensitivities: [],
        },
        prompt: {
          name: 'Default',
          schemaVersion: 1,
          rules: {},
        },
      })
      return
    }

    await fulfillJson(
      route,
      { error: { code: 'not_found', message: 'Test route not found' } },
      404,
    )
  })
}

async function expectRoute(
  page: Page,
  pathname: string,
  next: string | null = null,
): Promise<void> {
  await expect
    .poll(() => {
      const url = new URL(page.url())
      return {
        pathname: url.pathname,
        next: url.searchParams.get('next'),
      }
    })
    .toEqual({ pathname, next })
}

async function dispatchInstallPrompt(
  page: Page,
  outcome: 'accepted' | 'dismissed',
): Promise<void> {
  await page.evaluate((selectedOutcome) => {
    const target = window as Window & {
      __installPromptCalls?: number
    }
    target.__installPromptCalls = 0
    const event = new Event('beforeinstallprompt', {
      cancelable: true,
    })
    Object.defineProperties(event, {
      prompt: {
        value: () => {
          target.__installPromptCalls =
            (target.__installPromptCalls ?? 0) + 1
          return Promise.resolve()
        },
      },
      userChoice: {
        value: Promise.resolve({ outcome: selectedOutcome }),
      },
    })
    window.dispatchEvent(event)
  }, outcome)
}

async function expectNoHorizontalOverflow(page: Page): Promise<void> {
  expect(
    await page.evaluate(
      () =>
        document.documentElement.scrollWidth <=
        document.documentElement.clientWidth,
    ),
  ).toBe(true)
}

async function expectNoSeriousOrCriticalViolations(
  page: Page,
): Promise<void> {
  await page.evaluate(async () => {
    await document.fonts.ready
  })
  const results = await new AxeBuilder({ page }).analyze()
  const violations = results.violations
    .filter(
      (violation) =>
        violation.impact === 'serious' || violation.impact === 'critical',
    )
    .map((violation) => ({
      id: violation.id,
      impact: violation.impact,
      help: violation.help,
      nodes: violation.nodes.map((node) => ({
        target: node.target,
        summary: node.failureSummary,
      })),
    }))
  expect(violations).toEqual([])
}

test.describe('Public landing page', () => {
  test('root is public and exposes only canonical application routes', async ({
    page,
  }) => {
    const authStatusRequests: string[] = []
    page.on('request', (request) => {
      if (new URL(request.url()).pathname === '/api/v1/auth/status') {
        authStatusRequests.push(request.url())
      }
    })

    await gotoLanding(page)
    await page.evaluate(async () => {
      await document.fonts.ready
    })

    expect(authStatusRequests).toEqual([])
    await expectRoute(page, '/')
    await expect(
      page.getByRole('link', { name: 'Skip to main content' }),
    ).toHaveAttribute('href', '#main-content')
    await expect(page.getByRole('banner')).toHaveCount(1)
    await expect(page.getByRole('main')).toHaveAttribute('id', 'main-content')
    await expect(page.getByRole('contentinfo')).toHaveCount(1)

    for (const name of [
      'Browse stories',
      'Start reading',
      'Explore the complete library',
      'Browse all stories',
    ]) {
      await expect(
        page.getByRole('link', { name, exact: true }),
      ).toHaveAttribute('href', '/library')
    }

    for (const slug of [
      FEATURED_SLUG,
      'jack-and-the-beanstalk',
      'the-wonderful-wizard-of-oz',
    ]) {
      await expect(page.locator('a[href="/read/' + slug + '"]')).toHaveCount(2)
    }

    await expect(page.locator('a[href^="/stories"]')).toHaveCount(0)
    expect(await page.locator('body').innerText()).not.toContain(
      'No app store or account required',
    )
  })

  test('primary CTA preserves the library destination through Unlock and returns after success', async ({
    page,
  }) => {
    await installSessionRoutes(page)
    await gotoLanding(page)

    const startReading = page.getByRole('link', {
      name: 'Start reading',
      exact: true,
    })
    await expect(startReading).toHaveAttribute('href', '/library')
    await startReading.click()
    await expectRoute(page, '/unlock', '/library')

    await page.locator('input[autocomplete="one-time-code"]').fill('123456')
    await expectRoute(page, '/library')
  })

  test('featured story CTA preserves its intended Reader destination through Unlock', async ({
    page,
  }) => {
    await installSessionRoutes(page)
    await gotoLanding(page)

    const story = page.locator(
      'a[href="/read/' + FEATURED_SLUG + '"]',
    ).first()
    await expect(story).toHaveAttribute(
      'href',
      '/read/' + FEATURED_SLUG,
    )
    await story.click()
    await expectRoute(page, '/unlock', '/read/' + FEATURED_SLUG)

    await page.locator('input[autocomplete="one-time-code"]').fill('123456')
    await expectRoute(page, '/read/' + FEATURED_SLUG)
  })

  test('accepts the native install prompt when beforeinstallprompt is available', async ({
    page,
  }) => {
    await gotoLanding(page)
    await dispatchInstallPrompt(page, 'accepted')

    const trigger = page.locator('.site-header').getByRole('button', {
      name: 'Install Panda Pages',
    })
    await expect(trigger).toBeVisible()
    await trigger.click()

    expect(
      await page.evaluate(
        () =>
          (window as Window & { __installPromptCalls?: number })
            .__installPromptCalls,
      ),
    ).toBe(1)
    await expect(page.locator('[aria-live="polite"]')).toContainText(
      'installation accepted',
    )
    await expect(page.getByRole('dialog')).toHaveCount(0)
  })

  test('reports a dismissed native install prompt without opening fallback help', async ({
    page,
  }) => {
    await gotoLanding(page)
    await dispatchInstallPrompt(page, 'dismissed')

    await page.locator('.site-header').getByRole('button', {
      name: 'Install Panda Pages',
    }).click()

    await expect(page.locator('[aria-live="polite"]')).toContainText(
      'Installation dismissed',
    )
    await expect(page.getByRole('dialog')).toHaveCount(0)
  })

  test('generic install help is named, focus-trapped, dismissible, and returns focus', async ({
    page,
  }) => {
    await gotoLanding(page)
    const trigger = page.locator('.site-header').getByRole('button', {
      name: 'Add Panda Pages',
    })
    await trigger.click()

    const dialog = page.getByRole('dialog', {
      name: 'Add Panda Pages to this device',
    })
    await expect(dialog).toBeVisible()
    await expect(dialog).toContainText('create-shortcut option')
    await expect
      .poll(() =>
        dialog.evaluate((element) =>
          element.contains(document.activeElement),
        ),
      )
      .toBe(true)

    const close = dialog.getByRole('button', {
      name: 'Close install instructions',
    })
    const confirm = dialog.getByRole('button', { name: 'Got it' })
    await confirm.focus()
    await page.keyboard.press('Tab')
    await expect(close).toBeFocused()
    await page.keyboard.press('Shift+Tab')
    await expect(confirm).toBeFocused()

    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()
    await expect(trigger).toBeFocused()
  })

  for (const platform of [
    {
      name: 'iOS',
      userAgent:
        'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148 Safari/604.1',
      platform: 'iPhone',
      maxTouchPoints: 5,
      title: 'Add to your Home Screen',
      instructions: ['Safari', 'Share button', 'Add to Home Screen'],
    },
    {
      name: 'Android',
      userAgent:
        'Mozilla/5.0 (Linux; Android 15; Pixel 9) AppleWebKit/537.36 Chrome/140.0 Mobile Safari/537.36',
      platform: 'Linux armv8l',
      maxTouchPoints: 5,
      title: 'Install from your browser menu',
      instructions: ['browser menu', 'Install app', 'Confirm'],
    },
  ]) {
    test(platform.name + ' fallback install instructions remain usable', async ({
      page,
    }) => {
      await page.addInitScript((device) => {
        Object.defineProperties(window.navigator, {
          userAgent: { configurable: true, get: () => device.userAgent },
          platform: { configurable: true, get: () => device.platform },
          maxTouchPoints: {
            configurable: true,
            get: () => device.maxTouchPoints,
          },
        })
      }, platform)

      await gotoLanding(page)
      await page.locator('.site-header').getByRole('button', {
        name: 'Add Panda Pages',
      }).click()

      const dialog = page.getByRole('dialog', { name: platform.title })
      await expect(dialog).toBeVisible()
      for (const instruction of platform.instructions) {
        await expect(dialog).toContainText(instruction)
      }
      await dialog.getByRole('button', { name: 'Got it' }).click()
      await expect(dialog).toBeHidden()
    })
  }

  test('installed PWA button opens the protected library', async ({ page }) => {
    await page.addInitScript(() => {
      const original = window.matchMedia.bind(window)
      window.matchMedia = (query: string) => {
        if (query !== '(display-mode: standalone)') return original(query)
        return {
          matches: true,
          media: query,
          onchange: null,
          addListener: () => undefined,
          removeListener: () => undefined,
          addEventListener: () => undefined,
          removeEventListener: () => undefined,
          dispatchEvent: () => false,
        } as MediaQueryList
      }
    })
    await installSessionRoutes(page, true)
    await gotoLanding(page)

    const trigger = page.locator('.site-header').getByRole('button', {
      name: 'Open Panda Pages',
    })
    await expect(trigger).toBeVisible()
    await trigger.click()
    await expectRoute(page, '/library')
  })

  test('skip link moves keyboard focus to the main landmark', async ({ page }) => {
    await gotoLanding(page)
    const skip = page.getByRole('link', { name: 'Skip to main content' })

    await page.keyboard.press('Tab')
    await expect(skip).toBeFocused()
    await page.keyboard.press('Enter')
    await expect(page.locator('main#main-content')).toBeFocused()
  })

  test('320px mobile layout has no horizontal document overflow', async ({
    page,
  }) => {
    await page.setViewportSize({ width: 320, height: 640 })
    await gotoLanding(page)

    await expectNoHorizontalOverflow(page)
    await expect(
      page.getByRole('link', { name: 'Start reading' }),
    ).toBeVisible()
  })

  test('1440px desktop layout has no horizontal document overflow', async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    await gotoLanding(page)

    await expectNoHorizontalOverflow(page)
    await expect(page.locator('.story-grid .story-card')).toHaveCount(3)
    await expect(page.locator('.hero-visual')).toBeVisible()
  })

  test('axe reports no serious or critical violations', async ({ page }) => {
    await gotoLanding(page)
    await expectNoSeriousOrCriticalViolations(page)
  })
})
