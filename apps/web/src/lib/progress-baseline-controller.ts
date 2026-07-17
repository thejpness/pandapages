export type ProgressBaselineStatus = 'loading' | 'ready' | 'unavailable'

export type ProgressBaselineState<T> =
  | {
      status: 'loading'
      value: null
      error: null
      attempt: number
    }
  | {
      status: 'ready'
      value: T
      error: null
      attempt: number
    }
  | {
      status: 'unavailable'
      value: null
      error: unknown
      attempt: number
    }

export type ProgressBaselineController<T> = {
  load: () => Promise<ProgressBaselineState<T>>
  retry: () => Promise<ProgressBaselineState<T>>
  current: () => ProgressBaselineState<T>
  subscribe: (
    listener: (state: ProgressBaselineState<T>) => void
  ) => () => void
  dispose: () => void
}

export type ProgressBaselineControllerOptions<T> = {
  load: () => Promise<T>
}

export function createProgressBaselineController<T>(
  options: ProgressBaselineControllerOptions<T>
): ProgressBaselineController<T> {
  const listeners = new Set<(state: ProgressBaselineState<T>) => void>()
  let state: ProgressBaselineState<T> = {
    status: 'loading',
    value: null,
    error: null,
    attempt: 0,
  }
  let activeLoad: Promise<ProgressBaselineState<T>> | null = null
  let disposed = false

  function current(): ProgressBaselineState<T> {
    return { ...state }
  }

  function emit() {
    if (disposed) return
    const snapshot = current()
    for (const listener of listeners) listener(snapshot)
  }

  function transition(next: ProgressBaselineState<T>) {
    if (disposed) return
    state = next
    emit()
  }

  function load(): Promise<ProgressBaselineState<T>> {
    if (disposed || state.status === 'ready') {
      return Promise.resolve(current())
    }
    if (activeLoad) return activeLoad

    const attempt = state.attempt + 1
    transition({
      status: 'loading',
      value: null,
      error: null,
      attempt,
    })

    const request = Promise.resolve().then(options.load)
    const result = request.then<
      ProgressBaselineState<T>,
      ProgressBaselineState<T>
    >(
      (value) => {
        if (!disposed) {
          transition({
            status: 'ready',
            value,
            error: null,
            attempt,
          })
        }
        return current()
      },
      (error: unknown) => {
        if (!disposed) {
          transition({
            status: 'unavailable',
            value: null,
            error,
            attempt,
          })
        }
        return current()
      }
    )

    activeLoad = result
    void result.finally(() => {
      if (activeLoad === result) activeLoad = null
    })
    return result
  }

  function subscribe(listener: (state: ProgressBaselineState<T>) => void) {
    if (disposed) return () => {}
    listeners.add(listener)
    listener(current())
    return () => listeners.delete(listener)
  }

  function dispose() {
    if (disposed) return
    disposed = true
    activeLoad = null
    listeners.clear()
  }

  return {
    load,
    retry: load,
    current,
    subscribe,
    dispose,
  }
}
