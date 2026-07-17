import { AxeBuilder } from '@axe-core/playwright'
import type { Page } from '@playwright/test'
import {
  expect,
  progressFor,
  READER_SLUG,
  test,
} from './support/reader-api'
import { gotoReader } from './support/reader-page'

async function expectNoSeriousOrCriticalViolations(
  page: Page,
): Promise<void> {
  await page.evaluate(async () => {
    await document.fonts.ready
  })
  const results = await new AxeBuilder({ page }).analyze()
  const violations = results.violations
    .filter(
      (violation) =>
        violation.impact === 'serious' || violation.impact === 'critical',
    )
    .map((violation) => ({
      id: violation.id,
      impact: violation.impact,
      help: violation.help,
      nodes: violation.nodes.map((node) => ({
        target: node.target,
        summary: node.failureSummary,
      })),
    }))
  expect(violations).toEqual([])
}

test.describe('Reader automated accessibility checks', () => {
  test('axe: ready scroll Reader has no serious or critical violations', async ({
    page,
    api,
  }) => {
    await gotoReader(page, api, READER_SLUG)
    await expectNoSeriousOrCriticalViolations(page)
  })

  test('axe: Reading settings dialog has no serious or critical violations', async ({
    page,
    api,
  }) => {
    await gotoReader(page, api, READER_SLUG)
    await page.getByRole('button', { name: 'Reading settings' }).click()
    await expect(page.getByRole('dialog', { name: 'Reading settings' })).toBeVisible()
    await expectNoSeriousOrCriticalViolations(page)
  })

  test('axe: Chapters dialog has no serious or critical violations', async ({
    page,
    api,
  }) => {
    await gotoReader(page, api, READER_SLUG)
    await page.getByRole('button', { name: 'Chapters' }).click()
    await expect(page.getByRole('dialog', { name: 'Chapters' })).toBeVisible()
    await expectNoSeriousOrCriticalViolations(page)
  })

  test('axe: Resume dialog has no serious or critical violations', async ({
    page,
    api,
  }) => {
    const story = api.stories.get(READER_SLUG)
    expect(story).toBeDefined()
    if (!story) return
    api.setProgress(READER_SLUG, progressFor(story))
    await gotoReader(page, api, READER_SLUG)
    await expect(page.getByRole('dialog', { name: 'Continue reading?' })).toBeVisible()
    await expectNoSeriousOrCriticalViolations(page)
  })

  test('axe: Story not found has no serious or critical violations', async ({
    page,
    api,
  }) => {
    api.enqueueStory(READER_SLUG, {
      status: 404,
      body: { error: { code: 'not_found', message: 'Story not found' } },
    })
    await page.goto(`/read/${READER_SLUG}`)
    await expect(page.getByRole('heading', { name: 'Story not found' })).toBeVisible()
    await expectNoSeriousOrCriticalViolations(page)
  })

  test('axe: Story unavailable has no serious or critical violations', async ({
    page,
    api,
  }) => {
    api.enqueueStory(READER_SLUG, {
      status: 500,
      body: { error: { code: 'internal_error', message: 'Unavailable' } },
    })
    await page.goto(`/read/${READER_SLUG}`)
    await expect(page.getByRole('heading', { name: 'Story unavailable' })).toBeVisible()
    await expectNoSeriousOrCriticalViolations(page)
  })
})
