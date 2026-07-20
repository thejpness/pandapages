import {
  isReaderContentKey,
  parseReaderLocatorV2,
  type ReaderLocatorV2,
  type ReaderSegmentKind,
  type ReaderStorySegment,
} from './reader-locator-v2'

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

export type LibraryProgress = {
  version: number
  percent: number
  updatedAt: string
  isCurrentVersion: boolean
}

export type LibraryProgressAvailability = 'available' | 'unavailable'

export type LibraryStory = {
  slug: string
  title: string
  author: string | null
  language: string
  publishedVersion: number
  wordCount: number
  chapterCount: number
  progress: LibraryProgress | null
  progressAvailability: LibraryProgressAvailability
}

// Kept as an alias for existing imports while the additive response grows into
// the complete Library read model.
export type LibraryItem = LibraryStory

export type LibraryResponse = {
  items: LibraryStory[]
  unavailableItemCount: number
}

export class InvalidLibraryResponseError extends Error {
  constructor() {
    super('Invalid library response')
    this.name = 'InvalidLibraryResponseError'
  }
}

export function isInvalidLibraryResponseError(
  error: unknown,
): error is InvalidLibraryResponseError {
  return (
    error instanceof InvalidLibraryResponseError ||
    (error instanceof Error && error.name === 'InvalidLibraryResponseError')
  )
}

const libraryStoryRequiredKeys = [
  'slug',
  'title',
  'language',
  'publishedVersion',
  'wordCount',
  'chapterCount',
] as const

const libraryProgressKeys = [
  'version',
  'percent',
  'updatedAt',
  'isCurrentVersion',
] as const

const librarySlugPattern = /^[a-z0-9]+(?:-[a-z0-9]+)*$/
const rfc3339Pattern =
  /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?(?:Z|([+-])(\d{2}):(\d{2}))$/

const unsafeLibraryKeys = new Set([
  'account',
  'accountdata',
  'accountemail',
  'accountid',
  'accounts',
  'aid',
  'child',
  'html',
  'id',
  'locator',
  'markdown',
  'profile',
  'profiledata',
  'profileid',
  'profiles',
  'prompt',
  'publishedversionid',
  'renderedhtml',
  'segment',
  'segments',
  'settings',
  'storyid',
  'versionid',
])

function isUnsafeLibraryKey(key: string): boolean {
  const compact = key.replaceAll(/[_-]/g, '').toLocaleLowerCase('en-GB')
  return (
    unsafeLibraryKeys.has(compact) ||
    /(?:^|[_-])ids?$/iu.test(key) ||
    /(?:Id|ID|Ids|IDs)$/u.test(key)
  )
}

function hasUnsafeLibraryFields(
  value: unknown,
  seen: WeakSet<object> = new WeakSet(),
): boolean {
  if (Array.isArray(value)) {
    if (seen.has(value)) return false
    seen.add(value)
    return value.some((item) => hasUnsafeLibraryFields(item, seen))
  }
  if (!isRecord(value)) return false
  if (seen.has(value)) return false
  seen.add(value)
  return Object.entries(value).some(
    ([key, child]) =>
      isUnsafeLibraryKey(key) || hasUnsafeLibraryFields(child, seen),
  )
}

function isNonNegativeInteger(value: unknown): value is number {
  return Number.isSafeInteger(value) && Number(value) >= 0
}

function isPositiveSafeInteger(value: unknown): value is number {
  return Number.isSafeInteger(value) && Number(value) >= 1
}

function isRFC3339Timestamp(value: unknown): value is string {
  if (typeof value !== 'string') return false
  const match = rfc3339Pattern.exec(value)
  if (!match) return false

  const year = Number(match[1])
  const month = Number(match[2])
  const day = Number(match[3])
  const hour = Number(match[4])
  const minute = Number(match[5])
  const second = Number(match[6])
  const offsetHour = match[8] === undefined ? 0 : Number(match[8])
  const offsetMinute = match[9] === undefined ? 0 : Number(match[9])

  if (
    year < 1 ||
    month < 1 ||
    month > 12 ||
    day < 1 ||
    hour > 23 ||
    minute > 59 ||
    second > 59 ||
    offsetHour > 23 ||
    offsetMinute > 59
  ) {
    return false
  }

  const calendarDate = new Date(Date.UTC(year, month - 1, day))
  if (
    calendarDate.getUTCFullYear() !== year ||
    calendarDate.getUTCMonth() !== month - 1 ||
    calendarDate.getUTCDate() !== day
  ) {
    return false
  }

  return Number.isFinite(Date.parse(value))
}

