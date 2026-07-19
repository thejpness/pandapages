import type { LibraryStory } from './api'

export type LibrarySort = 'recent' | 'title' | 'shortest' | 'longest'

export type LibrarySortStorage = {
  getItem: (key: string) => string | null
  setItem: (key: string, value: string) => void
}

export const LIBRARY_SORT_STORAGE_KEY = 'pp_library_sort_v1'

const librarySorts: readonly LibrarySort[] = [
  'recent',
  'title',
  'shortest',
  'longest',
]

const titleCollator = new Intl.Collator('en', {
  sensitivity: 'base',
  numeric: true,
})

function normaliseSearchValue(value: string): string {
  return value
    .normalize('NFKD')
    .replace(/\p{Mark}/gu, '')
    .toLocaleLowerCase('en-GB')
    .trim()
}

function compareTitle(left: LibraryStory, right: LibraryStory): number {
  const title = titleCollator.compare(left.title, right.title)
  return title !== 0 ? title : left.slug.localeCompare(right.slug)
}

function progressTime(story: LibraryStory): number {
  if (story.progress === null) return Number.NEGATIVE_INFINITY
  const parsed = Date.parse(story.progress.updatedAt)
  return Number.isFinite(parsed) ? parsed : Number.NEGATIVE_INFINITY
}

function defaultStorage(): LibrarySortStorage | null {
  try {
    if (typeof localStorage === 'undefined') return null
    return localStorage
  } catch {
    return null
  }
}

export function filterLibraryStories(
  stories: readonly LibraryStory[],
  query: string,
): LibraryStory[] {
  const needle = normaliseSearchValue(query)
  if (!needle) return [...stories]

  return stories.filter((story) =>
    [story.title, story.author ?? '', story.slug].some((candidate) =>
      normaliseSearchValue(candidate).includes(needle),
    ),
  )
}

export function parseLibrarySortPreference(value: unknown): LibrarySort | null {
  return typeof value === 'string' &&
    librarySorts.includes(value as LibrarySort)
    ? (value as LibrarySort)
    : null
}

export function readLibrarySortPreference(
  storage: LibrarySortStorage | null = defaultStorage(),
): LibrarySort | null {
  if (storage === null) return null
  try {
    return parseLibrarySortPreference(storage.getItem(LIBRARY_SORT_STORAGE_KEY))
  } catch {
    return null
  }
}

export function writeLibrarySortPreference(
  sort: unknown,
  storage: LibrarySortStorage | null = defaultStorage(),
): boolean {
  const valid = parseLibrarySortPreference(sort)
  if (storage === null || valid === null) return false
  try {
    storage.setItem(LIBRARY_SORT_STORAGE_KEY, valid)
    return true
  } catch {
    return false
  }
}

export function defaultLibrarySort(
  stories: readonly LibraryStory[],
): LibrarySort {
  return stories.some((story) => story.progress !== null) ? 'recent' : 'title'
}

export function sortLibraryStories(
  stories: readonly LibraryStory[],
  sort: LibrarySort,
): LibraryStory[] {
  const selected = parseLibrarySortPreference(sort) ?? defaultLibrarySort(stories)
  const result = [...stories]

  result.sort((left, right) => {
    if (selected === 'recent') {
      const leftTime = progressTime(left)
      const rightTime = progressTime(right)
      if (leftTime !== rightTime) return rightTime > leftTime ? 1 : -1
      return compareTitle(left, right)
    }
    if (selected === 'shortest') {
      const length = left.wordCount - right.wordCount
      return length !== 0 ? length : compareTitle(left, right)
    }
    if (selected === 'longest') {
      const length = right.wordCount - left.wordCount
      return length !== 0 ? length : compareTitle(left, right)
    }
    return compareTitle(left, right)
  })

  return result
}

export function selectSurpriseStory(
  stories: readonly LibraryStory[],
  random: () => number = Math.random,
): LibraryStory | null {
  if (stories.length === 0) return null
  const value = random()
  const bounded = Number.isFinite(value) ? Math.max(0, Math.min(1, value)) : 0
  const index = Math.min(stories.length - 1, Math.floor(bounded * stories.length))
  return stories[index] ?? null
}
