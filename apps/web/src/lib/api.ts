const BASE = import.meta.env.VITE_API_BASE || ''
const ADMIN_KEY = import.meta.env.VITE_ADMIN_KEY || ''

export type APIErrorBody =
  | string
  | {
      error?: {
        code?: string
        message?: string
      }
      [k: string]: any
    }
  | null

export type APIError = Error & {
  status?: number
  code?: string
  body?: APIErrorBody
}

function buildHeaders(path: string, init: RequestInit): Headers {
  // Always return a Headers instance to keep TS happy.
  const headers = new Headers(init.headers as HeadersInit | undefined)

  const hasBody = init.body !== undefined && init.body !== null
  const isStringBody = typeof init.body === 'string'

  // Only set JSON content-type when body is a JSON string.
  // (Do NOT set for FormData)
  if (hasBody && isStringBody && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }

  // Only attach admin key for admin API routes
  if (path.startsWith('/api/v1/admin/') && ADMIN_KEY) {
    headers.set('X-PP-Admin-Key', ADMIN_KEY)
  }

  return headers
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    credentials: 'include',
    ...init,
    headers: buildHeaders(path, init),
  })

  const contentType = res.headers.get('content-type') || ''
  const isJSON = contentType.includes('application/json')

  let body: any = null
  if (res.status !== 204) {
    body = isJSON ? await res.json().catch(() => null) : await res.text().catch(() => '')
  }

  if (!res.ok) {
    const msg =
      typeof body === 'string'
        ? body || `Request failed: ${res.status}`
        : body?.error?.message || `Request failed: ${res.status}`

    const err: APIError = new Error(msg)
    err.status = res.status
    err.body = body
    err.code =
      body && typeof body === 'object' && body?.error?.code
        ? String(body.error.code)
        : undefined
    throw err
  }

  return body as T
}

/* ----------------------------- Auth ----------------------------- */

export async function unlock(passcode: string) {
  await request<{ ok: boolean }>('/api/v1/auth/unlock', {
    method: 'POST',
    body: JSON.stringify({ passcode }),
  })
}

export async function authStatus(): Promise<{ unlocked: boolean }> {
  try {
    return await request<{ unlocked: boolean }>('/api/v1/auth/status')
  } catch {
    return { unlocked: false }
  }
}

/* ---------------------------- Library --------------------------- */

export type LibraryItem = {
  slug: string
  title: string
  author: string | null
}

export async function getLibrary(): Promise<{ items: LibraryItem[] }> {
  const data = await request<{ items?: LibraryItem[] }>('/api/v1/library')
  return { items: Array.isArray(data.items) ? data.items : [] }
}

/* ----------------------------- Story ---------------------------- */

export type StoryPayload = {
  slug: string
  title: string
  author: string | null
  version: number
  renderedHtml: string
}

export async function getStory(slug: string): Promise<StoryPayload> {
  return request<StoryPayload>(`/api/v1/story/${encodeURIComponent(slug)}`)
}

export type StorySegment = {
  ordinal: number
  locator: unknown
  renderedHtml: string
}

export type StorySegmentsPayload = {
  slug: string
  version: number
  segments: StorySegment[]
}

export async function getStorySegments(slug: string): Promise<StorySegmentsPayload> {
  return request<StorySegmentsPayload>(`/api/v1/story/${encodeURIComponent(slug)}/segments`)
}

/* ----------------------------- Admin ---------------------------- */

export type AdminPreviewRequest = {
  markdown: string
}

export type AdminPreviewSegment = {
  ordinal: number
  locator: unknown
  renderedHtml: string
}

export type AdminPreviewResponse = {
  renderedHtml: string
  segments: AdminPreviewSegment[]
}

