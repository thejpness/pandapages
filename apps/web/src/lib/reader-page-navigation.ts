export type ReaderPageNavigationInput = {
  key: string
  currentPage: number
  pageCount: number
  modalOpen?: boolean
  defaultPrevented?: boolean
  altKey?: boolean
  ctrlKey?: boolean
  metaKey?: boolean
  shiftKey?: boolean
  targetTagName?: string | null
  targetIsContentEditable?: boolean
  targetIsInteractive?: boolean
}

const interactiveTags = new Set([
  'A',
  'BUTTON',
  'INPUT',
  'OPTION',
  'SELECT',
  'SUMMARY',
  'TEXTAREA',
])

function targetIsInteractive(input: ReaderPageNavigationInput): boolean {
  if (input.targetIsInteractive || input.targetIsContentEditable) return true
  const tagName = input.targetTagName?.toUpperCase()
  return tagName !== undefined && interactiveTags.has(tagName)
}

function boundedPage(value: number, pageCount: number): number {
  const finite = Number.isFinite(value) ? Math.trunc(value) : 0
  return Math.max(0, Math.min(pageCount - 1, finite))
}

/**
 * Returns the destination page for an accepted Reader keyboard command.
 * A null result means the browser/event owner keeps the key, including boundaries.
 */
export function readerPageNavigationTarget(
  input: ReaderPageNavigationInput,
): number | null {
  const pageCount =
    Number.isFinite(input.pageCount) && input.pageCount > 0
      ? Math.trunc(input.pageCount)
      : 0
  if (
    pageCount === 0 ||
    input.modalOpen ||
    input.defaultPrevented ||
    input.altKey ||
    input.ctrlKey ||
    input.metaKey ||
    input.shiftKey ||
    targetIsInteractive(input)
  ) {
    return null
  }

  const currentPage = boundedPage(input.currentPage, pageCount)
  let target: number
  switch (input.key) {
    case 'ArrowRight':
    case 'PageDown':
      target = currentPage + 1
      break
    case 'ArrowLeft':
    case 'PageUp':
      target = currentPage - 1
      break
    case 'Home':
      target = 0
      break
    case 'End':
      target = pageCount - 1
      break
    default:
      return null
  }

  if (target < 0 || target >= pageCount || target === currentPage) return null
  return target
}
