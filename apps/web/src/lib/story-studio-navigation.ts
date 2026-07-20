import type {
  AdminDraftOutcome,
  AdminStoryDetail,
  AdminStoryListItem,
  AdminStoryStatus,
  AdminVersionHealth,
  AdminVersionSummary,
  JsonObject,
} from './api'

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
  return version.health === 'ready' && !version.isPublished
}

export function storyCanUnpublish(story: AdminStoryDetail): boolean {
  return story.publishedVersion !== null
}

export function draftOutcomeMessage(
  outcome: AdminDraftOutcome,
  version: number,
): string {
  if (outcome === 'created_story') return `Story created as draft version ${version}.`
  if (outcome === 'created_version') return `Draft version ${version} created.`
  return `Existing healthy version ${version} reused.`
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
      message: 'Unlock Panda Pages to continue in Story Studio.',
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
