<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import {
  classifyLibraryProgress,
  libraryActionLabel,
  libraryProgressLabel,
  type LibraryStory,
} from '../../lib/library-read-model'

const props = defineProps<{ story: LibraryStory }>()

const progressKind = computed(() => classifyLibraryProgress(props.story))
const actionLabel = computed(() => libraryActionLabel(props.story))
const progressLabel = computed(() => libraryProgressLabel(props.story))
const percent = computed(() =>
  Math.round((props.story.progress?.percent ?? 0) * 100),
)

const eyebrow = computed(() => {
  if (progressKind.value === 'updated') return 'A new edition is waiting'
  if (progressKind.value === 'completed') return 'A favourite for another night'
  return 'Pick up where you left off'
})
</script>

<template>
  <section class="continue-section" aria-labelledby="continue-heading">
    <RouterLink
      class="continue-card"
      :to="`/read/${encodeURIComponent(story.slug)}`"
      :aria-label="`${actionLabel}: ${story.title}`"
    >
      <span class="continue-card__panda" aria-hidden="true">
        <i class="continue-card__ear continue-card__ear--left"></i>
        <i class="continue-card__ear continue-card__ear--right"></i>
        <i class="continue-card__face">
          <b></b><b></b><em></em>
        </i>
      </span>

      <span class="continue-card__copy">
        <span class="continue-card__eyebrow">{{ eyebrow }}</span>
        <strong id="continue-heading">{{ story.title }}</strong>
        <span v-if="story.author" class="continue-card__author">by {{ story.author }}</span>
        <span class="continue-card__status">{{ progressLabel }}</span>
        <span
          v-if="progressKind === 'in-progress' || progressKind === 'completed'"
          class="continue-card__progress"
          role="progressbar"
          aria-label="Reading progress"
          aria-valuemin="0"
          aria-valuemax="100"
          :aria-valuenow="percent"
        >
          <i :style="{ width: `${percent}%` }"></i>
        </span>
      </span>

      <span class="continue-card__action">
        {{ actionLabel }}
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path d="M5 12h14m-5-5 5 5-5 5" />
        </svg>
      </span>
    </RouterLink>
  </section>
</template>

<style scoped>
.continue-section {
  margin-bottom: clamp(2rem, 5vw, 3.75rem);
}

.continue-card {
  position: relative;
  display: grid;
  grid-template-columns: clamp(5.5rem, 12vw, 8.5rem) minmax(0, 1fr) auto;
  gap: clamp(1rem, 3vw, 2rem);
  align-items: center;
  min-height: 12rem;
  overflow: hidden;
  border: 2px solid var(--library-ink);
  border-radius: clamp(1.35rem, 3vw, 2.25rem);
  padding: clamp(1.2rem, 4vw, 2.25rem);
  background: var(--library-ink);
  color: var(--library-white);
  text-decoration: none;
  box-shadow: 0.75rem 0.75rem 0 var(--library-accent-soft);
}

.continue-card::after {
  content: "";
  position: absolute;
  inset: 0;
  background:
    radial-gradient(circle at 88% 22%, rgba(255, 255, 255, 0.12) 0 0.22rem, transparent 0.25rem),
    radial-gradient(circle at 82% 31%, rgba(255, 255, 255, 0.08) 0 0.12rem, transparent 0.15rem);
  background-size: 2.2rem 2.2rem, 2.8rem 2.8rem;
  pointer-events: none;
}

.continue-card__panda {
  position: relative;
  z-index: 1;
  display: grid;
  width: clamp(5.5rem, 12vw, 8.5rem);
  aspect-ratio: 1;
  place-items: center;
  border-radius: 50%;
  background: var(--library-white);
}

.continue-card__ear {
  position: absolute;
  top: -3%;
  width: 35%;
  aspect-ratio: 1;
  border-radius: 50%;
  background: var(--library-ink);
}

.continue-card__ear--left { left: 2%; }
.continue-card__ear--right { right: 2%; }

.continue-card__face {
  position: relative;
  display: block;
  width: 72%;
  aspect-ratio: 1.15;
  border-radius: 48%;
  background: var(--library-white);
}

.continue-card__face b {
  position: absolute;
  top: 27%;
  width: 27%;
  height: 35%;
  border-radius: 50%;
  background: var(--library-ink);
  transform: rotate(19deg);
}

.continue-card__face b:first-child { left: 12%; }
.continue-card__face b:nth-child(2) { right: 12%; transform: rotate(-19deg); }

.continue-card__face b::after {
  content: "";
  position: absolute;
  top: 30%;
  left: 38%;
  width: 19%;
  aspect-ratio: 1;
  border-radius: 50%;
  background: var(--library-white);
}

.continue-card__face em {
  position: absolute;
  bottom: 18%;
  left: 50%;
  width: 15%;
  aspect-ratio: 1.35;
  transform: translateX(-50%);
  border-radius: 50%;
  background: var(--library-ink);
}

.continue-card__copy {
  position: relative;
  z-index: 1;
  display: grid;
  min-width: 0;
}

.continue-card__eyebrow {
  margin-bottom: 0.45rem;
  color: #d6d1c5;
  font-size: 0.72rem;
  font-weight: 850;
  letter-spacing: 0.13em;
  text-transform: uppercase;
}

.continue-card__copy strong {
  max-width: 45rem;
  font-family: var(--library-serif);
  font-size: clamp(1.8rem, 5vw, 3.5rem);
  font-weight: 620;
  letter-spacing: -0.05em;
  line-height: 1.02;
  overflow-wrap: anywhere;
  text-wrap: balance;
}

.continue-card__author {
  margin-top: 0.45rem;
  color: #d6d1c5;
  font-size: 0.9rem;
}

.continue-card__status {
  margin-top: 1rem;
  font-size: 0.9rem;
  font-weight: 800;
}

.continue-card__progress {
  width: min(24rem, 100%);
  height: 0.42rem;
  margin-top: 0.5rem;
  overflow: hidden;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.2);
}

.continue-card__progress i {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: var(--library-accent);
}

.continue-card__action {
  position: relative;
  z-index: 1;
  display: flex;
  min-height: 3.2rem;
  align-items: center;
  justify-content: center;
  gap: 0.55rem;
  border-radius: 999px;
  padding: 0.7rem 1.1rem;
  background: var(--library-white);
  color: var(--library-ink);
  font-size: 0.88rem;
  font-weight: 900;
  white-space: nowrap;
}

.continue-card__action svg {
  width: 1.15rem;
  fill: none;
  stroke: currentColor;
  stroke-linecap: round;
  stroke-linejoin: round;
  stroke-width: 2;
}

@media (max-width: 44rem) {
  .continue-card {
    grid-template-columns: 4.7rem minmax(0, 1fr);
    min-height: 0;
  }

  .continue-card__panda {
    width: 4.7rem;
    align-self: start;
  }

  .continue-card__action {
    grid-column: 1 / -1;
    width: 100%;
  }
}

@media (max-width: 23rem) {
  .continue-card {
    grid-template-columns: 1fr;
  }

  .continue-card__panda {
    display: none;
  }
}

@media (prefers-reduced-motion: no-preference) {
  .continue-card,
  .continue-card__action svg {
    transition: transform 180ms ease, box-shadow 180ms ease;
  }

  .continue-card:hover {
    transform: translateY(-2px);
    box-shadow: 0.85rem 0.85rem 0 var(--library-accent-soft);
  }

  .continue-card:hover .continue-card__action svg {
    transform: translateX(0.2rem);
  }
}
</style>
