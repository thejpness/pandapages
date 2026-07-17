import type { ReaderStorySegment } from './reader-locator-v2'

export type ReaderChapter = {
  key: string
  occurrence: number
  ordinal: number
  title: string
}

function decodeEntity(entity: string): string {
  const numeric = entity.match(/^&#(\d+);$/)
  if (numeric) return String.fromCodePoint(Number(numeric[1]))
  const hex = entity.match(/^&#x([0-9a-f]+);$/i)
  if (hex) return String.fromCodePoint(Number.parseInt(hex[1], 16))
  return ({ '&amp;': '&', '&lt;': '<', '&gt;': '>', '&quot;': '"', '&#39;': "'", '&nbsp;': ' ' }[entity] ?? entity)
}

export function readerHeadingText(renderedHtml: string): string {
  if (typeof DOMParser !== 'undefined') {
    const parsed = new DOMParser().parseFromString(renderedHtml, 'text/html')
    return (parsed.body.textContent ?? '').replace(/\s+/g, ' ').trim()
  }
  return renderedHtml
    .replace(/<[^>]*>/g, '')
    .replace(/&(?:#\d+|#x[0-9a-f]+|amp|lt|gt|quot|nbsp|#39);/gi, decodeEntity)
    .replace(/\s+/g, ' ')
    .trim()
}

export function buildReaderChapters(
  segments: readonly ReaderStorySegment[],
): ReaderChapter[] {
  return segments.flatMap((segment) => {
    if (
      segment.kind !== 'heading' ||
      segment.headingLevel !== 2 ||
      segment.chapterKey === null ||
      segment.chapterOccurrence === null
    ) return []
    return [{
      key: segment.chapterKey,
      occurrence: segment.chapterOccurrence,
      ordinal: segment.ordinal,
      title: readerHeadingText(segment.renderedHtml) || 'Chapter',
    }]
  })
}

export function readerChapterAccessibleLabel(
  chapters: readonly ReaderChapter[],
  chapter: ReaderChapter,
): string {
  const matching = chapters.filter((candidate) => candidate.title === chapter.title)
  if (matching.length < 2) return chapter.title
  const position = matching.findIndex(
    (candidate) =>
      candidate.key === chapter.key &&
      candidate.occurrence === chapter.occurrence,
  )
  return chapter.title + ', ' + (Math.max(0, position) + 1) + ' of ' + matching.length
}

export function currentReaderChapter(
  chapters: readonly ReaderChapter[],
  segment: ReaderStorySegment | null,
): ReaderChapter | null {
  if (!segment) return null
  if (segment.chapterKey !== null && segment.chapterOccurrence !== null) {
    const exact = chapters.find(
      (chapter) =>
        chapter.key === segment.chapterKey &&
        chapter.occurrence === segment.chapterOccurrence,
    )
    if (exact) return exact
  }
  let current: ReaderChapter | null = null
  for (const chapter of chapters) {
    if (chapter.ordinal > segment.ordinal) break
    current = chapter
  }
  return current
}
