<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  shallowRef,
  watch,
} from 'vue'
import { useEventListener, useResizeObserver } from '@vueuse/core'
import {
  createReaderLocatorV2,
  settleProgrammaticReaderRestore,
  type ReaderLocatorV2,
  type ReaderStorySegment,
} from '../../lib/reader-locator-v2'
import { readerPageNavigationTarget } from '../../lib/reader-page-navigation'
import {
  buildReaderPages,
  readerPageForLocator,
  readerPageRepresentativeLocator,
  readerPageSegmentIdentity,
  type ReaderPage,
  type ReaderPageMetrics,
} from '../../lib/reader-pages'
import {
  captureReaderContainedScrollPosition,
  findReaderResumeSegment,
  readerContainedRestoreScrollTop,
  readerWeightedPercent,
  type ReaderScrollPosition,
} from '../../lib/reader-scroll-location'

const PAGE_INLINE_PADDING = 32
const PAGE_BLOCK_PADDING = 24
const MINIMUM_PAGE_SURFACE_HEIGHT = 220
const LAYOUT_BUCKET = 8
const REFLOW_DEBOUNCE_MS = 120
const SCROLL_SETTLE_MS = 140

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
  keyboardEnabled: boolean
}>()

const emit = defineEmits<{
  position: [position: ReaderScrollPosition]
  active: [ordinal: number]
}>()

const viewInstanceId = Symbol('paged-reader-view')
const articleRef = ref<HTMLElement | null>(null)
const viewportRef = ref<HTMLElement | null>(null)
const pages = shallowRef<ReaderPage[]>([])
const currentPage = ref(0)
const layoutReady = ref(false)
const surfaceHeight = ref(0)
const pageAnnouncement = ref('')
const navigationBusy = ref(true)
const measuredCorrectionCount = ref(0)
const pageElements = new Map<number, HTMLElement>()
const measuredOversizedByModelKey = new Map<string, ReadonlySet<string>>()

let mounted = false
let disposed = false
let readyResolved = false
let resolveReady: () => void = () => undefined
const readyPromise = new Promise<void>((resolve) => {
  resolveReady = resolve
})
let modelGeneration = 0
let lastModelKey = ''
let horizontalProgrammatic = false
const reflowPending = ref(false)
let reflowDirty = false
let pendingAnchor: ReaderScrollPosition | null = null
let pendingPageNavigation: number | null = null
let anchoredPosition: ReaderScrollPosition | null = null
let anchoredPage = -1
let settledPosition: ReaderScrollPosition | null = null
let settledPage = -1
let rebuildTimer: number | null = null
let horizontalFrame: number | null = null
let horizontalSettleTimer: number | null = null
let horizontalContactReleaseFrame: number | null = null
let horizontalScrollPending = false
let horizontalScrollEndDeferred = false
const activeHorizontalPointers = new Set<number>()
let activeHorizontalTouches = 0
let verticalFrame: number | null = null
let verticalSettleTimer: number | null = null
let headerObserver: ResizeObserver | null = null

const firstSegmentIsHeading = computed(() => {
  const first = props.segments[0]
  return first?.kind === 'heading' && first.headingLevel === 1
})
const pageCount = computed(() => pages.value.length)
const pageCountLabelWidth = computed(() => {
  const digits = String(Math.max(1, pageCount.value)).length
  return 9 + digits * 2 + 'ch'
})
const previousDisabled = computed(
  () =>
    !layoutReady.value ||
    navigationBusy.value ||
    reflowPending.value ||
    currentPage.value <= 0,
)
const nextDisabled = computed(
  () =>
    !layoutReady.value ||
    navigationBusy.value ||
    reflowPending.value ||
    currentPage.value >= pageCount.value - 1,
)
const articleStyle = computed(() => ({
  '--reader-font-family': props.fontFamily,
  '--reader-font-size': props.fontSize + 'px',
  '--reader-line-height': String(props.lineHeight),
  '--reader-content-width': props.contentWidth + 'px',
  '--reader-paged-height': surfaceHeight.value + 'px',
  '--reader-page-count-width': pageCountLabelWidth.value,
}))

