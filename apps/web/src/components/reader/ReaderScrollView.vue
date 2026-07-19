<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useEventListener, useResizeObserver } from '@vueuse/core'
import {
  captureReaderScrollPosition,
  findReaderResumeSegment,
  readerReadingLine,
  readerRestoreScrollTop,
  readerScrollBehavior,
  readerWeightedPercent,
  type ReaderScrollPosition,
} from '../../lib/reader-scroll-location'
import {
  createReaderLocatorV2,
  settleProgrammaticReaderRestore,
  type ReaderLocatorV2,
  type ReaderSegmentLayout,
  type ReaderStorySegment,
} from '../../lib/reader-locator-v2'

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
  reducedMotion: boolean
}>()

const emit = defineEmits<{
  position: [position: ReaderScrollPosition]
  active: [ordinal: number]
}>()

const viewInstanceId = Symbol('scroll-reader-view')
const articleRef = ref<HTMLElement | null>(null)
const elements = new Map<number, HTMLElement>()
let captureFrame: number | null = null
let movementPending = false
let programmaticRestore = false
let disposed = false
let resolveReady: () => void = () => undefined
const readyPromise = new Promise<void>((resolve) => {
  resolveReady = resolve
})

const firstSegmentIsHeading = computed(() => {
  const first = props.segments[0]
  return first?.kind === 'heading' && first.headingLevel === 1
})

const articleStyle = computed(() => ({
  '--reader-font-family': props.fontFamily,
  '--reader-font-size': props.fontSize + 'px',
  '--reader-line-height': String(props.lineHeight),
  '--reader-content-width': props.contentWidth + 'px',
}))

function setSegmentElement(
  element: Element | { $el?: Element } | null,
  ordinal: number,
) {
  const resolved =
    element instanceof HTMLElement
      ? element
      : element && "$el" in element && element.$el instanceof HTMLElement
        ? element.$el
        : null
  if (resolved) elements.set(ordinal, resolved)
  else elements.delete(ordinal)
}

function headerBottom(): number {
  return (
    document.querySelector<HTMLElement>('[data-reader-header]')
      ?.getBoundingClientRect().bottom ?? 0
  )
}

function viewport() {
  return { height: window.innerHeight, headerBottom: headerBottom() }
}

function layouts(): ReaderSegmentLayout[] {
  return props.segments.flatMap((segment) => {
    const element = elements.get(segment.ordinal)
    if (!element) return []
    const rect = element.getBoundingClientRect()
    return [{ ordinal: segment.ordinal, top: rect.top, bottom: rect.bottom }]
  })
}

function whenReady(): Promise<void> {
  return readyPromise
}

function capture(): ReaderScrollPosition | null {
  return captureReaderScrollPosition(props.segments, layouts(), viewport())
}

function publishPosition() {
  captureFrame = null
  if (disposed) return
  const intentionalMovement = movementPending
  movementPending = false
  const position = capture()
  if (!position) return
  emit('active', position.locator.segment.ordinal)
  if (props.captureEnabled && intentionalMovement) {
    emit('position', position)
  }
}

function scheduleCapture(intentionalMovement = false) {
  if (disposed) return
  movementPending ||= intentionalMovement
  if (captureFrame !== null) return
  captureFrame = window.requestAnimationFrame(publishPosition)
}

useEventListener(
  window,
  'scroll',
  () => scheduleCapture(!programmaticRestore),
  { passive: true },
)
useEventListener(window, 'resize', scheduleCapture, { passive: true })
useResizeObserver(articleRef, () => scheduleCapture())

async function waitForWindowScrollQuiet(): Promise<void> {
  await new Promise<void>((resolve) => {
    let finished = false
    let quietTimer: number | null = null
    const maximumTimer = window.setTimeout(complete, 500)

    function complete() {
      if (finished) return
      finished = true
      if (quietTimer !== null) window.clearTimeout(quietTimer)
      window.clearTimeout(maximumTimer)
      window.removeEventListener('scroll', onScroll)
      resolve()
    }

    function onScroll() {
      if (quietTimer !== null) window.clearTimeout(quietTimer)
      quietTimer = window.setTimeout(complete, 80)
    }

    window.addEventListener('scroll', onScroll, { passive: true })
    onScroll()
  })
}

