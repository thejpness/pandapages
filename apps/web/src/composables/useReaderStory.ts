import { nextTick, ref, shallowRef } from 'vue'
import {
  getAPIErrorStatus,
  getReaderStory,
  setReaderStoryEdition,
  type ReaderEditionKey,
  type ReaderResolvedStoryPayload,
  type ReaderResolutionPayload,
} from '../lib/api'
import { readerContentFailure, type ReaderContentState } from '../lib/reader-content-state'
import { createReaderLoadGeneration } from '../lib/reader-load-generation'
import { isProfileSessionInvalidError } from '../lib/profile-session'

export type UseReaderStoryOptions = {
  onSessionEnded: (slug: string) => Promise<void> | void
  onProfileInvalid: (slug: string) => Promise<void> | void
  onReady?: (story: ReaderResolvedStoryPayload) => Promise<void> | void
}

export function useReaderStory(options: UseReaderStoryOptions) {
  const story = shallowRef<ReaderResolvedStoryPayload | null>(null)
  const eligibleEditions = shallowRef<readonly ReaderEditionKey[]>([])
  const chooser = shallowRef<readonly ReaderEditionKey[] | null>(null)
  const editionBusy = ref(false)
  const contentState = ref<ReaderContentState>({ status: 'loading' })
  const loads = createReaderLoadGeneration()

  async function applyResolution(
    slug: string,
    resolution: ReaderResolutionPayload,
    generation: number,
  ): Promise<void> {
    if (!loads.isCurrent(generation)) return
    eligibleEditions.value = resolution.eligibleEditions

    if (resolution.state === 'chooser') {
      story.value = null
      chooser.value = resolution.eligibleEditions
      contentState.value = { status: 'ready' }
      document.title = 'Choose a story edition · Panda Pages'
      await nextTick()
      return
    }

    if (resolution.story.slug !== slug) {
      throw new Error('Reader response slug mismatch')
    }

    chooser.value = null
    story.value = resolution.story
    contentState.value = { status: 'ready' }
    document.title = resolution.story.title + ' · Panda Pages'
    await nextTick()
    if (loads.isCurrent(generation)) await options.onReady?.(resolution.story)
  }

  async function handleFailure(slug: string, error: unknown, generation: number) {
    if (!loads.isCurrent(generation)) return
    if (error instanceof DOMException && error.name === 'AbortError') return
    if (getAPIErrorStatus(error) === 401) {
      await options.onSessionEnded(slug)
      return
    }
    if (isProfileSessionInvalidError(error)) {
      await options.onProfileInvalid(slug)
      return
    }
    story.value = null
    chooser.value = null
    eligibleEditions.value = []
    contentState.value = readerContentFailure(getAPIErrorStatus(error))
    await nextTick()
  }

  async function load(slug: string, profileID: string): Promise<void> {
    const token = loads.begin()
    story.value = null
    chooser.value = null
    eligibleEditions.value = []
    contentState.value = { status: 'loading' }

    try {
      const resolution = await getReaderStory(slug, profileID, token.signal)
      await applyResolution(slug, resolution, token.generation)
    } catch (error) {
      await handleFailure(slug, error, token.generation)
    }
  }

  async function chooseEdition(
    slug: string,
    profileID: string,
    editionKey: ReaderEditionKey,
  ): Promise<void> {
    const token = loads.begin()
    editionBusy.value = true
    story.value = null
    chooser.value = null
    contentState.value = { status: 'loading' }

    try {
      await setReaderStoryEdition(slug, profileID, editionKey, token.signal)
      if (!loads.isCurrent(token.generation)) return

      const resolution = await getReaderStory(slug, profileID, token.signal)
      if (
        resolution.state !== 'selected' ||
        resolution.story.editionKey !== editionKey
      ) {
        throw new Error('Reader edition selection was not authoritative')
      }
      await applyResolution(slug, resolution, token.generation)
    } catch (error) {
      if (getAPIErrorStatus(error) === 404 && loads.isCurrent(token.generation)) {
        try {
          const resolution = await getReaderStory(slug, profileID, token.signal)
          await applyResolution(slug, resolution, token.generation)
          return
        } catch (reloadError) {
          await handleFailure(slug, reloadError, token.generation)
        }
      } else {
        await handleFailure(slug, error, token.generation)
      }
    } finally {
      if (loads.isCurrent(token.generation)) editionBusy.value = false
    }
  }

  function dispose() {
    loads.cancel()
    story.value = null
    chooser.value = null
    eligibleEditions.value = []
    editionBusy.value = false
  }

  return {
    story,
    eligibleEditions,
    chooser,
    editionBusy,
    contentState,
    load,
    chooseEdition,
    dispose,
  }
}
