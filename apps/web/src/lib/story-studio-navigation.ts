import type {
  AdminDraftOutcome,
  AdminEditionDetail,
  AdminEditionStatus,
  AdminSourceOutcome,
  AdminSourceStatus,
  AdminStoryDetail,
  AdminStoryEditionKey,
  AdminStoryListItem,
  AdminStoryStatus,
  AdminVersionHealth,
  AdminVersionSummary,
  JsonObject,
} from './api'

export const storyEditionOrder: readonly AdminStoryEditionKey[] = [
  'classic',
  'confident-readers',
  'growing-readers',
  'story-explorers',
  'little-listeners',
]

const editionLabels: Record<AdminStoryEditionKey, string> = {
  classic: 'Classic',
  'confident-readers': 'Confident Readers',
  'growing-readers': 'Growing Readers',
  'story-explorers': 'Story Explorers',
  'little-listeners': 'Little Listeners',
}
const editionDescriptions: Record<AdminStoryEditionKey, string> = {
  classic: 'The fullest Panda Pages adaptation and current Reader publication edition.',
  'confident-readers': 'A substantial independent reading edition with streamlined scope.',
  'growing-readers': 'A supported reading edition with reduced narrative complexity.',
  'story-explorers': 'A shorter exploratory edition built around the core story journey.',
  'little-listeners': 'The most compact read-aloud edition with the clearest narrative line.',
}
const editionStatusLabels: Record<AdminEditionStatus, string> = {
  empty: 'Not started',
  draft_only: 'Draft only',
  published: 'Published',
  published_with_draft: 'Published · New draft',
  unpublished: 'Unpublished',
  repair_required: 'Needs attention',
}
const sourceStatusLabels: Record<AdminSourceStatus, string> = {
  missing: 'Source not added',
  ready: 'Source ready',
  repair_required: 'Source needs attention',
}

export function parseStoryEditionKey(value: unknown): AdminStoryEditionKey | null {
  return typeof value === 'string' && storyEditionOrder.includes(value as AdminStoryEditionKey)
    ? (value as AdminStoryEditionKey)
    : null
}
export function storyEditionLabel(key: AdminStoryEditionKey): string { return editionLabels[key] }
export function storyEditionDescription(key: AdminStoryEditionKey): string { return editionDescriptions[key] }
export function editionStatusLabel(status: AdminEditionStatus): string { return editionStatusLabels[status] }
export function sourceStatusLabel(status: AdminSourceStatus): string { return sourceStatusLabels[status] }
export function editionPreferredVersion(edition: AdminEditionDetail): AdminVersionSummary | null {
  for (const pointer of [edition.draftVersion, edition.publishedVersion]) {
    if (!pointer) continue
    const version = edition.versions.find((candidate) => candidate.versionId === pointer.versionId && candidate.health === 'ready')
    if (version) return version
  }
  return edition.versions.find((version) => version.health === 'ready') ?? null
}
export function editionStartedCount(editions: readonly { status: AdminEditionStatus }[]): number {
  return editions.filter((edition) => edition.status !== 'empty').length
}

function apiErrorStatus(error: unknown): number | undefined {
  if (!(error instanceof Error)) return undefined
  const status = (error as Error & { status?: unknown }).status
  return typeof status === 'number' ? status : undefined
}

export const storyStatusOrder: readonly AdminStoryStatus[] = [
  'draft_only',
  'published',
  'published_with_draft',
  'unpublished',
  'repair_required',
]

const statusLabels: Record<AdminStoryStatus, string> = {
  draft_only: 'Draft only',
  published: 'Published',
  published_with_draft: 'Published · New draft',
  unpublished: 'Unpublished',
  repair_required: 'Needs attention',
}

const healthLabels: Record<AdminVersionHealth, string> = {
  ready: 'Ready',
  repair_required: 'Needs repair',
  unavailable: 'Unavailable',
}

export function storyStatusLabel(status: AdminStoryStatus): string {
  return statusLabels[status]
}

