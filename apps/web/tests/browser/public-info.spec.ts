import { AxeBuilder } from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'

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

test.describe('Public information pages', () => {
  test('Privacy Policy is directly reachable while signed out and describes the current service', async ({ page }) => {
    const apiRequests: string[] = []
    page.on('request', (request) => {
      if (new URL(request.url()).pathname.startsWith('/api/')) apiRequests.push(request.url())
    })

    await page.goto('/privacy')

    await expect(page).toHaveURL(/\/privacy$/)
    await expect(page).toHaveTitle('Privacy Policy | Panda Pages')
    await expect(page.getByRole('main')).toHaveCount(1)
    await expect(page.getByRole('heading', { level: 1, name: 'Privacy Policy' })).toBeVisible()
    for (const heading of [
      'Adult accounts and social login',
      'Reader profiles and children’s information',
      'Reading activity and preferences',
      'Story Studio and supplied or imported content',
      'Cookies, local storage, and PWA storage',
      'Data-protection rights',
    ]) {
      await expect(page.getByRole('heading', { level: 2, name: heading })).toBeVisible()
    }
    for (const text of ['Supabase', 'Google', 'Facebook', 'reading progress', 'behavioural advertising or behavioural analytics']) {
      await expect(page.getByText(text, { exact: false }).first()).toBeVisible()
    }
    expect(apiRequests).toEqual([])

    const legalNavigation = page.getByRole('navigation', { name: 'Legal navigation' })
    const privacyLink = legalNavigation.getByRole('link', { name: 'Privacy', exact: true })
    await privacyLink.focus()
    await expect(privacyLink).toBeFocused()
  })

  test('Privacy Policy remains accessible and readable at a narrow width', async ({ page }) => {
    await page.setViewportSize({ width: 320, height: 568 })
    await page.goto('/privacy')

    expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)
    await expectNoSeriousOrCriticalViolations(page)
  })
})
