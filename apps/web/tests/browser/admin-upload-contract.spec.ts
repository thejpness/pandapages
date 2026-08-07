import { expect, test } from './support/auth'

test('legacy admin upload links redirect safely to the new Story Studio editor', async ({
  page,
}) => {
  await page.goto('/admin/upload')
  await expect(page).toHaveURL(/\/admin\/stories\/new$/)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Create a story' }),
  ).toBeVisible()
})