function setPageElement(
  element: Element | { $el?: Element } | null,
  index: number,
) {
  const resolved =
    element instanceof HTMLElement
      ? element
      : element && '$el' in element && element.$el instanceof HTMLElement
        ? element.$el
        : null
  if (resolved) pageElements.set(index, resolved)
  else pageElements.delete(index)
}

function resolveLayoutReady() {
  if (readyResolved) return
  readyResolved = true
  resolveReady()
}

function whenReady(): Promise<void> {
  return layoutReady.value ? Promise.resolve() : readyPromise
}

function bucketDown(value: number): number {
  const finite = Number.isFinite(value) ? Math.max(1, value) : 1
  return Math.max(1, Math.floor(finite / LAYOUT_BUCKET) * LAYOUT_BUCKET)
}

function viewportHeight(): number {
  return window.visualViewport?.height ?? window.innerHeight
}

function mainBottomPadding(): number {
  const main = articleRef.value?.closest<HTMLElement>('.reader-main')
  if (!main) return 0
  const value = Number.parseFloat(window.getComputedStyle(main).paddingBottom)
  return Number.isFinite(value) ? value : 0
}

async function measurePageMetrics(): Promise<ReaderPageMetrics | null> {
  const article = articleRef.value
  const viewport = viewportRef.value
  if (!article || !viewport) return null

  const available = Math.max(
    MINIMUM_PAGE_SURFACE_HEIGHT,
    Math.floor(
      viewportHeight() -
        article.getBoundingClientRect().top -
        mainBottomPadding(),
    ),
  )
  surfaceHeight.value = available
  await nextTick()
  await new Promise<void>((resolve) => {
    window.requestAnimationFrame(() => resolve())
  })

  return {
    fontSize: props.fontSize,
    lineHeight: props.lineHeight,
    contentWidth: bucketDown(
      Math.max(1, viewport.clientWidth - PAGE_INLINE_PADDING),
    ),
    availableHeight: bucketDown(
      Math.max(1, viewport.clientHeight - PAGE_BLOCK_PADDING),
    ),
  }
}

function metricsKey(metrics: ReaderPageMetrics): string {
  return [
    metrics.fontSize,
    metrics.lineHeight,
    metrics.contentWidth,
    metrics.availableHeight,
    props.contentWidth,
    props.fontFamily,
    props.segments.length,
  ].join(':')
}

function nearestPage(): number {
  const viewport = viewportRef.value
  if (!viewport || pages.value.length === 0) return 0
  const index = Math.round(
    viewport.scrollLeft / Math.max(1, viewport.clientWidth),
  )
  return Math.max(0, Math.min(pages.value.length - 1, index))
}

function pageIsScrollable(page: ReaderPage): boolean {
  const element = pageElements.get(page.index)
  return Boolean(
    element && element.scrollHeight - element.clientHeight > 1,
  )
}

function scrollablePageLocator(page: ReaderPage): ReaderLocatorV2 | null {
  const element = pageElements.get(page.index)
  if (!element || element.scrollHeight - element.clientHeight <= 1) return null

  const pageRect = element.getBoundingClientRect()
  const layouts = Array.from(
    element.querySelectorAll<HTMLElement>('[data-reader-paged-segment]'),
  ).flatMap((segmentElement) => {
    const ordinal = Number(
      segmentElement.getAttribute('data-reader-segment-ordinal'),
    )
    if (!Number.isInteger(ordinal)) return []
    const rect = segmentElement.getBoundingClientRect()
    const top = rect.top - pageRect.top + element.scrollTop
    return [{ ordinal, top, bottom: top + rect.height }]
  })
  return captureReaderContainedScrollPosition(props.segments, layouts, {
    scrollTop: element.scrollTop,
    scrollHeight: element.scrollHeight,
    clientHeight: element.clientHeight,
  })?.locator ?? null
}

