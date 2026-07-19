import assert from 'node:assert/strict'
import test from 'node:test'
import { loadTypeScript } from './helpers/typescript-module.mjs'

async function sorting() {
  return (await loadTypeScript('../src/lib/library-sorting.ts', import.meta.url)).module
}

function story(overrides = {}) {
  return {
    slug: 'three-little-pigs',
    title: 'The Three Little Pigs',
    author: 'Traditional',
    language: 'en',
    publishedVersion: 2,
    wordCount: 1260,
    chapterCount: 4,
    progress: null,
    ...overrides,
  }
}

function progress(updatedAt) {
  return {
    version: 2,
    percent: 0.42,
    updatedAt,
    isCurrentVersion: true,
  }
}

function slugs(stories) {
  return stories.map((item) => item.slug)
}

test('search matches title, author, and hidden slug fallback without mutating input', async () => {
  const module = await sorting()
  const stories = [
    story({ slug: 'moonlit-cafe', title: 'Moonlit Café', author: 'Ada Panda' }),
    story({ slug: 'secret-bamboo-path', title: 'The Quiet Path', author: null }),
    story({ slug: 'oz', title: 'The Wizard of Oz', author: 'L. Frank Baum' }),
  ]

  assert.deepEqual(slugs(module.filterLibraryStories(stories, 'cafe')), ['moonlit-cafe'])
  assert.deepEqual(slugs(module.filterLibraryStories(stories, 'ada')), ['moonlit-cafe'])
  assert.deepEqual(slugs(module.filterLibraryStories(stories, 'bamboo')), ['secret-bamboo-path'])
  assert.deepEqual(slugs(module.filterLibraryStories(stories, 'FRANK')), ['oz'])
  assert.deepEqual(module.filterLibraryStories(stories, 'missing'), [])
  const copy = module.filterLibraryStories(stories, '  ')
  assert.deepEqual(copy, stories)
  assert.notEqual(copy, stories)
})

test('all four sort modes are deterministic and leave source ordering untouched', async () => {
  const module = await sorting()
  const stories = [
    story({
      slug: 'zebra',
      title: 'Zebra',
      wordCount: 500,
      progress: progress('2026-07-18T12:00:00Z'),
    }),
    story({ slug: 'middle', title: 'Middle', wordCount: 500 }),
    story({
      slug: 'alpha-two',
      title: 'Alpha 2',
      wordCount: 900,
      progress: progress('2026-07-19T12:00:00Z'),
    }),
    story({ slug: 'alpha-ten', title: 'Alpha 10', wordCount: 100 }),
  ]

  assert.deepEqual(slugs(module.sortLibraryStories(stories, 'recent')), [
    'alpha-two',
    'zebra',
    'alpha-ten',
    'middle',
  ])
  assert.deepEqual(slugs(module.sortLibraryStories(stories, 'title')), [
    'alpha-two',
    'alpha-ten',
    'middle',
    'zebra',
  ])
  assert.deepEqual(slugs(module.sortLibraryStories(stories, 'shortest')), [
    'alpha-ten',
    'middle',
    'zebra',
    'alpha-two',
  ])
  assert.deepEqual(slugs(module.sortLibraryStories(stories, 'longest')), [
    'alpha-two',
    'middle',
    'zebra',
    'alpha-ten',
  ])
  assert.deepEqual(slugs(stories), ['zebra', 'middle', 'alpha-two', 'alpha-ten'])
})

test('default and persisted sort preferences accept only supported values', async () => {
  const module = await sorting()
  assert.equal(module.defaultLibrarySort([story()]), 'title')
  assert.equal(
    module.defaultLibrarySort([story({ progress: progress('2026-07-19T12:00:00Z') })]),
    'recent',
  )

  for (const value of ['recent', 'title', 'shortest', 'longest']) {
    assert.equal(module.parseLibrarySortPreference(value), value)
  }
  for (const value of [null, '', 'Recent', 'newest', 1, {}, ['title']]) {
    assert.equal(module.parseLibrarySortPreference(value), null)
  }

  const values = new Map()
  const storage = {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
  }
  assert.equal(module.readLibrarySortPreference(storage), null)
  assert.equal(module.writeLibrarySortPreference('longest', storage), true)
  assert.equal(module.readLibrarySortPreference(storage), 'longest')
  assert.equal(module.writeLibrarySortPreference('invalid', storage), false)
  values.set(module.LIBRARY_SORT_STORAGE_KEY, 'untrusted')
  assert.equal(module.readLibrarySortPreference(storage), null)

  const failingStorage = {
    getItem: () => { throw new Error('blocked') },
    setItem: () => { throw new Error('blocked') },
  }
  assert.equal(module.readLibrarySortPreference(failingStorage), null)
  assert.equal(module.writeLibrarySortPreference('title', failingStorage), false)
})

test('surprise selection is isolated, injectable, bounded, and empty-safe', async () => {
  const module = await sorting()
  const stories = [
    story({ slug: 'one' }),
    story({ slug: 'two' }),
    story({ slug: 'three' }),
  ]

  assert.equal(module.selectSurpriseStory([], () => 0.5), null)
  assert.equal(module.selectSurpriseStory(stories, () => 0).slug, 'one')
  assert.equal(module.selectSurpriseStory(stories, () => 0.5).slug, 'two')
  assert.equal(module.selectSurpriseStory(stories, () => 0.999).slug, 'three')
  assert.equal(module.selectSurpriseStory(stories, () => 1).slug, 'three')
  assert.equal(module.selectSurpriseStory(stories, () => -2).slug, 'one')
  assert.equal(module.selectSurpriseStory(stories, () => Number.NaN).slug, 'one')
})
