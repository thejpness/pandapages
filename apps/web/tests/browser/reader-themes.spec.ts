import type { Locator, Page } from '@playwright/test'
import {
  expect,
  makePagedReaderStory,
  makeReaderStory,
  progressFor,
  READER_SLUG,
  test,
} from './support/reader-api'
import {
  expectNoProgressPut,
  expectSegmentAtReadingLine,
  gotoReader,
  seedReaderPreferences,
  waitForPagedReady,
} from './support/reader-page'

const themes = [
  {
    id: 'clear',
    name: 'Clear',
    description: 'Bright page with the strongest contrast',
    help: 'Best when you want the crispest text, especially in bright light.',
    background: '#FAFBFC',
    surface: '#FFFFFF',
    text: '#111111',
    heading: '#0B0F14',
    link: '#0B57D0',
    selectionBackground: '#DCEBFF',
    selectionText: '#09111F',
  },
  {
    id: 'paper',
    name: 'Paper',
    description: 'Soft neutral page for everyday reading',
    help: 'A calm print-like page designed as the default starting point.',
    background: '#F5F1E8',
    surface: '#FBF8F2',
    text: '#1C1A17',
    heading: '#141210',
    link: '#0B57D0',
    selectionBackground: '#E7D8B7',
    selectionText: '#141210',
  },
  {
    id: 'warm',
    name: 'Warm',
    description: 'Cream page with gentle warmth',
    help: 'A warmer page tone for readers who prefer a sepia-style feel.',
    background: '#F0E4D2',
    surface: '#F7ECDD',
    text: '#2C2318',
    heading: '#231B12',
    link: '#8A3F00',
    selectionBackground: '#D7BA8E',
    selectionText: '#231B12',
  },
  {
    id: 'mist',
    name: 'Mist',
    description: 'Cool grey page with a softer feel',
    help: 'A cooler light page for readers who dislike bright white or cream.',
    background: '#E7EDF0',
    surface: '#F4F7F9',
    text: '#172126',
    heading: '#0F171C',
    link: '#005A9C',
    selectionBackground: '#CFE2F3',
    selectionText: '#0F171C',
  },
  {
    id: 'night',
    name: 'Night',
    description: 'Dark page for dim rooms',
    help: 'A low-light dark theme for bedtime or darker spaces.',
    background: '#121417',
    surface: '#1A1E23',
    text: '#EEF2F7',
    heading: '#FFFFFF',
    link: '#8AB4F8',
    selectionBackground: '#26466F',
    selectionText: '#FFFFFF',
  },
] as const

type Theme = (typeof themes)[number]

function pageStyleDisclosure(dialog: Locator, themeName: string) {
  return dialog.getByRole('button', {
    name: 'Page style ' + themeName + ' selected Change page colours',
    exact: true,
  })
}

async function expandPageStyle(
  dialog: Locator,
  themeName: string,
): Promise<Locator> {
  const disclosure = pageStyleDisclosure(dialog, themeName)
  if ((await disclosure.getAttribute('aria-expanded')) !== 'true') {
    await disclosure.click()
  }
  await expect(disclosure).toHaveAttribute('aria-expanded', 'true')
  const group = dialog.getByRole('group', { name: 'Page style' })
  await expect(group).toBeVisible()
  return group
}

function asRgb(hex: string): string {
  const value = Number.parseInt(hex.slice(1), 16)
  return `rgb(${value >> 16}, ${(value >> 8) & 255}, ${value & 255})`
}

async function waitForTheme(page: Page, theme: Theme): Promise<void> {
  await waitForActiveTheme(page, theme)
  await expect
    .poll(() =>
      page.evaluate(() => {
        const stored = localStorage.getItem('pp_reader_prefs_v2')
        return stored ? (JSON.parse(stored) as { theme?: string }).theme : null
      }),
    )
    .toBe(theme.id)
}

async function waitForActiveTheme(page: Page, theme: Theme): Promise<void> {
  const shell = page.locator('.reader-shell')
  await expect(shell).toHaveAttribute('data-reader-theme', theme.id)
  await expect(shell).toHaveAttribute('data-reader-preference-pending', 'false')
  await expect(shell).toHaveCSS('background-color', asRgb(theme.background))
  await expect
    .poll(() => page.locator('html').getAttribute('data-reader-theme-booting'))
    .toBeNull()
}

type PagedSemanticPosition = {
  locator: {
    segment: {
      key: string
      occurrence: number
      ordinal: number
      offset: number
    }
  }
  percent: number
}

