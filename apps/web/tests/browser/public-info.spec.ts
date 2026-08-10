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
    const privacyText = await page.getByRole('main').innerText()
    for (const fact of [
      'South Coast Apps Ltd',
      'privacy@southcoastapps.co.uk',
      '10 August 2026',
      'Hetzner',
      'Finland',
      'Supabase',
      'AWS EU-West',
      'daily backups',
      'Hetzner Object Storage',
      'up to 30 days',
      'up to 90 days',
      '24 months of inactivity',
      'last authenticated adult account activity',
    ]) expect(privacyText).toContain(fact)
    expect(privacyText).not.toMatch(/eu-west-[123]/i)
    expect(privacyText).not.toMatch(/\[[A-Z /]+REQUIRED\]/)
    await expect(page.getByRole('link', { name: 'privacy@southcoastapps.co.uk' }).first()).toHaveAttribute('href', 'mailto:privacy@southcoastapps.co.uk')


    const legalNavigation = page.getByRole('navigation', { name: 'Legal navigation' })
    const privacyLink = legalNavigation.getByRole('link', { name: 'Privacy', exact: true })
    await privacyLink.focus()
    await expect(privacyLink).toBeFocused()
  })

  test('Content & Copyright is directly reachable while signed out and explains provenance boundaries', async ({ page }) => {
    const apiRequests: string[] = []
    page.on('request', (request) => {
      if (new URL(request.url()).pathname.startsWith('/api/')) apiRequests.push(request.url())
    })

    await page.goto('/content-and-copyright')

    await expect(page).toHaveURL(/\/content-and-copyright$/)
    await expect(page).toHaveTitle('Content & Copyright | Panda Pages')
    await expect(page.getByRole('main')).toHaveCount(1)
    await expect(page.getByRole('heading', { level: 1, name: 'Content & Copyright' })).toBeVisible()
    for (const heading of [
      'Public-domain source works',
      'Panda Pages adaptations',
      'Story Studio supplied material',
      'Source and provenance information',
      'Copyright and attribution concerns',
      'Other content concerns',
    ]) await expect(page.getByRole('heading', { level: 2, name: heading })).toBeVisible()

    const contentText = await page.getByRole('main').innerText()
    for (const text of ['public-domain literature', 'verbatim public-domain text', 'Story Studio', 'rights metadata', 'source versions', 'suspected copyright infringement', 'formatting or import problems']) expect(contentText).toContain(text)
    expect(contentText).not.toContain('All Panda Pages stories are public domain')
    expect(contentText).not.toContain('support@southcoastapps.co.uk')
    expect(apiRequests).toEqual([])

    const privacyLink = page.getByRole('link', { name: 'Panda Pages Privacy Policy' })
    await privacyLink.focus()
    await expect(privacyLink).toBeFocused()
    await page.reload()
    await expect(page.getByRole('heading', { level: 1, name: 'Content & Copyright' })).toBeVisible()
  })

  test('Content & Copyright remains accessible and readable at a narrow width', async ({ page }) => {
    await page.setViewportSize({ width: 320, height: 568 })
    await page.goto('/content-and-copyright')

    expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)
    await expectNoSeriousOrCriticalViolations(page)
  })
  test('Privacy Policy remains accessible and readable at a narrow width', async ({ page }) => {
    await page.setViewportSize({ width: 320, height: 568 })
    await page.goto('/privacy')

    expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)
    await expectNoSeriousOrCriticalViolations(page)
  })
})
