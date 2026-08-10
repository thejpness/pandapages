import assert from 'node:assert/strict'
import test from 'node:test'
import { loadTypeScript } from './helpers/typescript-module.mjs'

async function readModel() {
  return loadTypeScript('../src/lib/library-read-model.ts', import.meta.url)
}

function edition(overrides = {}) {
  return { editionKey: 'growing-readers', version: 2, wordCount: 1260, chapterCount: 4, ...overrides }
}
function progress(overrides = {}) {
  return { version: 2, percent: 0.42, updatedAt: '2026-07-19T12:00:00Z', isResolvedVersion: true, ...overrides }
}
function story(overrides = {}) {
  return {
    slug: 'three-little-pigs', title: 'The Three Little Pigs', author: 'Traditional', language: 'en',
    state: 'selected', eligibleEditions: [edition()], selectedEdition: 'growing-readers',
    progress: progress(), progressAvailability: 'available', ...overrides,
  }
}
test('progress classification uses backend-authoritative resolved-version state', async () => {
  const { module } = await readModel()
  assert.equal(module.classifyLibraryProgress(null), 'not-started')
  assert.equal(module.classifyLibraryProgress(story({ progress: null, progressAvailability: 'unavailable' })), 'unavailable')
  assert.equal(module.classifyLibraryProgress(progress({ percent: 0 })), 'beginning')
  assert.equal(module.classifyLibraryProgress(progress({ percent: 0.021 })), 'in-progress')
  assert.equal(module.classifyLibraryProgress(progress({ percent: 0.98 })), 'completed')
  assert.equal(module.classifyLibraryProgress(progress({ version: 1, isResolvedVersion: false })), 'updated')
})

test('selected actions retain existing semantics', async () => {
  const { module } = await readModel()
  assert.equal(module.libraryActionLabel(null), 'Read')
  assert.equal(module.libraryActionLabel(progress({ percent: 0.424 })), 'Continue at 42%')
  assert.equal(module.libraryActionLabel(progress({ percent: 0.98 })), 'Read again')
  assert.equal(module.libraryActionLabel(progress({ isResolvedVersion: false })), 'Open updated story')
  assert.equal(module.libraryDisplayPercent(progress({ percent: 0.979 })), 97)
})

test('cover presentation is stable, CSS-ready, and never random', async () => {
  const { module, source } = await readModel()
  const first = module.libraryCoverPresentation(story())
  assert.deepEqual(module.libraryCoverPresentation(story()), first)
  assert.equal(first.initials, 'TT')
  assert.ok(['dots', 'arches', 'rays', 'checks'].includes(first.pattern))
  assert.doesNotMatch(source, /Math\.random/)
})

test('selected stories show exact resolved-edition counts', async () => {
  const { module } = await readModel()
  assert.equal(module.libraryLengthLabel(story({ eligibleEditions: [edition({ wordCount: 1 })] })), '1 word')
  assert.equal(module.libraryLengthLabel(story()), '1,260 words')
  assert.equal(module.libraryChapterLabel(story({ eligibleEditions: [edition({ chapterCount: 0 })] })), 'No chapter breaks')
  assert.equal(module.libraryChapterLabel(story()), '4 chapters')
})

test('hero selection keeps resumable progress ahead of updated history', async () => {
  const { module } = await readModel()
  const current = story({ slug: 'current', progress: progress({ updatedAt: '2026-07-18T12:00:00Z' }) })
  const updatedStory = story({ slug: 'updated', progress: progress({ version: 1, isResolvedVersion: false, updatedAt: '2026-07-20T12:00:00Z' }) })
  const completed = story({ slug: 'completed', progress: progress({ percent: 1, updatedAt: '2026-07-21T12:00:00Z' }) })
  assert.equal(module.selectLibraryHero([completed, updatedStory, current]).slug, 'current')
  assert.equal(module.selectLibraryHero([completed, updatedStory]).slug, 'updated')
  assert.equal(module.selectLibraryHero([story({ progress: null })]), null)
})
