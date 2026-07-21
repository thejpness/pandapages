import { AxeBuilder } from '@axe-core/playwright'
import {
  expect,
  test as base,
  type Page,
  type Route,
} from '@playwright/test'

type SettingsFixture = {
  child: {
    id?: string
    name: string
    ageMonths: number
    interests: string[]
    sensitivities: string[]
  }
  prompt: {
    id?: string
    name: string
    schemaVersion: number
    rules: Record<string, unknown>
  }
}

type CapturedRequest = {
  method: string
  pathname: string
}

const DEFAULT_SETTINGS: SettingsFixture = {
  child: {
    id: 'child-test-id',
    name: 'Mina',
    ageMonths: 72,
    interests: ['Stars'],
    sensitivities: ['Loud storms'],
  },
  prompt: {
    id: 'prompt-test-id',
    name: 'Default prompt v1',
    schemaVersion: 1,
    rules: {
      tone: 'cosy',
      genre: 'animals',
      readingTimeMinutes: 8,
      complexity: 'simple',
    },
  },
}

async function fulfillJson(
  route: Route,
  body: unknown,
  status = 200,
): Promise<void> {
  await route.fulfill({
    status,
    contentType: 'application/json; charset=utf-8',
    headers: { 'Cache-Control': 'no-store' },
    body: JSON.stringify(body),
  })
}

class JourneyApiMock {
  readonly requests: CapturedRequest[] = []
  readonly savedPayloads: SettingsFixture[] = []
  readonly unhandledRequests: CapturedRequest[] = []

  settings: unknown = structuredClone(DEFAULT_SETTINGS)
  private nextLoadFailure: { status: number; body: unknown } | null = null
  private abortNextLoadRequest = false
  private malformedNextLoadResponse = false
  private nextSaveFailure: { status: number; body: unknown } | null = null
  private abortNextSaveRequest = false
  private readonly page: Page

  constructor(page: Page) {
    this.page = page
  }

  async install(): Promise<void> {
    await this.page.route('**/api/v1/**', async (route) => {
      await this.handle(route)
    })
  }

  setSettings(settings: unknown): void {
    this.settings = structuredClone(settings)
  }

  failNextLoad(status = 503): void {
    this.nextLoadFailure = {
      status,
      body: {
        error: {
          code: status === 401 ? 'unlock_required' : 'settings_unavailable',
          message:
            status === 401 ? 'unlock required' : 'Reading profile unavailable.',
        },
      },
    }
  }

  abortNextLoad(): void {
    this.abortNextLoadRequest = true
  }

  malformNextLoad(): void {
    this.malformedNextLoadResponse = true
  }

  failNextSave(status = 503): void {
    this.nextSaveFailure = {
      status,
      body: {
        error: {
          code: 'settings_unavailable',
          message: 'Reading profile could not be saved.',
        },
      },
    }
  }

  abortNextSave(): void {
    this.abortNextSaveRequest = true
  }

  count(method: string, pathname: string): number {
    return this.requests.filter(
      (request) =>
        request.method === method && request.pathname === pathname,
    ).length
  }

  private async handle(route: Route): Promise<void> {
    const request = route.request()
    const pathname = new URL(request.url()).pathname
    const captured = { method: request.method(), pathname }
    this.requests.push(captured)

    if (
      request.method() === 'GET' &&
      pathname === '/api/v1/auth/status'
    ) {
      await fulfillJson(route, { unlocked: true })
      return
    }

    if (pathname === '/api/v1/settings' && request.method() === 'GET') {
      if (this.abortNextLoadRequest) {
        this.abortNextLoadRequest = false
        await route.abort('failed')
        return
      }
      if (this.malformedNextLoadResponse) {
        this.malformedNextLoadResponse = false
        await route.fulfill({
          status: 200,
          contentType: 'application/json; charset=utf-8',
          body: '{',
        })
        return
      }
      if (this.nextLoadFailure !== null) {
        const failure = this.nextLoadFailure
        this.nextLoadFailure = null
        await fulfillJson(route, failure.body, failure.status)
        return
      }
      await fulfillJson(route, this.settings)
      return
    }

    if (pathname === '/api/v1/settings' && request.method() === 'PUT') {
      const payload = request.postDataJSON() as SettingsFixture
      this.savedPayloads.push(structuredClone(payload))

      if (this.abortNextSaveRequest) {
        this.abortNextSaveRequest = false
        await route.abort('failed')
        return
      }

      if (this.nextSaveFailure !== null) {
        const failure = this.nextSaveFailure
        this.nextSaveFailure = null
        await fulfillJson(route, failure.body, failure.status)
        return
      }

      this.settings = {
        child: {
          ...payload.child,
          id: payload.child.id ?? 'child-test-id',
        },
        prompt: {
          ...payload.prompt,
          id: payload.prompt.id ?? 'prompt-test-id',
        },
      }
      await fulfillJson(route, this.settings)
      return
    }

    if (request.method() === 'GET' && pathname === '/api/v1/library') {
      await fulfillJson(route, { items: [], unavailableItemCount: 0 })
      return
    }

    if (request.method() === 'GET' && pathname === '/api/v1/continue') {
      await fulfillJson(route, { items: [] })
      return
    }

    this.unhandledRequests.push(captured)
    await fulfillJson(
      route,
      {
        error: {
          code: 'unhandled_test_route',
          message: 'Unhandled Journey test route',
        },
      },
      501,
    )
  }
}

