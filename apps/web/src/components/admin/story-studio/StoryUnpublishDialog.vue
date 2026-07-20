<script setup lang="ts">
import StoryStudioDialog from './StoryStudioDialog.vue'

defineProps<{
  open: boolean
  title: string
  busy: boolean
}>()

const emit = defineEmits<{ confirm: []; cancel: [] }>()
</script>

<template>
  <StoryStudioDialog
    :open="open"
    title="Unpublish this story?"
    :description="`${title} will disappear from the Library and Reader.`"
    confirm-label="Unpublish story"
    :busy="busy"
    danger
    @confirm="emit('confirm')"
    @cancel="emit('cancel')"
  >
    <p>Drafts, immutable versions and historical reading progress remain. This does not delete the story.</p>
  </StoryStudioDialog>
</template>
