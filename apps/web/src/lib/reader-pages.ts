import {
  clampReaderOffset,
  createReaderLocatorV2,
  type ReaderLocatorV2,
  type ReaderStorySegment,
} from './reader-locator-v2'

export type ReaderPageMetrics = {
  fontSize: number
  lineHeight: number
  contentWidth: number
  availableHeight: number
}

export type ReaderPageCapacity = {
  usableWidth: number
  usableHeight: number
  fontSize: number
  lineHeight: number
  lineHeightPixels: number
  averageCharacterWidth: number
  charactersPerLine: number
  wordsPerLine: number
  capacityLines: number
}

export type ReaderPage = {
  index: number
  segments: ReaderStorySegment[]
  startOrdinal: number
  endOrdinal: number
  estimatedLines: number
  capacityLines: number
  oversized: boolean
}

type ReaderOverflowGeometry = {
  scrollHeight: number
  clientHeight: number
}

const DEFAULT_FONT_SIZE = 20
const DEFAULT_LINE_HEIGHT = 1.65
const DEFAULT_CONTENT_WIDTH = 720
const DEFAULT_AVAILABLE_HEIGHT = 640
const AVERAGE_CHARACTER_EM = 0.52
const AVERAGE_WORD_CHARACTERS_WITH_SPACE = 6
const MINIMUM_CHARACTERS_PER_LINE = 8

function positiveOr(value: number, fallback: number): number {
  return Number.isFinite(value) && value > 0 ? value : fallback
}

/**
 * Deterministic capacity model:
 *
 * usable width / average glyph width (0.52em) -> characters per line
 * characters / six characters per average word -> words per line
 * usable height / configured line box -> capacity in text lines
 *
 * DOM measurements may choose the usable box supplied here, but identical
 * metrics always produce identical capacity and page assignment.
 */
export function readerPageCapacity(
  metrics: ReaderPageMetrics,
): ReaderPageCapacity {
  const fontSize = positiveOr(metrics.fontSize, DEFAULT_FONT_SIZE)
  const lineHeight = positiveOr(metrics.lineHeight, DEFAULT_LINE_HEIGHT)
  const usableWidth = positiveOr(metrics.contentWidth, DEFAULT_CONTENT_WIDTH)
  const usableHeight = positiveOr(
    metrics.availableHeight,
    DEFAULT_AVAILABLE_HEIGHT,
  )
  const lineHeightPixels = fontSize * lineHeight
  const averageCharacterWidth = fontSize * AVERAGE_CHARACTER_EM
  const charactersPerLine = Math.max(
    MINIMUM_CHARACTERS_PER_LINE,
    Math.floor(usableWidth / averageCharacterWidth),
  )
  const wordsPerLine = Math.max(
    1,
    Math.floor(charactersPerLine / AVERAGE_WORD_CHARACTERS_WITH_SPACE),
  )
  const capacityLines = Math.max(1, Math.floor(usableHeight / lineHeightPixels))

  return {
    usableWidth,
    usableHeight,
    fontSize,
    lineHeight,
    lineHeightPixels,
    averageCharacterWidth,
    charactersPerLine,
    wordsPerLine,
    capacityLines,
  }
}

function safeWordCount(segment: ReaderStorySegment): number {
  return Number.isFinite(segment.wordCount) && segment.wordCount > 0
    ? segment.wordCount
    : 1
}

function headingScale(level: number | null): number {
  if (level === 1) return 1.75
  if (level === 2) return 1.42
  if (level === 3) return 1.22
  return 1.12
}

function headingSpacing(level: number | null): number {
  if (level === 1) return 2.25
  if (level === 2) return 2
  return 1.5
}

function readerSegmentEstimatedLines(
  segment: ReaderStorySegment,
  capacity: ReaderPageCapacity,
): number {
  const words = safeWordCount(segment)
  if (segment.kind === 'heading') {
    const scale = headingScale(segment.headingLevel)
    const headingWordsPerLine = Math.max(1, capacity.wordsPerLine / scale)
    const textLines = Math.ceil(words / headingWordsPerLine)
    return Math.max(
      1,
      Math.ceil(textLines * 1.12 + headingSpacing(segment.headingLevel)),
    )
  }

  const textLines = Math.max(1, Math.ceil(words / capacity.wordsPerLine))
  // Every coherent segment is a block. Reserving at least one line for block
  // separation prevents many tiny paragraphs from being packed unrealistically.
  return textLines + (segment.kind === 'other' ? 2 : 1)
}

function isHeading(segment: ReaderStorySegment): boolean {
  return segment.kind === 'heading'
}

