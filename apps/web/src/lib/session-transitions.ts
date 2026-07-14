export type LockTransition = {
  requestLogout: () => Promise<void>
  clearAccountState: () => void
  markLocked: () => void
  navigateToUnlock: () => Promise<unknown>
}

export type LockTransitionResult = 'navigated' | 'navigation-failed'

// Vue Router resolves successful navigation with undefined and resolves
// prevented/cancelled navigation with a NavigationFailure object.
export function navigationDidFail(result: unknown): boolean {
  return result !== undefined
}

export async function runLockTransition(
  transition: LockTransition
): Promise<LockTransitionResult> {
  await transition.requestLogout()
  transition.clearAccountState()
  transition.markLocked()

  try {
    const result = await transition.navigateToUnlock()
    return navigationDidFail(result) ? 'navigation-failed' : 'navigated'
  } catch {
    return 'navigation-failed'
  }
}
