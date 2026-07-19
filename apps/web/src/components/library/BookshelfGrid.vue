<script setup lang="ts">
import type { LibraryStory } from '../../lib/library-read-model'
import BookshelfCard from './BookshelfCard.vue'

defineProps<{ stories: LibraryStory[] }>()
const emit = defineEmits<{ details: [story: LibraryStory] }>()
</script>

<template>
  <section class="bookshelf" aria-labelledby="bookshelf-heading">
    <div class="bookshelf__heading">
      <div>
        <p>Your bookshelf</p>
        <h2 id="bookshelf-heading">Choose tonight’s story</h2>
      </div>
      <span aria-hidden="true">Turn a page, begin an adventure.</span>
    </div>

    <div class="bookshelf__grid">
      <BookshelfCard
        v-for="story in stories"
        :key="story.slug"
        :story="story"
        @details="emit('details', $event)"
      />
    </div>
  </section>
</template>

<style scoped>
.bookshelf {
  min-width: 0;
}

.bookshelf__heading {
  display: flex;
  align-items: end;
  justify-content: space-between;
  gap: 1rem;
  margin-bottom: 1.3rem;
}

.bookshelf__heading p {
  margin: 0 0 0.35rem;
  color: var(--library-muted);
  font-size: 0.7rem;
  font-weight: 900;
  letter-spacing: 0.14em;
  text-transform: uppercase;
}

.bookshelf__heading h2 {
  margin: 0;
  font-family: var(--library-serif);
  font-size: clamp(1.8rem, 4vw, 2.75rem);
  font-weight: 630;
  letter-spacing: -0.045em;
  line-height: 1.05;
  text-wrap: balance;
}

.bookshelf__heading > span {
  color: var(--library-muted);
  font-family: var(--library-serif);
  font-size: 0.86rem;
  font-style: italic;
  text-align: right;
}

.bookshelf__grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: clamp(0.85rem, 2.2vw, 1.4rem);
}

@media (min-width: 75rem) {
  .bookshelf__grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}

@media (max-width: 45rem) {
  .bookshelf__grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 32rem) {
  .bookshelf__heading > span {
    display: none;
  }
}
</style>