function positionForPage(pageIndex: number): ReaderScrollPosition | null {
  const page = pages.value[pageIndex]
  if (!page) return null
  const locator = scrollablePageLocator(page) ?? readerPageRepresentativeLocator(page)
  if (!locator) return null
  return {
    locator,
    percent: readerWeightedPercent(
      props.segments,
      locator.segment.ordinal,
      locator.segment.offset,
    ),
  }
}

function rememberSettled(position: ReaderScrollPosition | null) {
  settledPosition = position
  settledPage = position ? currentPage.value : -1
}

function stableAnchor(): ReaderScrollPosition | null {
  if (anchoredPosition && anchoredPage === currentPage.value) {
    return anchoredPosition
  }
  if (settledPosition && settledPage === currentPage.value) {
    return settledPosition
  }
  return positionForPage(currentPage.value)
}

function capture(): ReaderScrollPosition | null {
  if (!layoutReady.value) return null
  if (anchoredPosition && anchoredPage === currentPage.value) {
    return anchoredPosition
  }
  if (
    (reflowPending.value || horizontalProgrammatic || navigationBusy.value) &&
    settledPosition &&
    settledPage === currentPage.value
  ) {
    return settledPosition
  }
  return positionForPage(currentPage.value)
}

function clearHorizontalSettling() {
  if (horizontalFrame !== null) {
    window.cancelAnimationFrame(horizontalFrame)
    horizontalFrame = null
  }
  if (horizontalSettleTimer !== null) {
    window.clearTimeout(horizontalSettleTimer)
    horizontalSettleTimer = null
  }
  if (horizontalContactReleaseFrame !== null) {
    window.cancelAnimationFrame(horizontalContactReleaseFrame)
    horizontalContactReleaseFrame = null
  }
  horizontalScrollPending = false
  horizontalScrollEndDeferred = false
}

function clearVerticalSettling() {
  if (verticalFrame !== null) {
    window.cancelAnimationFrame(verticalFrame)
    verticalFrame = null
  }
  if (verticalSettleTimer !== null) {
    window.clearTimeout(verticalSettleTimer)
    verticalSettleTimer = null
  }
}

function emitCurrentPosition(announcePage: boolean) {
  const position = capture()
  if (!position) return
  rememberSettled(position)
  emit('active', position.locator.segment.ordinal)
  if (announcePage) {
    pageAnnouncement.value =
      'Page ' + (currentPage.value + 1) + ' of ' + pageCount.value
  }
  if (props.captureEnabled) emit('position', position)
}

function applySettledPage(index: number, announcePage: boolean) {
  const bounded = Math.max(0, Math.min(pageCount.value - 1, index))
  if (bounded === currentPage.value) return
  currentPage.value = bounded
  anchoredPosition = null
  anchoredPage = -1
  emitCurrentPosition(announcePage)
}

async function waitForSmoothScroll(element: HTMLElement): Promise<void> {
  await new Promise<void>((resolve) => {
    let finished = false
    const complete = () => {
      if (finished) return
      finished = true
      element.removeEventListener('scrollend', complete)
      window.clearTimeout(timeout)
      resolve()
    }
    const timeout = window.setTimeout(complete, 500)
    element.addEventListener('scrollend', complete, { once: true })
  })
}

async function settleHorizontalPage(
  index: number,
  behavior: ScrollBehavior,
  generation: number,
): Promise<void> {
  const viewport = viewportRef.value
  if (!viewport) return
  const scroll = (nextBehavior: ScrollBehavior) => {
    if (disposed || generation !== modelGeneration) return
    viewport.scrollTo({
      left: index * viewport.clientWidth,
      behavior: nextBehavior,
    })
  }

  scroll(behavior)
  if (behavior === 'smooth') await waitForSmoothScroll(viewport)
  await settleProgrammaticReaderRestore(() => scroll('auto'))
}

