import { expect, makeReaderStory, READER_SLUG, test } from './support/reader-api'
import {
  expectSegmentAtReadingLine,
  gotoReader,
  scrollToSegment,
} from './support/reader-page'

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
})