export function versionHealthLabel(health: AdminVersionHealth): string {
  return healthLabels[health]
}

export function storyRightsSummary(rights: JsonObject): string {
  if (typeof rights.label === 'string' && rights.label.trim()) {
    return rights.label.trim()
  }
  return Object.keys(rights).length > 0 ? 'Rights recorded' : 'Rights not specified'
}

export function filterStoryCatalogue(
  items: readonly AdminStoryListItem[],
  query: string,
  status: AdminStoryStatus | 'all',
): AdminStoryListItem[] {
  const needle = query.trim().toLocaleLowerCase('en-GB')
  return items.filter((story) => {
    if (status !== 'all' && story.status !== status) return false
    if (!needle) return true
    return [story.title, story.author ?? '', story.slug].some((value) =>
      value.toLocaleLowerCase('en-GB').includes(needle),
    )
  })
}

export function versionRoleLabels(version: AdminVersionSummary): string[] {
  const labels: string[] = []
  if (version.isPublished) labels.push('Published')
  if (version.isDraft) labels.push('Current draft')
  if (labels.length === 0) labels.push('Historical')
  return labels
}

export function versionCanSeedDraft(version: AdminVersionSummary): boolean {
  return version.health === 'ready'
}

export function versionCanPublish(version: AdminVersionSummary): boolean {
  return version.editionKey === 'classic' && version.health === 'ready' && !version.isPublished
}

export function storyCanUnpublish(story: AdminStoryDetail): boolean {
  return story.publishedVersion !== null
}

export function draftOutcomeMessage(
  outcome: AdminDraftOutcome,
  version: number,
  editionKey: AdminStoryEditionKey,
): string {
  const edition = storyEditionLabel(editionKey)
  if (outcome === 'created_story') return `Story created with ${edition} draft version ${version}.`
  if (outcome === 'created_version') return `${edition} draft version ${version} created.`
  return `Existing healthy ${edition} version ${version} reused.`
}

export function sourceOutcomeMessage(outcome: AdminSourceOutcome, version: number): string {
  if (outcome === 'created_source') return `Canonical source added as revision ${version}.`
  if (outcome === 'created_version') return `Canonical source revision ${version} created.`
  return `Existing canonical source revision ${version} restored as current.`
}

export function previewIsOutdated(
  previewFingerprint: string | null,
  currentFingerprint: string,
): boolean {
  return previewFingerprint !== null && previewFingerprint !== currentFingerprint
}

export type StoryStudioErrorKind =
  | 'session'
  | 'forbidden'
  | 'not-found'
  | 'repair'
  | 'validation'
  | 'retry'

export type StoryStudioError = {
  kind: StoryStudioErrorKind
  title: string
  message: string
  retryable: boolean
}

export function projectStoryStudioError(error: unknown): StoryStudioError {
  const status = apiErrorStatus(error)
  if (status === 401) {
    return {
      kind: 'session',
      title: 'Session ended',
      message: 'Sign in to Panda Pages to continue in Story Studio.',
      retryable: false,
    }
  }
  if (status === 403) {
    return {
      kind: 'forbidden',
      title: 'Story Studio is unavailable',
      message: 'Administrator access is not available for this request.',
      retryable: false,
    }
  }
  if (status === 404) {
    return {
      kind: 'not-found',
      title: 'Story unavailable',
      message: 'This story or version could not be opened.',
      retryable: false,
    }
  }
  if (status === 409) {
    return {
      kind: 'repair',
      title: 'Needs attention',
      message: 'The stored version cannot safely be reused or published.',
      retryable: false,
    }
  }
  if (status === 400 || status === 413) {
    return {
      kind: 'validation',
      title: 'Check the story',
      message:
        status === 413
          ? 'This story is too large to process.'
          : 'Some story fields need attention.',
      retryable: false,
    }
  }
  return {
    kind: 'retry',
    title: 'Story Studio could not finish that request',
    message: 'The connection or server may be temporarily unavailable. Try again.',
    retryable: true,
  }
}
