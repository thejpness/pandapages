import type { LibraryProgress, LibraryStory } from './api'

export type { LibraryProgress, LibraryStory } from './api'

export type LibraryProgressKind =
  | 'not-started'
  | 'unavailable'
  | 'beginning'
  | 'in-progress'
  | 'completed'
  | 'updated'

export type LibraryCoverPattern = 'dots' | 'arches' | 'rays' | 'checks'

export type LibraryCoverPresentation = {
  background: string
  accent: string
  ink: string
  pattern: LibraryCoverPattern
  initials: string
}

export const LIBRARY_BEGINNING_PERCENT = 0.02
export const LIBRARY_COMPLETED_PERCENT = 0.98

type ProgressInput = LibraryStory | LibraryProgress | null

type ProgressDetails = {
  progress: LibraryProgress | null
  unavailable: boolean
}

const coverPalettes = [
  { background: '#e9dfc5', accent: '#1d6b58', ink: '#12110f' },
  { background: '#d8e4d2', accent: '#a53d35', ink: '#12110f' },
  { background: '#d9e4ed', accent: '#385f89', ink: '#12110f' },
  { background: '#efd6c5', accent: '#96582d', ink: '#12110f' },
  { background: '#ded7e8', accent: '#5c4d7d', ink: '#12110f' },
  { background: '#1d1d1b', accent: '#f4c95d', ink: '#fffefa' },
] as const

const coverPatterns: readonly LibraryCoverPattern[] = [
  'dots',
  'arches',
  'rays',
  'checks',
]

const numberFormatter = new Intl.NumberFormat('en-GB')

function progressDetails(input: ProgressInput): ProgressDetails {
  if (input === null) return { progress: null, unavailable: false }
  if ('progress' in input) {
    return {
      progress: input.progress,
      unavailable: input.progressAvailability === 'unavailable',
    }
  }
  return { progress: input, unavailable: false }
}

export function libraryDisplayPercent(input: ProgressInput): number {
  const { progress } = progressDetails(input)
  if (progress === null) return 0

  const percent = Number.isFinite(progress.percent) ? progress.percent : 0
  const rounded = Math.round(Math.max(0, Math.min(1, percent)) * 100)
  return classifyLibraryProgress(input) === 'in-progress'
    ? Math.min(97, rounded)
    : rounded
}

export function classifyLibraryProgress(
  input: ProgressInput,
): LibraryProgressKind {
  const { progress, unavailable } = progressDetails(input)
  if (unavailable) return 'unavailable'
  if (progress === null) return 'not-started'
  if (!progress.isCurrentVersion) return 'updated'
  if (progress.percent <= LIBRARY_BEGINNING_PERCENT) return 'beginning'
  if (progress.percent >= LIBRARY_COMPLETED_PERCENT) return 'completed'
  return 'in-progress'
}

export function libraryActionLabel(input: ProgressInput): string {
  const { progress } = progressDetails(input)
  const kind = classifyLibraryProgress(input)
  if (kind === 'in-progress' && progress !== null) {
    return `Continue at ${libraryDisplayPercent(input)}%`
  }
  if (kind === 'completed') return 'Read again'
  if (kind === 'updated') return 'Open updated story'
  return 'Read'
}

export function libraryProgressLabel(input: ProgressInput): string {
  const { progress } = progressDetails(input)
  const kind = classifyLibraryProgress(input)
  if (kind === 'unavailable') return 'Progress unavailable'
  if (kind === 'beginning') return 'At the beginning'
  if (kind === 'in-progress' && progress !== null) {
    return `${libraryDisplayPercent(input)}% read`
  }
  if (kind === 'completed') return 'Finished'
  if (kind === 'updated') return 'Story updated since you last read'
  return 'Not started'
}

function stableHash(value: string): number {
  let hash = 0x811c9dc5
  for (let index = 0; index < value.length; index += 1) {
    hash ^= value.charCodeAt(index)
    hash = Math.imul(hash, 0x01000193)
  }
  return hash >>> 0
}

function coverInitials(title: string): string {
  const words = title.trim().split(/\s+/u).filter(Boolean)
  if (words.length === 0) return 'PP'

  const first = Array.from(words[0] ?? '')
  const letters =
    words.length > 1
      ? [first[0], Array.from(words[1] ?? '')[0]]
      : first.slice(0, 2)
  const initials = letters.filter((letter) => letter !== undefined).join('')
  return initials.toLocaleUpperCase('en-GB') || 'PP'
}

export function libraryCoverPresentation(
  story: Pick<LibraryStory, 'slug' | 'title'>,
): LibraryCoverPresentation {
  const hash = stableHash(story.slug)
  const palette = coverPalettes[hash % coverPalettes.length] ?? coverPalettes[0]
  const pattern =
    coverPatterns[Math.floor(hash / coverPalettes.length) % coverPatterns.length] ??
    'dots'
  return {
    ...palette,
    pattern,
    initials: coverInitials(story.title),
  }
}

export function libraryLengthLabel(wordCount: number): string {
  return `${numberFormatter.format(wordCount)} ${wordCount === 1 ? 'word' : 'words'}`
}

export function libraryChapterLabel(chapterCount: number): string {
  if (chapterCount === 0) return 'No chapter breaks'
  return `${numberFormatter.format(chapterCount)} ${chapterCount === 1 ? 'chapter' : 'chapters'}`
}

const heroPriority: Partial<Record<LibraryProgressKind, number>> = {
  'in-progress': 0,
  updated: 1,
  completed: 2,
}

export function selectLibraryHero(
  stories: readonly LibraryStory[],
): LibraryStory | null {
  const eligible = stories.filter((story) => {
    const kind = classifyLibraryProgress(story)
    return heroPriority[kind] !== undefined
  })

  eligible.sort((left, right) => {
    const leftKind = classifyLibraryProgress(left)
    const rightKind = classifyLibraryProgress(right)
    const priority =
      Number(heroPriority[leftKind]) - Number(heroPriority[rightKind])
    if (priority !== 0) return priority

    const recency =
      Date.parse(right.progress?.updatedAt ?? '') -
      Date.parse(left.progress?.updatedAt ?? '')
    if (recency !== 0) return recency

    const title = left.title.localeCompare(right.title, 'en', {
      sensitivity: 'base',
      numeric: true,
    })
    return title !== 0 ? title : left.slug.localeCompare(right.slug)
  })

  return eligible[0] ?? null
}