async function restorePageOffset(
  pageIndex: number,
  locator: ReaderLocatorV2,
  generation: number,
): Promise<void> {
  const page = pages.value[pageIndex]
  const element = pageElements.get(pageIndex)
  if (!page || !element || !pageIsScrollable(page)) return

  const restoreScrollTop = () => {
    if (disposed || generation !== modelGeneration) return
    const segmentElement = element.querySelector<HTMLElement>(
      '[data-reader-segment-ordinal="' + locator.segment.ordinal + '"]',
    )
    if (!segmentElement) return
    const pageRect = element.getBoundingClientRect()
    const segmentRect = segmentElement.getBoundingClientRect()
    const segmentTop = segmentRect.top - pageRect.top + element.scrollTop
    const top = readerContainedRestoreScrollTop({
      layout: {
        ordinal: locator.segment.ordinal,
        top: segmentTop,
        bottom: segmentTop + segmentRect.height,
      },
      offset: locator.segment.offset,
      scrollHeight: element.scrollHeight,
      clientHeight: element.clientHeight,
    })
    element.scrollTo({ top, behavior: 'auto' })
  }
  await settleProgrammaticReaderRestore(restoreScrollTop)
}

function finishProgrammaticOperation(generation: number) {
  if (disposed || generation !== modelGeneration) return
  horizontalProgrammatic = false
  reflowPending.value = false
  navigationBusy.value = false
  if (reflowDirty) {
    reflowDirty = false
    queueRebuild()
    return
  }
  if (pendingPageNavigation !== null) {
    const destination = pendingPageNavigation
    pendingPageNavigation = null
    void navigateToPage(destination)
  }
}

async function rebuildPages(
  anchor: ReaderScrollPosition | null,
  force = false,
  allowMotion = false,
): Promise<boolean> {
  const generation = ++modelGeneration
  horizontalProgrammatic = true
  reflowPending.value = true
  reflowDirty = false
  navigationBusy.value = true
  clearHorizontalSettling()
  clearVerticalSettling()

  const metrics = await measurePageMetrics()
  if (disposed || generation !== modelGeneration) return false
  if (!metrics) {
    finishProgrammaticOperation(generation)
    return false
  }
  const key = metricsKey(metrics)
  let nextPages =
    !force && key === lastModelKey && pages.value.length > 0
      ? pages.value
      : buildReaderPages(props.segments, metrics)
  if (nextPages.length === 0) {
    finishProgrammaticOperation(generation)
    return false
  }

  pages.value = nextPages
  lastModelKey = key
  await nextTick()
  if (disposed || generation !== modelGeneration) return false

  const shouldMeasure = !measuredOversizedByModelKey.has(key)
  const corrected = new Set<string>(measuredOversizedByModelKey.get(key) ?? [])
  if (shouldMeasure) {
    for (const page of nextPages) {
      if (page.oversized) continue
      const pageElement = pageElements.get(page.index)
      if (!pageElement || pageElement.scrollHeight - pageElement.clientHeight <= 1) continue
      for (const segment of page.segments) {
        const segmentElement = pageElement.querySelector<HTMLElement>(
          "[data-reader-segment-ordinal=\"" + segment.ordinal + "\"]",
        )
        if (segmentElement && segmentElement.getBoundingClientRect().height - pageElement.clientHeight > 1) {
          corrected.add(readerPageSegmentIdentity(segment))
        }
      }
    }
    measuredOversizedByModelKey.set(key, corrected)
  }
  if (shouldMeasure && corrected.size > 0) {
    measuredCorrectionCount.value += 1
    nextPages = buildReaderPages(props.segments, metrics, {
      forcedOversizedSegmentIdentities: corrected,
    })
    pages.value = nextPages
    await nextTick()
    if (disposed || generation !== modelGeneration) return false
  }

  // Reused keyed page elements retain their own vertical scroll position.
  // Reset every page before restoring the canonical target anchor so Start
  // Over and later revisits cannot resurrect an obsolete oversized offset.
  for (const element of pageElements.values()) {
    element.scrollTo({ top: 0, behavior: 'auto' })
  }
  const located = anchor
    ? readerPageForLocator(nextPages, props.segments, anchor.locator)
    : 0
  const targetPage = located >= 0 ? located : 0
  currentPage.value = targetPage
  anchoredPosition = located >= 0 ? anchor : null
  anchoredPage = located >= 0 ? targetPage : -1

  await nextTick()
  const behavior: ScrollBehavior =
    allowMotion && !props.reducedMotion && layoutReady.value
      ? 'smooth'
      : 'auto'
  await settleHorizontalPage(targetPage, behavior, generation)
  if (disposed || generation !== modelGeneration) return false
  if (anchor && located >= 0) {
    await restorePageOffset(targetPage, anchor.locator, generation)
  }
  if (disposed || generation !== modelGeneration) return false

  currentPage.value = targetPage
  anchoredPosition = located >= 0 ? anchor : null
  anchoredPage = located >= 0 ? targetPage : -1
  layoutReady.value = true
  const current = capture()
  rememberSettled(current)
  if (current) emit('active', current.locator.segment.ordinal)
  pageAnnouncement.value = ''
  resolveLayoutReady()
  finishProgrammaticOperation(generation)
  return located >= 0 || anchor === null
}

