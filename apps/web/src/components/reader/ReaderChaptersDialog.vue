<script setup lang="ts">
import {
  readerChapterAccessibleLabel,
  type ReaderChapter,
} from '../../lib/reader-chapters'
import ReaderDialogShell from './ReaderDialogShell.vue'

const props = defineProps<{
  open: boolean
  chapters: readonly ReaderChapter[]
  currentChapter: ReaderChapter | null
}>()

function chapterLabel(chapter: ReaderChapter): string {
  return readerChapterAccessibleLabel(props.chapters, chapter)
}

const emit = defineEmits<{
  'update:open': [open: boolean]
  select: [chapter: ReaderChapter]
}>()
</script>

<template>
  <ReaderDialogShell
    :open="open"
    content-id="reader-chapters-dialog"
    title="Chapters"
    description="Move to a chapter in this story."
    @update:open="emit('update:open', $event)"
  >
    <nav aria-label="Story chapters" class="reader-chapter-list">
      <button
        v-for="chapter in chapters"
        :key="chapter.key + '-' + chapter.occurrence"
        class="reader-chapter-button"
        type="button"
        :aria-label="chapterLabel(chapter)"
        :aria-current="
          currentChapter?.key === chapter.key &&
          currentChapter?.occurrence === chapter.occurrence
            ? 'location'
            : undefined
        "
        @click="emit('select', chapter)"
      >
        <span>{{ chapter.title }}</span>
        <span v-if="
          currentChapter?.key === chapter.key &&
          currentChapter?.occurrence === chapter.occurrence
        " class="reader-chapter-current">Current</span>
      </button>
    </nav>
  </ReaderDialogShell>
</template>
