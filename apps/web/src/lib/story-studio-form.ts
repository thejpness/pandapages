import type {
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

export function normaliseStoryForm(form: StoryStudioForm): AdminStoryInput {
  const rights = cloneJsonObject(form.rights)
  const rightsLabel = form.rightsLabel.trim()
  if (rightsLabel) rights.label = rightsLabel
  else delete rights.label

  return {
    slug: slugifyStoryTitle(form.slug),
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

export function storyFormFingerprint(form: StoryStudioForm): string {
  return JSON.stringify(canonicalJson(normaliseStoryForm(form)))
}

export function storyFormIsDirty(
  form: StoryStudioForm,
  baselineFingerprint: string,
): boolean {
  return storyFormFingerprint(form) !== baselineFingerprint
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

export function htmlStoryToMarkdown(html: string): string {
  return decodeHTMLText(
    normaliseNewlines(html)
      .replace(/<(script|style)\b[^>]*>[\s\S]*?<\/\1>/giu, '')
      .replace(/<h1\b[^>]*>([\s\S]*?)<\/h1>/giu, '\n# $1\n')
      .replace(/<h[2-6]\b[^>]*>([\s\S]*?)<\/h[2-6]>/giu, '\n## $1\n')
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
