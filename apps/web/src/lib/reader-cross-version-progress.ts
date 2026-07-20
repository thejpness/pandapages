import {
  createReaderLocatorV2,
  isReaderContentKey,
  parseReaderLocatorV2,
  type ReaderLocatorV2,
  type ReaderStorySegment,
} from './reader-locator-v2'

export type CrossVersionMapping =
  | {
      kind: 'segment'
      confidence: 'high'
      locator: ReaderLocatorV2
      percent: number
    }
  | {
      kind: 'chapter'
      confidence: 'medium'
      locator: ReaderLocatorV2
      percent: number
    }
  | {
      kind: 'percentage'
      confidence: 'low'
      locator: ReaderLocatorV2
      percent: number
    }
  | {
      kind: 'none'
      confidence: 'none'
    }

export type CrossVersionMappingInput = {
  oldVersion: number
  oldLocator: unknown
  oldPercent: number
  currentVersion: number
  currentSegments: readonly ReaderStorySegment[]
}

const minimumSegmentWeight = 1

function isPositiveInteger(value: unknown): value is number {
  return Number.isInteger(value) && Number(value) >= 1
}

function validCurrentSegments(
  segments: readonly ReaderStorySegment[],
): boolean {
  if (segments.length === 0) return false

  let previousOrdinal = 0
  for (const segment of segments) {
    const chapterAbsent =
      segment.chapterKey === null && segment.chapterOccurrence === null
    const chapterValid =
      isReaderContentKey(segment.chapterKey) &&
      isPositiveInteger(segment.chapterOccurrence)
    const headingValid =
      segment.kind === 'heading'
        ? Number.isInteger(segment.headingLevel) &&
          Number(segment.headingLevel) >= 1 &&
          Number(segment.headingLevel) <= 6
        : segment.headingLevel === null

    if (
      !isPositiveInteger(segment.ordinal) ||
      segment.ordinal <= previousOrdinal ||
      !['heading', 'paragraph', 'other'].includes(segment.kind) ||
      !headingValid ||
      !isReaderContentKey(segment.contentKey) ||
      !isPositiveInteger(segment.contentOccurrence) ||
      (!chapterAbsent && !chapterValid) ||
      typeof segment.renderedHtml !== 'string' ||
      !Number.isInteger(segment.wordCount) ||
      segment.wordCount < 0
    ) {
      return false
    }
    previousOrdinal = segment.ordinal
  }
  return true
}

function segmentWeight(segment: ReaderStorySegment): number {
  return Math.max(minimumSegmentWeight, segment.wordCount)
}

function weightedPercent(
  segments: readonly ReaderStorySegment[],
  target: ReaderStorySegment,
  offset: number,
): number {
  let completed = 0
  let total = 0
  for (const segment of segments) {
    const weight = segmentWeight(segment)
    total += weight
    if (segment.ordinal < target.ordinal) completed += weight
  }
  const boundedOffset = Math.max(0, Math.min(1, offset))
  return Math.max(
    0,
    Math.min(1, (completed + segmentWeight(target) * boundedOffset) / total),
  )
}

function sameChapter(
  segment: ReaderStorySegment,
  locator: ReaderLocatorV2,
): boolean {
  if (!locator.chapter) {
    return segment.chapterKey === null && segment.chapterOccurrence === null
  }
  return (
    segment.chapterKey === locator.chapter.key &&
    segment.chapterOccurrence === locator.chapter.occurrence
  )
}

function percentageTarget(
  segments: readonly ReaderStorySegment[],
  percent: number,
): { segment: ReaderStorySegment; offset: number } | null {
  if (!Number.isFinite(percent) || percent < 0 || percent > 1) return null
  const first = segments[0]
  const last = segments.at(-1)
  if (!first || !last) return null
  if (percent === 0) return { segment: first, offset: 0 }
  if (percent === 1) return { segment: last, offset: 1 }

  const total = segments.reduce(
    (sum, segment) => sum + segmentWeight(segment),
    0,
  )
  const targetWeight = total * percent
  let completed = 0
  for (const segment of segments) {
    const weight = segmentWeight(segment)
    const next = completed + weight
    // A target exactly on a segment boundary belongs to the next segment at
    // offset zero. That keeps boundary inversion canonical and deterministic.
    if (targetWeight < next) {
      return {
        segment,
        offset: Math.max(0, Math.min(1, (targetWeight - completed) / weight)),
      }
    }
    completed = next
  }
  return { segment: last, offset: 1 }
}

export function mapReaderProgressAcrossVersions({
  oldVersion,
  oldLocator,
  oldPercent,
  currentVersion,
  currentSegments,
}: CrossVersionMappingInput): CrossVersionMapping {
  if (
    !isPositiveInteger(oldVersion) ||
    !isPositiveInteger(currentVersion) ||
    oldVersion >= currentVersion ||
    !validCurrentSegments(currentSegments)
  ) {
    return { kind: 'none', confidence: 'none' }
  }

  let locator: ReaderLocatorV2
  try {
    locator = parseReaderLocatorV2(oldLocator)
  } catch {
    return { kind: 'none', confidence: 'none' }
  }

  const exactSegment = currentSegments.find(
    (segment) =>
      segment.contentKey === locator.segment.key &&
      segment.contentOccurrence === locator.segment.occurrence &&
      sameChapter(segment, locator),
  )
  if (exactSegment) {
    const mappedLocator = createReaderLocatorV2(
      exactSegment,
      locator.segment.offset,
    )
    return {
      kind: 'segment',
      confidence: 'high',
      locator: mappedLocator,
      percent: weightedPercent(
        currentSegments,
        exactSegment,
        mappedLocator.segment.offset,
      ),
    }
  }

  if (locator.chapter) {
    const chapterHeading = currentSegments.find(
      (segment) =>
        segment.kind === 'heading' &&
        segment.headingLevel === 2 &&
        segment.chapterKey === locator.chapter?.key &&
        segment.chapterOccurrence === locator.chapter?.occurrence,
    )
    if (chapterHeading) {
      const mappedLocator = createReaderLocatorV2(chapterHeading, 0)
      return {
        kind: 'chapter',
        confidence: 'medium',
        locator: mappedLocator,
        percent: weightedPercent(currentSegments, chapterHeading, 0),
      }
    }
  }

  const approximate = percentageTarget(currentSegments, oldPercent)
  if (!approximate) return { kind: 'none', confidence: 'none' }
  const mappedLocator = createReaderLocatorV2(
    approximate.segment,
    approximate.offset,
  )
  return {
    kind: 'percentage',
    confidence: 'low',
    locator: mappedLocator,
    percent: weightedPercent(
      currentSegments,
      approximate.segment,
      approximate.offset,
    ),
  }
}