function cancelQueuedRebuild(): ReaderScrollPosition | null {
  if (rebuildTimer !== null) {
    window.clearTimeout(rebuildTimer)
    rebuildTimer = null
  }
  const anchor = pendingAnchor
  pendingAnchor = null
  reflowPending.value = false
  reflowDirty = false
  return anchor
}

function queueRebuild() {
  if (!mounted || disposed || !layoutReady.value) return
  if (horizontalProgrammatic || navigationBusy.value) {
    reflowDirty = true
    return
  }
  pendingAnchor ??= stableAnchor()
  reflowPending.value = true
  if (rebuildTimer !== null) window.clearTimeout(rebuildTimer)
  rebuildTimer = window.setTimeout(() => {
    rebuildTimer = null
    const anchor = pendingAnchor
    pendingAnchor = null
    void rebuildPages(anchor)
  }, REFLOW_DEBOUNCE_MS)
}

function onObservedResize() {
  if (horizontalProgrammatic || navigationBusy.value) return
  queueRebuild()
}

function onHeaderResize() {
  queueRebuild()
}

function horizontalContactActive(): boolean {
  return activeHorizontalPointers.size > 0 || activeHorizontalTouches > 0
}

function viewportSupportsScrollEnd(
  viewport = viewportRef.value,
): viewport is HTMLElement {
  return Boolean(viewport && 'onscrollend' in viewport)
}

function finishNativeHorizontalScroll() {
  if (horizontalContactActive()) {
    horizontalScrollEndDeferred = true
    return
  }
  const hadPendingScroll = horizontalScrollPending
  clearHorizontalSettling()
  if (
    !hadPendingScroll ||
    horizontalProgrammatic ||
    reflowPending.value ||
    navigationBusy.value ||
    !layoutReady.value
  ) {
    return
  }
  applySettledPage(nearestPage(), true)
}

function armHorizontalQuietFallback() {
  const viewport = viewportRef.value
  if (
    !viewport ||
    viewportSupportsScrollEnd(viewport) ||
    horizontalContactActive() ||
    !horizontalScrollPending
  ) {
    return
  }
  if (horizontalSettleTimer !== null) {
    window.clearTimeout(horizontalSettleTimer)
  }
  horizontalSettleTimer = window.setTimeout(
    finishNativeHorizontalScroll,
    SCROLL_SETTLE_MS,
  )
}

function onHorizontalScroll() {
  if (
    horizontalProgrammatic ||
    reflowPending.value ||
    navigationBusy.value ||
    !layoutReady.value
  ) {
    return
  }
  horizontalScrollPending = true
  horizontalScrollEndDeferred = false
  if (horizontalFrame === null) {
    horizontalFrame = window.requestAnimationFrame(() => {
      horizontalFrame = null
      nearestPage()
    })
  }
  if (viewportSupportsScrollEnd()) {
    if (horizontalSettleTimer !== null) {
      window.clearTimeout(horizontalSettleTimer)
      horizontalSettleTimer = null
    }
    return
  }
  if (horizontalContactActive()) return
  armHorizontalQuietFallback()
}