function invalidLibraryResponse(): never {
  throw new InvalidLibraryResponseError()
}

function parseLibraryProgress(
  value: unknown,
  publishedVersion: number,
): Pick<LibraryStory, 'progress' | 'progressAvailability'> {
  if (value === null) {
    return { progress: null, progressAvailability: 'available' }
  }
  if (!isRecord(value)) {
    return { progress: null, progressAvailability: 'unavailable' }
  }

  if (
    !libraryProgressKeys.every((key) => Object.hasOwn(value, key)) ||
    !isPositiveSafeInteger(value.version) ||
    typeof value.percent !== 'number' ||
    !Number.isFinite(value.percent) ||
    value.percent < 0 ||
    value.percent > 1 ||
    !isRFC3339Timestamp(value.updatedAt) ||
    typeof value.isCurrentVersion !== 'boolean' ||
    value.isCurrentVersion !== (value.version === publishedVersion)
  ) {
    return { progress: null, progressAvailability: 'unavailable' }
  }

  return {
    progress: {
      version: value.version,
      percent: value.percent,
      updatedAt: value.updatedAt,
      isCurrentVersion: value.isCurrentVersion,
    },
    progressAvailability: 'available',
  }
}

function parseLibraryStory(value: unknown): LibraryStory {
  if (
    !isRecord(value) ||
    !libraryStoryRequiredKeys.every((key) => Object.hasOwn(value, key)) ||
    typeof value.slug !== 'string' ||
    !librarySlugPattern.test(value.slug) ||
    typeof value.title !== 'string' ||
    value.title.trim().length === 0 ||
    typeof value.language !== 'string' ||
    value.language.trim().length === 0 ||
    !isPositiveSafeInteger(value.publishedVersion) ||
    !isNonNegativeInteger(value.wordCount) ||
    !isNonNegativeInteger(value.chapterCount)
  ) {
    return invalidLibraryResponse()
  }

  const author = Object.hasOwn(value, 'author') ? value.author : null
  if (
    author !== null &&
    (typeof author !== 'string' || author.trim().length === 0)
  ) {
    return invalidLibraryResponse()
  }

  const parsedProgress = Object.hasOwn(value, 'progress')
    ? parseLibraryProgress(value.progress, value.publishedVersion)
    : { progress: null, progressAvailability: 'unavailable' as const }

  return {
    slug: value.slug,
    title: value.title,
    author,
    language: value.language,
    publishedVersion: value.publishedVersion,
    wordCount: value.wordCount,
    chapterCount: value.chapterCount,
    ...parsedProgress,
  }
}

export function parseLibraryResponse(value: unknown): LibraryResponse {
  if (
    !isRecord(value) ||
    hasUnsafeLibraryFields(value) ||
    !Object.hasOwn(value, 'items') ||
    !Array.isArray(value.items)
  ) {
    return invalidLibraryResponse()
  }

  const unavailableItemCount = Object.hasOwn(value, 'unavailableItemCount')
    ? value.unavailableItemCount
    : 0
  if (!isNonNegativeInteger(unavailableItemCount)) {
    return invalidLibraryResponse()
  }

  const items = value.items.map(parseLibraryStory)
  const slugs = new Set<string>()
  for (const item of items) {
    if (slugs.has(item.slug)) return invalidLibraryResponse()
    slugs.add(item.slug)
  }
  return { items, unavailableItemCount }
}

export async function getLibrary(): Promise<LibraryResponse> {
  const data = await request<unknown>('/api/v1/library')
  return parseLibraryResponse(data)
}

/* ----------------------------- Story ---------------------------- */

export type ReaderStoryPayload = {
  slug: string
  title: string
  author: string | null
  language: string
  version: number
  segments: ReaderStorySegment[]
}

function hasExactKeys(
  record: Record<string, unknown>,
  required: readonly string[],
): boolean {
  const allowed = new Set(required)
  return (
    required.every((key) => Object.hasOwn(record, key)) &&
    Object.keys(record).every((key) => allowed.has(key))
  )
}

