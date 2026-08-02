import type { Page } from '@playwright/test'
import { expect, makeReaderStory, READER_SLUG, test } from './support/reader-api'
import {
  expectSegmentAtReadingLine,
  gotoReader,
  seedReaderPreferences,
  scrollToSegment,
} from './support/reader-page'

const hostileSinkHTML = [
  '<script src="https://story-xss.invalid/script.js"></script>',
  '<iframe srcdoc="<script>window.__storyXSS = true</script>"></iframe>',
  '<form><input name="secret"><button>Submit</button></form>',
  '<object data="https://story-xss.invalid/object"></object>',
  '<embed src="https://story-xss.invalid/embed">',
  '<p onclick="window.__storyXSS = true">Hostile story</p>',
  '<a href="javascript:window.__storyXSS = true">Hostile link</a>',
].join('')

function observeActiveContent(page: Page) {
  const dialogs: string[] = []
  const unexpectedRequests: string[] = []
  page.on('dialog', async (dialog) => {
    dialogs.push(dialog.message())
    await dialog.dismiss()
  })
  page.on('request', (request) => {
    if (new URL(request.url()).hostname === 'story-xss.invalid') {
      unexpectedRequests.push(request.url())
    }
  })
  return { dialogs, unexpectedRequests }
}

async function expectNoHostileNodes(page: Page, sink: string) {
  await expect(
    page.locator(
      `${sink} script, ${sink} iframe, ${sink} form, ${sink} input, ${sink} button, ${sink} object, ${sink} embed, ${sink} [onload], ${sink} [onerror], ${sink} [onclick], ${sink} a[href^="javascript:" i], ${sink} a[href^="data:" i]`,
    ),
  ).toHaveCount(0)
}

test.describe('Reader content boundaries', () => {
  test('the final story segment can reach the reading line and report completion', async ({
    page,
    api,
  }) => {
    await gotoReader(page, api, READER_SLUG)
    await scrollToSegment(page, 6, 1)

    await expectSegmentAtReadingLine(page, 6, 1)
    await expect(page.getByRole('progressbar', { name: 'Reading progress' }))
      .toHaveAttribute('aria-valuenow', '100')
  })

  test('repeated chapter titles have distinct accessible navigation names', async ({
    page,
    api,
  }) => {
    const story = makeReaderStory()
    const firstChapter = story.segments[2]
    const repeatedChapter = story.segments[4]
    const repeatedParagraph = story.segments[5]
    if (!firstChapter || !repeatedChapter || !repeatedParagraph) {
      throw new Error('Reader fixture chapter shape changed')
    }
    repeatedChapter.renderedHtml = firstChapter.renderedHtml
    repeatedChapter.contentKey = firstChapter.contentKey
    repeatedChapter.contentOccurrence = 2
    repeatedChapter.chapterKey = firstChapter.chapterKey
    repeatedChapter.chapterOccurrence = 2
    repeatedParagraph.chapterKey = firstChapter.chapterKey
    repeatedParagraph.chapterOccurrence = 2
    api.setStory(story)

    await gotoReader(page, api, READER_SLUG)
    await page.getByRole('button', { name: 'Chapters' }).click()
    const dialog = page.getByRole('dialog', { name: 'Chapters' })
    await expect(
      dialog.getByRole('button', {
        name: 'Chapter One — Lanterns, 1 of 2',
        exact: true,
      }),
    ).toBeVisible()
    await expect(
      dialog.getByRole('button', {
        name: 'Chapter One — Lanterns, 2 of 2',
        exact: true,
      }),
    ).toBeVisible()
  })

  for (const mode of ['scroll', 'paged'] as const) {
    test(`rejects hostile API HTML before it reaches the Reader ${mode} v-html sink`, async ({
      page,
      api,
    }) => {
      await seedReaderPreferences(page, { mode })
      const observed = observeActiveContent(page)
      const story = makeReaderStory()
      const segment = story.segments[1]
      if (!segment) throw new Error('Reader fixture segment shape changed')
      segment.renderedHtml = hostileSinkHTML
      api.setStory(story)

      await page.goto(`/read/${encodeURIComponent(READER_SLUG)}`)

      await expect(
        page.getByRole('heading', { name: 'Story unavailable' }),
      ).toBeVisible()
      await expectNoHostileNodes(page, '[data-reader-scroll-view]')
      await expectNoHostileNodes(page, '[data-reader-paged-view]')
      expect(observed.dialogs).toEqual([])
      expect(observed.unexpectedRequests).toEqual([])
      expect(
        await page.evaluate(
          () => (window as Window & { __storyXSS?: boolean }).__storyXSS,
        ),
      ).toBeUndefined()
    })
  }
})