const test = base.extend<{ api: JourneyApiMock }>({
  api: [
    async ({ page }, use) => {
      const api = new JourneyApiMock(page)
      await api.install()
      await use(api)
      expect(
        api.unhandledRequests,
        'Journey browser test left API requests unhandled',
      ).toEqual([])
    },
    { auto: true },
  ],
})

async function gotoJourney(page: Page): Promise<void> {
  await page.goto('/journey')
  await expect(
    page.getByRole('heading', { level: 1, name: 'Reading profile' }),
  ).toBeVisible()
  await expect(page.getByLabel('Nickname')).toHaveValue('Mina')
}

async function expectJourneyUnavailable(page: Page): Promise<void> {
  await expect(
    page.getByRole('heading', {
      level: 1,
      name: 'Reading profile unavailable',
    }),
  ).toBeVisible()
  await expect(
    page.getByText(
      /connection, server or database may be temporarily unavailable/i,
    ),
  ).toBeVisible()
  await expect(page.getByRole('button', { name: 'Try again' })).toBeVisible()
  await expect(page.getByLabel('Nickname')).toHaveCount(0)
}

async function expectRoute(
  page: Page,
  pathname: string,
  next: string | null = null,
): Promise<void> {
  await expect
    .poll(() => {
      const url = new URL(page.url())
      return { pathname: url.pathname, next: url.searchParams.get('next') }
    })
    .toEqual({ pathname, next })
}

