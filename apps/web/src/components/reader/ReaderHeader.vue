<script setup lang="ts">
defineProps<{
  title: string
  chapterTitle: string
  percent: number
  statusText: string
  retryKind: 'baseline' | 'save' | null
  retryDisabled: boolean
  chaptersAvailable: boolean
  readerReady: boolean
  settingsOpen: boolean
  chaptersOpen: boolean
  navigating: boolean
}>()

const emit = defineEmits<{
  library: []
  settings: []
  chapters: []
  retry: []
}>()
</script>

<template>
  <div
    class="reader-progress-track"
    role="progressbar"
    aria-label="Reading progress"
    aria-valuemin="0"
    aria-valuemax="100"
    :aria-valuenow="Math.round(percent * 100)"
  >
    <div
      class="reader-progress-value"
      :style="{ width: Math.round(percent * 100) + '%' }"
    />
  </div>
  <header class="reader-header" data-reader-header>
    <div class="reader-header-inner">
      <button
        class="reader-header-action reader-header-library"
        type="button"
        :disabled="navigating"
        @click="emit('library')"
      >
        <span aria-hidden="true">←</span>
        <span>Return to Library</span>
      </button>

      <div class="reader-header-context" aria-hidden="true">
        <span class="reader-header-title">{{ chapterTitle || title }}</span>
        <span class="reader-header-percent">{{ Math.round(percent * 100) }}%</span>
      </div>

      <div class="reader-header-controls">
        <div class="reader-save-status" role="status" aria-live="polite" aria-atomic="true">
          <span>{{ statusText }}</span>
          <button
            v-if="retryKind"
            class="reader-status-retry"
            type="button"
            :disabled="retryDisabled"
            @click="emit('retry')"
          >
            Retry
          </button>
        </div>

        <button
          v-if="chaptersAvailable"
          class="reader-header-action"
          type="button"
          aria-controls="reader-chapters-dialog"
          :aria-expanded="chaptersOpen"
          :disabled="!readerReady"
          @click="emit('chapters')"
        >
          Chapters
        </button>
        <button
          class="reader-header-action reader-settings-trigger"
          type="button"
          aria-label="Reading settings"
          aria-controls="reader-settings-dialog"
          :aria-expanded="settingsOpen"
          :disabled="!readerReady"
          @click="emit('settings')"
        >
          <span aria-hidden="true">Aa</span>
        </button>
      </div>
    </div>
  </header>
</template>