async function capturePagedSemanticPosition(
  page: Page,
): Promise<PagedSemanticPosition> {
  return page.locator('[data-reader-paged-view]').evaluate((element) => {
    // The component already exposes capture() as its semantic placement seam.
    // This narrow bridge avoids adding a production-only browser-test API.
    const component = (
      element as HTMLElement & {
        __vueParentComponent?: {
          exposed?: {
            capture?: () => PagedSemanticPosition | null
          }
        }
      }
    ).__vueParentComponent
    const position = component?.exposed?.capture?.()
    if (!position) throw new Error('Paged Reader did not expose a semantic position')
    return {
      locator: {
        segment: {
          key: position.locator.segment.key,
          occurrence: position.locator.segment.occurrence,
          ordinal: position.locator.segment.ordinal,
          offset: position.locator.segment.offset,
        },
      },
      percent: position.percent,
    }
  })
}

async function initializeStoredTheme(
  page: Page,
  theme: Theme['id'],
  mode: 'scroll' | 'paged' = 'scroll',
): Promise<void> {
  await page.goto('/logo.png')
  await page.evaluate(
    ({ selectedTheme, selectedMode }) => {
      localStorage.setItem(
        'pp_reader_prefs_v2',
        JSON.stringify({
          schema: 2,
          mode: selectedMode,
          theme: selectedTheme,
          fontFamily: 'book',
          fontSize: 20,
          lineHeight: 1.65,
          contentWidth: 720,
        }),
      )
    },
    { selectedTheme: theme, selectedMode: mode },
  )
}

async function expectNoHorizontalOverflow(page: Page): Promise<void> {
  await expect
    .poll(() =>
      page.evaluate(
        () =>
          document.documentElement.scrollWidth -
          document.documentElement.clientWidth,
      ),
    )
    .toBeLessThanOrEqual(1)
}

