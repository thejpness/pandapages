import {
  expect,
  makePagedReaderStory,
  makeReaderStory,
  READER_SLUG,
  test,
} from './support/reader-api'
import {
  gotoReader,
  pagedViewport,
  seedReaderPreferences,
} from './support/reader-page'

async function expectNoHorizontalOverflow(page: import('@playwright/test').Page) {
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
      ),
    )
    .toBeLessThanOrEqual(1)
}

test.describe('Reader responsive smoke contracts', () => {
  test('mobile portrait keeps controls reachable and settings inside safe bounds', async ({
    page,
    api,
  }) => {
    await page.setViewportSize({ width: 320, height: 700 })
    await gotoReader(page, api, READER_SLUG)
    await expectNoHorizontalOverflow(page)

    const controls = page.locator('.reader-header button')
    for (let index = 0; index < await controls.count(); index += 1) {
      const box = await controls.nth(index).boundingBox()
      expect(box?.height ?? 0).toBeGreaterThanOrEqual(44)
    }

    await page.getByRole('button', { name: 'Reading settings' }).click()
    const dialog = page.getByRole('dialog', { name: 'Reading settings' })
    const bounds = await dialog.boundingBox()
    expect(bounds).not.toBeNull()
    expect(bounds?.x ?? -1).toBeGreaterThanOrEqual(0)
    expect((bounds?.x ?? 0) + (bounds?.width ?? 0)).toBeLessThanOrEqual(320)
    await expectNoHorizontalOverflow(page)
  })

  test('landscape and large type keep the story readable without page overflow', async ({
    page,
    api,
  }) => {
    await page.setViewportSize({ width: 667, height: 375 })
    await page.addInitScript(() => {
      localStorage.setItem(
        'pp_reader_prefs_v2',
        JSON.stringify({
          schema: 2,
          mode: 'scroll',
          theme: 'warm',
          fontFamily: 'clear',
          fontSize: 32,
          lineHeight: 2,
          contentWidth: 900,
        }),
      )
    })
    await gotoReader(page, api, READER_SLUG)

    await expect(page.locator('.reader-story')).toHaveCSS('font-size', '32px')
    await expect(page.locator('.reader-shell')).toHaveAttribute(
      'data-reader-theme',
      'warm',
    )
    await expectNoHorizontalOverflow(page)
  })

  test('a long title remains contained at a 200%-zoom-equivalent layout width', async ({
    page,
    api,
  }) => {
    await page.setViewportSize({ width: 640, height: 450 })
    const title =
      'TEST ONLY — The Moonlit Harbour and the Very Long Journey Home Together'
    api.setStory(makeReaderStory({ title }))
    await gotoReader(page, api, READER_SLUG)

    await expect(page.getByRole('heading', { level: 1, name: title })).toBeVisible()
    await expectNoHorizontalOverflow(page)
  })

  test('paged metadata without a story H1 leaves constrained navigation reachable', async ({
    page,
    api,
  }) => {
    await page.setViewportSize({ width: 320, height: 450 })
    await seedReaderPreferences(page, {
      fontSize: 32,
      lineHeight: 2,
      contentWidth: 900,
    })

    const title =
      'TEST ONLY — The Moonlit Harbour and the Exceptionally Long Journey Home Together'
    const story = makePagedReaderStory({ title })
    const opening = story.segments[0]
    if (!opening) {
      throw new Error('Paged test fixture unexpectedly has no opening segment')
    }
    opening.kind = 'paragraph'
    opening.headingLevel = null
    opening.renderedHtml =
      '<p>Opening segment deliberately has no story-level heading.</p>'
    opening.wordCount = 7
    api.setStory(story)

    await gotoReader(page, api, READER_SLUG)

    await expect(page.getByRole('heading', { level: 1, name: title })).toBeVisible()
    const viewport = pagedViewport(page)
    await expect(viewport).toBeVisible()
    expect((await viewport.boundingBox())?.height ?? 0).toBeGreaterThan(0)

    const navigation = page.getByRole('navigation', { name: 'Page navigation' })
    await expect(navigation).toBeVisible()
    for (const name of ['Previous page', 'Next page']) {
      const control = navigation.getByRole('button', { name })
      await expect(control).toBeVisible()
      const bounds = await control.boundingBox()
      expect(bounds?.height ?? 0).toBeGreaterThanOrEqual(44)
      expect((bounds?.y ?? 0) + (bounds?.height ?? 0)).toBeLessThanOrEqual(450)
    }
    await expectNoHorizontalOverflow(page)
  })

  for (const viewportSize of [
    { width: 844, height: 390 },
    { width: 640, height: 450 },
  ]) {
    test(`paged large-type navigation fits ${viewportSize.width}x${viewportSize.height}`, async ({
      page,
      api,
    }) => {
      await page.setViewportSize(viewportSize)
      await seedReaderPreferences(page, {
        fontFamily: 'clear',
        fontSize: 32,
        lineHeight: 2,
        contentWidth: 900,
      })

      const title =
        'TEST ONLY — The Moonlit Harbour and the Exceptionally Long Journey Home Together'
      const story = makePagedReaderStory({ title })
      const opening = story.segments[0]
      if (!opening) {
        throw new Error('Paged test fixture unexpectedly has no opening segment')
      }
      opening.kind = 'paragraph'
      opening.headingLevel = null
      opening.renderedHtml =
        '<p>Opening segment deliberately has no story-level heading.</p>'
      opening.wordCount = 7
      api.setStory(story)

      await gotoReader(page, api, READER_SLUG)

      const viewport = pagedViewport(page)
      await expect(viewport).toBeVisible()
      const viewportBounds = await viewport.boundingBox()
      expect(viewportBounds).not.toBeNull()
      expect(viewportBounds?.height ?? 0).toBeGreaterThan(0)

      const navigation = page.getByRole('navigation', { name: 'Page navigation' })
      await expect(navigation).toBeVisible()
      for (const name of ['Previous page', 'Next page']) {
        const control = navigation.getByRole('button', { name })
        await expect(control).toBeVisible()
        const bounds = await control.boundingBox()
        expect(bounds).not.toBeNull()
        expect(bounds?.height ?? 0).toBeGreaterThanOrEqual(44)
        expect((bounds?.y ?? 0) + (bounds?.height ?? 0)).toBeLessThanOrEqual(
          viewportSize.height,
        )
      }
      await expectNoHorizontalOverflow(page)
    })
  }
})
