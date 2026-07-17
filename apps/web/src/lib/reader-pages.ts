import type { ReaderStorySegment } from './reader-locator-v2'

export type ReaderPage = {
  index: number
  startOrdinal: number
  endOrdinal: number
  segments: ReaderStorySegment[]
}

// Paged mode remains a transitional two-segment representation. The next
// Reader roadmap PR owns deterministic pagination and reflow.
export function buildTransitionalReaderPages(
  segments: readonly ReaderStorySegment[],
): ReaderPage[] {
  const pages: ReaderPage[] = []
  for (let index = 0; index < segments.length; index += 2) {
    const pageSegments = segments.slice(index, index + 2)
    const first = pageSegments[0]
    const last = pageSegments.at(-1)
    if (!first || !last) continue
    pages.push({
      index: pages.length,
      startOrdinal: first.ordinal,
      endOrdinal: last.ordinal,
      segments: pageSegments,
    })
  }
  return pages
}

export function readerPageForOrdinal(
  pages: readonly ReaderPage[],
  ordinal: number,
): number {
  return pages.findIndex(
    (page) => ordinal >= page.startOrdinal && ordinal <= page.endOrdinal,
  )
}
