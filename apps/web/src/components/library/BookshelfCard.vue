<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import {
  classifyLibraryProgress,
  libraryActionLabel,
  libraryChapterLabel,
  libraryCoverPresentation,
  libraryDisplayPercent,
  libraryLengthLabel,
  libraryProgressLabel,
  type LibraryStory,
} from '../../lib/library-read-model'

const props = defineProps<{ story: LibraryStory }>()
const emit = defineEmits<{ details: [story: LibraryStory] }>()

const cover = computed(() => libraryCoverPresentation(props.story))
const progressKind = computed(() => classifyLibraryProgress(props.story))
const percent = computed(() => libraryDisplayPercent(props.story))
const progressLabel = computed(() => libraryProgressLabel(props.story))
const actionLabel = computed(() => libraryActionLabel(props.story))
const lengthLabel = computed(() => libraryLengthLabel(props.story.wordCount))
const chapterLabel = computed(() => libraryChapterLabel(props.story.chapterCount))
const titleId = computed(() => `bookshelf-card-title-${props.story.slug}`)
</script>

<template>
  <article class="bookshelf-card" :aria-labelledby="titleId">
    <div
      class="bookshelf-card__read"
    >
      <span
        class="story-cover"
        :class="`story-cover--${cover.pattern}`"
        :style="{
          '--cover-background': cover.background,
          '--cover-accent': cover.accent,
          '--cover-ink': cover.ink,
        }"
        aria-hidden="true"
      >
        <i class="story-cover__spine"></i>
        <i class="story-cover__pattern"></i>
        <span class="story-cover__label">
          <small>Panda Pages</small>
          <strong>{{ cover.initials }}</strong>
        </span>
        <span class="story-cover__corner">✦</span>
      </span>

      <span class="bookshelf-card__copy">
        <span class="bookshelf-card__kicker">Story</span>
        <h3 :id="titleId" class="bookshelf-card__title">{{ story.title }}</h3>
        <span v-if="story.author" class="bookshelf-card__author">by {{ story.author }}</span>
        <span class="bookshelf-card__facts">
          <span>{{ lengthLabel }}</span>
          <span v-if="chapterLabel">{{ chapterLabel }}</span>
        </span>

        <span class="bookshelf-card__progress-state" :data-kind="progressKind">
          {{ progressLabel }}
        </span>
        <span
          v-if="progressKind === 'in-progress' || progressKind === 'completed'"
          class="bookshelf-card__progress"
          role="progressbar"
          :aria-label="`Reading progress for ${story.title}`"
          aria-valuemin="0"
          aria-valuemax="100"
          :aria-valuenow="percent"
        >
          <i :style="{ width: `${percent}%` }"></i>
        </span>
      </span>
    </div>

    <div class="bookshelf-card__footer">
      <RouterLink
        class="bookshelf-card__action"
        :to="`/read/${encodeURIComponent(story.slug)}`"
        :aria-label="`${actionLabel}: ${story.title}`"
      >
        {{ actionLabel }}
        <span aria-hidden="true">→</span>
      </RouterLink>
      <button
        class="bookshelf-card__details"
        type="button"
        :aria-label="`Details for ${story.title}`"
        @click="emit('details', story)"
      >
        Details
      </button>
    </div>
  </article>
</template>

<style scoped>
.bookshelf-card {
  display: grid;
  min-width: 0;
  overflow: hidden;
  border: 1px solid var(--library-line-strong);
  border-radius: 1.35rem;
  background: var(--library-white);
  box-shadow: 0 0.45rem 1.4rem rgba(17, 17, 15, 0.06);
}

.bookshelf-card__read {
  display: grid;
  grid-template-columns: minmax(6.2rem, 0.78fr) minmax(0, 1.15fr);
  gap: clamp(0.9rem, 3vw, 1.25rem);
  min-width: 0;
  padding: 1rem 1rem 0.9rem;
  color: inherit;
  text-decoration: none;
}

.story-cover {
  position: relative;
  display: grid;
  width: 100%;
  min-width: 0;
  aspect-ratio: 0.72;
  place-items: center;
  overflow: hidden;
  border: 1px solid rgba(17, 17, 15, 0.34);
  border-radius: 0.45rem 0.85rem 0.85rem 0.45rem;
  background: var(--cover-background);
  color: var(--cover-ink);
  box-shadow: 0.35rem 0.45rem 0 rgba(17, 17, 15, 0.16);
}

.story-cover__spine {
  position: absolute;
  inset: 0 auto 0 0;
  width: 11%;
  border-right: 1px solid rgba(17, 17, 15, 0.25);
  background: color-mix(in srgb, var(--cover-ink) 12%, transparent);
}

.story-cover__pattern {
  position: absolute;
  inset: 0;
  opacity: 0.66;
}

.story-cover--dots .story-cover__pattern {
  background: radial-gradient(circle, var(--cover-accent) 0 0.22rem, transparent 0.25rem);
  background-size: 1.4rem 1.4rem;
}

