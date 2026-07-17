export type ReaderLoadToken = {
  generation: number
  signal: AbortSignal
}

export type ReaderLoadGeneration = {
  begin: () => ReaderLoadToken
  isCurrent: (generation: number) => boolean
  cancel: () => void
}

export function createReaderLoadGeneration(): ReaderLoadGeneration {
  let generation = 0
  let controller: AbortController | null = null

  return {
    begin() {
      controller?.abort()
      controller = new AbortController()
      generation += 1
      return { generation, signal: controller.signal }
    },
    isCurrent(candidate) {
      return candidate === generation && controller?.signal.aborted === false
    },
    cancel() {
      controller?.abort()
      controller = null
      generation += 1
    },
  }
}
