export type ReaderContentState =
  | { status: 'loading' }
  | { status: 'ready' }
  | { status: 'not-found' }
  | { status: 'unavailable' }

export function readerContentFailure(
  status: number | undefined,
): ReaderContentState {
  return status === 404 ? { status: 'not-found' } : { status: 'unavailable' }
}