.story-cover--arches .story-cover__pattern {
  background:
    radial-gradient(circle at 50% 100%, transparent 0 30%, var(--cover-accent) 31% 35%, transparent 36%) 0 0 / 3rem 3rem;
}

.story-cover--rays .story-cover__pattern {
  inset: -35%;
  background: repeating-conic-gradient(from 8deg, transparent 0 13deg, var(--cover-accent) 14deg 25deg);
}

.story-cover--checks .story-cover__pattern {
  background:
    linear-gradient(45deg, var(--cover-accent) 25%, transparent 25% 75%, var(--cover-accent) 75%) 0 0 / 2rem 2rem,
    linear-gradient(45deg, var(--cover-accent) 25%, transparent 25% 75%, var(--cover-accent) 75%) 1rem 1rem / 2rem 2rem;
}

.story-cover__label {
  position: relative;
  z-index: 1;
  display: grid;
  width: 66%;
  min-height: 38%;
  place-items: center;
  border: 1px solid currentColor;
  padding: 0.65rem 0.4rem;
  background: color-mix(in srgb, var(--cover-background) 86%, white);
  text-align: center;
}

.story-cover__label small {
  font-size: clamp(0.42rem, 1vw, 0.58rem);
  font-weight: 900;
  letter-spacing: 0.1em;
  text-transform: uppercase;
}

.story-cover__label strong {
  margin-top: 0.2rem;
  font-family: var(--library-serif);
  font-size: clamp(1.35rem, 5vw, 2.4rem);
  font-weight: 650;
  letter-spacing: -0.06em;
}

.story-cover__corner {
  position: absolute;
  right: 0.55rem;
  bottom: 0.4rem;
  color: var(--cover-accent);
  font-size: 1rem;
}

.bookshelf-card__copy {
  display: flex;
  min-width: 0;
  flex-direction: column;
  align-items: flex-start;
}

.bookshelf-card__kicker {
  color: var(--library-muted);
  font-size: 0.66rem;
  font-weight: 900;
  letter-spacing: 0.13em;
  text-transform: uppercase;
}

.bookshelf-card__title {
  margin: 0.3rem 0 0;
  font-family: var(--library-serif);
  font-size: clamp(1.18rem, 3vw, 1.6rem);
  font-weight: 650;
  letter-spacing: -0.035em;
  line-height: 1.12;
  overflow-wrap: anywhere;
  text-wrap: balance;
}

.bookshelf-card__author {
  margin-top: 0.35rem;
  color: var(--library-muted);
  font-size: 0.82rem;
  line-height: 1.3;
  overflow-wrap: anywhere;
}

.bookshelf-card__facts {
  display: flex;
  flex-wrap: wrap;
  gap: 0.25rem 0.75rem;
  margin-top: 0.8rem;
  color: var(--library-muted);
  font-size: 0.72rem;
  font-weight: 720;
}

.bookshelf-card__facts span + span::before {
  content: "·";
  margin-right: 0.75rem;
}

.bookshelf-card__progress-state {
  margin-top: auto;
  padding-top: 0.85rem;
  font-size: 0.78rem;
  font-weight: 850;
}

.bookshelf-card__progress-state[data-kind="updated"],
.bookshelf-card__progress-state[data-kind="unavailable"] {
  color: #7c4800;
}

.bookshelf-card__progress {
  width: 100%;
  height: 0.32rem;
  margin-top: 0.45rem;
  overflow: hidden;
  border-radius: 999px;
  background: var(--library-mist);
}

.bookshelf-card__progress i {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: var(--library-ink);
}

.bookshelf-card__footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  border-top: 1px solid var(--library-line);
  padding: 0.75rem 1rem;
}

.bookshelf-card__action,
.bookshelf-card__details {
  min-width: 2.75rem;
  min-height: 2.75rem;
  color: inherit;
  font-size: 0.8rem;
  font-weight: 900;
}

.bookshelf-card__action {
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
  text-decoration: none;
}

.bookshelf-card__details {
  border: 0;
  border-radius: 999px;
  padding: 0.35rem 0.7rem;
  background: var(--library-mist);
  cursor: pointer;
}

@media (min-width: 64rem) {
  .bookshelf-card__read {
    grid-template-columns: minmax(7.2rem, 0.8fr) minmax(0, 1.12fr);
  }
}

@media (max-width: 23rem) {
  .bookshelf-card__read {
    grid-template-columns: minmax(5.6rem, 0.68fr) minmax(0, 1fr);
    padding-inline: 0.75rem;
  }

  .bookshelf-card__footer {
    padding-inline: 0.75rem;
  }
}

@media (prefers-reduced-motion: no-preference) {
  .bookshelf-card,
  .story-cover {
    transition: transform 180ms ease, box-shadow 180ms ease;
  }

  .bookshelf-card:hover {
    transform: translateY(-0.18rem);
    box-shadow: 0 0.8rem 2rem rgba(17, 17, 15, 0.1);
  }

  .bookshelf-card__read:hover .story-cover {
    transform: rotate(-1deg);
  }
}
</style>
