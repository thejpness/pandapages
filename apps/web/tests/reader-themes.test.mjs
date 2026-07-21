import assert from 'node:assert/strict'
import test from 'node:test'
import { loadTypeScript } from './helpers/typescript-module.mjs'

const EXPECTED_IDS = ['clear', 'paper', 'warm', 'mist', 'night']

const CORE_TOKENS = [
  'background',
  'surface',
  'textPrimary',
  'textSecondary',
  'heading',
  'link',
  'focusRing',
  'selectionBackground',
  'selectionText',
  'progress',
  'divider',
  'controlBackground',
  'controlText',
]

const ALL_TOKENS = [
  ...CORE_TOKENS,
  'controlBorder',
  'selectedIndicator',
  'controlHover',
  'controlPressed',
  'disabledText',
  'mutedControlSurface',
  'alertBackground',
  'alertText',
  'alertBorder',
  'pageBorder',
  'overlay',
  'shadow',
]

const EXPECTED_PRESETS = {
  clear: {
    name: 'Clear',
    description: 'Bright page with the strongest contrast',
    helpText:
      'Best when you want the crispest text, especially in bright light.',
    polarity: 'positive',
    tokens: {
      background: '#FAFBFC',
      surface: '#FFFFFF',
      textPrimary: '#111111',
      textSecondary: '#414A55',
      heading: '#0B0F14',
      link: '#0B57D0',
      focusRing: '#8A4B00',
      selectionBackground: '#DCEBFF',
      selectionText: '#09111F',
      progress: '#0B57D0',
      divider: '#8A939D',
      controlBackground: '#FFFFFF',
      controlText: '#111111',
    },
  },
  paper: {
    name: 'Paper',
    description: 'Soft neutral page for everyday reading',
    helpText: 'A calm print-like page designed as the default starting point.',
    polarity: 'positive',
    tokens: {
      background: '#F5F1E8',
      surface: '#FBF8F2',
      textPrimary: '#1C1A17',
      textSecondary: '#5A554E',
      heading: '#141210',
      link: '#0B57D0',
      focusRing: '#8A5A00',
      selectionBackground: '#E7D8B7',
      selectionText: '#141210',
      progress: '#8A5A00',
      divider: '#938877',
      controlBackground: '#FBF8F2',
      controlText: '#1C1A17',
    },
  },
  warm: {
    name: 'Warm',
    description: 'Cream page with gentle warmth',
    helpText: 'A warmer page tone for readers who prefer a sepia-style feel.',
    polarity: 'positive',
    tokens: {
      background: '#F0E4D2',
      surface: '#F7ECDD',
      textPrimary: '#2C2318',
      textSecondary: '#675A46',
      heading: '#231B12',
      link: '#8A3F00',
      focusRing: '#8A5A00',
      selectionBackground: '#D7BA8E',
      selectionText: '#231B12',
      progress: '#9B5C00',
      divider: '#8C7555',
      controlBackground: '#F7ECDD',
      controlText: '#2C2318',
    },
  },
  mist: {
    name: 'Mist',
    description: 'Cool grey page with a softer feel',
    helpText:
      'A cooler light page for readers who dislike bright white or cream.',
    polarity: 'positive',
    tokens: {
      background: '#E7EDF0',
      surface: '#F4F7F9',
      textPrimary: '#172126',
      textSecondary: '#495B66',
      heading: '#0F171C',
      link: '#005A9C',
      focusRing: '#005A9C',
      selectionBackground: '#CFE2F3',
      selectionText: '#0F171C',
      progress: '#005A9C',
      divider: '#728490',
      controlBackground: '#F4F7F9',
      controlText: '#172126',
    },
  },
  night: {
    name: 'Night',
    description: 'Dark page for dim rooms',
    helpText: 'A low-light dark theme for bedtime or darker spaces.',
    polarity: 'negative',
    tokens: {
      background: '#121417',
      surface: '#1A1E23',
      textPrimary: '#EEF2F7',
      textSecondary: '#BAC3CF',
      heading: '#FFFFFF',
      link: '#8AB4F8',
      focusRing: '#F5C451',
      selectionBackground: '#26466F',
      selectionText: '#FFFFFF',
      progress: '#8AB4F8',
      divider: '#6B7480',
      controlBackground: '#1A1E23',
      controlText: '#EEF2F7',
    },
  },
}

