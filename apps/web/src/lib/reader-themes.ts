export type ReaderThemePolarity = 'positive' | 'negative'

export type ReaderThemeTokens = {
  background: string
  surface: string
  textPrimary: string
  textSecondary: string
  heading: string
  link: string
  focusRing: string
  selectionBackground: string
  selectionText: string
  progress: string
  divider: string
  controlBackground: string
  controlText: string
  controlBorder: string
  selectedIndicator: string
  controlHover: string
  controlPressed: string
  disabledText: string
  mutedControlSurface: string
  alertBackground: string
  alertText: string
  alertBorder: string
  pageBorder: string
  overlay: string
  shadow: string
}

type ReaderThemeDefinition<ThemeId extends string> = {
  id: ThemeId
  name: string
  description: string
  helpText: string
  polarity: ReaderThemePolarity
  tokens: Readonly<Omit<ReaderThemeTokens, 'overlay' | 'shadow'>>
}

export type ReaderThemeCssVariables = Readonly<Record<string, string>>

const preview = Object.freeze({
  heading: 'A little adventure',
  body: 'The little panda turned the page and found a path through the trees.',
  link: 'Keep reading',
})

function preset<const ThemeId extends string>(
  definition: ReaderThemeDefinition<ThemeId>,
) {
  return Object.freeze({
    ...definition,
    tokens: Object.freeze({
      ...definition.tokens,
      // Neutral overlay and shadow effects never introduce a theme hue.
      overlay: '#000000',
      shadow: '#000000',
    }),
    preview,
  })
}

export const READER_THEMES = Object.freeze([
  preset({
    id: 'clear',
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
      controlBorder: '#8A939D',
      selectedIndicator: '#0B0F14',
      controlHover: '#DCEBFF',
      controlPressed: '#DCEBFF',
      disabledText: '#414A55',
      mutedControlSurface: '#FAFBFC',
      alertBackground: '#DCEBFF',
      alertText: '#0B0F14',
      alertBorder: '#8A4B00',
      pageBorder: '#8A939D',
    },
  }),
  preset({
    id: 'paper',
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
      controlBorder: '#938877',
      selectedIndicator: '#141210',
      controlHover: '#E7D8B7',
      controlPressed: '#E7D8B7',
      disabledText: '#5A554E',
      mutedControlSurface: '#F5F1E8',
      alertBackground: '#E7D8B7',
      alertText: '#141210',
      alertBorder: '#8A5A00',
      pageBorder: '#938877',
    },
  }),
  preset({
    id: 'warm',
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
      controlBorder: '#8C7555',
      selectedIndicator: '#231B12',
      controlHover: '#D7BA8E',
      controlPressed: '#D7BA8E',
      disabledText: '#675A46',
      mutedControlSurface: '#F0E4D2',
      alertBackground: '#D7BA8E',
      alertText: '#231B12',
      alertBorder: '#8A5A00',
      pageBorder: '#8C7555',
    },
  }),
  preset({
    id: 'mist',
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
      controlBorder: '#728490',
      selectedIndicator: '#0F171C',
      controlHover: '#CFE2F3',
      controlPressed: '#CFE2F3',
      disabledText: '#495B66',
      mutedControlSurface: '#E7EDF0',
      alertBackground: '#CFE2F3',
      alertText: '#0F171C',
      alertBorder: '#005A9C',
      pageBorder: '#728490',
    },
  }),
  preset({
    id: 'night',
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
      controlBorder: '#6B7480',
      selectedIndicator: '#FFFFFF',
      controlHover: '#26466F',
      controlPressed: '#26466F',
      disabledText: '#BAC3CF',
      mutedControlSurface: '#121417',
      alertBackground: '#26466F',
      alertText: '#FFFFFF',
      alertBorder: '#F5C451',
      pageBorder: '#6B7480',
    },
  }),
])

export type ReaderThemeId = (typeof READER_THEMES)[number]['id']
export type ReaderThemePreset = (typeof READER_THEMES)[number]

export const READER_THEME_IDS: readonly ReaderThemeId[] = Object.freeze(
  READER_THEMES.map(({ id }) => id),
)

export const DEFAULT_READER_THEME_ID: ReaderThemeId = 'paper'

const themesById = new Map(
  READER_THEMES.map((theme) => [theme.id, theme] as const),
)
const readerThemeIds = new Set<string>(READER_THEME_IDS)

function requireReaderTheme(id: ReaderThemeId): ReaderThemePreset {
  const theme = themesById.get(id)
  if (theme) return theme
  throw new Error('Reader theme "' + id + '" is missing from the registry.')
}

const defaultReaderTheme = requireReaderTheme(DEFAULT_READER_THEME_ID)

export function isReaderThemeId(value: unknown): value is ReaderThemeId {
  return typeof value === 'string' && readerThemeIds.has(value)
}

export function readerTheme(value: unknown): Readonly<ReaderThemePreset> {
  if (!isReaderThemeId(value)) return defaultReaderTheme
  return themesById.get(value) ?? defaultReaderTheme
}

export function readerThemeCssVariables(
  value: unknown,
): ReaderThemeCssVariables {
  const theme = readerTheme(value)
  const { tokens } = theme
  return Object.freeze({
    '--reader-background': tokens.background,
    '--reader-surface': tokens.surface,
    '--reader-text-primary': tokens.textPrimary,
    '--reader-text-secondary': tokens.textSecondary,
    '--reader-heading': tokens.heading,
    '--reader-link': tokens.link,
    '--reader-focus-ring': tokens.focusRing,
    '--reader-selection-background': tokens.selectionBackground,
    '--reader-selection-text': tokens.selectionText,
    '--reader-progress': tokens.progress,
    '--reader-divider': tokens.divider,
    '--reader-control-background': tokens.controlBackground,
    '--reader-control-text': tokens.controlText,
    '--reader-control-border': tokens.controlBorder,
    '--reader-selected-indicator': tokens.selectedIndicator,
    '--reader-control-hover': tokens.controlHover,
    '--reader-control-pressed': tokens.controlPressed,
    '--reader-disabled-text': tokens.disabledText,
    '--reader-muted-control-surface': tokens.mutedControlSurface,
    '--reader-alert-background': tokens.alertBackground,
    '--reader-alert-text': tokens.alertText,
    '--reader-alert-border': tokens.alertBorder,
    '--reader-page-border': tokens.pageBorder,
    '--reader-overlay': tokens.overlay,
    '--reader-shadow': tokens.shadow,
    '--reader-color-scheme': theme.polarity === 'negative' ? 'dark' : 'light',
  })
}

export function readerThemePreviewCssVariables(
  value: unknown,
): ReaderThemeCssVariables {
  const { tokens } = readerTheme(value)
  return Object.freeze({
    '--reader-preview-background': tokens.background,
    '--reader-preview-surface': tokens.surface,
    '--reader-preview-text': tokens.textPrimary,
    '--reader-preview-heading': tokens.heading,
    '--reader-preview-link': tokens.link,
    '--reader-preview-divider': tokens.divider,
  })
}
