<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { useEventListener } from '@vueuse/core'
import {
  capturePagedReaderLocator,
  createReaderLocatorV2,
  type ReaderLocatorV2,
  type ReaderStorySegment,
} from '../../lib/reader-locator-v2'
import {
  clampReaderPercent,
  findReaderResumeSegment,
  readerWeightedPercent,
  type ReaderScrollPosition,
} from '../../lib/reader-scroll-location'
import {
  buildTransitionalReaderPages,
  readerPageForOrdinal,
} from '../../lib/reader-pages'

const props = defineProps<{
  title: string
  author: string | null
  language: string
  segments: readonly ReaderStorySegment[]
  fontFamily: string
  fontSize: number
  lineHeight: number
  contentWidth: number
  captureEnabled: boolean
}>()

const emit = defineEmits<{
  position: [position: ReaderScrollPosition]
  active: [ordinal: number]
}>()

const viewportRef = ref<HTMLElement | null>(null)
const articleRef = ref<HTMLElement | null>(null)
const currentPage = ref(0)
const pages = computed(() => buildTransitionalReaderPages(props.segments))
const firstSegmentIsHeading = computed(() => {
  const first = props.segments[0]
  return first?.kind === 'heading' && first.headingLevel === 1
})
let captureFrame: number | null = null
let restoredPosition: ReaderScrollPosition | null = null
let restoredPage = -1

const articleStyle = computed(() => ({
  '--reader-font-family': props.fontFamily,
  '--reader-font-size': props.fontSize + 'px',
  '--reader-line-height': String(props.lineHeight),
  '--reader-content-width': props.contentWidth + 'px',
}))

function capture(): ReaderScrollPosition | null {
  if (restoredPosition && currentPage.value === restoredPage) {
    return restoredPosition
  }
  const page = pages.value[currentPage.value]
  if (!page) return null
  const locator = capturePagedReaderLocator(props.segments, page.startOrdinal)
  if (!locator) return null
  return {
    locator,
    percent: clampReaderPercent(
      readerWeightedPercent(props.segments, page.startOrdinal, 0),
    ),
  }
}

function publishPosition() {
  captureFrame = null
  const element = viewportRef.value
  if (!element || !pages.value.length) return
  const index = Math.round(element.scrollLeft / Math.max(1, element.clientWidth))
  currentPage.value = Math.max(0, Math.min(pages.value.length - 1, index))
  if (restoredPosition && currentPage.value === restoredPage) {
    emit('active', restoredPosition.locator.segment.ordinal)
    return
  }
  restoredPosition = null
  restoredPage = -1
  const position = capture()
  if (!position) return
  emit('active', position.locator.segment.ordinal)
  if (props.captureEnabled) emit('position', position)
}

function scheduleCapture() {
  if (captureFrame !== null) return
  captureFrame = window.requestAnimationFrame(publishPosition)
}

useEventListener(viewportRef, 'scroll', scheduleCapture, { passive: true })

async function restore(locator: ReaderLocatorV2): Promise<boolean> {
  const match = findReaderResumeSegment(props.segments, locator)
  if (!match) return false
  const index = readerPageForOrdinal(pages.value, match.segment.ordinal)
  if (index < 0) return false
  currentPage.value = index
  const restoredLocator = createReaderLocatorV2(
    match.segment,
    locator.segment.offset,
  )
  restoredPosition = {
    locator: restoredLocator,
    percent: readerWeightedPercent(
      props.segments,
      match.segment.ordinal,
      restoredLocator.segment.offset,
    ),
  }
  restoredPage = index
  await nextTick()
  viewportRef.value?.scrollTo({
    left: index * (viewportRef.value?.clientWidth ?? 0),
    behavior: 'auto',
  })
  emit('active', match.segment.ordinal)
  return true
}

async function moveToOrdinal(
  ordinal: number,
): Promise<ReaderScrollPosition | null> {
  const segment =
    props.segments.find((candidate) => candidate.ordinal === ordinal) ?? null
  if (!segment) return null
  const locator = createReaderLocatorV2(segment, 0)
  await restore(locator)
  return capture()
}

function focusContent() {
  articleRef.value?.focus({ preventScroll: true })
}

defineExpose({ capture, restore, moveToOrdinal, focusContent })
</script>

<template>
  <article
    ref="articleRef"
    class="reader-story reader-paged-story"
    aria-labelledby="reader-story-title"
    :lang="language"
    :style="articleStyle"
    tabindex="-1"
  >
    <header v-if="!firstSegmentIsHeading" class="reader-story-metadata">
      <h1 id="reader-story-title">{{ title }}</h1>
      <p v-if="author" class="reader-story-author">{{ author }}</p>
    </header>
    <div ref="viewportRef" class="reader-paged-view" aria-label="Story pages">
      <section
        v-for="page in pages"
        :key="page.index"
        class="reader-page"
        :aria-label="'Page ' + (page.index + 1)"
      >
        <div
          v-for="(segment, segmentIndex) in page.segments"
          :id="page.index === 0 && segmentIndex === 0 && firstSegmentIsHeading ? 'reader-story-title' : undefined"
          :key="segment.contentKey + '-' + segment.contentOccurrence"
          class="reader-segment"
          :data-reader-segment-ordinal="segment.ordinal"
          :data-reader-content-key="segment.contentKey"
          :data-reader-content-occurrence="segment.contentOccurrence"
          v-html="segment.renderedHtml"
        />
      </section>
    </div>
  </article>
</template>
