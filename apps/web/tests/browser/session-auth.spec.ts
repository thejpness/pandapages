import { AxeBuilder } from '@axe-core/playwright'
import {
  expect,
  test as base,
  type Page,
  type Route,
} from '@playwright/test'

type JsonResponse = {
  kind?: never
  body: unknown
  status?: number
}

type AbortedResponse = {
  kind: 'abort'
}

type ResponseGate = {
  started: Promise<void>
  fulfill: (response?: JsonResponse) => void
}

type InternalGate = {
  kind: 'gate'
  signalStarted: () => void
  response: Promise<JsonResponse>
  publicGate: ResponseGate
}

type QueuedResponse = JsonResponse | AbortedResponse | InternalGate

type CapturedRequest = {
  method: string
  pathname: string
  body?: unknown
}

function createGate(): InternalGate {
  let signalStarted: () => void = () => undefined
  let settle!: (response: JsonResponse) => void
  let settled = false

  const started = new Promise<void>((resolve) => {
    signalStarted = resolve
  })
  const response = new Promise<JsonResponse>((resolve) => {
    settle = (value) => {
      if (settled) return
      settled = true
      resolve(value)
    }
  })

  return {
    kind: 'gate',
    signalStarted,
    response,
    publicGate: {
      started,
      fulfill: (value = { body: { ok: true } }) => settle(value),
    },
  }
}

async function fulfillJson(route: Route, response: JsonResponse): Promise<void> {
  await route.fulfill({
    status: response.status ?? 200,
    contentType: 'application/json; charset=utf-8',
    headers: { 'Cache-Control': 'no-store' },
    body: JSON.stringify(response.body),
  })
}

class AuthApiMock {
  readonly requests: CapturedRequest[] = []

  private readonly page: Page
  private readonly statusResponses: QueuedResponse[] = []
  private readonly unlockResponses: QueuedResponse[] = []
  private unlocked = false

  constructor(page: Page) {
    this.page = page
  }

  async install(): Promise<void> {
    await this.page.route('**/api/v1/**', async (route) => {
      await this.handle(route)
    })
  }

  enqueueStatus(response: JsonResponse | AbortedResponse): void {
    this.statusResponses.push(response)
  }

  enqueueUnlock(response: JsonResponse | AbortedResponse): void {
    this.unlockResponses.push(response)
  }

  deferUnlock(): ResponseGate {
    const gate = createGate()
    this.unlockResponses.push(gate)
    return gate.publicGate
  }

  count(method: string, pathname: string): number {
    return this.requests.filter(
      (request) =>
        request.method === method && request.pathname === pathname,
    ).length
  }

  bodies(method: string, pathname: string): unknown[] {
    return this.requests
      .filter(
        (request) =>
          request.method === method && request.pathname === pathname,
      )
      .map((request) => request.body)
  }

  private resolve(
    queued: QueuedResponse | undefined,
    fallback: JsonResponse,
  ): Promise<JsonResponse | AbortedResponse> {
    if (queued === undefined) return Promise.resolve(fallback)
    if (queued.kind === 'abort') return Promise.resolve(queued)
    if (queued.kind === 'gate') {
      queued.signalStarted()
      return queued.response
    }
    return Promise.resolve(queued)
  }

  private async handle(route: Route): Promise<void> {
    const request = route.request()
    const url = new URL(request.url())
    let body: unknown
    if (request.method() !== 'GET') {
      body = request.postDataJSON()
    }
    this.requests.push({
      method: request.method(),
      pathname: url.pathname,
      body,
    })

    if (
      request.method() === 'GET' &&
      url.pathname === '/api/v1/auth/status'
    ) {
      const response = await this.resolve(this.statusResponses.shift(), {
        body: { unlocked: this.unlocked },
      })
      if (response.kind === 'abort') {
        await route.abort('failed')
        return
      }
      await fulfillJson(route, response)
      return
    }

    if (
      request.method() === 'POST' &&
      url.pathname === '/api/v1/auth/unlock'
    ) {
      const response = await this.resolve(this.unlockResponses.shift(), {
        body: { ok: true },
      })
      if (response.kind === 'abort') {
        await route.abort('failed')
        return
      }
      if ((response.status ?? 200) >= 200 && (response.status ?? 200) < 300) {
        this.unlocked = true
      }
      await fulfillJson(route, response)
      return
    }

    await fulfillJson(route, {
      status: 404,
      body: {
        error: {
          code: 'not_found',
          message: 'Test route not found',
        },
      },
    })
  }
}

