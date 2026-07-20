import assert from 'node:assert/strict'
import test from 'node:test'
import { loadTypeScript } from './helpers/typescript-module.mjs'

const versionId = '11111111-1111-4111-8111-111111111111'

async function loadForm() {
  return (await loadTypeScript('../src/lib/story-studio-form.ts', import.meta.url)).module
}

async function loadNavigation() {
  return (
    await loadTypeScript(
      '../src/lib/story-studio-navigation.ts',
      import.meta.url,
    )
  ).module
}

function story(status = 'published') {
  return {
    slug: 'the-panda-tale',
    title: 'The Panda Tale',
    author: 'Panda Author',
    language: 'en-GB',
    rights: { label: 'Public domain' },
    sourceUrl: null,
    status,
    publishedVersion:
      status === 'published' ? { versionId, version: 1 } : null,
    draftVersion: { versionId, version: 1 },
    versionCount: 1,
    updatedAt: '2026-07-20T10:00:00Z',
  }
}

function version(overrides = {}) {
  return {
    versionId,
    version: 1,
    createdAt: '2026-07-20T10:00:00Z',
    isDraft: true,
    isPublished: false,
    segmentCount: 2,
    wordCount: 8,
    chapterCount: 1,
    health: 'ready',
    ...overrides,
  }
}

test('slug follows title until the administrator edits it', async () => {
  const form = await loadForm()
  assert.equal(
    form.followedStorySlug('À Panda’s Picnic!', '', false),
    'a-pandas-picnic',
  )
  assert.equal(
    form.followedStorySlug('Changed title', 'chosen-slug', true),
    'chosen-slug',
  )
})

test('form normalization trims fields and preserves immutable rights metadata', async () => {
  const form = await loadForm()
  const input = {
    title: '  Panda Tale  ',
    author: '  Author  ',
    slug: ' Panda Tale ',
    language: ' en-GB ',
    rightsLabel: ' Public domain ',
    rights: { year: 1908, nested: { source: 'archive' } },
    sourceUrl: ' https://example.invalid/story ',
    markdown: '# Panda Tale\r\n\r\nHello.  ',
  }
  const before = structuredClone(input)
  assert.deepEqual(form.normaliseStoryForm(input), {
    slug: 'panda-tale',
    title: 'Panda Tale',
    author: 'Author',
    language: 'en-GB',
    rights: {
      year: 1908,
      nested: { source: 'archive' },
      label: 'Public domain',
    },
    sourceUrl: 'https://example.invalid/story',
    markdown: '# Panda Tale\n\nHello.\n',
  })
  assert.deepEqual(input, before)
})

test('dirty comparison ignores formatting normalized by the contract', async () => {
  const form = await loadForm()
  const value = {
    ...form.createBlankStoryForm(),
    title: 'Panda Tale',
    slug: 'panda-tale',
    markdown: '# Panda Tale\n',
  }
  const baseline = form.storyFormFingerprint(value)
  const equivalent = { ...value, title: ' Panda Tale ', markdown: '# Panda Tale\r\n' }
  assert.equal(form.storyFormIsDirty(equivalent, baseline), false)
  assert.equal(
    form.storyFormIsDirty({ ...value, markdown: '# Panda Tale\n\nNew.' }, baseline),
    true,
  )
})

test('filename inference handles author suffix without mutating input', async () => {
  const form = await loadForm()
  const filename = 'The Quiet Panda - A. Writer.markdown'
  assert.deepEqual(form.inferStoryMetadataFromFilename(filename), {
    title: 'The Quiet Panda',
    author: 'A. Writer',
  })
  assert.equal(filename, 'The Quiet Panda - A. Writer.markdown')
})

test('Gutenberg boilerplate is trimmed at both sentinels', async () => {
  const form = await loadForm()
  const text = `Header\n*** START OF THE PROJECT GUTENBERG EBOOK SAMPLE ***\nStory body.\n*** END OF THE PROJECT GUTENBERG EBOOK SAMPLE ***\nFooter`
  assert.equal(form.stripGutenbergBoilerplate(text), 'Story body.')
})

test('plain-text chapter headings are promoted deterministically', async () => {
  const form = await loadForm()
  assert.equal(
    form.promoteStoryChapters('CHAPTER iv. A New Friend\nText.\nPART 2\nMore.'),
    '## CHAPTER IV — A New Friend\n\nText.\n## PART 2\n\nMore.',
  )
})