const EXPECTED_REFERENCE_CONTRAST = {
  clear: {
    body: 18.23,
    secondary: 8.68,
    heading: 18.55,
    link: 6.16,
    focus: 6.57,
    progress: 6.16,
    divider: 3.01,
    control: 18.88,
    selection: 15.63,
  },
  paper: {
    body: 15.4,
    secondary: 6.55,
    heading: 16.58,
    link: 5.67,
    focus: 5.26,
    progress: 5.26,
    divider: 3.09,
    control: 16.38,
    selection: 13.26,
  },
  warm: {
    body: 12.3,
    secondary: 5.35,
    heading: 13.53,
    link: 5.99,
    focus: 4.72,
    progress: 4.26,
    divider: 3.49,
    control: 13.23,
    selection: 9.14,
  },
  mist: {
    body: 13.86,
    secondary: 5.98,
    heading: 15.32,
    link: 6.04,
    focus: 6.04,
    progress: 6.04,
    divider: 3.28,
    control: 15.23,
    selection: 13.65,
  },
  night: {
    body: 16.41,
    secondary: 10.36,
    heading: 18.45,
    link: 8.76,
    focus: 11.33,
    progress: 8.76,
    divider: 3.9,
    control: 14.9,
    selection: 9.6,
  },
}

async function themesModule() {
  return (
    await loadTypeScript('../src/lib/reader-themes.ts', import.meta.url)
  ).module
}

function relativeLuminance(hex) {
  const channels = [1, 3, 5].map((offset) =>
    Number.parseInt(hex.slice(offset, offset + 2), 16),
  )
  const linear = channels.map((channel) => {
    const srgb = channel / 255
    return srgb <= 0.04045
      ? srgb / 12.92
      : ((srgb + 0.055) / 1.055) ** 2.4
  })
  return 0.2126 * linear[0] + 0.7152 * linear[1] + 0.0722 * linear[2]
}

function contrast(foreground, background) {
  const foregroundLuminance = relativeLuminance(foreground)
  const backgroundLuminance = relativeLuminance(background)
  return (
    (Math.max(foregroundLuminance, backgroundLuminance) + 0.05) /
    (Math.min(foregroundLuminance, backgroundLuminance) + 0.05)
  )
}

function assertContrastAtLeast(themeId, purpose, foreground, backgrounds, floor) {
  for (const background of backgrounds) {
    assert.ok(
      contrast(foreground, background) >= floor,
      `${themeId} ${purpose} must be at least ${floor}:1 against ${background}; got ${contrast(foreground, background).toFixed(2)}:1`,
    )
  }
}

test('the registry contains exactly the five canonical presets with Paper as default', async () => {
  const themes = await themesModule()
  const ids = themes.READER_THEMES.map((theme) => theme.id)

  assert.deepEqual(themes.READER_THEME_IDS, EXPECTED_IDS)
  assert.deepEqual(ids, EXPECTED_IDS)
  assert.equal(new Set(ids).size, EXPECTED_IDS.length)
  assert.equal(themes.DEFAULT_READER_THEME_ID, 'paper')
  assert.equal(themes.readerTheme(undefined).id, 'paper')
  assert.equal(themes.readerTheme('removed-theme').id, 'paper')
  assert.equal(themes.isReaderThemeId('paper'), true)
  assert.equal(themes.isReaderThemeId('warm'), true)
  assert.equal(themes.isReaderThemeId('night'), true)
  assert.equal(themes.isReaderThemeId('sepia'), false)
  assert.equal(themes.isReaderThemeId(null), false)
})

test('every preset has the prescribed metadata, exact core colours, and complete finite hex tokens', async () => {
  const themes = await themesModule()
  const hex = /^#[0-9A-F]{6}$/

  for (const theme of themes.READER_THEMES) {
    const expected = EXPECTED_PRESETS[theme.id]
    assert.ok(expected)
    assert.equal(theme.name, expected.name)
    assert.equal(theme.description, expected.description)
    assert.equal(theme.helpText, expected.helpText)
    assert.equal(theme.polarity, expected.polarity)
    assert.deepEqual(
      Object.keys(theme.tokens).sort(),
      [...ALL_TOKENS].sort(),
    )
    assert.deepEqual(
      Object.fromEntries(CORE_TOKENS.map((key) => [key, theme.tokens[key]])),
      expected.tokens,
    )
    for (const token of ALL_TOKENS) {
      assert.match(theme.tokens[token], hex, `${theme.id}.${token}`)
    }
    assert.equal(theme.tokens.overlay, '#000000')
    assert.equal(theme.tokens.shadow, '#000000')
    assert.deepEqual(theme.preview, {
      heading: 'A little adventure',
      body: 'The little panda turned the page and found a path through the trees.',
      link: 'Keep reading',
    })
  }
})