function onHorizontalScrollEnd() {
  if (horizontalProgrammatic || reflowPending.value || navigationBusy.value) return
  if (!horizontalScrollPending) return
  if (horizontalContactActive()) {
    horizontalScrollEndDeferred = true
    return
  }
  finishNativeHorizontalScroll()
}

function finishHorizontalContact() {
  if (horizontalContactActive() || !horizontalScrollPending) return
  if (!viewportSupportsScrollEnd()) {
    horizontalScrollEndDeferred = false
    armHorizontalQuietFallback()
    return
  }
  if (!horizontalScrollEndDeferred) return
  if (horizontalContactReleaseFrame !== null) {
    window.cancelAnimationFrame(horizontalContactReleaseFrame)
  }
  horizontalContactReleaseFrame = window.requestAnimationFrame(() => {
    horizontalContactReleaseFrame = null
    if (!horizontalContactActive() && horizontalScrollEndDeferred) {
      finishNativeHorizontalScroll()
    }
  })
}

function onHorizontalPointerDown(event: PointerEvent) {
  activeHorizontalPointers.add(event.pointerId)
  horizontalScrollEndDeferred = false
  if (horizontalSettleTimer !== null) {
    window.clearTimeout(horizontalSettleTimer)
    horizontalSettleTimer = null
  }
}

function onHorizontalPointerEnd(event: PointerEvent) {
  activeHorizontalPointers.delete(event.pointerId)
  finishHorizontalContact()
}

function onHorizontalTouchChange(event: TouchEvent) {
  activeHorizontalTouches = event.touches.length
  if (activeHorizontalTouches > 0) {
    horizontalScrollEndDeferred = false
    if (horizontalSettleTimer !== null) {
      window.clearTimeout(horizontalSettleTimer)
      horizontalSettleTimer = null
    }
    return
  }
  finishHorizontalContact()
}

function finishPageScroll(pageIndex: number) {
  if (horizontalSettleTimer !== null || horizontalFrame !== null) {
    if (verticalSettleTimer !== null) {
      window.clearTimeout(verticalSettleTimer)
    }
    verticalSettleTimer = window.setTimeout(
      () => finishPageScroll(pageIndex),
      SCROLL_SETTLE_MS,
    )
    return
  }
  clearVerticalSettling()
  if (
    horizontalProgrammatic ||
    reflowPending.value ||
    navigationBusy.value ||
    pageIndex !== currentPage.value
  ) {
    return
  }
  const page = pages.value[pageIndex]
  if (!page || !pageIsScrollable(page)) return
  anchoredPosition = null
  anchoredPage = -1
  emitCurrentPosition(false)
}

function onPageScroll(page: ReaderPage) {
  if (
    page.index !== currentPage.value ||
    !pageIsScrollable(page) ||
    horizontalProgrammatic ||
    reflowPending.value ||
    navigationBusy.value
  ) {
    return
  }
  if (verticalFrame === null) {
    verticalFrame = window.requestAnimationFrame(() => {
      verticalFrame = null
      positionForPage(page.index)
    })
  }
  if (verticalSettleTimer !== null) window.clearTimeout(verticalSettleTimer)
  verticalSettleTimer = window.setTimeout(
    () => finishPageScroll(page.index),
    SCROLL_SETTLE_MS,
  )
}

async function navigateToPage(index: number, allowMotion = true) {
  if (index < 0 || index >= pageCount.value || index === currentPage.value) {
    return
  }
  if (navigationBusy.value || reflowPending.value || horizontalProgrammatic) {
    pendingPageNavigation = index
    return
  }
  const generation = ++modelGeneration
  navigationBusy.value = true
  horizontalProgrammatic = true
  clearHorizontalSettling()
  clearVerticalSettling()
  const destination = Math.max(0, Math.min(pageCount.value - 1, index))
  const behavior: ScrollBehavior =
    allowMotion && !props.reducedMotion ? 'smooth' : 'auto'
  await settleHorizontalPage(destination, behavior, generation)
  if (disposed || generation !== modelGeneration) return
  applySettledPage(destination, true)
  finishProgrammaticOperation(generation)
}

