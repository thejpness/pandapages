import type {
  AdminSourceUpsertRequest,
  AdminSourceVersion,
  AdminStoryDetail,
  AdminStoryEditionKey,
  AdminStoryInput,
  AdminVersionSource,
  JsonObject,
  JsonValue,
} from './api'

export type StoryStudioForm = {
  title: string
  author: string
  slug: string
  language: string
  rightsLabel: string
  rights: JsonObject
  sourceUrl: string
  markdown: string
}

export type ImportedStory = {
  filename: string
  title: string
  author: string
  markdown: string
}

export type ImportedStoryFile = {
  filename: string
  mediaType: string
  text: string
}

const supportedStoryExtensions = new Set([
  'txt',
  'md',
  'markdown',
  'html',
  'htm',
])

export function slugifyStoryTitle(input: string): string {
  return input
    .normalize('NFKD')
    .replace(/[\u0300-\u036f]/g, '')
    .toLocaleLowerCase('en-GB')
    .trim()
    .replace(/['’"]/g, '')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 80)
}

export function followedStorySlug(
  title: string,
  currentSlug: string,
  slugWasEdited: boolean,
): string {
  return slugWasEdited ? currentSlug : slugifyStoryTitle(title)
}

export function createBlankStoryForm(): StoryStudioForm {
  return {
    title: '',
    author: '',
    slug: '',
    language: 'en-GB',
    rightsLabel: '',
    rights: {},
    sourceUrl: '',
    markdown: '',
  }
}

export function storyFormFromVersion(
  source: AdminVersionSource,
): StoryStudioForm {
  return {
    title: source.title,
    author: source.author ?? '',
    slug: source.slug,
    language: source.language,
    rightsLabel:
      typeof source.rights.label === 'string' ? source.rights.label : '',
    rights: cloneJsonObject(source.rights),
    sourceUrl: source.sourceUrl ?? '',
    markdown: source.markdown,
  }
}

function cloneJsonValue(value: JsonValue): JsonValue {
  if (Array.isArray(value)) return value.map(cloneJsonValue)
  if (value !== null && typeof value === 'object') {
    return cloneJsonObject(value)
  }
  return value
}

export function cloneJsonObject(value: JsonObject): JsonObject {
  return Object.fromEntries(
    Object.entries(value).map(([key, child]) => [key, cloneJsonValue(child)]),
  )
}

export function normaliseStoryForm(
  form: StoryStudioForm,
  editionKey: AdminStoryEditionKey,
): AdminStoryInput {
  const rights = cloneJsonObject(form.rights)
  const rightsLabel = form.rightsLabel.trim()
  if (rightsLabel) rights.label = rightsLabel
  else delete rights.label

  return {
    slug: slugifyStoryTitle(form.slug),
    editionKey,
    title: form.title.trim(),
    author: form.author.trim() || null,
    language: form.language.trim() || 'en-GB',
    rights,
    sourceUrl: form.sourceUrl.trim() || null,
    markdown: normaliseNewlines(form.markdown).trimEnd() + '\n',
  }
}

function canonicalJson(value: JsonValue): JsonValue {
  if (Array.isArray(value)) return value.map(canonicalJson)
  if (value !== null && typeof value === 'object') {
    return Object.fromEntries(
      Object.keys(value)
        .sort()
        .map((key) => [key, canonicalJson(value[key])]),
    )
  }
  return value
}

export function storyFormFingerprint(
  form: StoryStudioForm,
  editionKey: AdminStoryEditionKey = 'classic',
): string {
  return JSON.stringify(canonicalJson(normaliseStoryForm(form, editionKey)))
}

export function storyFormIsDirty(
  form: StoryStudioForm,
  baselineFingerprint: string,
  editionKey: AdminStoryEditionKey = 'classic',
): boolean {
  return storyFormFingerprint(form, editionKey) !== baselineFingerprint
}

export function storyFormFromStory(story: AdminStoryDetail): StoryStudioForm {
  return {
    title: story.title,
    author: story.author ?? '',
    slug: story.slug,
    language: story.language,
    rightsLabel: typeof story.rights.label === 'string' ? story.rights.label : '',
    rights: cloneJsonObject(story.rights),
    sourceUrl: story.sourceUrl ?? '',
    markdown: '',
  }
}

export type StorySourceForm = {
  title: string
  author: string
  language: string
  rightsLabel: string
  rights: JsonObject
  sourceUrl: string
  sourceText: string
}

export function sourceFormFromStory(story: AdminStoryDetail): StorySourceForm {
  return {
    title: story.title,
    author: story.author ?? '',
    language: story.language,
    rightsLabel: typeof story.rights.label === 'string' ? story.rights.label : '',
    rights: cloneJsonObject(story.rights),
    sourceUrl: story.sourceUrl ?? '',
    sourceText: '',
  }
}

export function sourceFormFromVersion(source: AdminSourceVersion): StorySourceForm {
  return {
    title: source.title,
    author: source.author ?? '',
    language: source.language,
    rightsLabel: typeof source.rights.label === 'string' ? source.rights.label : '',
    rights: cloneJsonObject(source.rights),
    sourceUrl: source.sourceUrl ?? '',
    sourceText: source.sourceText,
  }
}

export function normaliseSourceForm(form: StorySourceForm): AdminSourceUpsertRequest {
  const rights = cloneJsonObject(form.rights)
  const rightsLabel = form.rightsLabel.trim()
  if (rightsLabel) rights.label = rightsLabel
  else delete rights.label
  return {
    title: form.title.trim(),
    author: form.author.trim() || null,
    language: form.language.trim() || 'en-GB',
    rights,
    sourceUrl: form.sourceUrl.trim() || null,
    // Source provenance stays byte-for-byte as entered; no adaptation transforms.
    sourceText: form.sourceText,
  }
}

export function sourceFormFingerprint(form: StorySourceForm): string {
  return JSON.stringify(canonicalJson(normaliseSourceForm(form)))
}

export function normaliseNewlines(value: string): string {
  return value.replace(/\r\n/g, '\n').replace(/\r/g, '\n')
}

export function inferStoryMetadataFromFilename(filename: string): {
  title: string
  author: string
} {
  const base = filename.replace(/\.[^.]+$/, '').trim()
  const parts = base
    .split(' - ')
    .map((part) => part.trim())
    .filter(Boolean)
  if (parts.length >= 2) {
    return { title: parts[0], author: parts.slice(1).join(' - ') }
  }
  return { title: base, author: '' }
}

export function stripGutenbergBoilerplate(text: string): string {
  const normalized = normaliseNewlines(text)
  const start =
    /\*\*\*\s*START OF (?:THE|THIS) PROJECT GUTENBERG EBOOK[\s\S]*?\*\*\*/i
  const end =
    /\*\*\*\s*END OF (?:THE|THIS) PROJECT GUTENBERG EBOOK[\s\S]*?\*\*\*/i

  const startMatch = start.exec(normalized)
  const withoutStart = startMatch
    ? normalized.slice(startMatch.index + startMatch[0].length)
    : normalized
  const endMatch = end.exec(withoutStart)
  return (endMatch ? withoutStart.slice(0, endMatch.index) : withoutStart).trim()
}

export function promoteStoryChapters(text: string): string {
  const chapter = /^(chapter|book|letter|part)\s+([0-9ivxlcdm]+)\b\.?\s*(.*)$/i
  const output: string[] = []
  for (const raw of normaliseNewlines(text).split('\n')) {
    const line = raw.trimEnd()
    const match = chapter.exec(line.trim())
    if (!match) {
      output.push(line)
      continue
    }
    const suffix = match[3]?.trim()
    output.push(
      `## ${match[1].toUpperCase()} ${match[2].toUpperCase()}${suffix ? ` — ${suffix}` : ''}`,
      '',
    )
  }
  return output.join('\n').trim()
}

export function ensureStoryH1(title: string, markdown: string): string {
  if (/^#\s+\S/m.test(markdown.trimStart())) return markdown.trimEnd() + '\n'
  const safeTitle = title.trim() || 'Untitled'
  return `# ${safeTitle}\n\n${markdown.trim()}\n`
}

function decodeHTMLText(value: string): string {
  const named: Record<string, string> = {
    amp: '&',
    apos: "'",
    gt: '>',
    lt: '<',
    nbsp: ' ',
    quot: '"',
  }
  return value.replace(
    /&(#\d+|#x[0-9a-f]+|amp|apos|gt|lt|nbsp|quot);/giu,
    (entity, token: string) => {
      if (token.startsWith('#x')) {
        return String.fromCodePoint(Number.parseInt(token.slice(2), 16))
      }
      if (token.startsWith('#')) {
        return String.fromCodePoint(Number.parseInt(token.slice(1), 10))
      }
      return named[token.toLocaleLowerCase('en-GB')] ?? entity
    },
  )
}

function headingTextFromHTML(value: string): string {
  return value
    .replace(/<br\s*\/?>(?:\r?\n)?/giu, ' ')
    .replace(/<[^>]+>/g, '')
    .replace(/\s+/g, ' ')
    .trim()
}

function markdownHeading(level: number, value: string): string {
  const text = headingTextFromHTML(value)
  return text ? `\n${'#'.repeat(level)} ${text}\n` : '\n'
}
function htmlAttributeValue(attributes: string, name: string): string | null {

  for (const match of attributes.matchAll(/\b([a-z][\w:.-]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'=<>]+))/giu)) {
    if (match[1].toLocaleLowerCase('en-GB') === name) {
      return match[2] ?? match[3] ?? match[4] ?? null
    }
  }
  return null
}

function navigationContainerEnd(html: string, start: number, tag: string): number | null {
  const tags = /<\/?([a-z][\w:-]*)\b[^>]*>/giu
  tags.lastIndex = start
  let depth = 1
  for (const match of html.matchAll(tags)) {
    if (match[1].toLocaleLowerCase('en-GB') !== tag) continue
    if (match[0].startsWith('</')) {
      depth -= 1
      if (depth === 0) return (match.index ?? 0) + match[0].length
    } else if (!match[0].endsWith('/>')) {
      depth += 1
    }
  }
  return null
}

function isMarkedNavigationContainer(tag: string, attributes: string): boolean {
  if (tag === 'nav') return true
  const role = htmlAttributeValue(attributes, 'role')
  if (role?.split(/\s+/).includes('navigation')) return true
  const marker = /\b(?:toc|table[-_ ]of[-_ ]contents|navigation)\b/iu
  return marker.test(htmlAttributeValue(attributes, 'class') ?? '') || marker.test(htmlAttributeValue(attributes, 'id') ?? '')
}

function stripMarkedNavigationContainers(html: string): string {
  const openingTags = /<([a-z][\w:-]*)\b([^>]*)>/giu
  let output = ''
  let cursor = 0
  for (const match of html.matchAll(openingTags)) {
    const index = match.index ?? 0
    if (index < cursor) continue
    const tag = match[1].toLocaleLowerCase('en-GB')
    if (!isMarkedNavigationContainer(tag, match[2])) continue
    const end = navigationContainerEnd(html, index + match[0].length, tag)
    if (end === null) continue
    output += html.slice(cursor, index)
    cursor = end
  }
  return output + html.slice(cursor)
}

function fragmentTargetIDs(html: string): Set<string> {
  const ids = new Set<string>()
  for (const match of html.matchAll(/\bid\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'=<>]+))/giu)) {
    const value = match[1] ?? match[2] ?? match[3]
    if (value) ids.add(value)
  }
  return ids
}

function contentsNavigationLinks(value: string): string[] | null {
  const links: string[] = []
  for (const match of value.matchAll(/<a\b([^>]*)>[\s\S]*?<\/a>/giu)) {
    const href = htmlAttributeValue(match[1], 'href')
    if (!href || !/^#[^\s#]+$/u.test(href)) return null
    links.push(href.slice(1))
  }
  const prose = value.replace(/<a\b[^>]*>[\s\S]*?<\/a>/giu, '').replace(/<[^>]+>/g, '').replace(/\s+/g, '')
  return links.length > 0 && prose === '' ? links : null
}

function stripContentsNavigation(html: string): string {
  const targets = fragmentTargetIDs(html)
  return html.replace(
    /<h([1-6])\b[^>]*>([\s\S]*?)<\/h\1>\s*<(table|ol|ul)\b[^>]*>([\s\S]*?)<\/\3>/giu,
    (block: string, _level: string, heading: string, _container: string, content: string) => {
      const label = decodeHTMLText(headingTextFromHTML(heading)).replace(/\s+/g, ' ').trim().toLocaleLowerCase('en-GB')
      const links = contentsNavigationLinks(content)
      if ((label !== 'contents' && label !== 'table of contents') || links === null || !links.every((target) => targets.has(target))) return block
      return ''
    },
  )
}

function stripHTMLNavigation(html: string): string {
  return stripContentsNavigation(stripMarkedNavigationContainers(html))
}

export function htmlStoryToMarkdown(html: string): string {
  return decodeHTMLText(
    normaliseNewlines(stripHTMLNavigation(html))
      .replace(/<(script|style)\b[^>]*>[\s\S]*?<\/\1>/giu, '')
      .replace(/<h1\b[^>]*>([\s\S]*?)<\/h1>/giu, (_match, value: string) =>
        markdownHeading(1, value),
      )
      .replace(/<h[2-6]\b[^>]*>([\s\S]*?)<\/h[2-6]>/giu, (_match, value: string) =>
        markdownHeading(2, value),
      )
      .replace(/<li\b[^>]*>/giu, '\n- ')
      .replace(/<br\s*\/?>/giu, '\n')
      .replace(/<\/(p|div|li|blockquote|section|article)>/giu, '\n\n')
      .replace(/<[^>]+>/g, ''),
  )
    .replace(/[ \t]+\n/g, '\n')
    .replace(/\n{3,}/g, '\n\n')
    .trim()
}

export function convertImportedStoryFile(file: ImportedStoryFile): ImportedStory {
  const extension = file.filename.split('.').pop()?.toLocaleLowerCase('en-GB') ?? ''
  if (!supportedStoryExtensions.has(extension)) {
    throw new Error('Choose a .txt, .md, .markdown, .html or .htm file.')
  }
  if (!file.text.trim() || file.text.includes('\uFFFD')) {
    throw new Error('This file could not be read as text.')
  }

  const metadata = inferStoryMetadataFromFilename(file.filename)
  const isHTML =
    file.mediaType.toLocaleLowerCase('en-GB').includes('html') ||
    extension === 'html' ||
    extension === 'htm'
  const source = isHTML ? htmlStoryToMarkdown(file.text) : file.text
  const markdown = ensureStoryH1(
    metadata.title,
    promoteStoryChapters(stripGutenbergBoilerplate(source)),
  )
  return { filename: file.filename, ...metadata, markdown }
}

export function importWouldReplaceSubstantialMarkdown(
  currentMarkdown: string,
  importedMarkdown: string,
): boolean {
  const current = normaliseNewlines(currentMarkdown).trim()
  return current.length >= 200 && current !== importedMarkdown.trim()
}
