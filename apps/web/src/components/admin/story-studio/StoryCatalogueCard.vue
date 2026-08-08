<script setup lang="ts">
import { computed } from 'vue'
import type { AdminStoryListItem } from '@/lib/api'
import { editionStartedCount, sourceStatusLabel, storyStatusLabel } from '@/lib/story-studio-navigation'
import StoryStatusBadge from './StoryStatusBadge.vue'

const props = defineProps<{ story: AdminStoryListItem }>()
const emit = defineEmits<{ open: [slug: string] }>()
const started = computed(() => editionStartedCount(props.story.editions))
const releaseFact = computed(() => {
  const release = props.story.currentRelease
  if (release) {
    const count = release.editions.length
    return `Release ${release.release} · ${count} ${count === 1 ? 'edition' : 'editions'}`
  }
  if (props.story.releaseCount > 0) return `Not published · ${props.story.releaseCount} historical ${props.story.releaseCount === 1 ? 'release' : 'releases'}`
  return 'Not published'
})
</script>

<template>
  <article class="story-card">
    <div class="story-card__top">
      <div>
        <p class="story-card__slug">{{ story.slug }}</p>
        <h2>{{ story.title }}</h2>
        <p>{{ story.author || 'Author not recorded' }}</p>
      </div>
      <StoryStatusBadge :status="story.status" />
    </div>
    <dl>
      <div><dt>Source</dt><dd>{{ sourceStatusLabel(story.source.status) }}</dd></div>
      <div><dt>Editions</dt><dd>{{ started }}/5 started</dd></div>
      <div><dt>Publication</dt><dd>{{ releaseFact }}</dd></div>
      <div><dt>Status</dt><dd>{{ storyStatusLabel(story.status) }}</dd></div>
    </dl>
    <button type="button" class="studio-button studio-button--quiet" @click="emit('open', story.slug)">Review story</button>
  </article>
</template>

<style scoped>
.story-card{display:grid;gap:1rem;border:1px solid var(--studio-line);border-radius:var(--panda-radius-card);background:var(--studio-card);padding:1rem;box-shadow:var(--studio-shadow-soft)}.story-card__top{display:flex;align-items:flex-start;justify-content:space-between;gap:1rem}.story-card__slug{overflow-wrap:anywhere;color:var(--studio-muted);font-size:.72rem;font-weight:700;letter-spacing:.04em}.story-card h2{margin-top:.2rem;font-family:var(--panda-serif);font-size:1.2rem;font-weight:650}.story-card__top p:last-child{margin-top:.2rem;color:var(--studio-muted);font-size:.82rem}.story-card dl{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:.65rem}.story-card dt{color:var(--studio-muted);font-size:.68rem;text-transform:uppercase;letter-spacing:.05em}.story-card dd{margin-top:.15rem;font-size:.84rem;font-weight:650}.story-card>.studio-button{justify-self:start}@media(max-width:520px){.story-card__top{align-items:stretch;flex-direction:column}.story-card dl{grid-template-columns:1fr}}
</style>
