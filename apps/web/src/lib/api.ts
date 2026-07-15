const rawBase = (import.meta.env.VITE_API_BASE || '').trim()

// Normalise base:
// - allow '' (same-origin)
// - strip trailing slashes so `${BASE}${path}` doesn't become `//api/...`
const BASE = rawBase.replace(/\/+$/, '')

export type JsonValue =
  | null
  | boolean
  | number
  | string
  | JsonValue[]
  | JsonObject

export type JsonObject = {
  [key: string]: JsonValue
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

export function isJsonValue(value: unknown): value is JsonValue {
  if (value === null) return true
  if (typeof value === 'string' || typeof value === 'boolean') return true
  if (typeof value === 'number') return Number.isFinite(value)
  if (Array.isArray(value)) return value.every(isJsonValue)
  return isJsonObject(value)
}

export function isJsonObject(value: unknown): value is JsonObject {
  return isRecord(value) && Object.values(value).every(isJsonValue)
}

export type APIErrorBody = JsonValue

export type APIError = Error & {
  status?: number
  code?: string
  body?: APIErrorBody
}

export function getAPIErrorStatus(error: unknown): number | undefined {
  if (typeof error !== 'object' || error === null || !(error instanceof Error)) {
    return undefined
  }
  const status = (error as APIError).status
  return typeof status === 'number' ? status : undefined
}

function getErrorDetails(body: APIErrorBody): {
  code?: string
  message?: string
} {
  if (!isJsonObject(body) || !isJsonObject(body.error)) return {}

  const code =
    typeof body.error.code === 'string' && body.error.code
      ? body.error.code
      : undefined

  const message =
    typeof body.error.message === 'string' && body.error.message
      ? body.error.message
      : undefined

  return { code, message }
}

function buildHeaders(init: RequestInit): Headers {
  const headers = new Headers(init.headers)

  const hasBody = init.body !== undefined && init.body !== null
  const isStringBody = typeof init.body === 'string'

  // Only set JSON content-type when body is a JSON string. (Never for FormData)
  if (hasBody && isStringBody && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }

  return headers
}

function buildUrl(path: string): string {
  // ensure path always starts with /
  const p = path.startsWith('/') ? path : `/${path}`
  return `${BASE}${p}`
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const res = await fetch(buildUrl(path), {
    credentials: 'include',
    ...init,
    headers: buildHeaders(init),
  })

  const contentType = res.headers.get('content-type') || ''
  const isJSON = contentType.includes('application/json')

  let rawBody: unknown = null
  if (res.status !== 204) {
    rawBody = isJSON
      ? await res.json().catch(() => null)
      : await res.text().catch(() => '')
  }

  const body: APIErrorBody = isJsonValue(rawBody) ? rawBody : null

  if (!res.ok) {
    const details = getErrorDetails(body)
    const message =
      typeof body === 'string'
        ? body || `Request failed: ${res.status}`
        : details.message ?? `Request failed: ${res.status}`

    const error: APIError = new Error(message)
    error.status = res.status
    error.body = body
    error.code = details.code
    throw error
  }

  return body as T
}

/* ----------------------------- Auth ----------------------------- */

export async function unlock(passcode: string) {
  const result = await request<unknown>('/api/v1/auth/unlock', {
    method: 'POST',
    body: JSON.stringify({ passcode }),
  })
  if (!isRecord(result) || result.ok !== true) {
    throw new Error('Invalid unlock response')
  }
}

export async function authStatus(): Promise<{ unlocked: boolean }> {
  const result = await request<unknown>('/api/v1/auth/status')
  if (!isRecord(result) || typeof result.unlocked !== 'boolean') {
    throw new Error('Invalid authentication status response')
  }
  return { unlocked: result.unlocked }
}

export async function logout(): Promise<void> {
  const result = await request<unknown>('/api/v1/auth/logout', {
    method: 'POST',
  })
  if (!isRecord(result) || result.ok !== true) {
    throw new Error('Invalid logout response')
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

export type AdminPreviewRequest = { markdown: string }

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
  rights?: JsonObject
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

export type AdminStoriesListResponse = { items: AdminStoryListItem[] }

export async function adminListStories(): Promise<AdminStoriesListResponse> {
  const data = await request<{ items?: AdminStoryListItem[] }>('/api/v1/admin/stories')
  return { items: Array.isArray(data.items) ? data.items : [] }
}

/* ---------------------------- Progress -------------------------- */

export type ProgressState = {
  version: number
  locator: unknown
  percent: number
}

export async function getProgress(slug: string): Promise<ProgressState> {
  return request<ProgressState>(`/api/v1/progress/${encodeURIComponent(slug)}`)
}

export async function saveProgress(
  slug: string,
  version: number,
  locator: JsonValue,
  percent: number,
  options: { keepalive?: boolean } = {}
): Promise<void> {
  const result = await request<unknown>(
    `/api/v1/progress/${encodeURIComponent(slug)}`,
    {
      method: 'PUT',
      body: JSON.stringify({ version, locator, percent }),
      keepalive: options.keepalive,
    }
  )
  if (!isRecord(result) || result.ok !== true) {
    throw new Error('Invalid progress-save response')
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
  rules: JsonObject
}

export type SettingsPayload = {
  child: ChildProfile
  prompt: PromptProfile
}

export type SettingsUpsert = {
  child: ChildProfile
  prompt: PromptProfile
}

function isStringArray(value: unknown): value is string[] {
  return Array.isArray(value) && value.every((item) => typeof item === 'string')
}

function finiteNumber(value: unknown, fallback: number): number {
  const number = Number(value ?? fallback)
  return Number.isFinite(number) ? number : fallback
}

function normaliseSettings(data: unknown): SettingsPayload {
  const root = isRecord(data) ? data : {}
  const child = isRecord(root.child) ? root.child : {}
  const prompt = isRecord(root.prompt) ? root.prompt : {}

  return {
    child: {
      id: typeof child.id === 'string' ? child.id : undefined,
      name: typeof child.name === 'string' ? child.name : '',
      ageMonths: finiteNumber(child.ageMonths, 0),
      interests: isStringArray(child.interests) ? child.interests : [],
      sensitivities: isStringArray(child.sensitivities)
        ? child.sensitivities
        : [],
    },
    prompt: {
      id: typeof prompt.id === 'string' ? prompt.id : undefined,
      name: typeof prompt.name === 'string' ? prompt.name : '',
      schemaVersion: finiteNumber(prompt.schemaVersion, 1),
      rules: isJsonObject(prompt.rules) ? prompt.rules : {},
    },
  }
}

export async function getSettings(): Promise<SettingsPayload> {
  const data = await request<unknown>('/api/v1/settings')
  return normaliseSettings(data)
}

export async function saveSettings(payload: SettingsUpsert): Promise<SettingsPayload> {
  const data = await request<unknown>('/api/v1/settings', {
    method: 'PUT',
    body: JSON.stringify({
      child: payload.child,
      prompt: { ...payload.prompt, rules: payload.prompt.rules ?? {} },
    }),
  })
  return normaliseSettings(data)
}
