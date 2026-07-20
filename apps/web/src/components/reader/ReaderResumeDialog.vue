<script setup lang="ts">
import ReaderDialogShell from './ReaderDialogShell.vue'

defineProps<{
  open: boolean
  percent?: number
}>()

const emit = defineEmits<{
  'update:open': [open: boolean]
  resume: []
  startOver: []
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
    title="Continue reading?"
    :description="'You were about ' + Math.round((percent ?? 0) * 100) + '% through.'"
    :show-close="false"
    @update:open="onOpenChange"
  >
    <div class="reader-dialog-actions reader-dialog-actions--stack">
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
  </ReaderDialogShell>
</template>
