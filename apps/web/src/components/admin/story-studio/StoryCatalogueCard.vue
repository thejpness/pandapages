<script setup lang="ts">
import type { AdminStoryListItem } from '@/lib/api'
import { storyRightsSummary } from '@/lib/story-studio-navigation'
import StoryStatusBadge from './StoryStatusBadge.vue'

defineProps<{ story: AdminStoryListItem }>()
const emit = defineEmits<{ open: [slug: string] }>()

const dateFormatter = new Intl.DateTimeFormat('en-GB', {
  day: 'numeric',
  month: 'short',
  year: 'numeric',
})
</script>

<template>
  <article class="story-card">
    <div class="story-card__topline">
      <StoryStatusBadge :status="story.status" />
      <time :datetime="story.updatedAt">Updated {{ dateFormatter.format(new Date(story.updatedAt)) }}</time>
    </div>
    <h2>{{ story.title }}</h2>
    <p v-if="story.author" class="story-card__author">by {{ story.author }}</p>
    <code>{{ story.slug }}</code>

    <dl class="story-card__facts">
      <div>
        <dt>Published</dt>
        <dd>{{ story.publishedVersion ? `v${story.publishedVersion.version}` : '—' }}</dd>
      </div>
      <div>
        <dt>Draft</dt>
        <dd>{{ story.draftVersion ? `v${story.draftVersion.version}` : '—' }}</dd>
      </div>
      <div>
        <dt>Versions</dt>
        <dd>{{ story.versionCount }}</dd>
      </div>
    </dl>

    <p class="story-card__meta">
      {{ story.language }} <span aria-hidden="true">·</span>
      {{ storyRightsSummary(story.rights) }}
    </p>

    <button type="button" class="story-card__open" @click="emit('open', story.slug)">
      {{ story.status === 'repair_required' ? 'Review story' : 'Manage story' }}
      <span aria-hidden="true">→</span>
    </button>
  </article>
</template>

<style scoped>
.story-card {
  display: flex;
  min-width: 0;
  flex-direction: column;
  border: 1px solid var(--studio-line);
  border-radius: 1.15rem;
  background: var(--studio-card);
  padding: 1.2rem;
  box-shadow: var(--studio-shadow-soft);
}

.story-card__topline {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
}

.story-card__topline time {
  color: var(--studio-muted);
  font-size: 0.75rem;
}

.story-card h2 {
  overflow-wrap: anywhere;
  margin-top: 1rem;
  color: var(--studio-ink);
  font-family: 'Literata Variable', Georgia, serif;
  font-size: 1.28rem;
  font-weight: 650;
  line-height: 1.25;
}

.story-card__author {
  margin-top: 0.25rem;
  color: var(--studio-muted);
  font-size: 0.9rem;
}

.story-card code {
  overflow-wrap: anywhere;
  margin-top: 0.7rem;
  color: #607174;
  font-size: 0.75rem;
}

.story-card__facts {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  margin-top: 1.1rem;
  border-block: 1px solid var(--studio-line);
  padding-block: 0.8rem;
}

.story-card__facts div + div {
  border-left: 1px solid var(--studio-line);
  padding-left: 0.8rem;
}

.story-card__facts dt {
  color: var(--studio-muted);
  font-size: 0.7rem;
  text-transform: uppercase;
  letter-spacing: 0.06em;
}

.story-card__facts dd {
  margin-top: 0.2rem;
  color: var(--studio-ink);
  font-weight: 720;
}

.story-card__meta {
  margin-top: 0.8rem;
  color: var(--studio-muted);
  font-size: 0.8rem;
}

.story-card__open {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 2.75rem;
  margin-top: auto;
  padding-top: 1rem;
  color: var(--studio-green);
  font-weight: 720;
}

.story-card__open:hover { color: var(--studio-green-dark); }
</style>