test.describe('Reader colour themes', () => {
  test('Paper is the first-time default and all five choices are accessible text-preview cards', async ({
    page,
    api,
  }) => {
    await gotoReader(page, api, READER_SLUG)

    await expect(page.locator('html')).toHaveAttribute(
      'data-reader-theme',
      'paper',
    )
    await expect(page.locator('.reader-shell')).toHaveAttribute(
      'data-reader-theme',
      'paper',
    )

    await expect
      .poll(() => page.locator('html').getAttribute('data-reader-theme-booting'))
      .toBeNull()
    await page.getByRole('button', { name: 'Reading settings' }).click()
    const dialog = page.getByRole('dialog', { name: 'Reading settings' })
    const disclosure = pageStyleDisclosure(dialog, 'Paper')
    await expect(disclosure).toHaveAttribute('aria-expanded', 'false')
    await expect(disclosure).toHaveAttribute('aria-controls', 'reader-theme-options')
    await expect(
      disclosure.locator('.reader-settings-disclosure__label'),
    ).toHaveText('Page style')
    await expect(
      disclosure.locator('.reader-settings-disclosure__current'),
    ).toHaveText('Paper selected')
    await expect(
      disclosure.locator('.reader-settings-disclosure__help'),
    ).toHaveText('Change page colours')
    await expect(
      disclosure.locator('.reader-settings-disclosure__indicator'),
    ).toBeVisible()
    await expect(disclosure).not.toContainText('—')
    await expect(dialog.getByRole('group', { name: 'Page style' })).toBeHidden()
    const pageStyle = await expandPageStyle(dialog, 'Paper')
    await expect(pageStyle.getByRole('radio')).toHaveCount(5)

    for (const theme of themes) {
      const radio = pageStyle.getByRole('radio', {
        name: theme.name,
        exact: true,
      })
      await expect(radio).toBeVisible()
      await expect(radio).toHaveAccessibleDescription(
        `${theme.description} ${theme.help}`,
      )
      const card = radio.locator('..')
      await expect(card).toContainText(theme.name)
      await expect(card).toContainText(theme.description)
      await expect(card.locator('.reader-theme-preview')).toContainText(
        'A little adventure',
      )
      await expect(card.locator('.reader-theme-preview')).toContainText(
        'The little panda turned the page and found a path through the trees.',
      )
      await expect(card.locator('.reader-theme-preview')).toContainText(
        'Keep reading',
      )
      await expect(card.locator('.reader-theme-preview__page')).toHaveCSS(
        'background-color',
        asRgb(theme.surface),
      )
    }

    await expect(
      pageStyle.getByRole('radio', { name: 'Paper', exact: true }),
    ).toBeChecked()

    await pageStyle
      .getByRole('radio', { name: 'Clear', exact: true })
      .locator('..')
      .click()
    const clear = pageStyle.getByRole('radio', { name: 'Clear', exact: true })
    const paper = pageStyle.getByRole('radio', { name: 'Paper', exact: true })
    await expect(clear).toBeChecked()
    await expect(
      clear.locator('..').locator('.reader-theme-card__selected'),
    ).toBeVisible()
    await expect(
      paper.locator('..').locator('.reader-theme-card__selected'),
    ).toBeHidden()
    await expect(paper).not.toBeChecked()
    await expect(
      pageStyleDisclosure(dialog, 'Clear'),
    ).toBeVisible()
  })

  test('all five presets apply and persist immediately without moving or saving scroll progress', async ({
    page,
    api,
  }) => {
    await seedReaderPreferences(page, { mode: 'scroll', theme: 'paper' })
    const story = makeReaderStory()
    const anchored = story.segments.find((segment) => segment.ordinal === 4)
    if (!anchored) throw new Error('Missing anchored Reader segment')
    anchored.renderedHtml = anchored.renderedHtml.replace(
      '</p>',
      ' <a href="/library">Keep reading</a>.</p>',
    )
    anchored.wordCount += 2
    api.setStory(story)
    api.setProgress(READER_SLUG, progressFor(story, 4, 0.4, 0.52))

    await gotoReader(page, api, READER_SLUG)
    const resume = page.getByRole('dialog', { name: 'Continue reading?' })
    await resume
      .getByRole('button', { name: 'Resume' })
      .click()
    await expect(resume).toBeHidden()
    await expectSegmentAtReadingLine(page, 4, 0.4)
    await expectNoProgressPut(page, api, READER_SLUG)

    const trigger = page.getByRole('button', { name: 'Reading settings' })
    await trigger.click()
    const dialog = page.getByRole('dialog', { name: 'Reading settings' })
    const pageStyle = await expandPageStyle(dialog, 'Paper')
    const article = page.locator('.reader-story')
    const heading = article.locator('h2').first()
    const link = article.locator('a', { hasText: 'Keep reading' })

    for (const theme of themes) {
      const radio = pageStyle.getByRole('radio', {
        name: theme.name,
        exact: true,
      })
      await radio.check()
      await waitForTheme(page, theme)
      await expect(dialog).toBeVisible()
      await expect(radio).toBeFocused()
      await expect(radio).toBeChecked()
      await expect(article).toHaveCSS('color', asRgb(theme.text))
      await expect(heading).toHaveCSS('color', asRgb(theme.heading))
      await expect(link).toHaveCSS('color', asRgb(theme.link))
      await expect(link).toHaveCSS('text-decoration-line', 'underline')
      for (const selectable of [article, heading, link]) {
        const selection = await selectable.evaluate((element) => {
          const range = document.createRange()
          range.selectNodeContents(element)
          const current = window.getSelection()
          current?.removeAllRanges()
          current?.addRange(range)
          const style = getComputedStyle(element, '::selection')
          return {
            background: style.backgroundColor,
            color: style.color,
            text: current?.toString() ?? '',
          }
        })
        expect(selection).toEqual({
          background: asRgb(theme.selectionBackground),
          color: asRgb(theme.selectionText),
          text: expect.stringMatching(/\S/u),
        })
      }
      await expectSegmentAtReadingLine(page, 4, 0.4)
    }

    await expectNoProgressPut(page, api, READER_SLUG)
    expect(api.progressPuts(READER_SLUG)).toHaveLength(0)
    await dialog.getByRole('button', { name: 'Done' }).click()
    await expect(dialog).toBeHidden()
    await expect(trigger).toBeFocused()
  })

  test('native radio keyboard selection stays open, persists, and Escape restores Aa focus', async ({
    page,
    api,
  }) => {
    await gotoReader(page, api, READER_SLUG)
    const trigger = page.getByRole('button', { name: 'Reading settings' })
    await trigger.click()
    const dialog = page.getByRole('dialog', { name: 'Reading settings' })
    const pageStyle = await expandPageStyle(dialog, 'Paper')
    const paper = pageStyle.getByRole('radio', { name: 'Paper', exact: true })
    const warm = pageStyle.getByRole('radio', { name: 'Warm', exact: true })

    await paper.focus()
    await page.keyboard.press('ArrowDown')
    await expect(warm).toBeFocused()
    await expect(warm).toBeChecked()
    await waitForTheme(page, themes[2])
    await expect(dialog).toBeVisible()

    await page.keyboard.press('ArrowUp')
    await expect(paper).toBeFocused()
    await expect(paper).toBeChecked()
    await waitForTheme(page, themes[1])

    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()
    await expect(trigger).toBeFocused()
    await expectNoProgressPut(page, api, READER_SLUG)
  })

  test('a UI-selected theme persists and restores on reload without test reseeding', async ({
    page,
    api,
  }) => {
    await initializeStoredTheme(page, 'paper')
    await gotoReader(page, api, READER_SLUG)

    await page.getByRole('button', { name: 'Reading settings' }).click()
    const dialog = page.getByRole('dialog', { name: 'Reading settings' })
    const night = (await expandPageStyle(dialog, 'Paper'))
      .getByRole('radio', { name: 'Night', exact: true })
    await night.check()
    await expect
      .poll(() =>
        page.evaluate(() => {
          const stored = localStorage.getItem('pp_reader_prefs_v2')
          return stored
            ? (JSON.parse(stored) as { theme?: string }).theme
            : null
        }),
      )
      .toBe('night')

    await page.reload()
    await expect(page.locator('[data-reader-scroll-view]')).toBeVisible()
    await expect
      .poll(() => api.count('GET', '/api/v1/progress/' + READER_SLUG))
      .toBe(2)
    await waitForTheme(page, themes[4])

    await page.getByRole('button', { name: 'Reading settings' }).click()
    const reloadedDialog = page.getByRole('dialog', {
      name: 'Reading settings',
    })
    const reloadedPageStyle = await expandPageStyle(reloadedDialog, 'Night')
    await expect(
      reloadedPageStyle.getByRole('radio', { name: 'Night', exact: true }),
    ).toBeChecked()
    await expectNoProgressPut(page, api, READER_SLUG)
  })

  test('direct Reader navigation remains hidden until saved Night is installed', async ({
    page,
    api,
  }) => {
    await initializeStoredTheme(page, 'night')
    await page.addInitScript(() => {
      const target = window as Window & {
        __readerMainGate?: Promise<void>
        __releaseReaderMain?: () => void
        __readerThemeSamples?: Array<{
          booting: string | null
          rootTheme: string | null
          visibility: string
          shellTheme: string | null
          shellBackground: string | null
        }>
      }
      target.__readerMainGate = new Promise<void>((resolve) => {
        target.__releaseReaderMain = resolve
      })
      target.__readerThemeSamples = []
      const sample = () => {
        const shell = document.querySelector<HTMLElement>('.reader-shell')
        target.__readerThemeSamples?.push({
          booting: document.documentElement.dataset.readerThemeBooting ?? null,
          rootTheme: document.documentElement.dataset.readerTheme ?? null,
          visibility: getComputedStyle(document.documentElement).visibility,
          shellTheme: shell?.dataset.readerTheme ?? null,
          shellBackground: shell
            ? getComputedStyle(shell).backgroundColor
            : null,
        })
        if (!shell) requestAnimationFrame(sample)
      }
      requestAnimationFrame(sample)
    })

    let mainInstrumented = false
    await page.route('**/src/main.ts*', async (route) => {
      const response = await route.fetch()
      const source = await response.text()
      const instrumented = source.replace(
        /bootstrapReaderTheme\(\);?/,
        'window.__readerMainPaused = true; await window.__readerMainGate; bootstrapReaderTheme();',
      )
      mainInstrumented = instrumented !== source
      await route.fulfill({
        response,
        body: instrumented,
        headers: {
          ...response.headers(),
          'content-type': 'application/javascript; charset=utf-8',
        },
      })
    })

    await page.goto('/read/' + READER_SLUG, { waitUntil: 'commit' })
    await expect
      .poll(() =>
        page.evaluate(
          () =>
            (window as Window & { __readerMainPaused?: boolean })
              .__readerMainPaused ?? false,
        ),
      )
      .toBe(true)
    expect(mainInstrumented).toBe(true)
    await expect(page.locator('html')).toHaveAttribute(
      'data-reader-theme-booting',
      'true',
    )
    await expect
      .poll(() =>
        page.evaluate(() => getComputedStyle(document.documentElement).visibility),
      )
      .toBe('hidden')
    await expect(page.locator('html')).not.toHaveAttribute('data-reader-theme', /.+/u)

    await page.evaluate(() =>
      (window as Window & { __releaseReaderMain?: () => void })
        .__releaseReaderMain?.(),
    )
    await expect(page.locator('[data-reader-scroll-view]')).toBeVisible()
    await expect
      .poll(() => api.count('GET', '/api/v1/progress/' + READER_SLUG))
      .toBe(1)
    await waitForTheme(page, themes[4])

    const samples = await page.evaluate(
      () =>
        (
          window as Window & {
            __readerThemeSamples?: Array<{
              booting: string | null
              rootTheme: string | null
              visibility: string
              shellTheme: string | null
              shellBackground: string | null
            }>
          }
        ).__readerThemeSamples ?? [],
    )
    expect(samples).toContainEqual(
      expect.objectContaining({
        booting: 'true',
        rootTheme: null,
        visibility: 'hidden',
      }),
    )
    expect(samples).not.toContainEqual(
      expect.objectContaining({ rootTheme: 'paper' }),
    )
    expect(
      samples.filter((sample) => sample.visibility === 'visible'),
    ).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ rootTheme: 'night' }),
      ]),
    )
    const visibleSamples = samples.filter(
      (sample) => sample.visibility === 'visible',
    )
    expect(visibleSamples).not.toContainEqual(
      expect.objectContaining({ rootTheme: 'paper' }),
    )
    expect(visibleSamples).not.toContainEqual(
      expect.objectContaining({ shellTheme: 'paper' }),
    )
    expect(visibleSamples).not.toContainEqual(
      expect.objectContaining({ shellBackground: asRgb(themes[1].background) }),
    )
    expect(samples).toContainEqual(
      expect.objectContaining({
        rootTheme: 'night',
        visibility: 'visible',
        shellTheme: 'night',
        shellBackground: asRgb(themes[4].background),
      }),
    )
  })

  test('rapid and repeated selection coalesces to the final theme and can close immediately', async ({
    page,
    api,
  }) => {
    await seedReaderPreferences(page, { mode: 'scroll', theme: 'paper' })
    await gotoReader(page, api, READER_SLUG)
    await page.getByRole('button', { name: 'Reading settings' }).click()
    const dialog = page.getByRole('dialog', { name: 'Reading settings' })
    const pageStyle = await expandPageStyle(dialog, 'Paper')
    const paper = pageStyle.getByRole('radio', { name: 'Paper', exact: true })

    await paper.check()
    await expect(paper).toBeChecked()
    await pageStyle.getByRole('radio', { name: 'Clear', exact: true }).check()
    await pageStyle.getByRole('radio', { name: 'Warm', exact: true }).check()
    await pageStyle.getByRole('radio', { name: 'Night', exact: true }).check()
    await expect(page.locator('.reader-shell')).toHaveAttribute(
      'data-reader-preference-pending',
      'true',
    )
    await page.evaluate(() =>
      window.dispatchEvent(new PageTransitionEvent('pagehide')),
    )
    await dialog.getByRole('button', { name: 'Done' }).click()

    await expect(dialog).toBeHidden()
    await waitForTheme(page, themes[4])
    await expectNoProgressPut(page, api, READER_SLUG)
    expect(api.progressPuts(READER_SLUG)).toHaveLength(0)
  })

  test('a storage write failure keeps the active theme and does not surface an error', async ({
    page,
    api,
  }) => {
    await initializeStoredTheme(page, 'paper')
    await gotoReader(page, api, READER_SLUG)
    const pageErrors: Error[] = []
    page.on('pageerror', (error) => pageErrors.push(error))
    await page.evaluate(() => {
      const original = Object.getOwnPropertyDescriptor(
        Storage.prototype,
        'setItem',
      )?.value as (this: Storage, key: string, value: string) => void
      Object.defineProperty(Storage.prototype, 'setItem', {
        configurable: true,
        value(this: Storage, key: string, value: string) {
          if (key === 'pp_reader_prefs_v2') {
            throw new DOMException('Test-only storage failure', 'QuotaExceededError')
          }
          return original.call(this, key, value)
        },
      })
    })

    await page.getByRole('button', { name: 'Reading settings' }).click()
    const dialog = page.getByRole('dialog', { name: 'Reading settings' })
    await (await expandPageStyle(dialog, 'Paper'))
      .getByRole('radio', { name: 'Mist', exact: true })
      .check()
    await waitForActiveTheme(page, themes[3])
    await expect(dialog).toBeVisible()
    await expect(
      pageStyleDisclosure(dialog, 'Mist'),
    ).toBeVisible()
    expect(
      await page.evaluate(() =>
        JSON.parse(localStorage.getItem('pp_reader_prefs_v2') ?? 'null'),
      ),
    ).toEqual(expect.objectContaining({ theme: 'paper' }))
    expect(pageErrors).toEqual([])
    await expectNoProgressPut(page, api, READER_SLUG)
  })

  test('selection immediately before unmount cannot leave Reader theme state or progress behind', async ({
    page,
    api,
  }) => {
    await initializeStoredTheme(page, 'paper')
    await gotoReader(page, api, READER_SLUG)
    await page.getByRole('button', { name: 'Reading settings' }).click()
    const dialog = page.getByRole('dialog', { name: 'Reading settings' })
    await (await expandPageStyle(dialog, 'Paper'))
      .getByRole('radio', { name: 'Night', exact: true })
      .check()
    await expect(page.locator('.reader-shell')).toHaveAttribute(
      'data-reader-preference-pending',
      'true',
    )

    await dialog.getByRole('button', { name: 'Done' }).click()
    await page.getByRole('button', { name: 'Return to Library' }).click()
    await expect(page).toHaveURL(/\/library$/u)
    await expect(page.locator('html')).not.toHaveAttribute('data-reader-theme', /.+/u)
    await expect(page.locator('html')).not.toHaveAttribute(
      'data-reader-route-theme',
      /.+/u,
    )
    const rootState = await page.locator('html').evaluate((element) => ({
      colorScheme: getComputedStyle(element).colorScheme,
      background: element.style.getPropertyValue('--reader-background'),
      readerColorScheme: element.style.getPropertyValue(
        '--reader-color-scheme',
      ),
    }))
    expect(rootState).toEqual({
      colorScheme: 'light',
      background: '',
      readerColorScheme: '',
    })
    await expectNoProgressPut(page, api, READER_SLUG)
  })

  test('Page style disclosure is keyboard operable and retains theme and disclosure state @paged-core', async ({
    page,
    api,
  }) => {
    await seedReaderPreferences(page, { mode: 'scroll', theme: 'paper' })
    await gotoReader(page, api, READER_SLUG)
    await page.getByRole('button', { name: 'Reading settings' }).click()
    const dialog = page.getByRole('dialog', { name: 'Reading settings' })
    let disclosure = pageStyleDisclosure(dialog, 'Paper')
    const pageStyle = dialog.getByRole('group', { name: 'Page style' })
    const indicator = disclosure.locator(
      '.reader-settings-disclosure__indicator',
    )

    await expect(disclosure).toHaveAttribute('aria-expanded', 'false')
    await expect
      .poll(() => indicator.evaluate((element) => getComputedStyle(element).transform))
      .toBe('none')
    const collapsedTransform = await indicator.evaluate(
      (element) => getComputedStyle(element).transform,
    )
    await expect(pageStyle).toBeHidden()
    const close = dialog.getByRole('button', { name: 'Close', exact: true })
    await expect(close).toBeFocused()
    await page.keyboard.press('Tab')
    await expect(disclosure).toBeFocused()
    const disclosureFocus = await disclosure.evaluate((element) => {
      const style = getComputedStyle(element)
      return {
        color: style.outlineColor,
        style: style.outlineStyle,
        width: Number.parseFloat(style.outlineWidth),
      }
    })
    expect(disclosureFocus.style).not.toBe('none')
    expect(disclosureFocus.width).toBeGreaterThanOrEqual(3)
    expect(disclosureFocus.color).not.toBe('transparent')
    expect(disclosureFocus.color).not.toBe('rgba(0, 0, 0, 0)')
    await page.keyboard.press('Space')
    await expect(disclosure).toHaveAttribute('aria-expanded', 'true')
    await expect(disclosure).toBeFocused()
    await expect(pageStyle).toBeVisible()
    await expect(pageStyle.getByRole('radio')).toHaveCount(5)
    await expect
      .poll(() => indicator.evaluate((element) => getComputedStyle(element).transform))
      .not.toBe('none')
    expect(
      await indicator.evaluate((element) => getComputedStyle(element).transform),
    ).not.toBe(collapsedTransform)
    await page.keyboard.press('Enter')
    await expect(disclosure).toHaveAttribute('aria-expanded', 'false')
    await expect(disclosure).toBeFocused()
    await expect(pageStyle).toBeHidden()
    await page.keyboard.press('Space')
    await expect(disclosure).toHaveAttribute('aria-expanded', 'true')
    await expect(disclosure).toBeFocused()
    await expect(pageStyle).toBeVisible()

    await pageStyle
      .getByRole('radio', { name: 'Mist', exact: true })
      .check()
    await waitForTheme(page, themes[3])
    disclosure = pageStyleDisclosure(dialog, 'Mist')
    await disclosure.focus()
    await page.keyboard.press('Space')
    await expect(disclosure).toHaveAttribute('aria-expanded', 'false')
    await expect(disclosure).toBeFocused()
    await expect(pageStyle).toBeHidden()
    await dialog.getByRole('button', { name: 'Done' }).click()

    await page.getByRole('button', { name: 'Reading settings' }).click()
    const reopened = page.getByRole('dialog', { name: 'Reading settings' })
    await expect(
      pageStyleDisclosure(reopened, 'Mist'),
    ).toHaveAttribute('aria-expanded', 'false')
    await expect(
      reopened.getByRole('group', { name: 'Page style' }),
    ).toBeHidden()
    await expectNoProgressPut(page, api, READER_SLUG)
  })

  test('collapsed Page style materially reduces mobile settings scrolling', async ({
    page,
    api,
  }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.setViewportSize({ width: 320, height: 640 })
    await gotoReader(page, api, READER_SLUG)
    await page.getByRole('button', { name: 'Reading settings' }).click()
    const dialog = page.getByRole('dialog', { name: 'Reading settings' })
    const content = dialog

    for (const viewport of [
      { width: 320, height: 640 },
      { width: 390, height: 700 },
    ]) {
      await page.setViewportSize(viewport)
      const collapsed = pageStyleDisclosure(dialog, 'Paper')
      if ((await collapsed.getAttribute('aria-expanded')) === 'false') {
        await collapsed.click()
      }
      await expect(collapsed).toHaveAttribute('aria-expanded', 'true')
      const expandedGeometry = await content.evaluate((element) => ({
        clientHeight: element.clientHeight,
        scrollHeight: element.scrollHeight,
      }))

      await collapsed.click()
      await expect(collapsed).toHaveAttribute('aria-expanded', 'false')
      const collapsedGeometry = await content.evaluate((element) => ({
        clientHeight: element.clientHeight,
        scrollHeight: element.scrollHeight,
      }))
      const disclosureGeometry = await collapsed.evaluate((element) => {
        const button = element.getBoundingClientRect()
        const label = element.querySelector<HTMLElement>(
          '.reader-settings-disclosure__label',
        )
        const current = element.querySelector<HTMLElement>(
          '.reader-settings-disclosure__current',
        )
        const indicator = element.querySelector<HTMLElement>(
          '.reader-settings-disclosure__indicator',
        )
        if (!label || !current || !indicator) {
          throw new Error('Missing Page style disclosure content')
        }
        const lineMetric = (target: HTMLElement) => {
          const style = getComputedStyle(target)
          const fontSize = Number.parseFloat(style.fontSize)
          const lineHeight = Number.parseFloat(style.lineHeight)
          return {
            height: target.getBoundingClientRect().height,
            lineHeight: Number.isFinite(lineHeight)
              ? lineHeight
              : fontSize * 1.3,
          }
        }
        const indicatorRect = indicator.getBoundingClientRect()
        const indicatorStyle = getComputedStyle(indicator)
        return {
          button: { left: button.left, right: button.right },
          current: lineMetric(current),
          indicator: {
            display: indicatorStyle.display,
            height: indicatorRect.height,
            left: indicatorRect.left,
            opacity: Number.parseFloat(indicatorStyle.opacity),
            right: indicatorRect.right,
            visibility: indicatorStyle.visibility,
            width: indicatorRect.width,
          },
          label: lineMetric(label),
        }
      })
      expect(disclosureGeometry.label.height).toBeLessThanOrEqual(
        disclosureGeometry.label.lineHeight * 1.25,
      )
      expect(disclosureGeometry.current.height).toBeLessThanOrEqual(
        disclosureGeometry.current.lineHeight * 1.25,
      )
      expect(disclosureGeometry.indicator.display).not.toBe('none')
      expect(disclosureGeometry.indicator.visibility).toBe('visible')
      expect(disclosureGeometry.indicator.opacity).toBeGreaterThan(0)
      expect(disclosureGeometry.indicator.width).toBeGreaterThan(0)
      expect(disclosureGeometry.indicator.height).toBeGreaterThan(0)
      expect(disclosureGeometry.indicator.left).toBeGreaterThanOrEqual(
        disclosureGeometry.button.left,
      )
      expect(disclosureGeometry.indicator.right).toBeLessThanOrEqual(
        disclosureGeometry.button.right,
      )
      expect(
        expandedGeometry.scrollHeight - collapsedGeometry.scrollHeight,
      ).toBeGreaterThanOrEqual(Math.round(viewport.height * 0.5))
      expect(
        collapsedGeometry.scrollHeight - collapsedGeometry.clientHeight,
      ).toBeLessThan(
        expandedGeometry.scrollHeight - expandedGeometry.clientHeight,
      )
      const textHeading = dialog.getByRole('heading', { name: 'Text' })
      await textHeading.scrollIntoViewIfNeeded()
      await expect(textHeading).toBeVisible()
      await expectNoHorizontalOverflow(page)
    }
  })

  test('phone settings reflow to one readable column without horizontal scrolling', async ({
    page,
    api,
  }) => {
    await page.setViewportSize({ width: 320, height: 640 })
    await gotoReader(page, api, READER_SLUG)
    await page.getByRole('button', { name: 'Reading settings' }).click()
    const dialog = page.getByRole('dialog', { name: 'Reading settings' })
    await expandPageStyle(dialog, 'Paper')
    const cards = dialog.locator('.reader-theme-card')
    await expect(cards).toHaveCount(5)
    await expectNoHorizontalOverflow(page)

    const boxes = await cards.evaluateAll((elements) =>
      elements.map((element) => {
        const rect = element.getBoundingClientRect()
        return { left: rect.left, right: rect.right, width: rect.width }
      }),
    )
    for (const box of boxes) {
      expect(box.left).toBeGreaterThanOrEqual(0)
      expect(box.right).toBeLessThanOrEqual(320)
      expect(box.width).toBeGreaterThan(240)
    }
    for (let index = 1; index < boxes.length; index += 1) {
      expect(Math.abs(boxes[index].left - boxes[0].left)).toBeLessThanOrEqual(
        1,
      )
    }
  })

  test('settings remain reachable with 32px root text and a short viewport', async ({
    page,
    api,
  }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.setViewportSize({ width: 640, height: 420 })
    await gotoReader(page, api, READER_SLUG)
    const largeText = await page.addStyleTag({
      content: 'html { font-size: 32px !important; }',
    })
    await page.getByRole('button', { name: 'Reading settings' }).click()

    const dialog = page.getByRole('dialog', { name: 'Reading settings' })
    const pageStyle = await expandPageStyle(dialog, 'Paper')
    await expect(dialog).toBeVisible()
    await expect(pageStyle.getByRole('radio')).toHaveCount(5)
    await pageStyle
      .getByRole('radio', { name: 'Night', exact: true })
      .scrollIntoViewIfNeeded()
    await expect(
      pageStyle.getByRole('radio', { name: 'Night', exact: true }),
    ).toBeVisible()
    await dialog.getByRole('button', { name: 'Done' }).scrollIntoViewIfNeeded()
    await expect(dialog.getByRole('button', { name: 'Done' })).toBeVisible()
    await expectNoHorizontalOverflow(page)

    const largeBounds = await dialog.boundingBox()
    expect(largeBounds).not.toBeNull()
    expect(largeBounds?.x ?? -1).toBeGreaterThanOrEqual(0)
    expect((largeBounds?.x ?? 0) + (largeBounds?.width ?? 0)).toBeLessThanOrEqual(
      640,
    )

    await largeText.evaluate((element) => {
      element.parentNode?.removeChild(element)
    })
    await page.setViewportSize({ width: 844, height: 390 })
    await expect(dialog).toBeVisible()
    await dialog.getByRole('button', { name: 'Done' }).scrollIntoViewIfNeeded()
    const doneBounds = await dialog
      .getByRole('button', { name: 'Done' })
      .boundingBox()
    expect(doneBounds).not.toBeNull()
    expect((doneBounds?.y ?? 0) + (doneBounds?.height ?? 0)).toBeLessThanOrEqual(
      390,
    )
    await expectNoHorizontalOverflow(page)
  })

  test(
    'paged Night application preserves its semantic anchor without a progress write @paged-core',
    async ({ page, api }) => {
      await seedReaderPreferences(page, { mode: 'paged', theme: 'paper' })
      const story = makePagedReaderStory()
      api.setStory(story)
      api.setProgress(READER_SLUG, progressFor(story, 7, 0.35, 0.72))

      await gotoReader(page, api, READER_SLUG)
      await page
        .getByRole('dialog', { name: 'Continue reading?' })
        .getByRole('button', { name: 'Resume' })
        .click()
      await waitForPagedReady(page)
      const before = await capturePagedSemanticPosition(page)
      expect(before.locator.segment.ordinal).toBe(7)
      expect(before.locator.segment.key).toBe(
        story.segments.find((segment) => segment.ordinal === 7)?.contentKey,
      )
      expect(before.locator.segment.occurrence).toBe(
        story.segments.find((segment) => segment.ordinal === 7)
          ?.contentOccurrence,
      )
      await expectNoProgressPut(page, api, READER_SLUG)

      await page.getByRole('button', { name: 'Reading settings' }).click()
      const dialog = page.getByRole('dialog', { name: 'Reading settings' })
      await (await expandPageStyle(dialog, 'Paper'))
        .getByRole('radio', { name: 'Night', exact: true })
        .check()
      await waitForTheme(page, themes[4])

      await expect(page.locator('.reader-page').first()).toHaveCSS(
        'background-color',
        asRgb(themes[4].surface),
      )
      const after = await capturePagedSemanticPosition(page)
      expect(after.locator.segment.key).toBe(before.locator.segment.key)
      expect(after.locator.segment.occurrence).toBe(
        before.locator.segment.occurrence,
      )
      expect(after.locator.segment.ordinal).toBe(before.locator.segment.ordinal)
      expect(after.locator.segment.offset).toBeCloseTo(
        before.locator.segment.offset,
        2,
      )
      expect(after.percent).toBeCloseTo(before.percent, 2)
      await expectNoProgressPut(page, api, READER_SLUG)
      expect(api.progressPuts(READER_SLUG)).toHaveLength(0)
    },
  )

  test('forced colours retains a checked radio and a non-colour selected cue', async ({
    page,
    api,
  }) => {
    await page.emulateMedia({ forcedColors: 'active' })
    await gotoReader(page, api, READER_SLUG)
    await page.getByRole('button', { name: 'Reading settings' }).click()
    const dialog = page.getByRole('dialog', { name: 'Reading settings' })
    const pageStyle = await expandPageStyle(dialog, 'Paper')
    const paper = pageStyle.getByRole('radio', {
      name: 'Paper',
      exact: true,
    })
    await expect(paper).toBeChecked()
    const selectedCue = paper.locator('..').locator('.reader-theme-card__selected')
    const clearCue = pageStyle
      .getByRole('radio', { name: 'Clear', exact: true })
      .locator('..')
      .locator('.reader-theme-card__selected')
    await expect(selectedCue).toBeVisible()
    await expect(selectedCue).toHaveText(/✓\s*Selected/u)
    await expect(clearCue).toBeHidden()
    await expect(
      pageStyle.getByRole('radio', { name: 'Clear', exact: true }),
    ).not.toBeChecked()
    await expect(paper.locator('..')).toHaveCSS('border-style', 'solid')
  })
})