async function expectNoHorizontalOverflow(page: Page): Promise<void> {
  await expect
    .poll(() =>
      page.evaluate(
        () =>
          document.documentElement.scrollWidth <=
          document.documentElement.clientWidth,
      ),
    )
    .toBe(true)
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

test.describe('Reading profile Journey', () => {
  test('loads existing settings and preserves the three-step save workflow', async ({
    page,
    api,
  }) => {
    await gotoJourney(page)

    await expect(page.getByText('Step 1 of 3', { exact: true })).toBeVisible()
    await expect(page.locator('h1')).toHaveCount(1)
    await expect(page.getByLabel('Age (months)')).toHaveValue('72')

    const shellStyle = await page.locator('.journey-shell').evaluate((element) => {
      const style = getComputedStyle(element)
      return {
        background: style.backgroundColor,
        color: style.color,
        font: style.fontFamily,
      }
    })
    expect(shellStyle.background).toBe('rgb(244, 241, 233)')
    expect(shellStyle.color).toBe('rgb(17, 17, 15)')
    expect(shellStyle.font).toContain('Atkinson Hyperlegible')
    await expect(page.getByRole('heading', { level: 1 })).toHaveCSS(
      'font-family',
      /Literata/,
    )

    const nickname = page.getByLabel('Nickname')
    await nickname.fill('')
    await expect(page.getByRole('button', { name: 'Next' })).toBeDisabled()
    await nickname.fill('Mina Rose')
    await page.getByRole('button', { name: 'Next' }).click()

    await expect(page.getByText('Step 2 of 3', { exact: true })).toBeVisible()
    await expect(page.getByLabel('Tone')).toHaveValue('cosy')
    await expect(page.getByLabel('Genre')).toHaveValue('animals')
    await expect(page.getByLabel('Minutes')).toHaveValue('8')
    await expect(page.getByLabel('Complexity')).toHaveValue('simple')

    const interests = page.getByRole('region', { name: 'Interests' })
    await interests.getByLabel('Add an interest').fill('Trains')
    await interests.getByRole('button', { name: 'Add' }).click()
    await expect(
      interests.getByRole('button', { name: 'Remove interest Trains' }),
    ).toBeVisible()
    await interests
      .getByRole('button', { name: 'Remove interest Stars' })
      .click()
    await expect(
      interests.getByRole('button', { name: 'Remove interest Stars' }),
    ).toHaveCount(0)

    const sensitivities = page.getByRole('region', {
      name: 'Avoid / sensitivities',
    })
    await sensitivities.getByLabel('Add a sensitivity').fill('Sudden noises')
    await sensitivities.getByRole('button', { name: 'Add' }).click()
    await sensitivities
      .getByRole('button', { name: 'Remove sensitivity Loud storms' })
      .click()
    await expect(
      sensitivities.getByRole('button', {
        name: 'Remove sensitivity Sudden noises',
      }),
    ).toBeVisible()

    await page.getByLabel('Tone').selectOption('adventurous')
    await page.getByLabel('Genre').selectOption('space')
    await page.getByLabel('Minutes').fill('12')
    await page.getByLabel('Complexity').selectOption('chaptery')
    await page.getByRole('button', { name: 'Save' }).click()

    await expect(
      page.getByRole('status').getByText('Saved.', { exact: true }),
    ).toBeVisible()
    await expect(page.getByText('Step 3 of 3', { exact: true })).toBeVisible()
    await expect(
      page.getByRole('heading', { level: 2, name: 'Done' }),
    ).toBeVisible()
    expect(api.savedPayloads).toHaveLength(1)
    expect(api.savedPayloads[0]).toMatchObject({
      child: {
        id: 'child-test-id',
        name: 'Mina Rose',
        ageMonths: 72,
        interests: ['Trains'],
        sensitivities: ['Sudden noises'],
      },
      prompt: {
        id: 'prompt-test-id',
        name: 'Default prompt v1',
        schemaVersion: 1,
        rules: {
          tone: 'adventurous',
          genre: 'space',
          readingTimeMinutes: 12,
          complexity: 'chaptery',
        },
      },
    })

    await page.getByRole('button', { name: 'Edit' }).click()
    await expect(page.getByText('Step 2 of 3', { exact: true })).toBeVisible()
  })

  test('uses form defaults only for the genuine empty settings contract', async ({
    page,
    api,
  }) => {
    api.setSettings({
      child: {
        name: '',
        ageMonths: 0,
        interests: null,
        sensitivities: null,
      },
      prompt: {
        name: '',
        schemaVersion: 0,
        rules: null,
      },
    })

    await page.goto('/journey')
    await expect(page.getByLabel('Nickname')).toBeVisible()
    await expect(page.getByLabel('Nickname')).toHaveValue('')
    await expect(page.getByLabel('Age (months)')).toHaveValue('36')
    await expect(page.getByRole('button', { name: 'Next' })).toBeDisabled()
  })

  test('a settings 401 confirms session loss and opens unlock with a safe Journey return', async ({
    page,
    api,
  }) => {
    api.failNextLoad(401)
    await page.goto('/journey')

    await expectRoute(page, '/unlock', '/journey')
    await expect(
      page.getByRole('heading', { level: 1, name: 'Unlock Panda Pages' }),
    ).toBeVisible()
    await expect(page.getByLabel('Nickname')).toHaveCount(0)
  })

  test('a network load failure shows no fake form and Retry performs a fresh successful request', async ({
    page,
    api,
  }) => {
    api.abortNextLoad()
    await page.goto('/journey')
    await expectJourneyUnavailable(page)
    expect(api.count('GET', '/api/v1/settings')).toBe(1)

    await page.getByRole('button', { name: 'Try again' }).click()
    await expect(page.getByLabel('Nickname')).toHaveValue('Mina')
    expect(api.count('GET', '/api/v1/settings')).toBe(2)
  })

  test('a 503 settings load remains unavailable rather than looking signed out or empty', async ({
    page,
    api,
  }) => {
    api.failNextLoad(503)
    await page.goto('/journey')

    await expectJourneyUnavailable(page)
    await expectRoute(page, '/journey')
  })

  test('a malformed successful settings response remains unavailable rather than inventing defaults', async ({
    page,
    api,
  }) => {
    api.malformNextLoad()
    await page.goto('/journey')

    await expectJourneyUnavailable(page)
    await expectRoute(page, '/journey')
  })

  test('keeps 5xx save failures visible, preserves values, and retries successfully', async ({
    page,
    api,
  }) => {
    api.failNextSave()
    await gotoJourney(page)
    await page.getByRole('button', { name: 'Next' }).click()
    await page.getByLabel('Tone').selectOption('adventurous')
    await page.getByRole('button', { name: 'Save' }).click()

    await expect(
      page
        .getByRole('alert')
        .getByText(
          'Panda Pages could not save the reading profile. Your changes are still here. Try again.',
          { exact: true },
        ),
    ).toBeVisible()
    await expect(page.getByText('Step 2 of 3', { exact: true })).toBeVisible()
    await expect(page.getByLabel('Tone')).toHaveValue('adventurous')
    await expect(
      page.getByRole('button', { name: 'Try saving again' }),
    ).toBeEnabled()
    await expect(page.getByRole('status')).toHaveCount(0)

    await page.getByRole('button', { name: 'Try saving again' }).click()
    await expect(
      page.getByRole('status').getByText('Saved.', { exact: true }),
    ).toBeVisible()
  })

  test('keeps validation failures distinct and preserves editable values', async ({
    page,
    api,
  }) => {
    api.failNextSave(400)
    await gotoJourney(page)
    await page.getByLabel('Nickname').fill('Mina Updated')
    await page.getByRole('button', { name: 'Next' }).click()
    await page.getByLabel('Genre').selectOption('space')
    await page.getByRole('button', { name: 'Save' }).click()

    await expect(
      page.getByRole('alert').getByText(
        'Some reading profile details could not be saved. Check them and try again.',
        { exact: true },
      ),
    ).toBeVisible()
    await expect(page.getByLabel('Genre')).toHaveValue('space')
    await page.getByRole('button', { name: 'Back' }).click()
    await expect(page.getByLabel('Nickname')).toHaveValue('Mina Updated')
    await expect(page.getByLabel('Nickname')).not.toHaveAttribute(
      'aria-invalid',
      'true',
    )
  })

  test('a save 401 confirms session loss and opens unlock', async ({ page, api }) => {
    api.failNextSave(401)
    await gotoJourney(page)
    await page.getByRole('button', { name: 'Next' }).click()
    await page.getByRole('button', { name: 'Save' }).click()

    await expectRoute(page, '/unlock', '/journey')
    await expect(
      page.getByRole('heading', { level: 1, name: 'Unlock Panda Pages' }),
    ).toBeVisible()
  })

  test('a connectivity save failure preserves the current form without claiming success', async ({
    page,
    api,
  }) => {
    api.abortNextSave()
    await gotoJourney(page)
    await page.getByRole('button', { name: 'Next' }).click()
    await page.getByLabel('Complexity').selectOption('chaptery')
    await page.getByRole('button', { name: 'Save' }).click()

    await expect(
      page.getByRole('alert').getByText(/Your changes are still here/),
    ).toBeVisible()
    await expect(page.getByLabel('Complexity')).toHaveValue('chaptery')
    await expect(page.getByRole('status')).toHaveCount(0)
  })

  test('returns to Library without changing settings', async ({ page, api }) => {
    await gotoJourney(page)
    await page.getByRole('button', { name: 'Return to Library' }).click()
    await expect.poll(() => new URL(page.url()).pathname).toBe('/library')
    expect(api.savedPayloads).toHaveLength(0)
  })

  test('supports mobile, desktop, large text, and short-height layouts', async ({
    page,
  }) => {
    for (const viewport of [
      { name: 'mobile', width: 320, height: 844 },
      { name: 'desktop', width: 1440, height: 900 },
      { name: 'short height', width: 844, height: 430 },
    ]) {
      await test.step(viewport.name, async () => {
        await page.setViewportSize(viewport)
        await gotoJourney(page)
        await expectNoHorizontalOverflow(page)
        await expect(
          page.getByRole('button', { name: 'Return to Library' }),
        ).toBeVisible()
      })
    }

    await page.setViewportSize({ width: 900, height: 900 })
    await gotoJourney(page)
    await page.addStyleTag({ content: 'html { font-size: 32px !important; }' })
    await expectNoHorizontalOverflow(page)
    await expect(page.getByRole('button', { name: 'Next' })).toBeVisible()
  })

  test('has no serious or critical axe findings on profile steps', async ({
    page,
  }) => {
    await gotoJourney(page)
    await expectNoSeriousOrCriticalViolations(page)
    await page.getByRole('button', { name: 'Next' }).click()
    await expectNoSeriousOrCriticalViolations(page)
  })
})