export async function adminPreview(payload: AdminPreviewRequest): Promise<AdminPreviewResponse> {
  return request<AdminPreviewResponse>('/api/v1/admin/preview', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export type AdminDraftUpsertRequest = {
  slug: string
  title: string
  author?: string | null
  markdown: string
  language?: string | null
  sourceUrl?: string | null
  rights?: Record<string, any>
}

export type AdminDraftUpsertResponse = {
  storyId: string
  storyVersionId: string
  slug: string
  version: number
  segmentsCount: number
  renderedHtml: string
}

export async function adminDraftUpsertStory(
  payload: AdminDraftUpsertRequest
): Promise<AdminDraftUpsertResponse> {
  return request<AdminDraftUpsertResponse>('/api/v1/admin/stories/draft', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export async function adminPublishStory(slug: string, versionId: string) {
  return request<{ ok: boolean }>(`/api/v1/admin/stories/${encodeURIComponent(slug)}/publish`, {
    method: 'POST',
    body: JSON.stringify({ versionId }),
  })
}

export type AdminStoryListItem = {
  slug: string
  title: string
  author: string | null
  isPublished: boolean
  updatedAt: string
}

export type AdminStoriesListResponse = {
  items: AdminStoryListItem[]
}

export async function adminListStories(): Promise<AdminStoriesListResponse> {
  const data = await request<{ items?: AdminStoryListItem[] }>('/api/v1/admin/stories')
  return { items: Array.isArray(data.items) ? data.items : [] }
}


/* ---------------------------- Progress -------------------------- */

export type ProgressState = {
  version: number
  locator: unknown | null
  percent: number
}

export async function getProgress(slug: string): Promise<ProgressState> {
  return request<ProgressState>(`/api/v1/progress/${encodeURIComponent(slug)}`)
}

export async function saveProgress(slug: string, version: number, locator: unknown, percent: number) {
  try {
    await request<{ ok: boolean }>(`/api/v1/progress/${encodeURIComponent(slug)}`, {
      method: 'PUT',
      body: JSON.stringify({ version, locator, percent }),
    })
  } catch {
    // ignore in v1
  }
}

/* ------------------------- Continue / Recent -------------------- */

export type ContinueItem = {
  slug: string
  percent: number
  updatedAt: string
}

export async function getContinue(limit = 3): Promise<{ items: ContinueItem[] }> {
  const data = await request<{ items?: ContinueItem[] }>(`/api/v1/continue?limit=${limit}`)
  return { items: Array.isArray(data.items) ? data.items : [] }
}

/* -------------------------- Settings / Journey ------------------- */

export type ChildProfile = {
  id?: string
  name: string
  ageMonths: number
  interests: string[]
  sensitivities: string[]
}

export type PromptProfile = {
  id?: string
  name: string
  schemaVersion: number
  rules: Record<string, any>
}

export type SettingsPayload = {
  child: ChildProfile
  prompt: PromptProfile
}

export type SettingsUpsert = {
  child: ChildProfile
  prompt: PromptProfile
}

function normaliseSettings(data: Partial<SettingsPayload> | null): SettingsPayload {
  const child = data?.child
  const prompt = data?.prompt

  return {
    child: {
      id: child?.id,
      name: child?.name ?? '',
      ageMonths: Number(child?.ageMonths ?? 0),
      interests: Array.isArray(child?.interests) ? (child?.interests as string[]) : [],
      sensitivities: Array.isArray(child?.sensitivities) ? (child?.sensitivities as string[]) : [],
    },
    prompt: {
      id: prompt?.id,
      name: prompt?.name ?? '',
      schemaVersion: Number(prompt?.schemaVersion ?? 1),
      rules: prompt?.rules && typeof prompt.rules === 'object' ? (prompt.rules as any) : {},
    },
  }
}

export async function getSettings(): Promise<SettingsPayload> {
  const data = await request<Partial<SettingsPayload>>('/api/v1/settings')
  return normaliseSettings(data)
}

export async function saveSettings(payload: SettingsUpsert): Promise<SettingsPayload> {
  const data = await request<SettingsPayload>('/api/v1/settings', {
    method: 'PUT',
    body: JSON.stringify({
      child: payload.child,
      prompt: { ...payload.prompt, rules: payload.prompt.rules ?? {} },
    }),
  })
  return normaliseSettings(data)
}
