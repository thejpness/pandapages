<script setup lang="ts">
import { computed, nextTick, watch } from 'vue'
import { RouterLink } from 'vue-router'
import {
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogOverlay,
  DialogPortal,
  DialogRoot,
  DialogTitle,
} from 'reka-ui'
import {
  classifyLibraryProgress,
  libraryActionLabel,
  libraryChapterLabel,
  libraryDisplayPercent,
  libraryLengthLabel,
  libraryProgressLabel,
  type LibraryStory,
} from '../../lib/library-read-model'

const props = defineProps<{
  open: boolean
  story: LibraryStory | null
}>()

const emit = defineEmits<{ 'update:open': [open: boolean] }>()

const progressKind = computed(() =>
  props.story ? classifyLibraryProgress(props.story) : 'not-started',
)
const actionLabel = computed(() =>
  props.story ? libraryActionLabel(props.story) : 'Read',
)
const progressLabel = computed(() =>
  props.story ? libraryProgressLabel(props.story) : '',
)
const lengthLabel = computed(() =>
  props.story ? libraryLengthLabel(props.story.wordCount) : '',
)
const chapterLabel = computed(() =>
  props.story ? libraryChapterLabel(props.story.chapterCount) : '',
)
const percent = computed(() =>
  props.story ? libraryDisplayPercent(props.story) : 0,
)

let returnFocus: HTMLElement | null = null

watch(
  () => props.open,
  async (open, previous) => {
    if (open && !previous) {
      returnFocus =
        document.activeElement instanceof HTMLElement
          ? document.activeElement
          : null
      return
    }

    if (!open && previous) {
      await nextTick()
      returnFocus?.focus({ preventScroll: true })
      returnFocus = null
    }
  },
)
</script>

<template>
  <DialogRoot :open="open" @update:open="emit('update:open', $event)">
    <DialogPortal>
      <DialogOverlay class="story-dialog__overlay" />
      <DialogContent v-if="story" class="story-dialog" data-testid="story-details-dialog">
        <div class="story-dialog__handle" aria-hidden="true"></div>
        <div class="story-dialog__heading">
          <div>
            <p>Story details</p>
            <DialogTitle class="story-dialog__title">{{ story.title }}</DialogTitle>
            <DialogDescription class="story-dialog__description">
              Reading information and progress for this story.
            </DialogDescription>
          </div>
          <DialogClose as-child>
            <button class="story-dialog__close" type="button" aria-label="Close story details">
              <span aria-hidden="true">×</span>
            </button>
          </DialogClose>
        </div>

        <p v-if="story.author" class="story-dialog__author">by {{ story.author }}</p>

        <dl class="story-dialog__facts">
          <div>
            <dt>Length</dt>
            <dd>{{ lengthLabel }}</dd>
          </div>
          <div>
            <dt>Chapters</dt>
            <dd>{{ chapterLabel || 'No chapter breaks' }}</dd>
          </div>
          <div>
            <dt>Reading</dt>
            <dd>{{ progressLabel }}</dd>
          </div>
        </dl>

        <div
          v-if="progressKind === 'in-progress' || progressKind === 'completed'"
          class="story-dialog__progress"
        >
          <div>
            <span>Reading progress</span>
            <strong>{{ percent }}%</strong>
          </div>
          <span
            class="story-dialog__progress-track"
            role="progressbar"
            :aria-label="`Reading progress for ${story.title}`"
            aria-valuemin="0"
            aria-valuemax="100"
            :aria-valuenow="percent"
          >
            <i :style="{ width: `${percent}%` }"></i>
          </span>
        </div>

        <p v-else-if="progressKind === 'updated'" class="story-dialog__updated">
          Story updated since you last read. Reader 2 will help you choose where to begin.
        </p>

        <p v-else-if="progressKind === 'unavailable'" class="story-dialog__updated">
          Progress is temporarily unavailable. You can still open the story safely.
        </p>

        <RouterLink
          class="story-dialog__action"
          :to="`/read/${encodeURIComponent(story.slug)}`"
          :aria-label="`${actionLabel}: ${story.title}`"
        >
          {{ actionLabel }}
          <span aria-hidden="true">→</span>
        </RouterLink>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>

<style scoped>
.story-dialog__overlay {
  position: fixed;
  z-index: 110;
  inset: 0;
  background: rgba(17, 17, 15, 0.58);
  backdrop-filter: blur(5px);
}

.story-dialog {
  position: fixed;
  z-index: 111;
  top: 50%;
  left: 50%;
  width: min(38rem, calc(100vw - 2rem - env(safe-area-inset-left) - env(safe-area-inset-right)));
  max-height: min(44rem, calc(100dvh - 2rem - env(safe-area-inset-top) - env(safe-area-inset-bottom)));
  overflow: auto;
  transform: translate(-50%, -50%);
  border: 2px solid #11110f;
  border-radius: 1.8rem;
  padding: clamp(1.15rem, 4vw, 2rem);
  padding-bottom: max(clamp(1.15rem, 4vw, 2rem), env(safe-area-inset-bottom));
  background: #fffefa;
  color: #11110f;
  box-shadow: 0 2rem 5rem rgba(17, 17, 15, 0.3);
  font-family: "Atkinson Hyperlegible Next Variable", ui-sans-serif, sans-serif;
}

