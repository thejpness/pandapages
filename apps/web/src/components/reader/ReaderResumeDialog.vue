<script setup lang="ts">
import ReaderDialogShell from './ReaderDialogShell.vue'

const props = defineProps<{
  open: boolean
  kind: 'resume' | 'changed'
  percent?: number
}>()

const emit = defineEmits<{
  'update:open': [open: boolean]
  resume: []
  startOver: []
  library: []
  dismiss: []
}>()

function onOpenChange(open: boolean) {
  emit('update:open', open)
  if (!open) emit('dismiss')
}
</script>

<template>
  <ReaderDialogShell
    :open="open"
    content-id="reader-resume-dialog"
    :title="kind === 'resume' ? 'Continue reading?' : 'This story has changed'"
    :description="
      kind === 'resume'
        ? 'You were about ' + Math.round((percent ?? 0) * 100) + '% through.'
        : 'Start this version from the beginning.'
    "
    :show-close="false"
    @update:open="onOpenChange"
  >
    <div v-if="props.kind === 'resume'" class="reader-dialog-actions reader-dialog-actions--stack">
      <button class="reader-button reader-button--primary" type="button" @click="emit('resume')">
        Resume
      </button>
      <button class="reader-button reader-button--quiet" type="button" @click="emit('startOver')">
        Start over
      </button>
      <button class="reader-button reader-button--quiet" type="button" @click="emit('dismiss')">
        Dismiss
      </button>
    </div>
    <div v-else class="reader-dialog-actions reader-dialog-actions--stack">
      <button class="reader-button reader-button--primary" type="button" @click="emit('startOver')">
        Start this version
      </button>
      <button class="reader-button reader-button--quiet" type="button" @click="emit('library')">
        Return to Library
      </button>
    </div>
  </ReaderDialogShell>
</template>
