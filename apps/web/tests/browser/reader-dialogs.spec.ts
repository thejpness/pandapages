import { expect, READER_SLUG, test } from './support/reader-api'
import {
  expectFocusTrapped,
  expectLocatorV2Request,
  expectSegmentAtReadingLine,
  gotoReader,
} from './support/reader-page'

test.describe('Reader settings and chapters', () => {
  test('scenarios 16–17: Reading settings traps focus, closes with Escape, and restores its trigger', async ({
    page,
    api,
  }) => {
    await gotoReader(page, api, READER_SLUG)
    const trigger = page.locator('.reader-settings-trigger')
    await expect(trigger).toHaveAttribute('aria-expanded', 'false')
    await trigger.click()
    await expect(trigger).toHaveAttribute('aria-expanded', 'true')
    await expect(trigger).toHaveAttribute('aria-controls', 'reader-settings-dialog')
    await expectFocusTrapped(page, 'Reading settings')

    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog', { name: 'Reading settings' })).toBeHidden()
    await expect(trigger).toBeFocused()
    await expect(trigger).toHaveAttribute('aria-expanded', 'false')
  })

  test('scenario 18: settings change Reader typography live and persist validated v2 preferences', async ({
    page,
    api,
  }) => {
    await gotoReader(page, api, READER_SLUG)
    await page.getByRole('button', { name: 'Reading settings' }).click()
    const dialog = page.getByRole('dialog', { name: 'Reading settings' })
    await dialog
      .getByRole('group', { name: 'Font' })
      .getByRole('radio', { name: 'Clear' })
      .check()
    await dialog.getByRole('slider', { name: 'Text size' }).fill('28')
    await dialog.getByRole('slider', { name: 'Line height' }).fill('1.9')
    await dialog.getByRole('slider', { name: 'Content width' }).fill('840')

    const article = page.locator('.reader-story')
    await expect.poll(() => article.evaluate((element) => getComputedStyle(element).fontSize)).toBe('28px')
    await expect
      .poll(() => article.evaluate((element) => getComputedStyle(element).fontFamily))
      .toContain('Atkinson Hyperlegible Next Variable')
    await expect
      .poll(() => article.evaluate((element) => getComputedStyle(element).lineHeight))
      .toBe('53.2px')
    await expect
      .poll(() => article.evaluate((element) => getComputedStyle(element).width))
      .toBe('840px')

    const stored = await page.evaluate(() => localStorage.getItem('pp_reader_prefs_v2'))
    expect(JSON.parse(stored ?? '{}')).toEqual({
      schema: 2,
      mode: 'scroll',
      theme: 'paper',
      fontFamily: 'clear',
      fontSize: 28,
      lineHeight: 1.9,
      contentWidth: 840,
    })
  })

  test('scenario 19: Reset to Defaults updates every setting and the rendered page', async ({
    page,
    api,
  }) => {
    await gotoReader(page, api, READER_SLUG)
    await page.getByRole('button', { name: 'Reading settings' }).click()
    const dialog = page.getByRole('dialog', { name: 'Reading settings' })
    await dialog
      .getByRole('group', { name: 'Font' })
      .getByRole('radio', { name: 'Clear' })
      .check()
    await dialog
      .getByRole('button', {
        name: 'Page style Paper selected Change page colours',
      })
      .click()
    await dialog
      .getByRole('group', { name: 'Page style' })
      .getByRole('radio', { name: 'Warm' })
      .check()
    await dialog.getByRole('slider', { name: 'Text size' }).fill('30')
    await dialog.getByRole('slider', { name: 'Line height' }).fill('2')
    await dialog.getByRole('slider', { name: 'Content width' }).fill('900')
    await dialog.getByRole('button', { name: 'Reset to Defaults' }).click()

    await expect(dialog.getByRole('radio', { name: 'Book' })).toBeChecked()
    await expect(
      dialog
        .getByRole('group', { name: 'Page style' })
        .getByRole('radio', { name: 'Paper' }),
    ).toBeChecked()
    await expect(dialog.getByRole('radio', { name: 'Scroll' })).toBeChecked()
    await expect(dialog.getByRole('slider', { name: 'Text size' })).toHaveValue('20')
    await expect(dialog.getByRole('slider', { name: 'Line height' })).toHaveValue('1.65')
    await expect(dialog.getByRole('slider', { name: 'Content width' })).toHaveValue('720')

    const article = page.locator('.reader-story')
    await expect.poll(() => article.evaluate((element) => getComputedStyle(element).fontSize)).toBe('20px')
    await expect
      .poll(() => article.evaluate((element) => getComputedStyle(element).fontFamily))
      .toContain('Literata Variable')
    await expect(page.locator('.reader-shell')).toHaveAttribute(
      'data-reader-theme',
      'paper',
    )
  })

  test('scenarios 20 and 22: Chapters lists H2s, traps focus, handles Escape, and restores focus', async ({
    page,
    api,
  }) => {
    await gotoReader(page, api, READER_SLUG)
    const trigger = page.getByRole('button', { name: 'Chapters' })
    await trigger.click()
    const dialog = page.getByRole('dialog', { name: 'Chapters' })
    await expect(dialog.getByRole('navigation', { name: 'Story chapters' })).toBeVisible()
    await expect(dialog.getByRole('button', { name: /Chapter One — Lanterns/ })).toBeVisible()
    await expect(dialog.getByRole('button', { name: /Chapter Two — 世界/ })).toBeVisible()
    await expectFocusTrapped(page, 'Chapters')

    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()
    await expect(trigger).toBeFocused()
  })

  test('scenario 21: chapter selection restores the H2, announces it, and publishes its canonical anchor', async ({
    page,
    api,
  }) => {
    const put = api.deferProgressPut(READER_SLUG)
    await gotoReader(page, api, READER_SLUG)
    const trigger = page.getByRole('button', { name: 'Chapters' })
    await trigger.click()
    await page.getByRole('dialog', { name: 'Chapters' })
      .getByRole('button', { name: /Chapter Two — 世界/ })
      .click()

    await expect(page.getByRole('dialog', { name: 'Chapters' })).toBeHidden()
    await expect(trigger).toBeFocused()
    await expect(page.locator('.reader-sr-only[role="status"]')).toContainText('Moved to Chapter Two — 世界.')
    await expectSegmentAtReadingLine(page, 5, 0)
    const request = await put.started
    expectLocatorV2Request(request, { ordinal: 5 })
    expect(request.body).toEqual(
      expect.objectContaining({
        locator: expect.objectContaining({
          chapter: expect.objectContaining({ occurrence: 1 }),
        }),
      }),
    )
    put.fulfill({ ok: true })
  })
})
