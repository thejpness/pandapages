export type ReaderModeTransitionPlan<T> = {
  anchor: T | null
}

export function planReaderModeTransition<T>(
  current: T | null,
): ReaderModeTransitionPlan<T> {
  return {
    anchor: current,
  }
}