.story-dialog__handle {
  display: none;
}

.story-dialog__heading {
  display: flex;
  align-items: start;
  justify-content: space-between;
  gap: 1rem;
}

.story-dialog__heading p {
  margin: 0 0 0.35rem;
  color: #656159;
  font-size: 0.7rem;
  font-weight: 900;
  letter-spacing: 0.13em;
  text-transform: uppercase;
}

.story-dialog__title {
  margin: 0;
  font-family: "Literata Variable", Georgia, serif;
  font-size: clamp(1.65rem, 6vw, 2.7rem);
  font-weight: 640;
  letter-spacing: -0.05em;
  line-height: 1.05;
  overflow-wrap: anywhere;
  text-wrap: balance;
}

.story-dialog__description {
  margin: 0.65rem 0 0;
  color: #656159;
  font-size: 0.86rem;
}

.story-dialog__close {
  display: grid;
  width: 2.75rem;
  height: 2.75rem;
  flex: 0 0 auto;
  place-items: center;
  border: 1px solid rgba(17, 17, 15, 0.24);
  border-radius: 50%;
  background: #f4f1e9;
  color: inherit;
  font-size: 1.35rem;
  cursor: pointer;
}

.story-dialog__author {
  margin: 0.7rem 0 0;
  color: #656159;
  font-family: "Literata Variable", Georgia, serif;
  font-style: italic;
}

.story-dialog__facts {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 0.6rem;
  margin: 1.5rem 0 0;
}

.story-dialog__facts div {
  min-width: 0;
  border: 1px solid rgba(17, 17, 15, 0.15);
  border-radius: 0.9rem;
  padding: 0.8rem;
  background: #f4f1e9;
}

.story-dialog__facts dt {
  color: #656159;
  font-size: 0.68rem;
  font-weight: 850;
  text-transform: uppercase;
}

.story-dialog__facts dd {
  margin: 0.3rem 0 0;
  font-size: 0.84rem;
  font-weight: 820;
  overflow-wrap: anywhere;
}

.story-dialog__progress {
  margin-top: 1.25rem;
}

.story-dialog__progress > div {
  display: flex;
  justify-content: space-between;
  gap: 1rem;
  font-size: 0.78rem;
}

.story-dialog__progress-track {
  display: block;
  height: 0.55rem;
  margin-top: 0.45rem;
  overflow: hidden;
  border-radius: 999px;
  background: #e7e3d9;
}

.story-dialog__progress-track i {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: #11110f;
}

.story-dialog__updated {
  margin: 1.25rem 0 0;
  border-left: 0.25rem solid #d38320;
  padding: 0.7rem 0.85rem;
  background: #fff2d8;
  color: #613800;
  font-size: 0.86rem;
  line-height: 1.5;
}

.story-dialog__action {
  display: flex;
  min-height: 3.4rem;
  align-items: center;
  justify-content: center;
  gap: 0.6rem;
  margin-top: 1.5rem;
  border-radius: 999px;
  background: #11110f;
  color: #fffefa;
  font-weight: 900;
  text-decoration: none;
}

@media (max-width: 34rem) {
  .story-dialog {
    top: auto;
    bottom: 0;
    left: max(env(safe-area-inset-left), 0px);
    width: calc(100vw - env(safe-area-inset-left) - env(safe-area-inset-right));
    max-height: calc(92dvh - env(safe-area-inset-top));
    transform: none;
    border-width: 2px 2px 0;
    border-radius: 1.6rem 1.6rem 0 0;
  }

  .story-dialog__handle {
    display: block;
    width: 2.7rem;
    height: 0.25rem;
    margin: -0.35rem auto 0.8rem;
    border-radius: 999px;
    background: rgba(17, 17, 15, 0.25);
  }

  .story-dialog__facts {
    grid-template-columns: 1fr;
  }
}

@media (prefers-reduced-motion: no-preference) {
  .story-dialog__overlay {
    animation: story-overlay-in 160ms ease-out;
  }

  .story-dialog {
    animation: story-dialog-in 190ms ease-out;
  }

  @keyframes story-overlay-in {
    from { opacity: 0; }
  }

  @keyframes story-dialog-in {
    from { opacity: 0; transform: translate(-50%, calc(-50% + 0.6rem)); }
  }
}

@media (prefers-reduced-motion: no-preference) and (max-width: 34rem) {
  @keyframes story-dialog-in {
    from { opacity: 0; transform: translateY(0.6rem); }
  }
}
</style>
