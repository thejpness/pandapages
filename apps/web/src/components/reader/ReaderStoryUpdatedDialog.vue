<script setup lang="ts">
import { computed } from 'vue'
import type { CrossVersionMapping } from '../../lib/reader-cross-version-progress'
import ReaderDialogShell from './ReaderDialogShell.vue'

const props = defineProps<{
  open: boolean
  mapping: CrossVersionMapping
  busy: boolean
}>()

const emit = defineEmits<{
  continue: []
  start: []
  library: []
}>()

const description = computed(() => {
  if (props.mapping.confidence === 'high') {
    return 'We found the same reading place in this updated version.'
  }
  if (props.mapping.confidence === 'medium') {
    return 'We found the same chapter in this updated version.'
  }
  if (props.mapping.confidence === 'low') {
    return 'We found an approximate place based on your reading progress.'
  }
  return 'We could not safely find your previous reading place in this updated version.'
})

function onOpenChange(open: boolean) {
  if (!open && !props.busy) emit('library')
}
</script>

<template>
  <ReaderDialogShell
    :open="open"
    content-id="reader-story-updated-dialog"
    title="Story updated"
    :description="description"
    :show-close="false"
    @update:open="onOpenChange"
  >
    <p class="reader-dialog-supporting-copy">
      Starting this version moves your saved place to its beginning. Returning
      to the Library leaves your existing progress unchanged.
    </p>
    <div class="reader-dialog-actions reader-dialog-actions--stack">
      <button
        v-if="mapping.kind !== 'none'"
        class="reader-button reader-button--primary"
        type="button"
        :disabled="busy"
        @click="emit('continue')"
      >
        Continue in the updated story
      </button>
      <button
        class="reader-button reader-button--quiet"
        type="button"
        :disabled="busy"
        @click="emit('start')"
      >
        Start this version
      </button>
      <button
        class="reader-button reader-button--quiet"
        type="button"
        :disabled="busy"
        @click="emit('library')"
      >
        Return to Library
      </button>
    </div>
  </ReaderDialogShell>
</template>
