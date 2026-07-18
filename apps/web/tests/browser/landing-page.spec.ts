import { AxeBuilder } from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'

async function gotoLanding(page: Page): Promise<void> {
  await page.goto('/')
  await expect(
    page.getByRole('heading', {
      level: 1,
      name: 'Stories that never grow old.',
    }),
  ).toBeVisible()
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
  test('renders without checking authentication and exposes canonical routes', async ({
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
    await expect(page.getByRole('link', { name: 'Skip to main content' }))
      .toHaveAttribute('href', '#main-content')
    await expect(page.locator('main#main-content')).toHaveCount(1)

    for (const name of [
      'Browse stories',
      'Start reading',
      'Explore the complete library',
      'Browse all stories',
    ]) {
      await expect(page.getByRole('link', { name, exact: true }))
        .toHaveAttribute('href', '/library')
    }

    for (const slug of [
      'the-three-little-pigs',
      'jack-and-the-beanstalk',
      'the-wonderful-wizard-of-oz',
    ]) {
      const links = page.locator(`a[href="/read/${slug}"]`)
      await expect(links).toHaveCount(2)
    }
  })

  test('install help is a named, focus-trapped and dismissible dialog', async ({
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
    await expect
      .poll(() =>
        dialog.evaluate((element) => element.contains(document.activeElement)),
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

  test('320px viewport has no horizontal document overflow', async ({ page }) => {
    await page.setViewportSize({ width: 320, height: 640 })
    await gotoLanding(page)

    expect(
      await page.evaluate(
        () =>
          document.documentElement.scrollWidth <=
          document.documentElement.clientWidth,
      ),
    ).toBe(true)
    await expect(page.getByRole('link', { name: 'Start reading' })).toBeVisible()
  })

  test('axe reports no serious or critical violations', async ({ page }) => {
    await gotoLanding(page)
    await expectNoSeriousOrCriticalViolations(page)
  })
})