test('H1 insertion only occurs when the imported story has no H1', async () => {
  const form = await loadForm()
  assert.equal(form.ensureStoryH1('Panda', 'Body.'), '# Panda\n\nBody.\n')
  assert.equal(
    form.ensureStoryH1('Ignored', '# Existing\n\nBody.'),
    '# Existing\n\nBody.\n',
  )
})

test('HTML import remains local and produces editable Markdown', async () => {
  const form = await loadForm()
  const imported = form.convertImportedStoryFile({
    filename: 'Panda Walk - Rowan.htm',
    mediaType: 'text/html',
    text: '<html><body><h1>Panda Walk</h1><h2>Chapter I</h2><p>Calm &amp; kind.</p></body></html>',
  })
  assert.equal(imported.title, 'Panda Walk')
  assert.equal(imported.author, 'Rowan')
  assert.match(imported.markdown, /^# Panda Walk/m)
  assert.match(imported.markdown, /## Chapter I/)
  assert.match(imported.markdown, /Calm & kind/)
})

test('substantial existing Markdown requires replacement confirmation', async () => {
  const form = await loadForm()
  assert.equal(form.importWouldReplaceSubstantialMarkdown('short', 'new'), false)
  assert.equal(
    form.importWouldReplaceSubstantialMarkdown('A'.repeat(220), 'new'),
    true,
  )
})

test('catalogue search and finite status filter preserve server order', async () => {
  const navigation = await loadNavigation()
  const items = [
    story('draft_only'),
    { ...story('repair_required'), slug: 'other', title: 'Moon Story', author: null },
  ]
  assert.deepEqual(
    navigation.filterStoryCatalogue(items, 'panda author', 'all').map((item) => item.slug),
    ['the-panda-tale'],
  )
  assert.deepEqual(
    navigation.filterStoryCatalogue(items, '', 'repair_required').map((item) => item.slug),
    ['other'],
  )
  assert.deepEqual(navigation.filterStoryCatalogue(items, '', 'all'), items)
})

test('story statuses and version health use human labels', async () => {
  const navigation = await loadNavigation()
  assert.deepEqual(
    navigation.storyStatusOrder.map(navigation.storyStatusLabel),
    [
      'Draft only',
      'Published',
      'Published · New draft',
      'Unpublished',
      'Needs attention',
    ],
  )
  assert.equal(navigation.versionHealthLabel('repair_required'), 'Needs repair')
  assert.equal(navigation.versionHealthLabel('unavailable'), 'Unavailable')
})

test('version action eligibility and unpublish availability follow finite state', async () => {
  const navigation = await loadNavigation()
  assert.equal(navigation.versionCanSeedDraft(version()), true)
  assert.equal(navigation.versionCanPublish(version()), true)
  assert.equal(navigation.versionCanPublish(version({ isPublished: true })), false)
  assert.equal(navigation.versionCanPublish(version({ health: 'repair_required' })), false)
  assert.equal(navigation.versionCanSeedDraft(version({ health: 'unavailable' })), false)
  assert.equal(navigation.storyCanUnpublish(story()), true)
  assert.equal(navigation.storyCanUnpublish(story('unpublished')), false)
})

test('preview outdated state only changes when input fingerprint changes', async () => {
  const navigation = await loadNavigation()
  assert.equal(navigation.previewIsOutdated(null, 'current'), false)
  assert.equal(navigation.previewIsOutdated('same', 'same'), false)
  assert.equal(navigation.previewIsOutdated('old', 'new'), true)
})

test('created and reused save outcomes remain truthful', async () => {
  const navigation = await loadNavigation()
  assert.equal(navigation.draftOutcomeMessage('created_story', 1), 'Story created as draft version 1.')
  assert.equal(navigation.draftOutcomeMessage('created_version', 2), 'Draft version 2 created.')
  assert.equal(navigation.draftOutcomeMessage('reused', 2), 'Existing healthy version 2 reused.')
})

test('sensitive server error text is never projected into Story Studio', async () => {
  const navigation = await loadNavigation()
  const error = new Error('pq: password=secret host=production.invalid')
  error.status = 500
  const projected = navigation.projectStoryStudioError(error)
  assert.equal(projected.kind, 'retry')
  assert.doesNotMatch(JSON.stringify(projected), /password|production\.invalid|pq:/)

  const conflict = new Error('hash mismatch: abc123')
  conflict.status = 409
  assert.deepEqual(navigation.projectStoryStudioError(conflict), {
    kind: 'repair',
    title: 'Needs attention',
    message: 'The stored version cannot safely be reused or published.',
    retryable: false,
  })
})
