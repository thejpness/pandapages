import type { AdminEditionBundleInput, AdminStoryEditionKey } from './api'

export const editionBundleDefinitions = [
  { editionKey: 'classic', filename: 'classic.md', label: 'Classic' },
  { editionKey: 'confident-readers', filename: 'confident-readers.md', label: 'Confident Readers' },
  { editionKey: 'growing-readers', filename: 'growing-readers.md', label: 'Growing Readers' },
  { editionKey: 'story-explorers', filename: 'story-explorers.md', label: 'Story Explorers' },
  { editionKey: 'little-listeners', filename: 'little-listeners.md', label: 'Little Listeners' },
] as const satisfies readonly { editionKey: AdminStoryEditionKey; filename: string; label: string }[]

export type EditionBundleLocalFile = Readonly<{ name: string; text: string }>
export type EditionBundleItem = Readonly<{
  editionKey: AdminStoryEditionKey
  filename: string
  label: string
  markdown: string
  characterCount: number
}>
export type EditionBundleSelection = Readonly<{ items: readonly EditionBundleItem[] }>

export class EditionBundleFileError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'EditionBundleFileError'
  }
}

const expectedFilenames = editionBundleDefinitions.map((item) => item.filename).join(', ')

export function parseEditionBundleFiles(files: readonly EditionBundleLocalFile[]): EditionBundleSelection {
  if (files.length !== editionBundleDefinitions.length) {
    throw new EditionBundleFileError(`Choose exactly these five files: ${expectedFilenames}.`)
  }
  const byName = new Map<string, string>()
  for (const file of files) {
    if (!editionBundleDefinitions.some((item) => item.filename === file.name)) {
      throw new EditionBundleFileError(`Use the exact Panda Pages filenames: ${expectedFilenames}.`)
    }
    if (byName.has(file.name)) {
      throw new EditionBundleFileError(`The file ${file.name} was selected more than once.`)
    }
    if (!file.text.trim()) throw new EditionBundleFileError(`${file.name} is empty.`)
    if (file.text.includes('\uFFFD')) {
      throw new EditionBundleFileError(`${file.name} could not be read as clean UTF-8 text.`)
    }
    byName.set(file.name, file.text)
  }
  return Object.freeze({
    items: Object.freeze(editionBundleDefinitions.map((definition) => {
      const markdown = byName.get(definition.filename)
      if (markdown === undefined) {
        throw new EditionBundleFileError(`Missing ${definition.filename}. Choose all five edition files together.`)
      }
      return Object.freeze({ ...definition, markdown, characterCount: markdown.length })
    })),
  })
}

export function editionBundleInputs(selection: EditionBundleSelection): AdminEditionBundleInput[] {
  return selection.items.map((item) => ({ editionKey: item.editionKey, markdown: item.markdown }))
}

export function inferEditionBundleTitle(selection: EditionBundleSelection): string | null {
  const classic = selection.items.find((item) => item.editionKey === 'classic')
  if (!classic) return null
  const match = /^#\s+(.+?)\s*$/mu.exec(classic.markdown)
  const title = match?.[1]?.trim() ?? ''
  return title || null
}
