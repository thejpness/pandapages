import type {
  AdminReleaseEditionRequest,
  AdminReleaseSummary,
  AdminStoryDetail,
  AdminStoryEditionKey,
  AdminVersionSummary,
} from './api'

const storyReleaseEditionOrder: readonly AdminStoryEditionKey[] = [
  'classic',
  'confident-readers',
  'growing-readers',
  'story-explorers',
  'little-listeners',
]

export type StoryReleaseCandidateRow = Readonly<{
  editionKey: AdminStoryEditionKey
  versions: readonly AdminVersionSummary[]
  selectedVersionId: string | null
  included: boolean
}>

function releaseEdition(
  release: AdminReleaseSummary | null,
  editionKey: AdminStoryEditionKey,
) {
  return release?.editions.find((edition) => edition.editionKey === editionKey) ?? null
}

function healthyVersion(
  versions: readonly AdminVersionSummary[],
  versionId: string | null | undefined,
): AdminVersionSummary | null {
  if (!versionId) return null
  return (
    versions.find(
      (version) =>
        version.versionId === versionId && version.health === 'ready',
    ) ?? null
  )
}

export function buildStoryReleaseCandidate(
  story: AdminStoryDetail,
): StoryReleaseCandidateRow[] {
  const reference = story.currentRelease ?? story.releases[0] ?? null
  const firstRelease = reference === null

  return storyReleaseEditionOrder.map((editionKey) => {
    const edition = story.editions.find((item) => item.editionKey === editionKey)
    const versions = (edition?.versions ?? []).filter(
      (version) => version.health === 'ready',
    )
    const referenceEdition = releaseEdition(reference, editionKey)
    const draft = healthyVersion(versions, edition?.draftVersion?.versionId)
    const referenced = healthyVersion(versions, referenceEdition?.versionId)
    const selected = referenceEdition
      ? (draft ?? referenced ?? versions[0] ?? null)
      : (draft ?? versions[0] ?? null)

    return {
      editionKey,
      versions,
      selectedVersionId: selected?.versionId ?? null,
      included: firstRelease ? selected !== null : referenceEdition !== null,
    }
  })
}

export function releaseCandidateRequest(
  rows: readonly StoryReleaseCandidateRow[],
): AdminReleaseEditionRequest[] {
  return storyReleaseEditionOrder.flatMap((editionKey) => {
    const row = rows.find((item) => item.editionKey === editionKey)
    return row?.included && row.selectedVersionId
      ? [{ editionKey, versionId: row.selectedVersionId }]
      : []
  })
}

export function releaseCandidateMatchesCurrent(
  request: readonly AdminReleaseEditionRequest[],
  current: AdminReleaseSummary | null,
): boolean {
  if (!current || request.length !== current.editions.length) return false
  return request.every((item, index) => {
    const live = current.editions[index]
    return (
      live?.editionKey === item.editionKey &&
      live.versionId === item.versionId
    )
  })
}