function isPositiveInteger(value: unknown): value is number {
  return Number.isInteger(value) && Number(value) >= 1
}

function parseReaderSegment(value: unknown): ReaderStorySegment {
  if (
    !isRecord(value) ||
    !hasExactKeys(value, [
      'ordinal',
      'kind',
      'headingLevel',
      'contentKey',
      'contentOccurrence',
      'chapterKey',
      'chapterOccurrence',
      'renderedHtml',
      'wordCount',
    ]) ||
    !isPositiveInteger(value.ordinal) ||
    !['heading', 'paragraph', 'other'].includes(String(value.kind)) ||
    !isReaderContentKey(value.contentKey) ||
    !isPositiveInteger(value.contentOccurrence) ||
    typeof value.renderedHtml !== 'string' ||
    !Number.isInteger(value.wordCount) ||
    Number(value.wordCount) < 0
  ) {
    throw new Error('Invalid Reader segment response')
  }

  const kind = value.kind as ReaderSegmentKind
  if (
    (kind === 'heading' &&
      (!Number.isInteger(value.headingLevel) ||
        Number(value.headingLevel) < 1 ||
        Number(value.headingLevel) > 6)) ||
    (kind !== 'heading' && value.headingLevel !== null)
  ) {
    throw new Error('Invalid Reader segment heading level')
  }

  const hasChapter = value.chapterKey !== null || value.chapterOccurrence !== null
  if (
    hasChapter &&
    (!isReaderContentKey(value.chapterKey) ||
      !isPositiveInteger(value.chapterOccurrence))
  ) {
    throw new Error('Invalid Reader segment chapter identity')
  }

  return {
    ordinal: value.ordinal,
    kind,
    headingLevel: kind === 'heading' ? Number(value.headingLevel) : null,
    contentKey: value.contentKey,
    contentOccurrence: value.contentOccurrence,
    chapterKey: hasChapter ? String(value.chapterKey) : null,
    chapterOccurrence: hasChapter ? Number(value.chapterOccurrence) : null,
    renderedHtml: value.renderedHtml,
    wordCount: Number(value.wordCount),
  }
}

export function parseReaderStoryPayload(value: unknown): ReaderStoryPayload {
  if (
    !isRecord(value) ||
    !hasExactKeys(value, [
      'slug',
      'title',
      'author',
      'language',
      'version',
      'segments',
    ]) ||
    typeof value.slug !== 'string' ||
    value.slug.length === 0 ||
    typeof value.title !== 'string' ||
    (value.author !== null && typeof value.author !== 'string') ||
    typeof value.language !== 'string' ||
    !isPositiveInteger(value.version) ||
    !Array.isArray(value.segments) ||
    value.segments.length === 0
  ) {
    throw new Error('Invalid Reader response')
  }

  const segments = value.segments.map(parseReaderSegment)
  for (let index = 1; index < segments.length; index += 1) {
    if (segments[index].ordinal <= segments[index - 1].ordinal) {
      throw new Error('Reader segments are not in strict ordinal order')
    }
  }

  return {
    slug: value.slug,
    title: value.title,
    author: value.author,
    language: value.language,
    version: value.version,
    segments,
  }
}

export async function getReaderStory(
  slug: string,
  signal?: AbortSignal,
): Promise<ReaderStoryPayload> {
  const data = await request<unknown>(
    `/api/v1/reader/${encodeURIComponent(slug)}`,
    { signal },
  )
  return parseReaderStoryPayload(data)
}

/* ----------------------------- Admin ---------------------------- */

export type AdminStoryInput = {
  slug: string
  title: string
  author?: string | null
  markdown: string
  language?: string | null
  sourceUrl?: string | null
  rights?: JsonObject
}

export type AdminPreviewRequest = AdminStoryInput
export type AdminDraftUpsertRequest = AdminStoryInput

export type AdminValidationIssue = {
  field: string
  code: string
  message: string
}

export type AdminPreviewResponse = {
  slug: string
  title: string
  author: string | null
  language: string
  rights: JsonObject
  sourceUrl: string | null
  renderedHtml: string
  segmentCount: number
  wordCount: number
  chapterCount: number
  warnings: AdminValidationIssue[]
}

export type AdminDraftOutcome =
  | 'created_story'
  | 'created_version'
  | 'reused'

