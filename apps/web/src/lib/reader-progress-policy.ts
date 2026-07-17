import type { ProgressBaselineStatus } from './progress-baseline-controller'
import type { ProgressSaveStatus } from './progress-save-coordinator'

export type ReaderProgressRetryKind = 'baseline' | 'save' | null

export type ReaderProgressPresentation = {
  text: string
  retryKind: ReaderProgressRetryKind
  retryDisabled: boolean
}

export function readerProgressPresentation({
  baselineStatus,
  baselineAttempt,
  saveStatus,
}: {
  baselineStatus: ProgressBaselineStatus
  baselineAttempt: number
  saveStatus: ProgressSaveStatus
}): ReaderProgressPresentation {
  const checking = baselineStatus === 'loading' && baselineAttempt > 1
  if (checking) {
    return {
      text: 'Checking progress…',
      retryKind: 'baseline',
      retryDisabled: true,
    }
  }
  if (baselineStatus === 'unavailable') {
    return {
      text: 'Progress unavailable',
      retryKind: 'baseline',
      retryDisabled: false,
    }
  }
  if (baselineStatus !== 'ready') {
    return { text: '', retryKind: null, retryDisabled: false }
  }

  const text = {
    idle: '',
    dirty: 'Unsaved',
    saving: 'Saving…',
    saved: 'Saved',
    error: 'Save failed',
  }[saveStatus]
  return {
    text,
    retryKind: saveStatus === 'error' ? 'save' : null,
    retryDisabled: saveStatus === 'saving',
  }
}

export function readerLifecyclePersistenceAllowed({
  baselineStatus,
  sessionLoss,
  decisionPending,
  awaitingIntent,
}: {
  baselineStatus: ProgressBaselineStatus
  sessionLoss: boolean
  decisionPending: boolean
  awaitingIntent: boolean
}): boolean {
  return (
    baselineStatus === 'ready' &&
    !sessionLoss &&
    !decisionPending &&
    !awaitingIntent
  )
}

export function readerLibraryPersistenceStrategy(
  baselineStatus: ProgressBaselineStatus,
): 'immediate' | 'drain' {
  return baselineStatus === 'ready' ? 'drain' : 'immediate'
}
