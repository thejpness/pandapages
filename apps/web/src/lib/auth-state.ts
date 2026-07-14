export type AuthState = 'unknown' | 'unlocked' | 'locked' | 'unavailable'

export type AuthStateSnapshot = {
  state: AuthState
  checkedAt: number | null
}

export type VerifySession = () => Promise<boolean>

export type AuthStateCache = {
  current: () => AuthStateSnapshot
  verify: () => Promise<AuthState>
  retry: () => Promise<AuthState>
  confirmUnlocked: () => void
  confirmLocked: () => void
  invalidate: () => void
}

const defaultCacheDurationMs = 5_000

export function createAuthStateCache(
  verifySession: VerifySession,
  cacheDurationMs = defaultCacheDurationMs,
  now: () => number = Date.now
): AuthStateCache {
  let snapshot: AuthStateSnapshot = { state: 'unknown', checkedAt: null }
  let pending: Promise<AuthState> | null = null
  let revision = 0

  function setConfirmed(state: 'unlocked' | 'locked') {
    revision += 1
    snapshot = { state, checkedAt: now() }
  }

  function invalidate() {
    revision += 1
    snapshot = { state: 'unknown', checkedAt: null }
  }

  function isFresh() {
    return (
      (snapshot.state === 'unlocked' || snapshot.state === 'locked') &&
      snapshot.checkedAt !== null &&
      now() - snapshot.checkedAt < cacheDurationMs
    )
  }

  async function runVerification(force: boolean): Promise<AuthState> {
    if (!force) {
      if (isFresh()) return snapshot.state
      if (snapshot.state === 'unavailable') return snapshot.state
    }
    if (pending) return pending

    const startedAtRevision = revision
    pending = (async () => {
      try {
        const unlocked = await verifySession()
        if (revision === startedAtRevision) {
          snapshot = {
            state: unlocked ? 'unlocked' : 'locked',
            checkedAt: now(),
          }
        }
      } catch {
        if (revision === startedAtRevision) {
          snapshot = { state: 'unavailable', checkedAt: now() }
        }
      } finally {
        pending = null
      }

      return snapshot.state
    })()

    return pending
  }

  return {
    current: () => ({ ...snapshot }),
    verify: () => runVerification(false),
    retry: () => runVerification(true),
    confirmUnlocked: () => setConfirmed('unlocked'),
    confirmLocked: () => setConfirmed('locked'),
    invalidate,
  }
}