async function settleAt(
  element: HTMLElement,
  offset: number,
  allowMotion: boolean,
): Promise<void> {
  const scroll = (behavior: ScrollBehavior) => {
    if (disposed || !element.isConnected) return
    const rect = element.getBoundingClientRect()
    const readingLine = readerReadingLine(viewport())
    const maximumScrollTop = Math.max(
      0,
      document.documentElement.scrollHeight - window.innerHeight,
    )
    window.scrollTo({
      top: readerRestoreScrollTop({
        elementTop: rect.top,
        elementHeight: rect.height,
        currentScrollTop: window.scrollY,
        readingLine,
        offset,
        maximumScrollTop,
      }),
      behavior,
    })
  }

  const behavior = allowMotion
    ? readerScrollBehavior(props.reducedMotion)
    : 'auto'
  scroll(behavior)
  if (behavior === 'smooth') {
    await new Promise<void>((resolve) => {
      let finished = false
      const complete = () => {
        if (finished) return
        finished = true
        window.removeEventListener('scrollend', complete)
        window.clearTimeout(timeout)
        resolve()
      }
      const timeout = window.setTimeout(complete, 500)
      window.addEventListener('scrollend', complete, { once: true })
    })
  }
  await settleProgrammaticReaderRestore(() => scroll('auto'))
}

async function restore(
  locator: ReaderLocatorV2,
  options: { allowMotion?: boolean } = {},
): Promise<boolean> {
  const match = findReaderResumeSegment(props.segments, locator)
  if (!match) return false
  await nextTick()
  if (disposed) return false
  const element = elements.get(match.segment.ordinal)
  if (!element) return false
  programmaticRestore = true
  movementPending = false
  try {
    await settleAt(element, locator.segment.offset, options.allowMotion ?? true)
    await waitForWindowScrollQuiet()
    if (disposed || !element.isConnected) return false
    emit('active', match.segment.ordinal)
    return true
  } finally {
    programmaticRestore = false
  }
}

async function moveToOrdinal(
  ordinal: number,
  offset = 0,
  options: { allowMotion?: boolean } = {},
): Promise<ReaderScrollPosition | null> {
  const segment =
    props.segments.find((candidate) => candidate.ordinal === ordinal) ?? null
  if (!segment) return null
  const locator = createReaderLocatorV2(segment, offset)
  const restored = await restore(locator, options)
  if (!restored) return null
  return {
    locator,
    percent: readerWeightedPercent(props.segments, segment.ordinal, offset),
  }
}

function focusContent() {
  articleRef.value?.focus({ preventScroll: true })
}

onMounted(async () => {
  await nextTick()
  await document.fonts?.ready
  if (disposed) return
  scheduleCapture()
  resolveReady()
})

onBeforeUnmount(() => {
  disposed = true
  if (captureFrame !== null) {
    window.cancelAnimationFrame(captureFrame)
    captureFrame = null
  }
  movementPending = false
  programmaticRestore = false
  resolveReady()
})

defineExpose({ capture, whenReady, restore, moveToOrdinal, focusContent, mode: 'scroll', instanceId: viewInstanceId })
</script>

<template>
  <article
    ref="articleRef"
    class="reader-story reader-scroll-view"
    data-reader-scroll-view
    data-reader-view-mode="scroll"
    :aria-labelledby="'reader-story-title'"
    :lang="language"
    :style="articleStyle"
    tabindex="-1"
  >
    <header v-if="!firstSegmentIsHeading" class="reader-story-metadata">
      <h1 id="reader-story-title">{{ title }}</h1>
      <p v-if="author" class="reader-story-author">{{ author }}</p>
    </header>
    <div class="reader-segments">
      <div
        v-for="(segment, index) in segments"
        :id="index === 0 && firstSegmentIsHeading ? 'reader-story-title' : undefined"
        :key="segment.contentKey + '-' + segment.contentOccurrence"
        :ref="(element) => setSegmentElement(element, segment.ordinal)"
        class="reader-segment"
        data-reader-scroll-segment
        :data-reader-segment-ordinal="segment.ordinal"
        v-html="segment.renderedHtml"
      />
    </div>
  </article>
</template>
