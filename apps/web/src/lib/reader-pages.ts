import {
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

export type ReaderPageBuildOptions = {
  forcedOversizedSegmentIdentities?: ReadonlySet<string>
}

export type ReaderSegmentTextWorkload = {
  visibleCharacters: number
  longestUnbrokenCharacters: number
  weightedCharacters: number
}

const DEFAULT_FONT_SIZE = 20
const DEFAULT_LINE_HEIGHT = 1.65
const DEFAULT_CONTENT_WIDTH = 720
const DEFAULT_AVAILABLE_HEIGHT = 640
const AVERAGE_CHARACTER_EM = 0.52
const AVERAGE_WORD_CHARACTERS_WITH_SPACE = 6
const MINIMUM_CHARACTERS_PER_LINE = 8
const LONG_UNBROKEN_RUN_MULTIPLIER = 1.08
const WIDE_CHARACTER_MULTIPLIER = 2
const WHITESPACE_ENTITIES = new Set([
  'nbsp',
  'ensp',
  'emsp',
  'thinsp',
  'hairsp',
  'numsp',
  'puncsp',
  'tab',
  'newline',
])
const KNOWN_ENTITIES = new Map([
  ['amp', '&'],
  ['apos', "'"],
  ['gt', '>'],
  ['lt', '<'],
  ['quot', '"'],
])
const BLOCK_TAGS = new Set([
  'address',
  'article',
  'aside',
  'blockquote',
  'br',
  'dd',
  'div',
  'dl',
  'dt',
  'figcaption',
  'figure',
  'footer',
  'h1',
  'h2',
  'h3',
  'h4',
  'h5',
  'h6',
  'header',
  'hr',
  'li',
  'main',
  'nav',
  'ol',
  'p',
  'pre',
  'section',
  'table',
  'tbody',
  'td',
  'tfoot',
  'th',
  'thead',
  'tr',
  'ul',
])

function positiveOr(value: number, fallback: number): number {
  return Number.isFinite(value) && value > 0 ? value : fallback
}

function isWideCodePoint(codePoint: number): boolean {
  return (
    (codePoint >= 0x1100 && codePoint <= 0x11ff) ||
    (codePoint >= 0x2e80 && codePoint <= 0xa4cf) ||
    (codePoint >= 0xac00 && codePoint <= 0xd7af) ||
    (codePoint >= 0xf900 && codePoint <= 0xfaff) ||
    (codePoint >= 0xfe10 && codePoint <= 0xfe6f) ||
    (codePoint >= 0xff01 && codePoint <= 0xff60) ||
    (codePoint >= 0xffe0 && codePoint <= 0xffe6) ||
    (codePoint >= 0x1f000 && codePoint <= 0x1faff) ||
    (codePoint >= 0x20000 && codePoint <= 0x3ffff)
  )
}

function validCodePoint(value: number): boolean {
  return (
    Number.isInteger(value) &&
    value >= 0 &&
    value <= 0x10ffff &&
    (value < 0xd800 || value > 0xdfff)
  )
}

function decodedEntity(body: string): string | null {
  const lower = body.toLowerCase()
  if (WHITESPACE_ENTITIES.has(lower)) return ' '
  const known = KNOWN_ENTITIES.get(lower)
  if (known !== undefined) return known

  let codePoint: number | null = null
  if (/^#x[0-9a-f]+$/i.test(body)) {
    codePoint = Number.parseInt(body.slice(2), 16)
  } else if (/^#[0-9]+$/.test(body)) {
    codePoint = Number.parseInt(body.slice(1), 10)
  }
  if (codePoint !== null) {
    return validCodePoint(codePoint) ? String.fromCodePoint(codePoint) : '\ufffd'
  }

  // A syntactically complete named entity represents one rendered code point.
  return /^[a-z][a-z0-9]+$/i.test(body) ? 'x' : null
}

function tagEnd(renderedHtml: string, start: number): number {
  const first = renderedHtml[start + 1]
  if (!first || !/[a-z!/?]/i.test(first)) return -1
  if (renderedHtml.startsWith('<!--', start)) {
    const commentEnd = renderedHtml.indexOf('-->', start + 4)
    return commentEnd < 0 ? -1 : commentEnd + 2
  }

  let quote: '"' | "'" | null = null
  for (let index = start + 1; index < renderedHtml.length; index += 1) {
    const character = renderedHtml[index]
    if (quote !== null) {
      if (character === quote) quote = null
      continue
    }
    if (character === '"' || character === "'") {
      quote = character
      continue
    }
    if (character === '>') return index
  }
  return -1
}

function tagCreatesBoundary(tag: string): boolean {
  const name = /^<\s*\/?\s*([a-z0-9-]+)/i.exec(tag)?.[1]?.toLowerCase()
  return name !== undefined && BLOCK_TAGS.has(name)
}

/**
 * Returns a deterministic approximation of rendered text without using a DOM.
 * Tag syntax is ignored, collapsed whitespace consumes one character, complete
 * entities consume one Unicode code point, and wide CJK/emoji code points carry
 * a conservative two-character layout cost.
 */
export function readerSegmentTextWorkload(
  renderedHtml: string,
): ReaderSegmentTextWorkload {
  let visibleCharacters = 0
  let longestUnbrokenCharacters = 0
  let currentUnbrokenCharacters = 0
  let weightedCharacters = 0
  let previousWasWhitespace = false

  const consume = (character: string) => {
    const whitespace = /\s/u.test(character)
    if (whitespace && previousWasWhitespace) return
    previousWasWhitespace = whitespace
    visibleCharacters += 1
    const codePoint = character.codePointAt(0) ?? 0
    weightedCharacters += isWideCodePoint(codePoint)
      ? WIDE_CHARACTER_MULTIPLIER
      : 1
    if (whitespace) {
      currentUnbrokenCharacters = 0
      return
    }
    currentUnbrokenCharacters += 1
    longestUnbrokenCharacters = Math.max(
      longestUnbrokenCharacters,
      currentUnbrokenCharacters,
    )
  }

  for (let index = 0; index < renderedHtml.length; ) {
    if (renderedHtml[index] === '<') {
      const end = tagEnd(renderedHtml, index)
      if (end >= 0) {
        if (tagCreatesBoundary(renderedHtml.slice(index, end + 1))) {
          currentUnbrokenCharacters = 0
        }
        index = end + 1
        continue
      }
    }

    if (renderedHtml[index] === '&') {
      const semicolon = renderedHtml.indexOf(';', index + 1)
      if (semicolon > index && semicolon - index <= 34) {
        const entity = decodedEntity(
          renderedHtml.slice(index + 1, semicolon),
        )
        if (entity !== null) {
          consume(entity)
          index = semicolon + 1
          continue
        }
      }
    }

    const codePoint = renderedHtml.codePointAt(index)
    if (codePoint === undefined) break
    const character = String.fromCodePoint(codePoint)
    consume(character)
    index += character.length
  }

  return {
    visibleCharacters,
    longestUnbrokenCharacters,
    weightedCharacters,
  }
}

export function readerPageSegmentIdentity(
  segment: Pick<ReaderStorySegment, 'contentKey' | 'contentOccurrence'>,
): string {
  return segment.contentKey + ':' + segment.contentOccurrence
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
  const workload = readerSegmentTextWorkload(segment.renderedHtml)
  if (segment.kind === 'heading') {
    const scale = headingScale(segment.headingLevel)
    const headingWordsPerLine = Math.max(1, capacity.wordsPerLine / scale)
    const headingCharactersPerLine = Math.max(
      1,
      capacity.charactersPerLine / scale,
    )
    const textLines = Math.max(
      Math.ceil(words / headingWordsPerLine),
      Math.ceil(workload.visibleCharacters / headingCharactersPerLine),
      Math.ceil(
        (workload.longestUnbrokenCharacters *
          LONG_UNBROKEN_RUN_MULTIPLIER) /
          headingCharactersPerLine,
      ),
      Math.ceil(workload.weightedCharacters / headingCharactersPerLine),
    )
    return Math.max(
      1,
      Math.ceil(textLines * 1.12 + headingSpacing(segment.headingLevel)),
    )
  }

  const textLines = Math.max(
    1,
    Math.ceil(words / capacity.wordsPerLine),
    Math.ceil(workload.visibleCharacters / capacity.charactersPerLine),
    Math.ceil(
      (workload.longestUnbrokenCharacters *
        LONG_UNBROKEN_RUN_MULTIPLIER) /
        capacity.charactersPerLine,
    ),
    Math.ceil(workload.weightedCharacters / capacity.charactersPerLine),
  )
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
  options: ReaderPageBuildOptions = {},
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

    const forcedOversized =
      options.forcedOversizedSegmentIdentities?.has(
        readerPageSegmentIdentity(segment),
      ) ?? false
    if (forcedOversized || segmentLines > capacity.capacityLines) {
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