function targetIsInteractive(target: EventTarget | null): boolean {
  if (!(target instanceof Element)) return false
  return Boolean(
    target.closest(
      'a, button, input, textarea, select, option, summary, [contenteditable="true"], [role="button"], [role="link"], [role="textbox"], [role="combobox"], [role="slider"], .reader-paged-metadata',
    ),
  )
}

function onKeydown(event: KeyboardEvent) {
  if (!props.keyboardEnabled) return
  const target = event.target instanceof HTMLElement ? event.target : null
  const destination = readerPageNavigationTarget({
    key: event.key,
    currentPage: currentPage.value,
    pageCount: pageCount.value,
    modalOpen: Boolean(document.querySelector('[role="dialog"]')),
    defaultPrevented: event.defaultPrevented,
    altKey: event.altKey,
    ctrlKey: event.ctrlKey,
    metaKey: event.metaKey,
    shiftKey: event.shiftKey,
    targetTagName: target?.tagName,
    targetIsContentEditable: target?.isContentEditable,
    targetIsInteractive: targetIsInteractive(event.target),
  })
  if (destination === null) return
  event.preventDefault()
  void navigateToPage(destination)
}

async function restore(
  locator: ReaderLocatorV2,
  options: { allowMotion?: boolean } = {},
): Promise<boolean> {
  const match = findReaderResumeSegment(props.segments, locator)
  if (!match) return false
  cancelQueuedRebuild()
  const restoredLocator = createReaderLocatorV2(
    match.segment,
    locator.segment.offset,
  )
  const position = {
    locator: restoredLocator,
    percent: readerWeightedPercent(
      props.segments,
      match.segment.ordinal,
      restoredLocator.segment.offset,
    ),
  }
  return rebuildPages(position, true, options.allowMotion ?? true)
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
  return restored ? capture() : null
}

function focusContent() {
  articleRef.value?.focus({ preventScroll: true })
}

useEventListener(viewportRef, 'scroll', onHorizontalScroll, { passive: true })
useEventListener(viewportRef, 'scrollend', onHorizontalScrollEnd, {
  passive: true,
})
useEventListener(viewportRef, 'pointerdown', onHorizontalPointerDown, {
  passive: true,
})
useEventListener(window, 'pointerup', onHorizontalPointerEnd, {
  passive: true,
})
useEventListener(window, 'pointercancel', onHorizontalPointerEnd, {
  passive: true,
})
useEventListener(viewportRef, 'lostpointercapture', onHorizontalPointerEnd, {
  passive: true,
})
useEventListener(viewportRef, 'touchstart', onHorizontalTouchChange, {
  passive: true,
})
useEventListener(viewportRef, 'touchend', onHorizontalTouchChange, {
  passive: true,
})
useEventListener(viewportRef, 'touchcancel', onHorizontalTouchChange, {
  passive: true,
})
useEventListener(window, 'keydown', onKeydown)
useEventListener(window, 'resize', queueRebuild, { passive: true })
useEventListener(window, 'orientationchange', queueRebuild, { passive: true })
if (window.visualViewport) {
  useEventListener(window.visualViewport, 'resize', queueRebuild, {
    passive: true,
  })
}
useResizeObserver(articleRef, onObservedResize)

watch(
  () => [
    props.fontFamily,
    props.fontSize,
    props.lineHeight,
    props.contentWidth,
    props.segments,
  ],
  queueRebuild,
)

onMounted(async () => {
  mounted = true
  await nextTick()
  await document.fonts?.ready
  if (disposed) return
  await rebuildPages(null, true)
  if (disposed) return

  const header = document.querySelector<HTMLElement>('[data-reader-header]')
  if (header && 'ResizeObserver' in window) {
    headerObserver = new ResizeObserver(onHeaderResize)
    headerObserver.observe(header)
  }
})

