import { expect, test } from '@playwright/test'

test('legacy admin upload links redirect safely to the new Story Studio editor', async ({
  page,
}) => {
  await page.route('**/api/v1/auth/status', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ unlocked: true }),
    }),
  )

  await page.goto('/admin/upload')
  await expect(page).toHaveURL(/\/admin\/stories\/new$/)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Create a story' }),
  ).toBeVisible()
})
