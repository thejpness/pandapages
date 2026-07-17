<script setup lang="ts">
import ReaderDialogShell from './ReaderDialogShell.vue'
import {
  READER_PREFERENCE_LIMITS,
  type ReaderFontFamily,
  type ReaderMode,
  type ReaderPreferencesV2,
  type ReaderTheme,
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

function update(patch: Partial<ReaderPreferencesV2>) {
  emit('update:modelValue', { ...props.modelValue, ...patch, schema: 2 })
}

function numberFrom(event: Event): number {
  return Number((event.target as HTMLInputElement).value)
}

function setMode(mode: ReaderMode) {
  emit('modeChange', mode)
}

function setTheme(theme: ReaderTheme) {
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
    <div class="reader-settings-grid">
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

      <fieldset class="reader-settings-fieldset">
        <legend>Theme</legend>
        <div class="reader-choice-row">
          <label v-for="theme in ['night', 'warm'] as const" :key="theme" class="reader-choice">
            <input
              type="radio"
              name="reader-theme"
              :value="theme"
              :checked="modelValue.theme === theme"
              @change="setTheme(theme)"
            >
            <span>{{ theme === 'night' ? 'Night' : 'Warm' }}</span>
          </label>
        </div>
      </fieldset>

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

    <div class="reader-dialog-actions">
      <button class="reader-button reader-button--quiet" type="button" @click="emit('reset')">
        Reset to Defaults
      </button>
    </div>
  </ReaderDialogShell>
</template>
