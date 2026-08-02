declare const safeRenderedStoryHTMLBrand: unique symbol

// This opaque type marks HTML that crossed the API canonical story-rendering
// contract. Only API response parsers may construct it; Vue v-html sinks must
// not accept arbitrary caller-provided strings.
export type SafeRenderedStoryHTML = string & {
  readonly [safeRenderedStoryHTMLBrand]: 'Panda Pages canonical story HTML'
}

const safeStoryElements = new Set([
  'p',
  'h1',
  'h2',
  'h3',
  'h4',
  'h5',
  'h6',
  'em',
  'strong',
  'blockquote',
  'ul',
  'ol',
  'li',
  'hr',
  'br',
  'pre',
  'code',
  'a',
])

function isCanonicalHeadingID(value: string): boolean {
  return (
    /^[A-Za-z]/.test(value) &&
    [...value].every((character) => /[A-Za-z0-9_-]/.test(character))
  )
}

function isSafeStoryLink(value: string): boolean {
  if (value.startsWith('//')) return false
  try {
    const url = new URL(value, window.location.origin)
    if (
      value === '/' ||
      value.startsWith('/') ||
      value.startsWith('./') ||
      value.startsWith('../') ||
      value.startsWith('?') ||
      value.startsWith('#')
    ) {
      return url.origin === window.location.origin
    }
    return (
      (url.protocol === 'http:' || url.protocol === 'https:') &&
      /^https?:\/\//i.test(value)
    )
  } catch {
    return false
  }
}

function isSafeRenderedStoryHTML(value: string): boolean {
  if (typeof document === 'undefined') {
    // Panda Pages is a browser application: the detached-fragment branch below
    // is the security boundary. This conservative fallback exists only so the
    // Node API-shape tests can exercise their surrounding synchronous parsers;
    // browser tests own the executable HTML validation contract.
    if (/<!--|<!doctype|<\?|<\s*\/?\s*(?:html|head|body|script|style|title|iframe|object|embed|form|input|button|textarea|select|link|meta|base|svg|math)\b/iu.test(value)) {
      return false
    }
    for (const match of value.matchAll(/<\s*\/?\s*([a-z][a-z0-9-]*)\b/giu)) {
      const name = match[1]
      if (!name || !safeStoryElements.has(name.toLowerCase())) return false
    }
    return !/(on[a-z]+\s*=|(?:javascript|vbscript|data)\s*:|srcdoc\s*=|style\s*=|href\s*=\s*["']?\s*\/\/)/iu.test(value)
  }

  const context = document.createElement('div')
  const range = document.createRange()
  range.selectNodeContents(context)
  const fragment = range.createContextualFragment(value)
  const validate = (node: Node): boolean => {
    if (node.nodeType === Node.TEXT_NODE) return true
    if (node.nodeType !== Node.ELEMENT_NODE) return false
    const element = node as HTMLElement
    if (element.namespaceURI !== 'http://www.w3.org/1999/xhtml') return false
    const name = element.localName.toLowerCase()
    if (!safeStoryElements.has(name)) return false
    for (const attribute of element.attributes) {
      if (attribute.namespaceURI !== null) return false
      const attributeName = attribute.name.toLowerCase()
      if (/^h[1-6]$/.test(name) && attributeName === 'id' && isCanonicalHeadingID(attribute.value)) continue
      if (name === 'a' && attributeName === 'href' && isSafeStoryLink(attribute.value)) continue
      if (name === 'a' && attributeName === 'rel') {
        const tokens = attribute.value.split(/\s+/).filter(Boolean)
        if (tokens.length > 0 && tokens.every((token) => token === 'nofollow' || token === 'noreferrer')) continue
      }
      return false
    }
    return [...node.childNodes].every(validate)
  }
  return [...fragment.childNodes].every(validate)
}

export function parseSafeRenderedStoryHTML(value: unknown): SafeRenderedStoryHTML {
  if (typeof value !== 'string' || !isSafeRenderedStoryHTML(value)) {
    throw new Error('Invalid canonical story HTML')
  }
  return value as SafeRenderedStoryHTML
}

export type ReaderSegmentKind = 'heading' | 'paragraph' | 'other'

export type ReaderStorySegment = {
  ordinal: number
  kind: ReaderSegmentKind
  headingLevel: number | null
  contentKey: string
  contentOccurrence: number
  chapterKey: string | null
  chapterOccurrence: number | null
  renderedHtml: SafeRenderedStoryHTML
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
  if (byIdentity) {
    return byIdentity.ordinal === locator.segment.ordinal ? byIdentity : null
  }
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