export type AdminDraftUpsertResponse = {
  slug: string
  versionId: string
  version: number
  segmentCount: number
  wordCount: number
  chapterCount: number
  renderedHtml: string
  outcome: AdminDraftOutcome
}

export type AdminStoryStatus =
  | 'draft_only'
  | 'published'
  | 'published_with_draft'
  | 'unpublished'
  | 'repair_required'

export type AdminVersionHealth =
  | 'ready'
  | 'repair_required'
  | 'unavailable'

export type AdminVersionPointer = {
  versionId: string
  version: number
}

export type AdminStoryListItem = {
  slug: string
  title: string
  author: string | null
  language: string
  rights: JsonObject
  sourceUrl: string | null
  status: AdminStoryStatus
  publishedVersion: AdminVersionPointer | null
  draftVersion: AdminVersionPointer | null
  versionCount: number
  updatedAt: string
}

export type AdminVersionSummary = {
  versionId: string
  version: number
  createdAt: string
  isDraft: boolean
  isPublished: boolean
  segmentCount: number
  wordCount: number
  chapterCount: number
  health: AdminVersionHealth
}

export type AdminStoryDetail = AdminStoryListItem & {
  createdAt: string
  versions: AdminVersionSummary[]
}

export type AdminVersionSource = {
  slug: string
  title: string
  author: string | null
  language: string
  rights: JsonObject
  sourceUrl: string | null
  versionId: string
  version: number
  markdown: string
  renderedHtml: string
  segmentCount: number
  wordCount: number
  chapterCount: number
  createdAt: string
  isDraft: boolean
  isPublished: boolean
  health: AdminVersionHealth
}

export type AdminStoriesListResponse = { items: AdminStoryListItem[] }

export type AdminStoryStatusResponse = {
  slug: string
  status: AdminStoryStatus
  publishedVersion: AdminVersionPointer | null
  draftVersion: AdminVersionPointer | null
  versionCount: number
  updatedAt: string
}

const adminStoryStatuses = new Set<AdminStoryStatus>([
  'draft_only',
  'published',
  'published_with_draft',
  'unpublished',
  'repair_required',
])
const adminVersionHealthValues = new Set<AdminVersionHealth>([
  'ready',
  'repair_required',
  'unavailable',
])
const adminDraftOutcomes = new Set<AdminDraftOutcome>([
  'created_story',
  'created_version',
  'reused',
])
const adminUUIDPattern =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/iu
const forbiddenAdminKeys = new Set([
  'account',
  'accountdata',
  'accountemail',
  'accountid',
  'accounts',
  'chapterkey',
  'chapteroccurrence',
  'contenthash',
  'contentkey',
  'contentoccurrence',
  'databaseid',
  'headinglevel',
  'internalid',
  'locator',
  'ordinal',
  'profile',
  'profiledata',
  'profileid',
  'profiles',
  'segment',
  'segments',
  'session',
  'sessiondata',
  'sessionid',
  'storyid',
])

function compactAdminKey(key: string): string {
  return key.replaceAll(/[_-]/g, '').toLocaleLowerCase('en-GB')
}

function hasForbiddenAdminFields(
  value: unknown,
  allowedContent: ReadonlySet<string>,
  seen: WeakSet<object> = new WeakSet(),
): boolean {
  if (Array.isArray(value)) {
    if (seen.has(value)) return false
    seen.add(value)
    return value.some((item) =>
      hasForbiddenAdminFields(item, allowedContent, seen),
    )
  }
  if (!isRecord(value)) return false
  if (seen.has(value)) return false
  seen.add(value)
  return Object.entries(value).some(([key, child]) => {
    const compact = compactAdminKey(key)
    if (forbiddenAdminKeys.has(compact)) return true
    if (
      (compact === 'markdown' || compact === 'renderedhtml') &&
      !allowedContent.has(compact)
    ) {
      return true
    }
    return hasForbiddenAdminFields(child, allowedContent, seen)
  })
}

function adminRecord(
  value: unknown,
  allowedContent: readonly string[] = [],
): Record<string, unknown> {
  if (
    !isRecord(value) ||
    !isJsonObject(value) ||
    hasForbiddenAdminFields(value, new Set(allowedContent))
  ) {
    throw new Error('Invalid admin response')
  }
  return value
}

