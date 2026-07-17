export type ReaderSegmentKind = 'heading' | 'paragraph' | 'other'

export type ReaderStorySegment = {
  ordinal: number
  kind: ReaderSegmentKind
  headingLevel: number | null
  contentKey: string
  contentOccurrence: number
  chapterKey: string | null
  chapterOccurrence: number | null
  renderedHtml: string
  wordCount: number
}

export type ReaderLocatorV2 = {
  schema: 2
  segment: {
    key: string
    occurrence: number
    ordinal: number
    offset: number
  }
  chapter?: {
    key: string
    occurrence: number
  }
}

export type ReaderSegmentLayout = {
  ordinal: number
  top: number
  bottom: number
}

const contentKeyPattern = /^[0-9a-f]{64}$/

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function hasExactKeys(
  record: Record<string, unknown>,
  required: readonly string[],
  optional: readonly string[] = [],
): boolean {
  const allowed = new Set([...required, ...optional])
  return (
    required.every((key) => Object.hasOwn(record, key)) &&
    Object.keys(record).every((key) => allowed.has(key))
  )
}

function isPositiveInteger(value: unknown): value is number {
  return Number.isInteger(value) && Number(value) >= 1
}

export function isReaderContentKey(value: unknown): value is string {
  return typeof value === 'string' && contentKeyPattern.test(value)
}

export function clampReaderOffset(value: number): number {
  if (!Number.isFinite(value)) return 0
  return Math.max(0, Math.min(1, value))
}

export function parseReaderLocatorV2(value: unknown): ReaderLocatorV2 {
  if (
    !isRecord(value) ||
    !hasExactKeys(value, ['schema', 'segment'], ['chapter']) ||
    value.schema !== 2 ||
    !isRecord(value.segment) ||
    !hasExactKeys(value.segment, [
      'key',
      'occurrence',
      'ordinal',
      'offset',
    ]) ||
    !isReaderContentKey(value.segment.key) ||
    !isPositiveInteger(value.segment.occurrence) ||
    !isPositiveInteger(value.segment.ordinal) ||
    typeof value.segment.offset !== 'number' ||
    !Number.isFinite(value.segment.offset) ||
    value.segment.offset < 0 ||
    value.segment.offset > 1
  ) {
    throw new Error('Invalid Reader Locator v2')
  }

  const locator: ReaderLocatorV2 = {
    schema: 2,
    segment: {
      key: value.segment.key,
      occurrence: value.segment.occurrence,
      ordinal: value.segment.ordinal,
      offset: value.segment.offset,
    },
  }

  if (Object.hasOwn(value, 'chapter')) {
    if (
      !isRecord(value.chapter) ||
      !hasExactKeys(value.chapter, ['key', 'occurrence']) ||
      !isReaderContentKey(value.chapter.key) ||
      !isPositiveInteger(value.chapter.occurrence)
    ) {
      throw new Error('Invalid Reader Locator v2 chapter')
    }
    locator.chapter = {
      key: value.chapter.key,
      occurrence: value.chapter.occurrence,
    }
  }

  return locator
}

export function createReaderLocatorV2(
  segment: ReaderStorySegment,
  offset: number,
): ReaderLocatorV2 {
  const locator: ReaderLocatorV2 = {
    schema: 2,
    segment: {
      key: segment.contentKey,
      occurrence: segment.contentOccurrence,
      ordinal: segment.ordinal,
      offset: clampReaderOffset(offset),
    },
  }
  if (segment.chapterKey !== null && segment.chapterOccurrence !== null) {
    locator.chapter = {
      key: segment.chapterKey,
      occurrence: segment.chapterOccurrence,
    }
  }
  return locator
}

export function findReaderSegment(
  segments: readonly ReaderStorySegment[],
  locator: ReaderLocatorV2,
): ReaderStorySegment | null {
  const byIdentity = segments.find(
    (segment) =>
      segment.contentKey === locator.segment.key &&
      segment.contentOccurrence === locator.segment.occurrence,
  )
  if (byIdentity) return byIdentity
  return (
    segments.find((segment) => segment.ordinal === locator.segment.ordinal) ??
    null
  )
}

export function captureScrollReaderLocator(
  segments: readonly ReaderStorySegment[],
  layouts: readonly ReaderSegmentLayout[],
  viewportHeight: number,
  readingLineRatio = 0.35,
): ReaderLocatorV2 | null {
  if (!segments.length || !layouts.length) return null
  const readingLine = Math.max(0, viewportHeight) * clampReaderOffset(readingLineRatio)
  const ordered = [...layouts].sort((left, right) => left.ordinal - right.ordinal)
  const containing =
    ordered.find(
      (layout) => layout.top <= readingLine && layout.bottom >= readingLine,
    ) ??
    ordered.find((layout) => layout.top > readingLine) ??
    ordered.at(-1)
  if (!containing) return null
  const segment =
    segments.find((candidate) => candidate.ordinal === containing.ordinal) ??
    null
  if (!segment) return null

  const height = containing.bottom - containing.top
  const offset =
    height > 0 ? (readingLine - containing.top) / height : 0
  return createReaderLocatorV2(segment, offset)
}

export function capturePagedReaderLocator(
  segments: readonly ReaderStorySegment[],
  startOrdinal: number,
): ReaderLocatorV2 | null {
  const segment =
    segments.find((candidate) => candidate.ordinal === startOrdinal) ?? null
  return segment ? createReaderLocatorV2(segment, 0) : null
}

// A representation change can deliver the final programmatic scroll event on
// the frame after the restore call. Waiting that extra frame lets Reader keep
// capture suppressed until the preserved anchor has settled.
export async function settleProgrammaticReaderRestore(
  restore: () => void,
  scheduleFrame: (callback: () => void) => unknown = (callback) =>
    requestAnimationFrame(() => callback()),
): Promise<void> {
  restore()
  await new Promise<void>((resolve) => {
    scheduleFrame(() => {
      restore()
      resolve()
    })
  })
  await new Promise<void>((resolve) => {
    scheduleFrame(resolve)
  })
}