onBeforeUnmount(() => {
  disposed = true
  modelGeneration += 1
  cancelQueuedRebuild()
  clearHorizontalSettling()
  clearVerticalSettling()
  activeHorizontalPointers.clear()
  activeHorizontalTouches = 0
  pendingPageNavigation = null
  headerObserver?.disconnect()
  headerObserver = null
  resolveLayoutReady()
})

defineExpose({ capture, whenReady, restore, moveToOrdinal, focusContent, mode: 'paged', instanceId: viewInstanceId })
</script>

<template>
  <article
    ref="articleRef"
    class="reader-story reader-paged-story"
    data-reader-paged-view
    data-reader-view-mode="paged"
    :data-reader-paged-ready="layoutReady ? 'true' : 'false'"
    :data-reader-current-page="currentPage + 1"
    :data-reader-page-count="pageCount"
    :aria-labelledby="firstSegmentIsHeading ? 'reader-story-title' : 'reader-paged-metadata-title'"
    :aria-busy="!layoutReady"
    :lang="language"
    :style="articleStyle"
    tabindex="-1"
  >
    <header
      v-if="!firstSegmentIsHeading"
      class="reader-story-metadata reader-paged-metadata"
      role="region"
      aria-label="Story title and author"
      tabindex="0"
    >
      <h1 id="reader-paged-metadata-title">{{ title }}</h1>
      <p v-if="author" class="reader-story-author">{{ author }}</p>
    </header>

    <div
      ref="viewportRef"
      class="reader-paged-view reader-paged-viewport"
      role="region"
      aria-label="Story pages"
      tabindex="0"
    >
      <section
        v-for="page in pages"
        :key="page.startOrdinal + '-' + page.endOrdinal"
        :ref="(element) => setPageElement(element, page.index)"
        class="reader-page"
        :class="{ 'reader-page--oversized': page.oversized }"
        role="group"
        :aria-label="'Page ' + (page.index + 1) + ' of ' + pageCount"
        :aria-current="page.index === currentPage ? 'page' : undefined"
        :inert="page.index === currentPage ? undefined : true"
        :tabindex="page.index === currentPage ? 0 : -1"
        :data-reader-page-index="page.index"
        :data-reader-page-current="page.index === currentPage ? 'true' : 'false'"
        :data-reader-page-oversized="page.oversized ? 'true' : 'false'"
        :data-reader-page-start-ordinal="page.startOrdinal"
        :data-reader-page-end-ordinal="page.endOrdinal"
        @scroll.passive="onPageScroll(page)"
      >
        <div
          v-for="(segment, segmentIndex) in page.segments"
          :id="page.index === 0 && segmentIndex === 0 && firstSegmentIsHeading ? 'reader-story-title' : undefined"
          :key="segment.contentKey + '-' + segment.contentOccurrence"
          class="reader-segment"
          data-reader-paged-segment
          :data-reader-segment-ordinal="segment.ordinal"
          :data-reader-content-key="segment.contentKey"
          :data-reader-content-occurrence="segment.contentOccurrence"
          v-html="segment.renderedHtml"
        />
      </section>
    </div>

    <nav
      class="reader-page-navigation"
      aria-label="Page navigation"
      :aria-hidden="!layoutReady"
    >
      <button
        type="button"
        class="reader-page-navigation-button"
        aria-label="Previous page"
        :disabled="previousDisabled"
        @click="navigateToPage(currentPage - 1)"
      >
        Previous
      </button>
      <p class="reader-page-count">
        Page <span>{{ currentPage + 1 }}</span> of <span>{{ pageCount }}</span>
      </p>
      <button
        type="button"
        class="reader-page-navigation-button"
        aria-label="Next page"
        :disabled="nextDisabled"
        @click="navigateToPage(currentPage + 1)"
      >
        Next
      </button>
    </nav>

    <p class="reader-sr-only" role="status" aria-live="polite" aria-atomic="true">
      {{ pageAnnouncement }}
    </p>
  </article>
</template>