function requiredAdminString(
  record: Record<string, unknown>,
  key: string,
): string {
  const value = record[key]
  if (typeof value !== 'string' || value.trim().length === 0) {
    throw new Error('Invalid admin response')
  }
  return value
}

function nullableAdminString(
  record: Record<string, unknown>,
  key: string,
): string | null {
  const value = record[key]
  if (value === null) return null
  if (typeof value !== 'string' || value.trim().length === 0) {
    throw new Error('Invalid admin response')
  }
  return value
}

function parseAdminSlug(value: unknown): string {
  if (typeof value !== 'string' || !librarySlugPattern.test(value)) {
    throw new Error('Invalid admin response')
  }
  return value
}

function parseAdminUUID(value: unknown): string {
  if (typeof value !== 'string' || !adminUUIDPattern.test(value)) {
    throw new Error('Invalid admin response')
  }
  return value
}

function parseAdminStatus(value: unknown): AdminStoryStatus {
  if (
    typeof value !== 'string' ||
    !adminStoryStatuses.has(value as AdminStoryStatus)
  ) {
    throw new Error('Invalid admin response')
  }
  return value as AdminStoryStatus
}

function parseAdminHealth(value: unknown): AdminVersionHealth {
  if (
    typeof value !== 'string' ||
    !adminVersionHealthValues.has(value as AdminVersionHealth)
  ) {
    throw new Error('Invalid admin response')
  }
  return value as AdminVersionHealth
}

function parseAdminPointer(value: unknown): AdminVersionPointer | null {
  if (value === null) return null
  const record = adminRecord(value)
  if (!isPositiveSafeInteger(record.version)) {
    throw new Error('Invalid admin response')
  }
  return {
    versionId: parseAdminUUID(record.versionId),
    version: record.version,
  }
}

function parseAdminMetadata(record: Record<string, unknown>) {
  if (
    !isJsonObject(record.rights) ||
    typeof record.language !== 'string' ||
    record.language.trim().length === 0
  ) {
    throw new Error('Invalid admin response')
  }
  return {
    title: requiredAdminString(record, 'title'),
    author: nullableAdminString(record, 'author'),
    language: record.language,
    rights: record.rights,
    sourceUrl: nullableAdminString(record, 'sourceUrl'),
  }
}

export function parseAdminStorySummary(value: unknown): AdminStoryListItem {
  const record = adminRecord(value)
  if (
    !isNonNegativeInteger(record.versionCount) ||
    !isRFC3339Timestamp(record.updatedAt)
  ) {
    throw new Error('Invalid admin response')
  }
  return {
    slug: parseAdminSlug(record.slug),
    ...parseAdminMetadata(record),
    status: parseAdminStatus(record.status),
    publishedVersion: parseAdminPointer(record.publishedVersion),
    draftVersion: parseAdminPointer(record.draftVersion),
    versionCount: record.versionCount,
    updatedAt: record.updatedAt,
  }
}

function parseAdminVersionSummary(value: unknown): AdminVersionSummary {
  const record = adminRecord(value)
  if (
    !isPositiveSafeInteger(record.version) ||
    !isRFC3339Timestamp(record.createdAt) ||
    typeof record.isDraft !== 'boolean' ||
    typeof record.isPublished !== 'boolean' ||
    !isNonNegativeInteger(record.segmentCount) ||
    !isNonNegativeInteger(record.wordCount) ||
    !isNonNegativeInteger(record.chapterCount)
  ) {
    throw new Error('Invalid admin response')
  }
  return {
    versionId: parseAdminUUID(record.versionId),
    version: record.version,
    createdAt: record.createdAt,
    isDraft: record.isDraft,
    isPublished: record.isPublished,
    segmentCount: record.segmentCount,
    wordCount: record.wordCount,
    chapterCount: record.chapterCount,
    health: parseAdminHealth(record.health),
  }
}

export function parseAdminStoriesListResponse(
  value: unknown,
): AdminStoriesListResponse {
  const record = adminRecord(value)
  if (!Array.isArray(record.items)) throw new Error('Invalid admin response')
  const items = record.items.map(parseAdminStorySummary)
  const slugs = new Set(items.map((item) => item.slug))
  if (slugs.size !== items.length) throw new Error('Invalid admin response')
  return { items }
}

