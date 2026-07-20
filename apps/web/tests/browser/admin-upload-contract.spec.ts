import { expect, test } from '@playwright/test'

const versionID = '11111111-1111-4111-8111-111111111111'
const updatedAt = '2026-07-20T09:15:00Z'

test('basic admin upload uses the typed Story Studio preview and publish contract', async ({
  page,
}) => {
  const requests: Array<{ pathname: string; body: unknown }> = []
  const unhandled: string[] = []

  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const body = request.postDataJSON() ?? null
    const respond = (responseBody: unknown, status = 200) =>
      route.fulfill({
        status,
        contentType: 'application/json',
        headers: { 'Cache-Control': 'no-store' },
        body: JSON.stringify(responseBody),
      })

    if (url.pathname === '/api/v1/auth/status') {
      await respond({ unlocked: true })
      return
    }
    if (url.pathname === '/api/v1/library') {
      await respond({ items: [], unavailableItemCount: 0 })
      return
    }
    if (
      url.pathname === '/api/v1/admin/stories' &&
      request.method() === 'GET'
    ) {
      await respond({ items: [] })
      return
    }
    if (
      url.pathname === '/api/v1/admin/preview' &&
      request.method() === 'POST'
    ) {
      requests.push({ pathname: url.pathname, body })
      await respond({
        slug: 'contract-story',
        title: 'Contract Story',
        author: 'Panda Author',
        language: 'en-GB',
        rights: {},
        sourceUrl: null,
        renderedHtml:
          '<h1>Contract Story</h1><p>Typed preview content.</p>',
        segmentCount: 2,
        wordCount: 5,
        chapterCount: 0,
        warnings: [],
      })
      return
    }
    if (
      url.pathname === '/api/v1/admin/stories/draft' &&
      request.method() === 'POST'
    ) {
      requests.push({ pathname: url.pathname, body })
      await respond({
        slug: 'contract-story',
        versionId: versionID,
        version: 1,
        segmentCount: 2,
        wordCount: 5,
        chapterCount: 0,
        renderedHtml:
          '<h1>Contract Story</h1><p>Typed preview content.</p>',
        outcome: 'created_story',
      })
      return
    }
    if (
      url.pathname === '/api/v1/admin/stories/contract-story/publish' &&
      request.method() === 'POST'
    ) {
      requests.push({ pathname: url.pathname, body })
      await respond({
        slug: 'contract-story',
        status: 'published',
        publishedVersion: { versionId: versionID, version: 1 },
        draftVersion: { versionId: versionID, version: 1 },
        versionCount: 1,
        updatedAt,
      })
      return
    }

    unhandled.push(`${request.method()} ${url.pathname}`)
    await respond(
      {
        error: {
          code: 'unhandled_test_route',
          message: 'Unhandled admin test route',
        },
      },
      501,
    )
  })

  await page.goto('/admin/upload')
  await expect(page.getByRole('heading', { name: 'Upload story' })).toBeVisible()

  await page.getByLabel('Title').fill('Contract Story')
  await page.getByLabel('Author').fill('Panda Author')
  await page
    .getByLabel('Markdown')
    .fill('# Contract Story\n\nTyped preview content.\n')
  await expect(page.getByLabel('Slug')).toHaveValue('contract-story')

  await page.getByRole('button', { name: 'Preview' }).click()
  await expect(page.getByText('Preview ready (2 segments)')).toBeVisible()
  await expect(page.getByText('Typed preview content.')).toBeVisible()

  await page.getByRole('button', { name: 'Save & Publish' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('heading', { name: 'Published — v1' })).toBeVisible()
  await dialog.getByRole('button', { name: 'Close' }).click()
  await expect(dialog).toBeHidden()

  assertRequest(requests[0], '/api/v1/admin/preview')
  assertRequest(requests[1], '/api/v1/admin/stories/draft')
  expect(requests[2]).toEqual({
    pathname: '/api/v1/admin/stories/contract-story/publish',
    body: { versionId: versionID },
  })
  expect(unhandled).toEqual([])
})

function assertRequest(
  request: { pathname: string; body: unknown },
  pathname: string,
) {
  expect(request).toEqual({
    pathname,
    body: {
      slug: 'contract-story',
      title: 'Contract Story',
      author: 'Panda Author',
      markdown: '# Contract Story\n\nTyped preview content.\n',
      sourceUrl: null,
    },
  })
}
