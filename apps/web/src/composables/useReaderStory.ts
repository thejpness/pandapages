import { nextTick, ref, shallowRef } from 'vue'
import {
  getAPIErrorStatus,
  getReaderStory,
  type ReaderStoryPayload,
} from '../lib/api'
import { readerContentFailure, type ReaderContentState } from '../lib/reader-content-state'
import { createReaderLoadGeneration } from '../lib/reader-load-generation'

export type UseReaderStoryOptions = {
  onSessionEnded: (slug: string) => Promise<void> | void
  onReady?: (story: ReaderStoryPayload) => Promise<void> | void
}

export function useReaderStory(options: UseReaderStoryOptions) {
  const story = shallowRef<ReaderStoryPayload | null>(null)
  const contentState = ref<ReaderContentState>({ status: 'loading' })
  const loads = createReaderLoadGeneration()

  async function load(slug: string): Promise<void> {
    const token = loads.begin()
    story.value = null
    contentState.value = { status: 'loading' }

    try {
      const loaded = await getReaderStory(slug, token.signal)
      if (!loads.isCurrent(token.generation)) return
      if (loaded.slug !== slug) throw new Error('Reader response slug mismatch')

      story.value = loaded
      contentState.value = { status: 'ready' }
      document.title = loaded.title + ' · Panda Pages'
      await nextTick()
      if (loads.isCurrent(token.generation)) await options.onReady?.(loaded)
    } catch (error) {
      if (!loads.isCurrent(token.generation)) return
      if (error instanceof DOMException && error.name === 'AbortError') return
      if (getAPIErrorStatus(error) === 401) {
        await options.onSessionEnded(slug)
        return
      }
      story.value = null
      contentState.value = readerContentFailure(getAPIErrorStatus(error))
      await nextTick()
    }
  }

  function dispose() {
    loads.cancel()
    story.value = null
  }

  return { story, contentState, load, dispose }
}