export function parseAdminStoryDetail(value: unknown): AdminStoryDetail {
  const record = adminRecord(value)
  const summary = parseAdminStorySummary(record)
  if (!isRFC3339Timestamp(record.createdAt) || !Array.isArray(record.versions)) {
    throw new Error('Invalid admin response')
  }
  const versions = record.versions.map(parseAdminVersionSummary)
  const ids = new Set<string>()
  let previousVersion = Number.POSITIVE_INFINITY
  for (const version of versions) {
    if (ids.has(version.versionId) || version.version >= previousVersion) {
      throw new Error('Invalid admin response')
    }
    ids.add(version.versionId)
    previousVersion = version.version
  }
  if (versions.length !== summary.versionCount) {
    throw new Error('Invalid admin response')
  }
  return { ...summary, createdAt: record.createdAt, versions }
}

export function parseAdminVersionSource(value: unknown): AdminVersionSource {
  const record = adminRecord(value, ['markdown', 'renderedhtml'])
  if (
    !isPositiveSafeInteger(record.version) ||
    !isRFC3339Timestamp(record.createdAt) ||
    typeof record.markdown !== 'string' ||
    typeof record.renderedHtml !== 'string' ||
    typeof record.isDraft !== 'boolean' ||
    typeof record.isPublished !== 'boolean' ||
    !isNonNegativeInteger(record.segmentCount) ||
    !isNonNegativeInteger(record.wordCount) ||
    !isNonNegativeInteger(record.chapterCount)
  ) {
    throw new Error('Invalid admin response')
  }
  return {
    slug: parseAdminSlug(record.slug),
    versionId: parseAdminUUID(record.versionId),
    version: record.version,
    ...parseAdminMetadata(record),
    markdown: record.markdown,
    renderedHtml: record.renderedHtml,
    segmentCount: record.segmentCount,
    wordCount: record.wordCount,
    chapterCount: record.chapterCount,
    createdAt: record.createdAt,
    isDraft: record.isDraft,
    isPublished: record.isPublished,
    health: parseAdminHealth(record.health),
  }
}

function parseAdminIssue(value: unknown): AdminValidationIssue {
  const record = adminRecord(value)
  return {
    field: requiredAdminString(record, 'field'),
    code: requiredAdminString(record, 'code'),
    message: requiredAdminString(record, 'message'),
  }
}

export function getAdminValidationIssues(
  error: unknown,
): AdminValidationIssue[] | null {
  if (!(error instanceof Error)) return null
  const body = (error as APIError).body
  if (!isJsonObject(body) || !isJsonObject(body.error)) return null
  if (!Array.isArray(body.error.issues)) return null
  try {
    return body.error.issues.map(parseAdminIssue)
  } catch {
    return null
  }
}

export function parseAdminPreviewResponse(
  value: unknown,
): AdminPreviewResponse {
  const record = adminRecord(value, ['renderedhtml'])
  if (
    typeof record.renderedHtml !== 'string' ||
    !isNonNegativeInteger(record.segmentCount) ||
    !isNonNegativeInteger(record.wordCount) ||
    !isNonNegativeInteger(record.chapterCount) ||
    !Array.isArray(record.warnings)
  ) {
    throw new Error('Invalid admin response')
  }
  return {
    slug: parseAdminSlug(record.slug),
    ...parseAdminMetadata(record),
    renderedHtml: record.renderedHtml,
    segmentCount: record.segmentCount,
    wordCount: record.wordCount,
    chapterCount: record.chapterCount,
    warnings: record.warnings.map(parseAdminIssue),
  }
}

export function parseAdminDraftUpsertResponse(
  value: unknown,
): AdminDraftUpsertResponse {
  const record = adminRecord(value, ['renderedhtml'])
  if (
    !isPositiveSafeInteger(record.version) ||
    !isNonNegativeInteger(record.segmentCount) ||
    !isNonNegativeInteger(record.wordCount) ||
    !isNonNegativeInteger(record.chapterCount) ||
    typeof record.renderedHtml !== 'string' ||
    typeof record.outcome !== 'string' ||
    !adminDraftOutcomes.has(record.outcome as AdminDraftOutcome)
  ) {
    throw new Error('Invalid admin response')
  }
  return {
    slug: parseAdminSlug(record.slug),
    versionId: parseAdminUUID(record.versionId),
    version: record.version,
    segmentCount: record.segmentCount,
    wordCount: record.wordCount,
    chapterCount: record.chapterCount,
    renderedHtml: record.renderedHtml,
    outcome: record.outcome as AdminDraftOutcome,
  }
}

