<script setup lang="ts">
import { computed, ref } from 'vue'
import ReaderDialogShell from './ReaderDialogShell.vue'
import {
  READER_THEMES,
  readerTheme,
  readerThemePreviewCssVariables,
  type ReaderThemeId,
} from '../../lib/reader-themes'
import {
  READER_PREFERENCE_LIMITS,
  type ReaderFontFamily,
  type ReaderMode,
  type ReaderPreferencesV2,
} from '../../lib/reader-preferences-v2'

const props = defineProps<{
  open: boolean
  modelValue: ReaderPreferencesV2
}>()

const emit = defineEmits<{
  'update:open': [open: boolean]
  'update:modelValue': [preferences: ReaderPreferencesV2]
  reset: []
  modeChange: [mode: ReaderMode]
}>()

const pageStyleExpanded = ref(false)
const currentTheme = computed(() => readerTheme(props.modelValue.theme))

function update(patch: Partial<ReaderPreferencesV2>) {
  emit('update:modelValue', { ...props.modelValue, ...patch, schema: 2 })
}

function numberFrom(event: Event): number {
  return Number((event.target as HTMLInputElement).value)
}

function setMode(mode: ReaderMode) {
  emit('modeChange', mode)
}

function setTheme(theme: ReaderThemeId) {
  update({ theme })
}

function setFont(fontFamily: ReaderFontFamily) {
  update({ fontFamily })
}
</script>

