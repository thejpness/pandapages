<script setup lang="ts">
import StoryStudioDialog from './StoryStudioDialog.vue'

defineProps<{
  open: boolean
  title: string
  version: number | null
  currentPublishedVersion: number | null
  busy: boolean
}>()

const emit = defineEmits<{ confirm: []; cancel: [] }>()
</script>

<template>
  <StoryStudioDialog
    :open="open"
    title="Publish this version?"
    :description="`${title} version ${version ?? ''} will become the story readers see.`"
    confirm-label="Publish version"
    :busy="busy"
    @confirm="emit('confirm')"
    @cancel="emit('cancel')"
  >
    <p v-if="currentPublishedVersion !== null">Version {{ currentPublishedVersion }} is currently published.</p>
    <p>Publishing makes this version the one readers see. Existing historical versions and reading progress are retained.</p>
  </StoryStudioDialog>
</template>