function rebalanceTinyFinalPage(
  pages: ReaderPage[],
  estimates: ReadonlyMap<ReaderStorySegment, number>,
  capacityLines: number,
): void {
  if (pages.length < 2) return
  const previous = pages.at(-2)
  const final = pages.at(-1)
  if (!previous || !final || previous.oversized || final.oversized) return

  const minimumUsefulLines = Math.max(2, Math.ceil(capacityLines * 0.35))
  while (
    final.estimatedLines < minimumUsefulLines &&
    previous.segments.length > 1
  ) {
    const candidate = previous.segments.at(-1)
    const beforeCandidate = previous.segments.at(-2)
    const finalFirst = final.segments[0]
    if (!candidate || !beforeCandidate || !finalFirst) break

    const candidateLines = estimates.get(candidate)
    if (candidateLines === undefined) break
    const nextPreviousLines = previous.estimatedLines - candidateLines
    const nextFinalLines = final.estimatedLines + candidateLines
    if (
      nextPreviousLines < minimumUsefulLines ||
      nextFinalLines > capacityLines ||
      isHeading(beforeCandidate) ||
      (isHeading(finalFirst) && !isHeading(candidate))
    ) {
      break
    }

    previous.segments = previous.segments.slice(0, -1)
    final.segments = [candidate, ...final.segments]
    previous.estimatedLines = nextPreviousLines
    final.estimatedLines = nextFinalLines

    const previousFirst = previous.segments[0]
    const previousLast = previous.segments.at(-1)
    const finalLast = final.segments.at(-1)
    if (!previousFirst || !previousLast || !finalLast) break
    previous.startOrdinal = previousFirst.ordinal
    previous.endOrdinal = previousLast.ordinal
    final.startOrdinal = candidate.ordinal
    final.endOrdinal = finalLast.ordinal
  }
}

export function buildReaderPages(
  segments: readonly ReaderStorySegment[],
  metrics: ReaderPageMetrics,
): ReaderPage[] {
  if (segments.length === 0) return []

  const capacity = readerPageCapacity(metrics)
  const estimates = segments.map((segment) =>
    readerSegmentEstimatedLines(segment, capacity),
  )
  const estimatesBySegment = new Map<ReaderStorySegment, number>(
    segments.map((segment, index) => [segment, estimates[index] ?? 1]),
  )
  const pages: ReaderPage[] = []
  let pageSegments: ReaderStorySegment[] = []
  let estimatedLines = 0

  const flush = (oversized = false) => {
    const first = pageSegments[0]
    const last = pageSegments.at(-1)
    if (!first || !last) return
    pages.push({
      index: pages.length,
      segments: pageSegments,
      startOrdinal: first.ordinal,
      endOrdinal: last.ordinal,
      estimatedLines,
      capacityLines: capacity.capacityLines,
      oversized,
    })
    pageSegments = []
    estimatedLines = 0
  }

  for (let index = 0; index < segments.length; index += 1) {
    const segment = segments[index]
    const segmentLines = estimates[index]
    if (!segment || segmentLines === undefined) continue

    if (segmentLines > capacity.capacityLines) {
      flush()
      pageSegments = [segment]
      estimatedLines = segmentLines
      flush(true)
      continue
    }

    const next = segments[index + 1]
    const nextLines = estimates[index + 1]
    const headingCanTravel =
      isHeading(segment) &&
      next !== undefined &&
      nextLines !== undefined &&
      nextLines <= capacity.capacityLines &&
      segmentLines + nextLines <= capacity.capacityLines

    // If a short heading and its following block fit together, start them on a
    // fresh page rather than orphaning the heading at the end of this one.
    if (
      pageSegments.length > 0 &&
      headingCanTravel &&
      estimatedLines + segmentLines + nextLines > capacity.capacityLines
    ) {
      flush()
    }

    if (
      pageSegments.length > 0 &&
      estimatedLines + segmentLines > capacity.capacityLines
    ) {
      flush()
    }

    pageSegments.push(segment)
    estimatedLines += segmentLines
  }
  flush()
  rebalanceTinyFinalPage(pages, estimatesBySegment, capacity.capacityLines)

  return pages
}

export function readerPageForOrdinal(
  pages: readonly ReaderPage[],
  ordinal: number,
): number {
  return pages.findIndex((page) =>
    page.segments.some((segment) => segment.ordinal === ordinal),
  )
}

export function readerPageForLocator(
  pages: readonly ReaderPage[],
  segments: readonly ReaderStorySegment[],
  locator: ReaderLocatorV2,
): number {
  const identity = segments.find(
    (segment) =>
      segment.contentKey === locator.segment.key &&
      segment.contentOccurrence === locator.segment.occurrence,
  )
  if (identity) {
    return identity.ordinal === locator.segment.ordinal
      ? readerPageForOrdinal(pages, identity.ordinal)
      : -1
  }
  return readerPageForOrdinal(pages, locator.segment.ordinal)
}

export function readerPageRepresentativeLocator(
  page: ReaderPage | null | undefined,
  oversizedOffset = 0,
): ReaderLocatorV2 | null {
  const segment = page?.segments[0]
  if (!segment) return null
  return createReaderLocatorV2(
    segment,
    page.oversized ? oversizedOffset : 0,
  )
}

function readerOverflowMaximum({
  scrollHeight,
  clientHeight,
}: ReaderOverflowGeometry): number {
  const content = Number.isFinite(scrollHeight) ? Math.max(0, scrollHeight) : 0
  const viewport = Number.isFinite(clientHeight) ? Math.max(0, clientHeight) : 0
  return Math.max(0, content - viewport)
}

export function readerOversizedOffset({
  scrollTop,
  scrollHeight,
  clientHeight,
}: ReaderOverflowGeometry & { scrollTop: number }): number {
  const maximum = readerOverflowMaximum({ scrollHeight, clientHeight })
  if (maximum <= 0) return 0
  const current = Number.isFinite(scrollTop) ? scrollTop : 0
  return clampReaderOffset(current / maximum)
}

export function readerOversizedScrollTop({
  offset,
  scrollHeight,
  clientHeight,
}: ReaderOverflowGeometry & { offset: number }): number {
  return (
    readerOverflowMaximum({ scrollHeight, clientHeight }) *
    clampReaderOffset(offset)
  )
}
