export type ReaderProfileSelection = Readonly<{
  id: string
}>

export type ReaderProfileCandidate = Readonly<{
  id: string
}>

export const SELECTED_READER_PROFILE_STORAGE_KEY =
  'pandapages.selected-reader-profile-id'

type StorageLike = Pick<Storage, 'getItem' | 'removeItem' | 'setItem'>

function browserStorage(): StorageLike {
  return window.localStorage
}

export function selectedReaderProfileID(
  storage: StorageLike = browserStorage(),
): string | null {
  try {
    const value = storage.getItem(SELECTED_READER_PROFILE_STORAGE_KEY)
    return value && value.trim() === value ? value : null
  } catch {
    return null
  }
}

export function selectReaderProfile(
  profileID: string,
  storage: StorageLike = browserStorage(),
): boolean {
  if (!profileID || profileID.trim() !== profileID) return false
  try {
    storage.setItem(SELECTED_READER_PROFILE_STORAGE_KEY, profileID)
    return true
  } catch {
    return false
  }
}

export function clearSelectedReaderProfile(
  storage: StorageLike = browserStorage(),
): void {
  try {
    storage.removeItem(SELECTED_READER_PROFILE_STORAGE_KEY)
  } catch {
    // Storage is an optional UX convenience, never an authority boundary.
  }
}

export function resolvePersistedReaderProfileSelection(
  persistedID: string | null,
  profiles: readonly ReaderProfileCandidate[],
): ReaderProfileSelection | null {
  if (persistedID === null) return null
  const profile = profiles.find((candidate) => candidate.id === persistedID)
  return profile ? { id: profile.id } : null
}

// reconcileReaderProfileSelection trusts only the current server-returned
// account-scoped list. It never chooses an arbitrary profile: auto-selection
// is limited to the unambiguous one-profile state used by the Profiles screen.
export function reconcileReaderProfileSelection(
  persistedID: string | null,
  profiles: readonly ReaderProfileCandidate[],
): ReaderProfileSelection | null {
  const persisted = resolvePersistedReaderProfileSelection(
    persistedID,
    profiles,
  )
  if (persisted !== null) return persisted
  if (profiles.length === 1) return { id: profiles[0].id }
  return null
}
