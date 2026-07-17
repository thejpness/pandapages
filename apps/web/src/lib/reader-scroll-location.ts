import {
  clampReaderOffset,
  createReaderLocatorV2,
  type ReaderLocatorV2,
  type ReaderSegmentLayout,
  type ReaderStorySegment,
} from './reader-locator-v2'

export const READER_LINE_RATIO = 0.35
export const MINIMUM_SEGMENT_WEIGHT = 1

export type ReaderViewport = {
  height: number
  headerBottom: number
  safeAreaTop?: number
}

export type ReaderScrollPosition = {
  locator: ReaderLocatorV2
  percent: number
}

export type ReaderResumeMatch = {
  segment: ReaderStorySegment
  matchedBy: 'identity' | 'ordinal'
}

export function clampReaderPercent(value: number): number {
  if (!Number.isFinite(value)) return 0
  return Math.max(0, Math.min(1, value))
}

export function readerReadingLine(
  viewport: ReaderViewport,
  ratio = READER_LINE_RATIO,
): number {
  const height = Math.max(0, viewport.height)
  const topInset = Math.min(
    height,
    Math.max(0, viewport.headerBottom, viewport.safeAreaTop ?? 0),
  )
  return topInset + (height - topInset) * clampReaderOffset(ratio)
}

export function activeReaderSegmentLayout(
  layouts: readonly ReaderSegmentLayout[],
  readingLine: number,
): ReaderSegmentLayout | null {
  if (!layouts.length) return null

  let firstBelow: ReaderSegmentLayout | null = null
  let last: ReaderSegmentLayout | null = null
  for (const layout of layouts) {
    if (!Number.isInteger(layout.ordinal) || layout.ordinal < 1) continue
    if (!Number.isFinite(layout.top) || !Number.isFinite(layout.bottom)) continue
    last = layout
    if (layout.top <= readingLine && layout.bottom >= readingLine) return layout
    if (firstBelow === null && layout.top > readingLine) firstBelow = layout
  }
  return firstBelow ?? last
}

export function readerSegmentOffset(
  layout: ReaderSegmentLayout,
  readingLine: number,
): number {
  const height = layout.bottom - layout.top
  if (!Number.isFinite(height) || height <= 0) return 0
  return clampReaderOffset((readingLine - layout.top) / height)
}

export function readerSegmentWeight(segment: ReaderStorySegment): number {
  if (!Number.isFinite(segment.wordCount)) return MINIMUM_SEGMENT_WEIGHT
  return Math.max(MINIMUM_SEGMENT_WEIGHT, segment.wordCount)
}

export function readerWeightedPercent(
  segments: readonly ReaderStorySegment[],
  activeOrdinal: number,
  offset: number,
): number {
  if (!segments.length) return 0

  let completed = 0
  let activeWeight = 0
  let total = 0
  for (const segment of segments) {
    const weight = readerSegmentWeight(segment)
    total += weight
    if (segment.ordinal < activeOrdinal) completed += weight
    if (segment.ordinal === activeOrdinal) activeWeight = weight
  }
  if (total <= 0 || activeWeight <= 0) return 0
  return clampReaderPercent(
    (completed + activeWeight * clampReaderOffset(offset)) / total,
  )
}

export function captureReaderScrollPosition(
  segments: readonly ReaderStorySegment[],
  layouts: readonly ReaderSegmentLayout[],
  viewport: ReaderViewport,
): ReaderScrollPosition | null {
  const readingLine = readerReadingLine(viewport)
  const layout = activeReaderSegmentLayout(layouts, readingLine)
  if (!layout) return null
  const segment =
    segments.find((candidate) => candidate.ordinal === layout.ordinal) ?? null
  if (!segment) return null

  const offset = readerSegmentOffset(layout, readingLine)
  return {
    locator: createReaderLocatorV2(segment, offset),
    percent: readerWeightedPercent(segments, segment.ordinal, offset),
  }
}

export function findReaderResumeSegment(
  segments: readonly ReaderStorySegment[],
  locator: ReaderLocatorV2,
): ReaderResumeMatch | null {
  const identity = segments.find(
    (segment) =>
      segment.contentKey === locator.segment.key &&
      segment.contentOccurrence === locator.segment.occurrence,
  )
  if (identity) {
    return identity.ordinal === locator.segment.ordinal
      ? { segment: identity, matchedBy: 'identity' }
      : null
  }

  const ordinal =
    segments.find((segment) => segment.ordinal === locator.segment.ordinal) ?? null
  return ordinal ? { segment: ordinal, matchedBy: 'ordinal' } : null
}

export function readerRestoreScrollTop({
  elementTop,
  elementHeight,
  currentScrollTop,
  readingLine,
  offset,
  maximumScrollTop = Number.POSITIVE_INFINITY,
}: {
  elementTop: number
  elementHeight: number
  currentScrollTop: number
  readingLine: number
  offset: number
  maximumScrollTop?: number
}): number {
  const target =
    currentScrollTop +
    elementTop +
    Math.max(0, elementHeight) * clampReaderOffset(offset) -
    readingLine
  const maximum = Number.isFinite(maximumScrollTop)
    ? Math.max(0, maximumScrollTop)
    : Number.POSITIVE_INFINITY
  return Math.min(maximum, Math.max(0, target))
}

export function readerScrollBehavior(
  reducedMotion: boolean,
): ScrollBehavior {
  return reducedMotion ? 'auto' : 'smooth'
}