test('the registry is immutable and lookup and CSS projection do not mutate inputs', async () => {
  const themes = await themesModule()
  assert.equal(Object.isFrozen(themes.READER_THEMES), true)
  assert.equal(Object.isFrozen(themes.READER_THEME_IDS), true)
  assert.throws(() => themes.READER_THEME_IDS.push('another-theme'))
  assert.deepEqual(themes.READER_THEME_IDS, EXPECTED_IDS)

  for (const theme of themes.READER_THEMES) {
    assert.equal(Object.isFrozen(theme), true)
    assert.equal(Object.isFrozen(theme.tokens), true)
    assert.equal(Object.isFrozen(theme.preview), true)
  }

  const input = Object.freeze({ theme: 'mist' })
  const paper = themes.readerTheme(input)
  const variables = themes.readerThemeCssVariables('mist')
  const previewVariables = themes.readerThemePreviewCssVariables('mist')
  assert.equal(paper.id, 'paper')
  assert.deepEqual(input, { theme: 'mist' })
  assert.equal(Object.isFrozen(variables), true)
  assert.equal(Object.isFrozen(previewVariables), true)
  assert.equal(variables['--reader-background'], '#E7EDF0')
  assert.equal(variables['--reader-color-scheme'], 'light')
  assert.equal(
    themes.readerThemeCssVariables('night')['--reader-color-scheme'],
    'dark',
  )
  assert.equal(previewVariables['--reader-preview-link'], '#005A9C')
})

test('prescribed reference contrast ratios agree with deterministic WCAG calculation', async () => {
  const themes = await themesModule()
  const tolerance = 0.015

  for (const theme of themes.READER_THEMES) {
    const token = theme.tokens
    const expected = EXPECTED_REFERENCE_CONTRAST[theme.id]
    const measured = {
      body: contrast(token.textPrimary, token.background),
      secondary: contrast(token.textSecondary, token.background),
      heading: contrast(token.heading, token.background),
      link: contrast(token.link, token.background),
      focus: contrast(token.focusRing, token.background),
      progress: contrast(token.progress, token.background),
      divider: contrast(token.divider, token.background),
      control: contrast(token.controlText, token.controlBackground),
      selection: contrast(token.selectionText, token.selectionBackground),
    }

    for (const [purpose, ratio] of Object.entries(measured)) {
      assert.ok(
        Math.abs(ratio - expected[purpose]) <= tolerance,
        `${theme.id} ${purpose} expected ${expected[purpose]}:1, got ${ratio.toFixed(4)}:1`,
      )
    }
  }
})

test('all text, controls, focus, progress, boundaries, and selection meet WCAG thresholds on actual surfaces', async () => {
  const themes = await themesModule()

  for (const theme of themes.READER_THEMES) {
    const token = theme.tokens
    const textSurfaces = [token.background, token.surface]
    const controlStates = [
      token.controlBackground,
      token.controlHover,
      token.controlPressed,
    ]
    assertContrastAtLeast(
      theme.id,
      'body text',
      token.textPrimary,
      textSurfaces,
      7,
    )
    for (const [purpose, colour] of [
      ['secondary text', token.textSecondary],
      ['heading', token.heading],
      ['underlined link', token.link],
    ]) {
      assertContrastAtLeast(theme.id, purpose, colour, textSurfaces, 4.5)
    }
    assertContrastAtLeast(
      theme.id,
      'control text',
      token.controlText,
      controlStates,
      4.5,
    )
    assertContrastAtLeast(
      theme.id,
      'disabled label',
      token.disabledText,
      [token.mutedControlSurface],
      4.5,
    )
    assertContrastAtLeast(
      theme.id,
      'focus ring',
      token.focusRing,
      [...textSurfaces, ...controlStates],
      3,
    )
    assertContrastAtLeast(
      theme.id,
      'progress',
      token.progress,
      [token.background, token.surface, token.mutedControlSurface],
      3,
    )
    for (const [purpose, colour] of [
      ['divider', token.divider],
      ['control boundary', token.controlBorder],
      ['page boundary', token.pageBorder],
    ]) {
      assertContrastAtLeast(theme.id, purpose, colour, textSurfaces, 3)
    }
    assertContrastAtLeast(
      theme.id,
      'selected card boundary',
      token.selectedIndicator,
      [...textSurfaces, ...controlStates],
      3,
    )
    assertContrastAtLeast(
      theme.id,
      'selection text',
      token.selectionText,
      [token.selectionBackground],
      4.5,
    )
    assertContrastAtLeast(
      theme.id,
      'alert text',
      token.alertText,
      [token.alertBackground],
      4.5,
    )
    assertContrastAtLeast(
      theme.id,
      'alert boundary',
      token.alertBorder,
      [token.alertBackground],
      3,
    )
  }
})