const test = base.extend<{ api: AuthApiMock }>({
  api: async ({ page }, use) => {
    const api = new AuthApiMock(page)
    await api.install()
    await use(api)
  },
})

async function gotoUnlock(
  page: Page,
  next = '/admin/ai',
): Promise<void> {
  await page.goto(`/unlock?next=${encodeURIComponent(next)}`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Unlock Panda Pages' }),
  ).toBeVisible()
}

async function enterWithKeypad(page: Page, code: string): Promise<void> {
  for (const digit of code) {
    await page.getByRole('button', { name: `Digit ${digit}` }).click()
  }
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

async function expectNoHorizontalOverflow(page: Page): Promise<void> {
  expect(
    await page.evaluate(
      () =>
        document.documentElement.scrollWidth <=
        document.documentElement.clientWidth,
    ),
  ).toBe(true)
}

async function expectReachable(page: Page, locator: ReturnType<Page['locator']>) {
  await locator.scrollIntoViewIfNeeded()
  const box = await locator.boundingBox()
  expect(box).not.toBeNull()
  if (!box) return

  const viewport = page.viewportSize()
  expect(viewport).not.toBeNull()
  if (!viewport) return

  expect(box.x).toBeGreaterThanOrEqual(-1)
  expect(box.x + box.width).toBeLessThanOrEqual(viewport.width + 1)
  expect(box.y).toBeLessThan(viewport.height)
  expect(box.y + box.height).toBeGreaterThan(0)
}

async function expectNoSeriousOrCriticalViolations(page: Page): Promise<void> {
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

test.describe('Unlock and session recovery', () => {
  test('six keypad digits unlock once and preserve the safe next destination', async ({
    page,
    api,
  }) => {
    await gotoUnlock(page)
    await enterWithKeypad(page, '123456')

    await expectRoute(page, '/admin/ai')
    await expect(page.getByRole('heading', { level: 1, name: 'AI create' })).toBeVisible()
    expect(api.count('POST', '/api/v1/auth/unlock')).toBe(1)
    expect(api.bodies('POST', '/api/v1/auth/unlock')).toEqual([
      { passcode: '123456' },
    ])
  })

  test('keyboard digits, Backspace, and Escape edit without submitting', async ({
    page,
    api,
  }) => {
    await gotoUnlock(page)
    const entry = page.getByRole('button', { name: /Passcode entry/ })

    await page.keyboard.type('1234')
    await expect(entry).toHaveAttribute(
      'aria-label',
      'Passcode entry, 4 of 6 digits entered',
    )
    await page.keyboard.press('Backspace')
    await expect(entry).toHaveAttribute(
      'aria-label',
      'Passcode entry, 3 of 6 digits entered',
    )
    await page.keyboard.press('Escape')
    await expect(entry).toHaveAttribute(
      'aria-label',
      'Passcode entry, 0 of 6 digits entered',
    )
    await expect(page.getByLabel('Six-digit passcode')).toBeFocused()
    expect(api.count('POST', '/api/v1/auth/unlock')).toBe(0)
  })

  test('OTP-style input normalizes a pasted or autofilled code and submits once', async ({
    page,
    api,
  }) => {
    await gotoUnlock(page)
    const otp = page.getByLabel('Six-digit passcode')
    await expect(otp).toHaveAttribute('autocomplete', 'one-time-code')
    await expect(otp).toHaveAttribute('inputmode', 'numeric')

    await otp.fill('12 34-56', { force: true })

    await expectRoute(page, '/admin/ai')
    expect(api.count('POST', '/api/v1/auth/unlock')).toBe(1)
    expect(api.bodies('POST', '/api/v1/auth/unlock')).toEqual([
      { passcode: '123456' },
    ])
  })

  test('a deferred unlock owns loading and disables every entry action', async ({
    page,
    api,
  }) => {
    const pending = api.deferUnlock()
    await gotoUnlock(page)
    await enterWithKeypad(page, '123456')
    await pending.started

    await expect(page.getByRole('button', { name: 'Unlocking…' })).toBeDisabled()
    await expect(page.getByRole('button', { name: 'Clear code' }).first()).toBeDisabled()
    expect(
      await page
        .locator('.unlock-keypad button')
        .evaluateAll((buttons) =>
          buttons.every((button) => (button as HTMLButtonElement).disabled),
        ),
    ).toBe(true)
    expect(api.count('POST', '/api/v1/auth/unlock')).toBe(1)

    pending.fulfill()
    await expectRoute(page, '/admin/ai')
    expect(api.count('POST', '/api/v1/auth/unlock')).toBe(1)
  })

  const failures: Array<{
    name: string
    response: JsonResponse | AbortedResponse
    message: string
    clears: boolean
  }> = [
    {
      name: '401',
      response: {
        status: 401,
        body: { error: { code: 'unauthorized', message: 'internal auth detail' } },
      },
      message: 'Wrong passcode',
      clears: true,
    },
    {
      name: '500',
      response: {
        status: 500,
        body: { error: { code: 'unlock_failed', message: 'database detail' } },
      },
      message: 'Could not unlock Panda Pages. Try again.',
      clears: false,
    },
    {
      name: 'connectivity failure',
      response: { kind: 'abort' },
      message: 'Could not unlock Panda Pages. Try again.',
      clears: false,
    },
  ]

  for (const scenario of failures) {
    test(`${scenario.name} keeps unlock failure messaging truthful`, async ({
      page,
      api,
    }) => {
      await page.clock.install()
      api.enqueueUnlock(scenario.response)
      await gotoUnlock(page)
      await page.keyboard.type('123456')

      await expect(page.getByRole('alert')).toHaveText(scenario.message)
      expect(api.count('POST', '/api/v1/auth/unlock')).toBe(1)
      const body = await page.locator('body').innerText()
      expect(body).not.toContain('internal auth detail')
      expect(body).not.toContain('database detail')

      await page.clock.fastForward(500)
      await expect(page.getByRole('button', { name: /Passcode entry/ })).toHaveAttribute(
        'aria-label',
        `Passcode entry, ${scenario.clears ? 0 : 6} of 6 digits entered`,
      )
    })
  }

  test('Session Unavailable retry continues to the safe next page when unlocked', async ({
    page,
    api,
  }) => {
    api.enqueueStatus({
      status: 503,
      body: { error: { code: 'unavailable', message: 'verification unavailable' } },
    })
    api.enqueueStatus({ body: { unlocked: true } })

    await page.goto('/admin/ai')
    await expectRoute(page, '/session-unavailable', '/admin/ai')
    await page.getByRole('button', { name: 'Try again' }).click()

    await expectRoute(page, '/admin/ai')
    await expect(page.getByRole('heading', { level: 1, name: 'AI create' })).toBeVisible()
    expect(api.count('GET', '/api/v1/auth/status')).toBe(2)
  })

  test('Session Unavailable retry goes to Unlock with the safe next when locked', async ({
    page,
    api,
  }) => {
    api.enqueueStatus({
      status: 503,
      body: { error: { code: 'unavailable', message: 'verification unavailable' } },
    })
    api.enqueueStatus({ body: { unlocked: false } })

    await page.goto('/admin/ai')
    await page.getByRole('button', { name: 'Try again' }).click()

    await expectRoute(page, '/unlock', '/admin/ai')
    await expect(
      page.getByRole('heading', { level: 1, name: 'Unlock Panda Pages' }),
    ).toBeVisible()
    expect(api.count('GET', '/api/v1/auth/status')).toBe(2)
  })

  test('Session Unavailable remains recoverable when verification is still unavailable', async ({
    page,
    api,
  }) => {
    api.enqueueStatus({
      status: 503,
      body: { error: { code: 'unavailable', message: 'first internal detail' } },
    })
    api.enqueueStatus({ kind: 'abort' })

    await page.goto('/admin/ai')
    await page.getByRole('button', { name: 'Try again' }).click()

    await expectRoute(page, '/session-unavailable', '/admin/ai')
    await expect(page.getByRole('alert')).toHaveText(
      'Panda Pages still cannot verify the session. The server or database may be temporarily unavailable.',
    )
    await expect(page.getByRole('button', { name: 'Try again' })).toBeEnabled()
    const body = await page.locator('body').innerText()
    expect(body).not.toContain('first internal detail')
    expect(body).toContain('Your session has not been treated as signed out.')
    expect(api.count('GET', '/api/v1/auth/status')).toBe(2)
  })

  test('Unlock controls stay reachable without horizontal overflow across constrained layouts', async ({
    page,
    api,
  }) => {
    const layouts = [
      { name: '320 mobile', width: 320, height: 640, rootSize: null },
      { name: 'desktop', width: 1280, height: 900, rootSize: null },
      { name: '32px root', width: 640, height: 900, rootSize: 32 },
      { name: 'short height', width: 390, height: 360, rootSize: null },
    ]

    for (const layout of layouts) {
      await test.step(layout.name, async () => {
        await page.setViewportSize({ width: layout.width, height: layout.height })
        await gotoUnlock(page)
        if (layout.rootSize !== null) {
          await page.addStyleTag({
            content: `:root { font-size: ${layout.rootSize}px !important; }`,
          })
        }

        await expectNoHorizontalOverflow(page)
        await expectReachable(
          page,
          page.getByRole('heading', { level: 1, name: 'Unlock Panda Pages' }),
        )
        await expectReachable(page, page.getByRole('button', { name: /Passcode entry/ }))
        for (const button of await page.getByRole('button').all()) {
          await expectReachable(page, button)
        }
      })
    }
    expect(api.count('POST', '/api/v1/auth/unlock')).toBe(0)
  })

  test('axe: normal Unlock has no serious or critical violations', async ({
    page,
    api,
  }) => {
    await gotoUnlock(page)
    await expectNoSeriousOrCriticalViolations(page)
    expect(api.count('POST', '/api/v1/auth/unlock')).toBe(0)
  })

  test('axe: invalid Unlock has no serious or critical violations', async ({
    page,
    api,
  }) => {
    await page.clock.install()
    api.enqueueUnlock({
      status: 401,
      body: { error: { code: 'unauthorized', message: 'wrong' } },
    })
    await gotoUnlock(page)
    await page.keyboard.type('123456')
    await expect(page.getByRole('alert')).toHaveText('Wrong passcode')
    await expectNoSeriousOrCriticalViolations(page)
  })

  test('axe: Session Unavailable has no serious or critical violations', async ({
    page,
    api,
  }) => {
    api.enqueueStatus({
      status: 503,
      body: { error: { code: 'unavailable', message: 'verification unavailable' } },
    })
    await page.goto('/admin/ai')
    await expect(
      page.getByRole('heading', {
        level: 1,
        name: 'Panda Pages could not verify the session',
      }),
    ).toBeVisible()
    await expectNoSeriousOrCriticalViolations(page)
  })

  test('@webkit-library OTP paste, keyboard editing, and focus stay coherent', async ({
    page,
    api,
  }) => {
    await gotoUnlock(page)
    const entry = page.getByRole('button', { name: /Passcode entry/ })
    const otp = page.getByLabel('Six-digit passcode')
    await entry.click()
    await expect(otp).toBeFocused()
    await expect(entry).toHaveCSS('outline-style', 'solid')
    await expect(entry).toHaveCSS('outline-color', 'rgb(27, 103, 84)')

    await otp.evaluate((input) => {
      const field = input as HTMLInputElement
      field.value = '123'
      field.dispatchEvent(
        new InputEvent('input', {
          bubbles: true,
          data: '123',
          inputType: 'insertFromPaste',
        }),
      )
    })
    await expect(entry).toHaveAttribute(
      'aria-label',
      'Passcode entry, 3 of 6 digits entered',
    )
    await page.keyboard.press('Backspace')
    await expect(entry).toHaveAttribute(
      'aria-label',
      'Passcode entry, 2 of 6 digits entered',
    )
    await page.keyboard.press('Escape')
    await expect(entry).toHaveAttribute(
      'aria-label',
      'Passcode entry, 0 of 6 digits entered',
    )
    await expect(otp).toBeFocused()

    await otp.evaluate((input) => {
      const field = input as HTMLInputElement
      field.value = '123456'
      field.dispatchEvent(
        new InputEvent('input', {
          bubbles: true,
          data: '123456',
          inputType: 'insertFromPaste',
        }),
      )
    })
    await expectRoute(page, '/admin/ai')
    expect(api.count('POST', '/api/v1/auth/unlock')).toBe(1)
    expect(api.bodies('POST', '/api/v1/auth/unlock')).toEqual([
      { passcode: '123456' },
    ])
  })
})
