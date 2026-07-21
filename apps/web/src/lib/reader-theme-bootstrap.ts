import { loadReaderPreferencesV2 } from './reader-preferences-v2'
import {
  DEFAULT_READER_THEME_ID,
  readerTheme,
  readerThemeCssVariables,
  type ReaderThemeId,
} from './reader-themes'

const readerThemeVariableNames = Object.keys(
  readerThemeCssVariables(DEFAULT_READER_THEME_ID),
)

export function isReaderRoute(pathname: string): boolean {
  return /^\/read\/[^/]+\/?$/.test(pathname)
}

export function applyReaderTheme(
  value: unknown,
  root: HTMLElement = document.documentElement,
): ReaderThemeId {
  try {
    const theme = readerTheme(value)
    const variables = readerThemeCssVariables(theme.id)

    for (const [name, token] of Object.entries(variables)) {
      root.style.setProperty(name, token)
    }
    root.dataset.readerTheme = theme.id
    root.dataset.readerRouteTheme = 'true'

    return theme.id
  } finally {
    releaseReaderThemeBootMarker(root)
  }
}

export function releaseReaderThemeBootMarker(
  root: HTMLElement = document.documentElement,
): void {
  delete root.dataset.readerThemeBooting
}

export function clearReaderTheme(
  root: HTMLElement = document.documentElement,
): void {
  for (const name of readerThemeVariableNames) {
    root.style.removeProperty(name)
  }
  delete root.dataset.readerTheme
  delete root.dataset.readerRouteTheme
  releaseReaderThemeBootMarker(root)
}

export function bootstrapReaderTheme(
  pathname: string = window.location.pathname,
  root?: HTMLElement,
): ReaderThemeId | null {
  if (!isReaderRoute(pathname)) return null
  const targetRoot = root ?? document.documentElement
  try {
    return applyReaderTheme(loadReaderPreferencesV2().theme, targetRoot)
  } catch {
    // A stale storage implementation or a partially applicable palette must
    // not leave direct Reader navigation hidden. Paper is the one safe retry;
    // if it cannot be applied either, its error remains fatal and visible.
    return applyReaderTheme(DEFAULT_READER_THEME_ID, targetRoot)
  } finally {
    releaseReaderThemeBootMarker(targetRoot)
  }
}