<template>
  <ReaderDialogShell
    :open="open"
    content-id="reader-settings-dialog"
    title="Reading settings"
    description="Make the page comfortable to read."
    wide
    @update:open="emit('update:open', $event)"
  >
    <div class="reader-settings-sections">
      <section class="reader-settings-disclosure">
        <button
          id="reader-page-style-disclosure"
          class="reader-settings-disclosure__trigger"
          type="button"
          :aria-expanded="pageStyleExpanded"
          aria-controls="reader-theme-options"
          @click="pageStyleExpanded = !pageStyleExpanded"
        >
          <span class="reader-settings-disclosure__copy">
            <span class="reader-settings-disclosure__label">Page style</span>
            <span class="reader-settings-disclosure__current">
              {{ currentTheme.name }} selected
            </span>
            <span class="reader-settings-disclosure__help">
              Change page colours
            </span>
          </span>
          <svg
            class="reader-settings-disclosure__indicator"
            viewBox="0 0 16 16"
            aria-hidden="true"
            focusable="false"
          >
            <path d="m3.5 6 4.5 4 4.5-4" />
          </svg>
        </button>

        <div
          v-show="pageStyleExpanded"
          id="reader-theme-options"
          class="reader-settings-disclosure__panel"
          role="region"
          aria-labelledby="reader-page-style-disclosure"
        >
          <fieldset
            class="reader-settings-fieldset reader-theme-picker"
            aria-describedby="reader-theme-help"
          >
            <legend class="reader-sr-only">Page style</legend>
            <p id="reader-theme-help" class="reader-settings-help">
              Choose the page colours that feel best. You can change this at any time.
            </p>
            <div class="reader-theme-grid">
              <label
                v-for="theme in READER_THEMES"
                :key="theme.id"
                class="reader-theme-card"
                :style="readerThemePreviewCssVariables(theme.id)"
              >
                <input
                  type="radio"
                  name="reader-theme"
                  :value="theme.id"
                  :checked="modelValue.theme === theme.id"
                  :aria-labelledby="`reader-theme-${theme.id}-name`"
                  :aria-describedby="`reader-theme-${theme.id}-description reader-theme-${theme.id}-help`"
                  @change="setTheme(theme.id)"
                >
                <span class="reader-theme-card__heading">
                  <strong :id="`reader-theme-${theme.id}-name`">{{ theme.name }}</strong>
                  <span class="reader-theme-card__selected" aria-hidden="true">
                    <span>✓</span>
                    Selected
                  </span>
                </span>
                <span
                  :id="`reader-theme-${theme.id}-description`"
                  class="reader-theme-card__description"
                >
                  {{ theme.description }}
                </span>
                <span
                  :id="`reader-theme-${theme.id}-help`"
                  class="reader-sr-only"
                >
                  {{ theme.helpText }}
                </span>
                <span class="reader-theme-preview" aria-hidden="true">
                  <span class="reader-theme-preview__page">
                    <strong>{{ theme.preview.heading }}</strong>
                    <span>{{ theme.preview.body }}</span>
                    <span class="reader-theme-preview__link">{{ theme.preview.link }}</span>
                  </span>
                </span>
              </label>
            </div>
          </fieldset>
        </div>
      </section>

      <section class="reader-text-settings" aria-labelledby="reader-text-settings-title">
        <div>
          <h2 id="reader-text-settings-title">Text</h2>
          <p class="reader-settings-help">
            Colour choices stay separate from font and spacing.
          </p>
        </div>

        <fieldset class="reader-settings-fieldset">
          <legend>Font</legend>
          <div class="reader-choice-row">
            <label v-for="option in [
              { value: 'book', label: 'Book' },
              { value: 'clear', label: 'Clear' },
              { value: 'system', label: 'System' },
            ]" :key="option.value" class="reader-choice">
              <input
                type="radio"
                name="reader-font"
                :value="option.value"
                :checked="modelValue.fontFamily === option.value"
                @change="setFont(option.value as ReaderFontFamily)"
              >
              <span>{{ option.label }}</span>
            </label>
          </div>
        </fieldset>

        <div class="reader-text-settings__grid">
          <label class="reader-range">
            <span>Text size</span>
            <output>{{ modelValue.fontSize }} px</output>
            <input
              aria-label="Text size"
              type="range"
              :min="READER_PREFERENCE_LIMITS.fontSize.min"
              :max="READER_PREFERENCE_LIMITS.fontSize.max"
              step="1"
              :value="modelValue.fontSize"
              @input="update({ fontSize: numberFrom($event) })"
            >
          </label>

          <label class="reader-range">
            <span>Line height</span>
            <output>{{ modelValue.lineHeight.toFixed(2) }}</output>
            <input
              aria-label="Line height"
              type="range"
              :min="READER_PREFERENCE_LIMITS.lineHeight.min"
              :max="READER_PREFERENCE_LIMITS.lineHeight.max"
              step="0.05"
              :value="modelValue.lineHeight"
              @input="update({ lineHeight: numberFrom($event) })"
            >
          </label>

          <label class="reader-range">
            <span>Content width</span>
            <output>{{ modelValue.contentWidth }} px</output>
            <input
              aria-label="Content width"
              type="range"
              :min="READER_PREFERENCE_LIMITS.contentWidth.min"
              :max="READER_PREFERENCE_LIMITS.contentWidth.max"
              step="20"
              :value="modelValue.contentWidth"
              @input="update({ contentWidth: numberFrom($event) })"
            >
          </label>
        </div>
      </section>

      <fieldset class="reader-settings-fieldset">
        <legend>Reading mode</legend>
        <div class="reader-choice-row">
          <label v-for="mode in ['scroll', 'paged'] as const" :key="mode" class="reader-choice">
            <input
              type="radio"
              name="reader-mode"
              :value="mode"
              :checked="modelValue.mode === mode"
              @change="setMode(mode)"
            >
            <span>{{ mode === 'scroll' ? 'Scroll' : 'Paged' }}</span>
          </label>
        </div>
      </fieldset>
    </div>

    <div class="reader-dialog-actions reader-dialog-actions--settings">
      <button class="reader-button reader-button--quiet" type="button" @click="emit('reset')">
        Reset to Defaults
      </button>
      <button
        class="reader-button reader-button--primary"
        type="button"
        @click="emit('update:open', false)"
      >
        Done
      </button>
    </div>
  </ReaderDialogShell>
</template>
