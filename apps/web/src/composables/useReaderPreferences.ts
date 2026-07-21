import { computed, ref, watch } from 'vue'
import {
  loadReaderPreferencesV2,
  resetReaderPreferencesV2,
  saveReaderPreferencesV2,
  type ReaderFontFamily,
  type ReaderMode,
  type ReaderPreferencesV2,
} from '../lib/reader-preferences-v2'
import type { ReaderThemeId } from '../lib/reader-themes'

const fontStacks: Record<ReaderFontFamily, string> = {
  book: '"Literata Variable", Georgia, serif',
  clear: '"Atkinson Hyperlegible Next Variable", Arial, sans-serif',
  system: 'ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
}

export function useReaderPreferences() {
  const preferences = ref<ReaderPreferencesV2>(loadReaderPreferencesV2())

  watch(
    preferences,
    (value) => {
      saveReaderPreferencesV2(value)
    },
    { deep: true },
  )

  const fontStack = computed(() => fontStacks[preferences.value.fontFamily])

  function setMode(mode: ReaderMode) {
    preferences.value.mode = mode
  }

  function setTheme(theme: ReaderThemeId) {
    preferences.value.theme = theme
  }

  function setFontFamily(fontFamily: ReaderFontFamily) {
    preferences.value.fontFamily = fontFamily
  }

  function reset(): ReaderPreferencesV2 {
    preferences.value = resetReaderPreferencesV2()
    return { ...preferences.value }
  }

  return {
    preferences,
    fontStack,
    setMode,
    setTheme,
    setFontFamily,
    reset,
  }
}
