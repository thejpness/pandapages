import {
  expect,
  makeReaderStory,
  READER_SLUG,
  test,
} from './support/reader-api'
import { gotoReader } from './support/reader-page'

const apiError = (code: string, message: string) => ({
  error: { code, message },
})

test.describe('Reader content and route states', () => {
  test('scenarios 1–4 and 26: ordered UTF-8 content uses one coherent endpoint and semantic progress', async ({
    page,
    api,
  }) => {
    const story = api.stories.get(READER_SLUG)
    expect(story).toBeDefined()
    await gotoReader(page, api, READER_SLUG)

    const article = page.getByRole('article', { name: story?.title })
    await expect(article).toBeVisible()
    await expect(article).toBeFocused()
    const segments = page.locator('[data-reader-scroll-segment]')
    await expect(segments).toHaveCount(6)
    expect(
      await segments.evaluateAll((elements) =>
        elements.map((element) => Number(element.getAttribute('data-reader-segment-ordinal'))),
      ),
    ).toEqual([1, 2, 3, 4, 5, 6])
    await expect(page.getByText(/Pöndá carried a lantern/).first()).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Chapter Two — 世界' })).toBeVisible()
    await expect(page.getByText(/星の光 shimmered/).first()).toBeVisible()
    await expect(page.getByRole('heading', { level: 1, name: story?.title })).toHaveCount(1)

    const progress = page.getByRole('progressbar', { name: 'Reading progress' })
    await expect(progress).toHaveAttribute('aria-valuemin', '0')
    await expect(progress).toHaveAttribute('aria-valuemax', '100')
    await expect(progress).toHaveAttribute('aria-valuenow', /^\d+$/)
    await expect(progress).not.toHaveAttribute('aria-live', /.+/)

    expect(api.count('GET', `/api/v1/reader/${READER_SLUG}`)).toBe(1)
    expect(api.legacyRequests).toEqual([])
  })

  test('scenario 13: a Reader 401 uses Unlock with the safe current story next path', async ({
    page,
    api,
  }) => {
    api.enqueueStory(READER_SLUG, {
      status: 401,
      body: apiError('unauthorized', 'Session ended'),
    })

    await page.goto(`/read/${READER_SLUG}`)
    await expect(page).toHaveURL(/\/unlock(?:\?|$)/)
    await expect(page.getByRole('heading', { name: 'Panda Pages' })).toBeVisible()
    expect(new URL(page.url()).searchParams.get('next')).toBe(`/read/${READER_SLUG}`)
  })

  test('scenario 14: a 404 is Story not found and never Unlock', async ({ page, api }) => {
    api.enqueueStory(READER_SLUG, {
      status: 404,
      body: apiError('not_found', 'Story not found'),
    })

    await page.goto(`/read/${READER_SLUG}`)
    await expect(page.getByRole('heading', { name: 'Story not found' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Return to Library' })).toBeVisible()
    await expect(page).toHaveURL(`/read/${READER_SLUG}`)
  })

  test('scenario 15: a service failure is retryable Story unavailable', async ({
    page,
    api,
  }) => {
    api.enqueueStory(READER_SLUG, {
      status: 500,
      body: apiError('internal_error', 'Unavailable'),
    })

    await page.goto(`/read/${READER_SLUG}`)
    await expect(page.getByRole('heading', { name: 'Story unavailable' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Return to Library' })).toBeVisible()
    await page.getByRole('button', { name: 'Retry' }).click()
    await expect(page.locator('[data-reader-scroll-view]')).toBeVisible()
    expect(api.count('GET', `/api/v1/reader/${READER_SLUG}`)).toBe(2)
  })

  test('scenario 15: a network failure and malformed success use the unavailable boundary', async ({
    page,
    api,
  }) => {
    api.enqueueStory(READER_SLUG, { abort: 'failed' })
    api.enqueueStory(READER_SLUG, {
      status: 200,
      body: {
        slug: READER_SLUG,
        title: 'Malformed test story',
        author: null,
        language: 'en-GB',
        version: 1,
        segments: null,
      },
    })

    await page.goto(`/read/${READER_SLUG}`)
    await expect(page.getByRole('heading', { name: 'Story unavailable' })).toBeVisible()
    await page.getByRole('button', { name: 'Retry' }).click()
    await expect(page.getByRole('heading', { name: 'Story unavailable' })).toBeVisible()
    await expect(page).toHaveURL(`/read/${READER_SLUG}`)
  })

  test('scenario 24: an aborted stale story response cannot replace the next route', async ({
    page,
    api,
  }) => {
    const storyA = makeReaderStory({ slug: 'story-a', title: 'TEST ONLY — Story A' })
    const storyB = makeReaderStory({ slug: 'story-b', title: 'TEST ONLY — Story B' })
    api.setStory(storyA)
    api.setStory(storyB)
    api.libraryItems = [
      { slug: storyA.slug, title: storyA.title, author: storyA.author },
      { slug: storyB.slug, title: storyB.title, author: storyB.author },
    ]
    const stale = api.deferStory(storyA.slug)
    const storyCard = (title: string) =>
      page.getByRole('article').filter({
        has: page.getByRole('heading', { level: 3, name: title }),
      })

    await page.goto('/library')
    await expect(storyCard(storyA.title)).toBeVisible()
    await storyCard(storyA.title)
      .getByRole('link', { name: `Read: ${storyA.title}`, exact: true })
      .click()
    await stale.started
    await page.goBack()
    await expect(page).toHaveURL('/library')
    await storyCard(storyB.title)
      .getByRole('link', { name: `Read: ${storyB.title}`, exact: true })
      .click()
    await expect(page.getByRole('heading', { level: 1, name: storyB.title })).toBeVisible()

    stale.fulfill(storyA)
    await expect(page.getByRole('heading', { level: 1, name: storyB.title })).toBeVisible()
    await expect(page.getByText(storyA.title, { exact: true })).toHaveCount(0)
    await expect(page).toHaveTitle(`${storyB.title} · Panda Pages`)
  })
})
