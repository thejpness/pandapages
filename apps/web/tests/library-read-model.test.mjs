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
function chooser(overrides = {}) {
  return story({
    slug: 'chooser', title: 'Chooser', state: 'chooser', selectedEdition: null,
    eligibleEditions: [edition({ wordCount: 700, chapterCount: 2 }), edition({ editionKey: 'little-listeners', version: 4, wordCount: 350, chapterCount: 0 })],
    progress: progress({ version: 1, isResolvedVersion: false }), ...overrides,
  })
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

test('chooser action wins over retained stale progress while selected actions retain existing semantics', async () => {
  const { module } = await readModel()
  assert.equal(module.libraryActionLabel(null), 'Read')
  assert.equal(module.libraryActionLabel(progress({ percent: 0.424 })), 'Continue at 42%')
  assert.equal(module.libraryActionLabel(progress({ percent: 0.98 })), 'Read again')
  assert.equal(module.libraryActionLabel(progress({ isResolvedVersion: false })), 'Open updated story')
  assert.equal(module.libraryActionLabel(chooser()), 'Choose edition')
  assert.equal(module.libraryProgressLabel(chooser()), 'Story updated since you last read')
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

test('selected stories show exact counts and chooser stories show truthful ranges', async () => {
  const { module } = await readModel()
  assert.equal(module.libraryLengthLabel(story({ eligibleEditions: [edition({ wordCount: 1 })] })), '1 word')
  assert.equal(module.libraryLengthLabel(story()), '1,260 words')
  assert.equal(module.libraryChapterLabel(story({ eligibleEditions: [edition({ chapterCount: 0 })] })), 'No chapter breaks')
  assert.equal(module.libraryChapterLabel(story()), '4 chapters')
  assert.equal(module.libraryLengthLabel(chooser()), '350–700 words')
  assert.equal(module.libraryChapterLabel(chooser()), '0–2 chapters')
  assert.deepEqual(module.libraryWordCountBounds(chooser()), { min: 350, max: 700 })
})

test('hero selection keeps resumable selected progress ahead of updated chooser history', async () => {
  const { module } = await readModel()
  const current = story({ slug: 'current', progress: progress({ updatedAt: '2026-07-18T12:00:00Z' }) })
  const updatedChooser = chooser({ progress: progress({ version: 1, isResolvedVersion: false, updatedAt: '2026-07-20T12:00:00Z' }) })
  const completed = story({ slug: 'completed', progress: progress({ percent: 1, updatedAt: '2026-07-21T12:00:00Z' }) })
  assert.equal(module.selectLibraryHero([completed, updatedChooser, current]).slug, 'current')
  assert.equal(module.selectLibraryHero([completed, updatedChooser]).slug, 'chooser')
  assert.equal(module.selectLibraryHero([story({ progress: null })]), null)
})
