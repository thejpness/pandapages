import assert from 'node:assert/strict'
import test from 'node:test'
import { loadTypeScript } from './helpers/typescript-module.mjs'

async function readModel() {
  return loadTypeScript('../src/lib/library-read-model.ts', import.meta.url)
}

function progress(overrides = {}) {
  return {
    version: 2,
    percent: 0.42,
    updatedAt: '2026-07-19T12:00:00Z',
    isCurrentVersion: true,
    ...overrides,
  }
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
    progress: progress(),
    ...overrides,
  }
}

test('progress classification has explicit unavailable, beginning, current, completed, and updated states', async () => {
  const { module } = await readModel()
  assert.equal(module.classifyLibraryProgress(null), 'not-started')
  assert.equal(
    module.classifyLibraryProgress(
      story({ progress: null, progressAvailability: 'unavailable' }),
    ),
    'unavailable',
  )
  assert.equal(module.classifyLibraryProgress(progress({ percent: 0 })), 'beginning')
  assert.equal(module.classifyLibraryProgress(progress({ percent: 0.02 })), 'beginning')
  assert.equal(module.classifyLibraryProgress(progress({ percent: 0.021 })), 'in-progress')
  assert.equal(module.classifyLibraryProgress(progress({ percent: 0.979 })), 'in-progress')
  assert.equal(module.classifyLibraryProgress(progress({ percent: 0.98 })), 'completed')
  assert.equal(
    module.classifyLibraryProgress(
      progress({ version: 1, percent: 0.99, isCurrentVersion: false }),
    ),
    'updated',
  )
})

test('story action and progress labels remain truthful at every progress boundary', async () => {
  const { module } = await readModel()
  const cases = [
    [null, 'Read', 'Not started'],
    [progress({ percent: 0.01 }), 'Read', 'At the beginning'],
    [progress({ percent: 0.424 }), 'Continue at 42%', '42% read'],
    [progress({ percent: 0.975 }), 'Continue at 97%', '97% read'],
    [progress({ percent: 0.979 }), 'Continue at 97%', '97% read'],
    [progress({ percent: 0.98 }), 'Read again', 'Finished'],
    [progress({ percent: 0.99 }), 'Read again', 'Finished'],
    [
      progress({ version: 1, isCurrentVersion: false }),
      'Open updated story',
      'Story updated since you last read',
    ],
  ]
  for (const [value, action, label] of cases) {
    assert.equal(module.libraryActionLabel(value), action)
    assert.equal(module.libraryProgressLabel(value), label)
  }

  assert.equal(
    module.libraryActionLabel(story({ progress: progress({ percent: 0.5 }) })),
    'Continue at 50%',
  )
  const unavailable = story({
    progress: null,
    progressAvailability: 'unavailable',
  })
  assert.equal(module.libraryActionLabel(unavailable), 'Read')
  assert.equal(module.libraryProgressLabel(unavailable), 'Progress unavailable')
  assert.equal(module.libraryDisplayPercent(progress({ percent: 0.975 })), 97)
  assert.equal(module.libraryDisplayPercent(progress({ percent: 0.979 })), 97)
  assert.equal(module.libraryDisplayPercent(progress({ percent: 0.98 })), 98)
})

test('cover presentation is stable, CSS-ready, and never uses random selection', async () => {
  const { module, source } = await readModel()
  const first = module.libraryCoverPresentation(story())
  assert.deepEqual(module.libraryCoverPresentation(story()), first)
  assert.deepEqual(Object.keys(first).sort(), [
    'accent',
    'background',
    'initials',
    'ink',
    'pattern',
  ])
  assert.equal(first.initials, 'TT')
  assert.match(first.background, /^#[0-9a-f]{6}$/i)
  assert.match(first.accent, /^#[0-9a-f]{6}$/i)
  assert.match(first.ink, /^#[0-9a-f]{6}$/i)
  assert.ok(['dots', 'arches', 'rays', 'checks'].includes(first.pattern))
  assert.notDeepEqual(
    module.libraryCoverPresentation(
      story({ slug: 'moonlit-cafe', title: 'Moonlit Café' }),
    ),
    first,
  )
  assert.doesNotMatch(source, /Math\.random/)
})

test('word and chapter labels report exact aggregate data', async () => {
  const { module } = await readModel()
  assert.equal(module.libraryLengthLabel(0), '0 words')
  assert.equal(module.libraryLengthLabel(1), '1 word')
  assert.equal(module.libraryLengthLabel(1260), '1,260 words')
  assert.equal(module.libraryChapterLabel(0), 'No chapter breaks')
  assert.equal(module.libraryChapterLabel(1), '1 chapter')
  assert.equal(module.libraryChapterLabel(4), '4 chapters')
})

test('hero selection prioritises resumable current progress, then recency and explicit fallback states', async () => {
  const { module } = await readModel()
  const currentOlder = story({
    slug: 'current-older',
    title: 'Current Older',
    progress: progress({ updatedAt: '2026-07-18T12:00:00Z' }),
  })
  const currentNewer = story({
    slug: 'current-newer',
    title: 'Current Newer',
    progress: progress({ updatedAt: '2026-07-19T12:00:00Z' }),
  })
  const updatedNewest = story({
    slug: 'updated-newest',
    title: 'Updated Newest',
    progress: progress({
      version: 1,
      updatedAt: '2026-07-20T12:00:00Z',
      isCurrentVersion: false,
    }),
  })
  const completed = story({
    slug: 'completed',
    title: 'Completed',
    progress: progress({ percent: 1, updatedAt: '2026-07-21T12:00:00Z' }),
  })

  assert.equal(
    module.selectLibraryHero([
      completed,
      updatedNewest,
      currentOlder,
      currentNewer,
    ]).slug,
    'current-newer',
  )
  assert.equal(
    module.selectLibraryHero([completed, updatedNewest]).slug,
    'updated-newest',
  )
  assert.equal(module.selectLibraryHero([completed]).slug, 'completed')
  assert.equal(
    module.selectLibraryHero([
      story({ progress: null }),
      story({
        slug: 'unavailable',
        progress: null,
        progressAvailability: 'unavailable',
      }),
      story({ slug: 'beginning', progress: progress({ percent: 0.01 }) }),
    ]),
    null,
  )
})