export function parseAdminStoryStatusResponse(
  value: unknown,
): AdminStoryStatusResponse {
  const record = adminRecord(value)
  if (
    !isNonNegativeInteger(record.versionCount) ||
    !isRFC3339Timestamp(record.updatedAt)
  ) {
    throw new Error('Invalid admin response')
  }
  return {
    slug: parseAdminSlug(record.slug),
    status: parseAdminStatus(record.status),
    publishedVersion: parseAdminPointer(record.publishedVersion),
    draftVersion: parseAdminPointer(record.draftVersion),
    versionCount: record.versionCount,
    updatedAt: record.updatedAt,
  }
}

export async function adminPreview(
  payload: AdminPreviewRequest,
): Promise<AdminPreviewResponse> {
  const data = await request<unknown>('/api/v1/admin/preview', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
  return parseAdminPreviewResponse(data)
}

export async function adminDraftUpsertStory(
  payload: AdminDraftUpsertRequest,
): Promise<AdminDraftUpsertResponse> {
  const data = await request<unknown>('/api/v1/admin/stories/draft', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
  return parseAdminDraftUpsertResponse(data)
}

export async function adminListStories(): Promise<AdminStoriesListResponse> {
  const data = await request<unknown>('/api/v1/admin/stories')
  return parseAdminStoriesListResponse(data)
}

export async function adminGetStory(slug: string): Promise<AdminStoryDetail> {
  const data = await request<unknown>(
    `/api/v1/admin/stories/${encodeURIComponent(slug)}`,
  )
  return parseAdminStoryDetail(data)
}

export async function adminGetVersionSource(
  slug: string,
  versionId: string,
): Promise<AdminVersionSource> {
  const data = await request<unknown>(
    `/api/v1/admin/stories/${encodeURIComponent(slug)}/versions/${encodeURIComponent(versionId)}`,
  )
  return parseAdminVersionSource(data)
}

export async function adminPublishStory(
  slug: string,
  versionId: string,
): Promise<AdminStoryStatusResponse> {
  const data = await request<unknown>(
    `/api/v1/admin/stories/${encodeURIComponent(slug)}/publish`,
    { method: 'POST', body: JSON.stringify({ versionId }) },
  )
  return parseAdminStoryStatusResponse(data)
}

export async function adminUnpublishStory(
  slug: string,
): Promise<AdminStoryStatusResponse> {
  const data = await request<unknown>(
    `/api/v1/admin/stories/${encodeURIComponent(slug)}/unpublish`,
    { method: 'POST' },
  )
  return parseAdminStoryStatusResponse(data)
}

/* ---------------------------- Progress -------------------------- */

export type ProgressState = {
  version: number
  locator: ReaderLocatorV2
  percent: number
}

export type ProgressResponse = {
  progress: ProgressState | null
}

export function parseProgressResponse(value: unknown): ProgressResponse {
  if (!isRecord(value) || !hasExactKeys(value, ['progress'])) {
    throw new Error('Invalid progress response')
  }
  if (value.progress === null) return { progress: null }
  if (
    !isRecord(value.progress) ||
    !hasExactKeys(value.progress, ['version', 'locator', 'percent']) ||
    !isPositiveInteger(value.progress.version) ||
    typeof value.progress.percent !== 'number' ||
    !Number.isFinite(value.progress.percent) ||
    value.progress.percent < 0 ||
    value.progress.percent > 1
  ) {
    throw new Error('Invalid progress response')
  }
  return {
    progress: {
      version: value.progress.version,
      locator: parseReaderLocatorV2(value.progress.locator),
      percent: value.progress.percent,
    },
  }
}

export async function getProgress(slug: string): Promise<ProgressResponse> {
  const data = await request<unknown>(
    `/api/v1/progress/${encodeURIComponent(slug)}`,
  )
  return parseProgressResponse(data)
}

export async function saveProgress(
  slug: string,
  version: number,
  locator: ReaderLocatorV2,
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
