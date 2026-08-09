import assert from 'node:assert/strict'
import test from 'node:test'
import { loadTypeScript } from './helpers/typescript-module.mjs'

async function sorting() {
  return (await loadTypeScript('../src/lib/library-sorting.ts', import.meta.url)).module
}
function edition(overrides = {}) { return { editionKey: 'growing-readers', version: 2, wordCount: 1260, chapterCount: 4, ...overrides } }
function story(overrides = {}) {
  return { slug: 'three-little-pigs', title: 'The Three Little Pigs', author: 'Traditional', language: 'en', state: 'selected', eligibleEditions: [edition()], selectedEdition: 'growing-readers', progress: null, progressAvailability: 'available', ...overrides }
}
function selectedWithWordCount(slug, title, wordCount, extraEligible = []) {
  return story({ slug, title, eligibleEditions: [edition({ wordCount }), ...extraEligible], selectedEdition: 'growing-readers' })
}
function chooser(slug, title, counts) {
  return story({ slug, title, state: 'chooser', selectedEdition: null, eligibleEditions: counts.map((wordCount, index) => edition({ editionKey: index === 0 ? 'growing-readers' : 'little-listeners', version: index + 1, wordCount })) })
}
function progress(updatedAt) { return { version: 2, percent: 0.42, updatedAt, isResolvedVersion: true } }
function slugs(stories) { return stories.map((item) => item.slug) }

test('search matches title, author, and hidden slug fallback without mutating input', async () => {
  const module = await sorting()
  const stories = [story({ slug: 'moonlit-cafe', title: 'Moonlit Café', author: 'Ada Panda' }), story({ slug: 'secret-bamboo-path', title: 'The Quiet Path', author: null }), story({ slug: 'oz', title: 'The Wizard of Oz', author: 'L. Frank Baum' })]
  assert.deepEqual(slugs(module.filterLibraryStories(stories, 'cafe')), ['moonlit-cafe'])
  assert.deepEqual(slugs(module.filterLibraryStories(stories, 'bamboo')), ['secret-bamboo-path'])
  assert.deepEqual(module.filterLibraryStories(stories, 'missing'), [])
})

test('sort uses selected exact length and chooser bounds without inventing an edition', async () => {
  const module = await sorting()
  const stories = [
    selectedWithWordCount('selected-long', 'Selected Long', 1000, [edition({ editionKey: 'little-listeners', version: 3, wordCount: 100 })]),
    chooser('chooser-wide', 'Chooser Wide', [300, 900]),
    chooser('chooser-narrow', 'Chooser Narrow', [300, 600]),
    selectedWithWordCount('selected-short', 'Selected Short', 200),
  ]
  assert.deepEqual(slugs(module.sortLibraryStories(stories, 'shortest')), ['selected-short', 'chooser-narrow', 'chooser-wide', 'selected-long'])
  assert.deepEqual(slugs(module.sortLibraryStories(stories, 'longest')), ['selected-long', 'chooser-wide', 'chooser-narrow', 'selected-short'])
})

test('recent/title ordering and sort preference persistence remain deterministic', async () => {
  const module = await sorting()
  const stories = [story({ slug: 'zebra', title: 'Zebra', progress: progress('2026-07-18T12:00:00Z') }), story({ slug: 'alpha-two', title: 'Alpha 2', progress: progress('2026-07-19T12:00:00Z') }), story({ slug: 'alpha-ten', title: 'Alpha 10' })]
  assert.deepEqual(slugs(module.sortLibraryStories(stories, 'recent')), ['alpha-two', 'zebra', 'alpha-ten'])
  assert.deepEqual(slugs(module.sortLibraryStories(stories, 'title')), ['alpha-two', 'alpha-ten', 'zebra'])
  assert.equal(module.defaultLibrarySort([story()]), 'title')
  assert.equal(module.defaultLibrarySort([story({ progress: progress('2026-07-19T12:00:00Z') })]), 'recent')
  const values = new Map()
  const storage = { getItem: (key) => values.get(key) ?? null, setItem: (key, value) => values.set(key, value) }
  assert.equal(module.writeLibrarySortPreference('longest', storage), true)
  assert.equal(module.readLibrarySortPreference(storage), 'longest')
  assert.equal(module.writeLibrarySortPreference('invalid', storage), false)
})

test('surprise selection remains isolated, injectable, bounded, and empty-safe', async () => {
  const module = await sorting()
  const stories = [story({ slug: 'one' }), story({ slug: 'two' }), story({ slug: 'three' })]
  assert.equal(module.selectSurpriseStory([], () => 0.5), null)
  assert.equal(module.selectSurpriseStory(stories, () => 0).slug, 'one')
  assert.equal(module.selectSurpriseStory(stories, () => 0.999).slug, 'three')
})
