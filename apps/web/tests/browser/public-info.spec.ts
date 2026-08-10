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
    const contentAndCopyrightLink = legalNavigation.getByRole('link', { name: 'Content & Copyright', exact: true })
    await expect(privacyLink).toHaveAttribute('href', '/privacy')
    await expect(contentAndCopyrightLink).toHaveAttribute('href', '/content-and-copyright')
    await contentAndCopyrightLink.focus()
    await expect(contentAndCopyrightLink).toBeFocused()
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

    const legalNavigation = page.getByRole('navigation', { name: 'Legal navigation' })
    const privacyLink = legalNavigation.getByRole('link', { name: 'Privacy', exact: true })
    const contentAndCopyrightLink = legalNavigation.getByRole('link', { name: 'Content & Copyright', exact: true })
    await expect(privacyLink).toHaveAttribute('href', '/privacy')
    await expect(contentAndCopyrightLink).toHaveAttribute('href', '/content-and-copyright')
    await privacyLink.focus()
    await expect(privacyLink).toBeFocused()
    await page.reload()
    await expect(page.getByRole('heading', { level: 1, name: 'Content & Copyright' })).toBeVisible()
  })

  test("Privacy and Content & Copyright share a scannable public-information presentation", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    await page.goto("/privacy")

    await expect(page.getByRole("heading", { level: 2, name: "At a glance" })).toBeVisible()
    for (const text of ["No behavioural advertising", "Adult-managed", "Data minimised", "Privacy contact"]) {
      await expect(page.getByText(text, { exact: true })).toBeVisible()
    }
    const privacyContents = page.getByRole("navigation", { name: "On this page" })
    await expect(privacyContents).toBeVisible()
    for (const [name, target] of [
      ["Overview", "#privacy-chapter-overview"],
      ["Accounts & readers", "#privacy-chapter-readers"],
      ["Stories & reading", "#privacy-chapter-stories"],
      ["Security & storage", "#privacy-chapter-security"],
      ["Retention & deletion", "#privacy-chapter-retention"],
      ["Your rights & contact", "#privacy-chapter-rights"],
    ]) {
      const link = privacyContents.getByRole("link", { name, exact: true })
      await expect(link).toHaveAttribute("href", target)
      await expect(page.locator(target)).toHaveCount(1)
    }
    const overviewLink = privacyContents.getByRole("link", { name: "Overview", exact: true })
    await overviewLink.focus()
    await expect(overviewLink).toBeFocused()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)
    await expect(page.getByRole("heading", { level: 3, name: "Retention at a glance" })).toBeVisible()
    for (const text of ["Daily in Hetzner Object Storage, Finland, retained for up to 30 days.", "May be retained for up to 90 days.", "Our retention policy is 24 months from the last authenticated adult account activity."]) {
      await expect(page.getByText(text, { exact: true })).toBeVisible()
    }
    await expect(page.getByRole("heading", { level: 3, name: "Questions about your privacy?" })).toBeVisible()
    await expect(page.locator(".privacy-policy__toc")).toHaveCSS("position", "sticky")

    await page.goto("/content-and-copyright")
    const contentGlance = page.locator(".content-copyright__glance")
    await expect(contentGlance.getByRole("heading", { level: 2, name: "At a glance" })).toBeVisible()
    for (const text of ["Source works", "Adapted editions", "Story Studio", "Provenance"]) {
      await expect(contentGlance.getByText(text, { exact: true })).toBeVisible()
    }
    const contentContents = page.getByRole("navigation", { name: "On this page" })
    for (const [name, target] of [
      ["Sources & rights", "#content-chapter-sources"],
      ["Adapted editions", "#content-adaptations"],
      ["Story Studio & provenance", "#content-chapter-studio"],
      ["Concerns & privacy", "#content-chapter-concerns"],
    ]) {
      const link = contentContents.getByRole("link", { name, exact: true })
      await expect(link).toHaveAttribute("href", target)
      await expect(page.locator(target)).toHaveCount(1)
    }
    await expect(page.getByRole("heading", { level: 3, name: "Public-domain source ≠ verbatim edition" })).toBeVisible()
    await expect(page.getByRole("heading", { level: 3, name: "Recorded provenance helps describe where content came from" })).toBeVisible()
    await expect(page.getByRole("heading", { level: 3, name: "Have a privacy concern?" })).toBeVisible()
    await expect(page.getByRole("link", { name: "privacy@southcoastapps.co.uk" }).last()).toHaveAttribute("href", "mailto:privacy@southcoastapps.co.uk")
    await expect(page.locator(".content-copyright__toc")).toHaveCSS("position", "static")
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)
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
