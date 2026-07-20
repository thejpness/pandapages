<script setup lang="ts">
import type { AdminStoryListItem } from '@/lib/api'
import StoryCatalogueCard from './StoryCatalogueCard.vue'

defineProps<{ stories: readonly AdminStoryListItem[] }>()
const emit = defineEmits<{ open: [slug: string] }>()
</script>

<template>
  <section class="story-catalogue" aria-label="Story catalogue">
    <StoryCatalogueCard
      v-for="story in stories"
      :key="story.slug"
      :story="story"
      @open="emit('open', $event)"
    />
  </section>
</template>

<style scoped>
.story-catalogue {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 18rem), 1fr));
  gap: 1rem;
}
</style>
