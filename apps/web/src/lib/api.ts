import {
  parseSafeRenderedStoryHTML,
  type SafeRenderedStoryHTML,
} from "./reader-locator-v2";
import {
  clearSelectedAccount,
  currentAccountContext,
  type AccountContext,
} from "./account-context";
import { signOutSupabaseSession } from "./supabase-auth";
import {
  isReaderContentKey,
  parseReaderLocatorV2,
  type ReaderLocatorV2,
  type ReaderSegmentKind,
  type ReaderStorySegment,
} from "./reader-locator-v2";

const rawBase = (import.meta.env.VITE_API_BASE || "").trim();

// Normalise base:
// - allow '' (same-origin)
// - strip trailing slashes so `${BASE}${path}` doesn't become `//api/...`
const BASE = rawBase.replace(/\/+$/, "");

export type JsonValue =
  null | boolean | number | string | JsonValue[] | JsonObject;

export type JsonObject = {
  [key: string]: JsonValue;
};

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function isJsonValue(value: unknown): value is JsonValue {
  if (value === null) return true;
  if (typeof value === "string" || typeof value === "boolean") return true;
  if (typeof value === "number") return Number.isFinite(value);
  if (Array.isArray(value)) return value.every(isJsonValue);
  return isJsonObject(value);
}

export function isJsonObject(value: unknown): value is JsonObject {
  return isRecord(value) && Object.values(value).every(isJsonValue);
}

export type APIErrorBody = JsonValue;

export type APIError = Error & {
  status?: number;
  code?: string;
  body?: APIErrorBody;
};

export function getAPIErrorStatus(error: unknown): number | undefined {
  if (
    typeof error !== "object" ||
    error === null ||
    !(error instanceof Error)
  ) {
    return undefined;
  }
  const status = (error as APIError).status;
  return typeof status === "number" ? status : undefined;
}

function getErrorDetails(body: APIErrorBody): {
  code?: string;
  message?: string;
} {
  if (!isJsonObject(body)) return {};
  if (typeof body.error === "string") return { code: body.error };
  if (!isJsonObject(body.error)) return {};

  const code =
    typeof body.error.code === "string" && body.error.code
      ? body.error.code
      : undefined;

  const message =
    typeof body.error.message === "string" && body.error.message
      ? body.error.message
      : undefined;

  return { code, message };
}

function buildHeaders(init: RequestInit): Headers {
  const headers = new Headers(init.headers);

  const hasBody = init.body !== undefined && init.body !== null;
  const isStringBody = typeof init.body === "string";

  // Only set JSON content-type when body is a JSON string. (Never for FormData)
  if (hasBody && isStringBody && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }

  return headers;
}

function buildUrl(path: string): string {
  // ensure path always starts with /
  const p = path.startsWith("/") ? path : `/${path}`;
  return `${BASE}${p}`;
}

type RequestContext = Readonly<{
  account?: AccountContext;
  profileID?: string;
}>;

async function request<T>(
  path: string,
  init: RequestInit = {},
  context: RequestContext = {},
): Promise<T> {
  const account = context.account ?? (await currentAccountContext());
  const headers = buildHeaders(init);
  headers.set("Authorization", `Bearer ${account.accessToken}`);
  headers.set("X-PP-Account-ID", account.membership.accountId);
  if (context.profileID !== undefined) {
    headers.set("X-PP-Profile-ID", context.profileID);
  }
  const res = await fetch(buildUrl(path), {
    credentials: "omit",
    ...init,
    headers,
  });

  const contentType = res.headers.get("content-type") || "";
  const isJSON = contentType.includes("application/json");

  let rawBody: unknown = null;
  if (res.status !== 204) {
    rawBody = isJSON
      ? await res.json().catch(() => null)
      : await res.text().catch(() => "");
  }

  const body: APIErrorBody = isJsonValue(rawBody) ? rawBody : null;

  if (!res.ok) {
    const details = getErrorDetails(body);
    const message =
      typeof body === "string"
        ? body || `Request failed: ${res.status}`
        : (details.message ?? `Request failed: ${res.status}`);

    const error: APIError = new Error(message);
    error.status = res.status;
    error.body = body;
    error.code = details.code;
    throw error;
  }

  return body as T;
}

// Browser sign-out is owned by the official Supabase client; Panda Pages does
// not call a cookie-auth endpoint or persist a custom bearer token.
export async function logout(): Promise<void> {
  await signOutSupabaseSession()
  clearSelectedAccount()
}

/* ------------------------ Reader profiles ----------------------- */

// Story Studio established the canonical five reading-edition keys. Reader
// profiles consume that same finite vocabulary rather than defining another.
export const adminStoryEditionKeys = [
  "classic",
  "confident-readers",
  "growing-readers",
  "story-explorers",
  "little-listeners",
] as const;
export type AdminStoryEditionKey = (typeof adminStoryEditionKeys)[number];

export const readerEditionKeys = adminStoryEditionKeys;
export type ReaderEditionKey = AdminStoryEditionKey;

export function isReaderEditionKey(value: unknown): value is ReaderEditionKey {
  return (
    typeof value === "string" &&
    (readerEditionKeys as readonly string[]).includes(value)
  );
}

export type ReaderProfile = Readonly<{
  id: string;
  name: string;
  pinEnabled: boolean;
  readingLevel: ReaderEditionKey;
}>;

const canonicalUUIDPattern =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

function parseReaderProfile(value: unknown): ReaderProfile {
  if (!isRecord(value)) throw new Error("Invalid reader profile response");
  const id = value.id;
  const name = value.name;
  const pinEnabled = value.pin_enabled;
  const readingLevel = value.reading_level;
  if (
    typeof id !== "string" ||
    !canonicalUUIDPattern.test(id) ||
    typeof name !== "string" ||
    name.length === 0 ||
    name.length > 80 ||
    name.trim() !== name ||
    typeof pinEnabled !== "boolean" ||
    !isReaderEditionKey(readingLevel)
  ) {
    throw new Error("Invalid reader profile response");
  }
  return Object.freeze({ id, name, pinEnabled, readingLevel });
}

function parseReaderProfiles(value: unknown): readonly ReaderProfile[] {
  if (!isRecord(value) || !Array.isArray(value.profiles)) {
    throw new Error("Invalid reader profile response");
  }
  return Object.freeze(value.profiles.map(parseReaderProfile));
}

function profilePayload(
  name: string,
  readingLevel: ReaderEditionKey,
): string {
  return JSON.stringify({ name, readingLevel });
}

export async function listReaderProfiles(
  account?: AccountContext,
): Promise<readonly ReaderProfile[]> {
  return parseReaderProfiles(
    await request<unknown>("/api/v1/profiles", {}, { account }),
  );
}

export async function createReaderProfile(
  name: string,
  readingLevel: ReaderEditionKey,
): Promise<ReaderProfile> {
  return parseReaderProfile(
    await request<unknown>("/api/v1/profiles", {
      method: "POST",
      body: profilePayload(name, readingLevel),
    }),
  );
}

export async function renameReaderProfile(
  profileID: string,
  name: string,
  readingLevel: ReaderEditionKey,
): Promise<ReaderProfile> {
  return parseReaderProfile(
    await request<unknown>(`/api/v1/profiles/${encodeURIComponent(profileID)}`, {
      method: "PATCH",
      body: profilePayload(name, readingLevel),
    }),
  );
}

export async function deleteReaderProfile(profileID: string): Promise<void> {
  await request<unknown>(`/api/v1/profiles/${encodeURIComponent(profileID)}`, {
    method: "DELETE",
  });
}

function pinPayload(pin: string): string {
  return JSON.stringify({ pin });
}

function parsePINState(value: unknown, field: "pin_enabled" | "verified"): boolean {
  if (!isRecord(value) || value[field] !== true) {
    throw new Error("Invalid profile PIN response");
  }
  return true;
}

export async function setReaderProfilePIN(profileID: string, pin: string): Promise<boolean> {
  return parsePINState(
    await request<unknown>(`/api/v1/profiles/${encodeURIComponent(profileID)}/pin`, {
      method: "PUT",
      body: pinPayload(pin),
    }),
    "pin_enabled",
  );
}

export async function removeReaderProfilePIN(profileID: string): Promise<boolean> {
  const result = await request<unknown>(`/api/v1/profiles/${encodeURIComponent(profileID)}/pin`, {
    method: "DELETE",
  });
  if (!isRecord(result) || result.pin_enabled !== false) {
    throw new Error("Invalid profile PIN response");
  }
  return false;
}

export async function verifyReaderProfilePIN(profileID: string, pin: string): Promise<boolean> {
  return parsePINState(
    await request<unknown>(`/api/v1/profiles/${encodeURIComponent(profileID)}/pin`, {
      method: "POST",
      body: pinPayload(pin),
    }),
    "verified",
  );
}

// profileScopedRequest deliberately requires a profile ID at the call site.
// Account-scoped APIs above never send this header.
export async function profileScopedRequest<T>(
  path: string,
  profileID: string,
  init: RequestInit = {},
): Promise<T> {
  return request<T>(path, init, { profileID });
}

/* ---------------------------- Library --------------------------- */

export type LibraryProgress = {
  version: number;
  percent: number;
  updatedAt: string;
  isCurrentVersion: boolean;
};

export type LibraryProgressAvailability = "available" | "unavailable";

export type LibraryStory = {
  slug: string;
  title: string;
  author: string | null;
  language: string;
  publishedVersion: number;
  wordCount: number;
  chapterCount: number;
  progress: LibraryProgress | null;
  progressAvailability: LibraryProgressAvailability;
};

// Kept as an alias for existing imports while the additive response grows into
// the complete Library read model.
export type LibraryItem = LibraryStory;

export type LibraryResponse = {
  items: LibraryStory[];
  unavailableItemCount: number;
};

export class InvalidLibraryResponseError extends Error {
  constructor() {
    super("Invalid library response");
    this.name = "InvalidLibraryResponseError";
  }
}

export function isInvalidLibraryResponseError(
  error: unknown,
): error is InvalidLibraryResponseError {
  return (
    error instanceof InvalidLibraryResponseError ||
    (error instanceof Error && error.name === "InvalidLibraryResponseError")
  );
}

const libraryStoryRequiredKeys = [
  "slug",
  "title",
  "language",
  "publishedVersion",
  "wordCount",
  "chapterCount",
] as const;

const libraryProgressKeys = [
  "version",
  "percent",
  "updatedAt",
  "isCurrentVersion",
] as const;

const librarySlugPattern = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;
const rfc3339Pattern =
  /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?(?:Z|([+-])(\d{2}):(\d{2}))$/;

const unsafeLibraryKeys = new Set([
  "account",
  "accountdata",
  "accountemail",
  "accountid",
  "accounts",
  "aid",
  "child",
  "html",
  "id",
  "locator",
  "markdown",
  "profile",
  "profiledata",
  "profileid",
  "profiles",
  "prompt",
  "publishedversionid",
  "renderedhtml",
  "segment",
  "segments",
  "settings",
  "storyid",
  "versionid",
]);

function isUnsafeLibraryKey(key: string): boolean {
  const compact = key.replaceAll(/[_-]/g, "").toLocaleLowerCase("en-GB");
  return (
    unsafeLibraryKeys.has(compact) ||
    /(?:^|[_-])ids?$/iu.test(key) ||
    /(?:Id|ID|Ids|IDs)$/u.test(key)
  );
}

function hasUnsafeLibraryFields(
  value: unknown,
  seen: WeakSet<object> = new WeakSet(),
): boolean {
  if (Array.isArray(value)) {
    if (seen.has(value)) return false;
    seen.add(value);
    return value.some((item) => hasUnsafeLibraryFields(item, seen));
  }
  if (!isRecord(value)) return false;
  if (seen.has(value)) return false;
  seen.add(value);
  return Object.entries(value).some(
    ([key, child]) =>
      isUnsafeLibraryKey(key) || hasUnsafeLibraryFields(child, seen),
  );
}

function isNonNegativeInteger(value: unknown): value is number {
  return Number.isSafeInteger(value) && Number(value) >= 0;
}

function isPositiveSafeInteger(value: unknown): value is number {
  return Number.isSafeInteger(value) && Number(value) >= 1;
}

function isRFC3339Timestamp(value: unknown): value is string {
  if (typeof value !== "string") return false;
  const match = rfc3339Pattern.exec(value);
  if (!match) return false;

  const year = Number(match[1]);
  const month = Number(match[2]);
  const day = Number(match[3]);
  const hour = Number(match[4]);
  const minute = Number(match[5]);
  const second = Number(match[6]);
  const offsetHour = match[8] === undefined ? 0 : Number(match[8]);
  const offsetMinute = match[9] === undefined ? 0 : Number(match[9]);

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
    return false;
  }

  const calendarDate = new Date(Date.UTC(year, month - 1, day));
  if (
    calendarDate.getUTCFullYear() !== year ||
    calendarDate.getUTCMonth() !== month - 1 ||
    calendarDate.getUTCDate() !== day
  ) {
    return false;
  }

  return Number.isFinite(Date.parse(value));
}

function invalidLibraryResponse(): never {
  throw new InvalidLibraryResponseError();
}

function parseLibraryProgress(
  value: unknown,
  publishedVersion: number,
): Pick<LibraryStory, "progress" | "progressAvailability"> {
  if (value === null) {
    return { progress: null, progressAvailability: "available" };
  }
  if (!isRecord(value)) {
    return { progress: null, progressAvailability: "unavailable" };
  }

  if (
    !libraryProgressKeys.every((key) => Object.hasOwn(value, key)) ||
    !isPositiveSafeInteger(value.version) ||
    typeof value.percent !== "number" ||
    !Number.isFinite(value.percent) ||
    value.percent < 0 ||
    value.percent > 1 ||
    !isRFC3339Timestamp(value.updatedAt) ||
    typeof value.isCurrentVersion !== "boolean" ||
    value.isCurrentVersion !== (value.version === publishedVersion)
  ) {
    return { progress: null, progressAvailability: "unavailable" };
  }

  return {
    progress: {
      version: value.version,
      percent: value.percent,
      updatedAt: value.updatedAt,
      isCurrentVersion: value.isCurrentVersion,
    },
    progressAvailability: "available",
  };
}

function parseLibraryStory(value: unknown): LibraryStory {
  if (
    !isRecord(value) ||
    !libraryStoryRequiredKeys.every((key) => Object.hasOwn(value, key)) ||
    typeof value.slug !== "string" ||
    !librarySlugPattern.test(value.slug) ||
    typeof value.title !== "string" ||
    value.title.trim().length === 0 ||
    typeof value.language !== "string" ||
    value.language.trim().length === 0 ||
    !isPositiveSafeInteger(value.publishedVersion) ||
    !isNonNegativeInteger(value.wordCount) ||
    !isNonNegativeInteger(value.chapterCount)
  ) {
    return invalidLibraryResponse();
  }

  const author = Object.hasOwn(value, "author") ? value.author : null;
  if (
    author !== null &&
    (typeof author !== "string" || author.trim().length === 0)
  ) {
    return invalidLibraryResponse();
  }

  const parsedProgress = Object.hasOwn(value, "progress")
    ? parseLibraryProgress(value.progress, value.publishedVersion)
    : { progress: null, progressAvailability: "unavailable" as const };

  return {
    slug: value.slug,
    title: value.title,
    author,
    language: value.language,
    publishedVersion: value.publishedVersion,
    wordCount: value.wordCount,
    chapterCount: value.chapterCount,
    ...parsedProgress,
  };
}

export function parseLibraryResponse(value: unknown): LibraryResponse {
  if (
    !isRecord(value) ||
    hasUnsafeLibraryFields(value) ||
    !Object.hasOwn(value, "items") ||
    !Array.isArray(value.items)
  ) {
    return invalidLibraryResponse();
  }

  const unavailableItemCount = Object.hasOwn(value, "unavailableItemCount")
    ? value.unavailableItemCount
    : 0;
  if (!isNonNegativeInteger(unavailableItemCount)) {
    return invalidLibraryResponse();
  }

  const items = value.items.map(parseLibraryStory);
  const slugs = new Set<string>();
  for (const item of items) {
    if (slugs.has(item.slug)) return invalidLibraryResponse();
    slugs.add(item.slug);
  }
  return { items, unavailableItemCount };
}

export async function getLibrary(): Promise<LibraryResponse> {
  const data = await request<unknown>("/api/v1/library");
  return parseLibraryResponse(data);
}

/* ----------------------------- Story ---------------------------- */

export type ReaderStoryPayload = {
  slug: string;
  title: string;
  author: string | null;
  language: string;
  version: number;
  segments: ReaderStorySegment[];
};

function hasExactKeys(
  record: Record<string, unknown>,
  required: readonly string[],
): boolean {
  const allowed = new Set(required);
  return (
    required.every((key) => Object.hasOwn(record, key)) &&
    Object.keys(record).every((key) => allowed.has(key))
  );
}

function isPositiveInteger(value: unknown): value is number {
  return Number.isInteger(value) && Number(value) >= 1;
}

function parseReaderSegment(value: unknown): ReaderStorySegment {
  if (
    !isRecord(value) ||
    !hasExactKeys(value, [
      "ordinal",
      "kind",
      "headingLevel",
      "contentKey",
      "contentOccurrence",
      "chapterKey",
      "chapterOccurrence",
      "renderedHtml",
      "wordCount",
    ]) ||
    !isPositiveInteger(value.ordinal) ||
    !["heading", "paragraph", "other"].includes(String(value.kind)) ||
    !isReaderContentKey(value.contentKey) ||
    !isPositiveInteger(value.contentOccurrence) ||
    typeof value.renderedHtml !== "string" ||
    !Number.isInteger(value.wordCount) ||
    Number(value.wordCount) < 0
  ) {
    throw new Error("Invalid Reader segment response");
  }

  const kind = value.kind as ReaderSegmentKind;
  if (
    (kind === "heading" &&
      (!Number.isInteger(value.headingLevel) ||
        Number(value.headingLevel) < 1 ||
        Number(value.headingLevel) > 6)) ||
    (kind !== "heading" && value.headingLevel !== null)
  ) {
    throw new Error("Invalid Reader segment heading level");
  }

  const hasChapter =
    value.chapterKey !== null || value.chapterOccurrence !== null;
  if (
    hasChapter &&
    (!isReaderContentKey(value.chapterKey) ||
      !isPositiveInteger(value.chapterOccurrence))
  ) {
    throw new Error("Invalid Reader segment chapter identity");
  }

  return {
    ordinal: value.ordinal,
    kind,
    headingLevel: kind === "heading" ? Number(value.headingLevel) : null,
    contentKey: value.contentKey,
    contentOccurrence: value.contentOccurrence,
    chapterKey: hasChapter ? String(value.chapterKey) : null,
    chapterOccurrence: hasChapter ? Number(value.chapterOccurrence) : null,
    renderedHtml: parseSafeRenderedStoryHTML(value.renderedHtml),
    wordCount: Number(value.wordCount),
  };
}

export function parseReaderStoryPayload(value: unknown): ReaderStoryPayload {
  if (
    !isRecord(value) ||
    !hasExactKeys(value, [
      "slug",
      "title",
      "author",
      "language",
      "version",
      "segments",
    ]) ||
    typeof value.slug !== "string" ||
    value.slug.length === 0 ||
    typeof value.title !== "string" ||
    (value.author !== null && typeof value.author !== "string") ||
    typeof value.language !== "string" ||
    !isPositiveInteger(value.version) ||
    !Array.isArray(value.segments) ||
    value.segments.length === 0
  ) {
    throw new Error("Invalid Reader response");
  }

  const segments = value.segments.map(parseReaderSegment);
  for (let index = 1; index < segments.length; index += 1) {
    if (segments[index].ordinal <= segments[index - 1].ordinal) {
      throw new Error("Reader segments are not in strict ordinal order");
    }
  }

  return {
    slug: value.slug,
    title: value.title,
    author: value.author,
    language: value.language,
    version: value.version,
    segments,
  };
}

export async function getReaderStory(
  slug: string,
  signal?: AbortSignal,
): Promise<ReaderStoryPayload> {
  const data = await request<unknown>(
    `/api/v1/reader/${encodeURIComponent(slug)}`,
    { signal },
  );
  return parseReaderStoryPayload(data);
}

/* ----------------------------- Admin ---------------------------- */
export type AdminEditionStatus = "empty" | "draft_only" | "published" | "published_with_draft" | "unpublished" | "repair_required";
export type AdminSourceStatus = "missing" | "ready" | "repair_required";
export type AdminSourceOutcome = "created_source" | "created_version" | "reused";

export type AdminStoryInput = {
  slug: string;
  editionKey: AdminStoryEditionKey;
  title: string;
  author?: string | null;
  markdown: string;
  language?: string | null;
  sourceUrl?: string | null;
  rights?: JsonObject;
};
export type AdminPreviewRequest = AdminStoryInput;
export type AdminDraftUpsertRequest = AdminStoryInput;
export type AdminSourceUpsertRequest = {
  title: string;
  author?: string | null;
  language?: string | null;
  sourceUrl?: string | null;
  rights?: JsonObject;
  sourceText: string;
};
export type AdminValidationIssue = { field: string; code: string; message: string };
export type AdminPreviewResponse = {
  slug: string; title: string; author: string | null; language: string; rights: JsonObject;
  sourceUrl: string | null; renderedHtml: SafeRenderedStoryHTML; segmentCount: number;
  wordCount: number; chapterCount: number; warnings: AdminValidationIssue[];
};
export type AdminDraftOutcome = "created_story" | "created_version" | "reused";
export type AdminDraftUpsertResponse = {
  slug: string; editionKey: AdminStoryEditionKey; versionId: string; version: number;
  segmentCount: number; wordCount: number; chapterCount: number;
  renderedHtml: SafeRenderedStoryHTML; outcome: AdminDraftOutcome;
};
export type AdminEditionIngestOutcome = "created" | "reused";
export type AdminEditionBundleInput = { editionKey: AdminStoryEditionKey; markdown: string };
export type AdminEditionBundleUpsertRequest = {
  slug: string; title: string; author?: string | null; language?: string | null;
  sourceUrl?: string | null; rights?: JsonObject; editions: AdminEditionBundleInput[];
};
export type AdminEditionBundleResult = {
  editionKey: AdminStoryEditionKey; versionId: string; version: number;
  segmentCount: number; wordCount: number; chapterCount: number; outcome: AdminEditionIngestOutcome;
};
export type AdminEditionBundleUpsertResponse = { slug: string; results: AdminEditionBundleResult[] };
export type AdminReleaseEditionRequest = {
  editionKey: AdminStoryEditionKey;
  versionId: string;
};
export type AdminCreateReleaseRequest = {
  editions: AdminReleaseEditionRequest[];
};
export type AdminReleaseEdition = {
  editionKey: AdminStoryEditionKey;
  versionId: string;
  version: number;
};
export type AdminReleaseSummary = {
  release: number;
  createdAt: string;
  editions: AdminReleaseEdition[];
};
export type AdminReleaseOutcome = "created" | "reused_current";
export type AdminCreateReleaseResponse = {
  slug: string;
  outcome: AdminReleaseOutcome;
  release: AdminReleaseSummary;
};
export type AdminStoryStatus = "draft_only" | "published" | "published_with_draft" | "unpublished" | "repair_required";
export type AdminVersionHealth = "ready" | "repair_required" | "unavailable";
export type AdminVersionPointer = { versionId: string; version: number };
export type AdminStorySourceSummary = {
  status: AdminSourceStatus; currentVersion: AdminVersionPointer | null;
  versionCount: number; updatedAt: string | null;
};
export type AdminEditionSummary = {
  editionKey: AdminStoryEditionKey; status: AdminEditionStatus;
  publishedVersion: AdminVersionPointer | null; draftVersion: AdminVersionPointer | null;
  versionCount: number; updatedAt: string | null;
};
export type AdminStoryListItem = {
  slug: string; title: string; author: string | null; language: string; rights: JsonObject;
  sourceUrl: string | null; status: AdminStoryStatus;
  publishedVersion: AdminVersionPointer | null; draftVersion: AdminVersionPointer | null;
  versionCount: number; updatedAt: string; source: AdminStorySourceSummary;
  editions: AdminEditionSummary[]; currentRelease: AdminReleaseSummary | null; releaseCount: number;
};
export type AdminVersionSummary = {
  editionKey: AdminStoryEditionKey; versionId: string; version: number; createdAt: string;
  isDraft: boolean; isPublished: boolean; segmentCount: number; wordCount: number;
  chapterCount: number; health: AdminVersionHealth;
};
export type AdminEditionDetail = AdminEditionSummary & { versions: AdminVersionSummary[] };
export type AdminStoryDetail = Omit<AdminStoryListItem, "editions"> & {
  createdAt: string; editions: AdminEditionDetail[]; releases: AdminReleaseSummary[];
};
export type AdminVersionSource = {
  slug: string; editionKey: AdminStoryEditionKey; title: string; author: string | null;
  language: string; rights: JsonObject; sourceUrl: string | null; versionId: string;
  version: number; markdown: string; renderedHtml: SafeRenderedStoryHTML;
  segmentCount: number; wordCount: number; chapterCount: number; createdAt: string;
  isDraft: boolean; isPublished: boolean; health: AdminVersionHealth;
};
export type AdminSourceVersionSummary = {
  versionId: string; version: number; title: string; author: string | null; language: string;
  rights: JsonObject; sourceUrl: string | null; createdAt: string; isCurrent: boolean;
};
export type AdminSourceDetail = {
  slug: string; status: AdminSourceStatus; currentVersion: AdminVersionPointer | null;
  versionCount: number; updatedAt: string | null; versions: AdminSourceVersionSummary[];
};
export type AdminSourceVersion = AdminSourceVersionSummary & { slug: string; sourceText: string };
export type AdminSourceUpsertResponse = { slug: string; versionId: string; version: number; outcome: AdminSourceOutcome };
export type AdminStoriesListResponse = { items: AdminStoryListItem[] };
export type AdminStoryStatusResponse = {
  slug: string; status: AdminStoryStatus;
  publishedVersion: AdminVersionPointer | null; draftVersion: AdminVersionPointer | null;
  versionCount: number; updatedAt: string; currentRelease: AdminReleaseSummary | null;
  releaseCount: number;
};

const adminStoryStatuses = new Set<AdminStoryStatus>(["draft_only", "published", "published_with_draft", "unpublished", "repair_required"]);
const adminEditionStatuses = new Set<AdminEditionStatus>(["empty", "draft_only", "published", "published_with_draft", "unpublished", "repair_required"]);
const adminSourceStatuses = new Set<AdminSourceStatus>(["missing", "ready", "repair_required"]);
const adminVersionHealthValues = new Set<AdminVersionHealth>(["ready", "repair_required", "unavailable"]);
const adminDraftOutcomes = new Set<AdminDraftOutcome>(["created_story", "created_version", "reused"]);
const adminEditionIngestOutcomes = new Set<AdminEditionIngestOutcome>(["created", "reused"]);
const adminReleaseOutcomes = new Set<AdminReleaseOutcome>(["created", "reused_current"]);
const adminSourceOutcomes = new Set<AdminSourceOutcome>(["created_source", "created_version", "reused"]);
const adminUUIDPattern =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/iu;
const forbiddenAdminKeys = new Set([
  "account",
  "accountdata",
  "accountemail",
  "accountid",
  "accounts",
  "chapterkey",
  "chapteroccurrence",
  "contenthash",
  "contentkey",
  "contentoccurrence",
  "databaseid",
  "headinglevel",
  "internalid",
  "locator",
  "ordinal",
  "profile",
  "profiledata",
  "releaseid",
  "profileid",
  "profiles",
  "segment",
  "segments",
  "session",
  "sessiondata",
  "sessionid",
  "storyid",
]);

function compactAdminKey(key: string): string {
  return key.replaceAll(/[_-]/g, "").toLocaleLowerCase("en-GB");
}

function hasForbiddenAdminFields(
  value: unknown,
  allowedContent: ReadonlySet<string>,
  seen: WeakSet<object> = new WeakSet(),
): boolean {
  if (Array.isArray(value)) {
    if (seen.has(value)) return false;
    seen.add(value);
    return value.some((item) =>
      hasForbiddenAdminFields(item, allowedContent, seen),
    );
  }
  if (!isRecord(value)) return false;
  if (seen.has(value)) return false;
  seen.add(value);
  return Object.entries(value).some(([key, child]) => {
    const compact = compactAdminKey(key);
    if (forbiddenAdminKeys.has(compact)) return true;
    if (
      (compact === "markdown" || compact === "renderedhtml" || compact === "sourcetext") &&
      !allowedContent.has(compact)
    ) {
      return true;
    }
    return hasForbiddenAdminFields(child, allowedContent, seen);
  });
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
    throw new Error("Invalid admin response");
  }
  return value;
}

function requiredAdminString(
  record: Record<string, unknown>,
  key: string,
): string {
  const value = record[key];
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new Error("Invalid admin response");
  }
  return value;
}

function nullableAdminString(
  record: Record<string, unknown>,
  key: string,
): string | null {
  const value = record[key];
  if (value === null) return null;
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new Error("Invalid admin response");
  }
  return value;
}

function parseAdminSlug(value: unknown): string {
  if (typeof value !== "string" || !librarySlugPattern.test(value)) {
    throw new Error("Invalid admin response");
  }
  return value;
}

function parseAdminUUID(value: unknown): string {
  if (typeof value !== "string" || !adminUUIDPattern.test(value)) {
    throw new Error("Invalid admin response");
  }
  return value;
}

function parseAdminStatus(value: unknown): AdminStoryStatus {
  if (typeof value !== "string" || !adminStoryStatuses.has(value as AdminStoryStatus)) throw new Error("Invalid admin response");
  return value as AdminStoryStatus;
}
export function parseAdminEditionKey(value: unknown): AdminStoryEditionKey {
  if (typeof value !== "string" || !adminStoryEditionKeys.includes(value as AdminStoryEditionKey)) throw new Error("Invalid admin response");
  return value as AdminStoryEditionKey;
}
function parseAdminEditionStatus(value: unknown): AdminEditionStatus {
  if (typeof value !== "string" || !adminEditionStatuses.has(value as AdminEditionStatus)) throw new Error("Invalid admin response");
  return value as AdminEditionStatus;
}
function parseAdminSourceStatus(value: unknown): AdminSourceStatus {
  if (typeof value !== "string" || !adminSourceStatuses.has(value as AdminSourceStatus)) throw new Error("Invalid admin response");
  return value as AdminSourceStatus;
}
function parseAdminHealth(value: unknown): AdminVersionHealth {
  if (typeof value !== "string" || !adminVersionHealthValues.has(value as AdminVersionHealth)) throw new Error("Invalid admin response");
  return value as AdminVersionHealth;
}
function parseAdminPointer(value: unknown): AdminVersionPointer | null {
  if (value === null) return null;
  const record = adminRecord(value);
  if (!isPositiveSafeInteger(record.version)) throw new Error("Invalid admin response");
  return { versionId: parseAdminUUID(record.versionId), version: record.version };
}
function parseAdminNullableTimestamp(value: unknown): string | null {
  if (value === null) return null;
  if (!isRFC3339Timestamp(value)) throw new Error("Invalid admin response");
  return value;
}
function parseAdminMetadata(record: Record<string, unknown>) {
  if (!isJsonObject(record.rights) || typeof record.language !== "string" || record.language.trim().length === 0) throw new Error("Invalid admin response");
  return { title: requiredAdminString(record, "title"), author: nullableAdminString(record, "author"), language: record.language, rights: record.rights, sourceUrl: nullableAdminString(record, "sourceUrl") };
}
function parseAdminSourceSummary(value: unknown): AdminStorySourceSummary {
  const record = adminRecord(value);
  if (!isNonNegativeInteger(record.versionCount)) throw new Error("Invalid admin response");
  const result = { status: parseAdminSourceStatus(record.status), currentVersion: parseAdminPointer(record.currentVersion), versionCount: record.versionCount, updatedAt: parseAdminNullableTimestamp(record.updatedAt) };
  if (result.status === "missing" && (result.currentVersion !== null || result.versionCount !== 0 || result.updatedAt !== null)) throw new Error("Invalid admin response");
  if (result.status === "ready" && (result.currentVersion === null || result.versionCount < 1)) throw new Error("Invalid admin response");
  return result;
}
function parseAdminVersionSummary(value: unknown): AdminVersionSummary {
  const record = adminRecord(value);
  if (!isPositiveSafeInteger(record.version) || !isRFC3339Timestamp(record.createdAt) || typeof record.isDraft !== "boolean" || typeof record.isPublished !== "boolean" || !isNonNegativeInteger(record.segmentCount) || !isNonNegativeInteger(record.wordCount) || !isNonNegativeInteger(record.chapterCount)) throw new Error("Invalid admin response");
  return { editionKey: parseAdminEditionKey(record.editionKey), versionId: parseAdminUUID(record.versionId), version: record.version, createdAt: record.createdAt, isDraft: record.isDraft, isPublished: record.isPublished, segmentCount: record.segmentCount, wordCount: record.wordCount, chapterCount: record.chapterCount, health: parseAdminHealth(record.health) };
}
function parseAdminEditionSummary(value: unknown): AdminEditionSummary {
  const record = adminRecord(value);
  if (!isNonNegativeInteger(record.versionCount)) throw new Error("Invalid admin response");
  return { editionKey: parseAdminEditionKey(record.editionKey), status: parseAdminEditionStatus(record.status), publishedVersion: parseAdminPointer(record.publishedVersion), draftVersion: parseAdminPointer(record.draftVersion), versionCount: record.versionCount, updatedAt: parseAdminNullableTimestamp(record.updatedAt) };
}
function assertCanonicalEditionOrder(editions: readonly { editionKey: AdminStoryEditionKey }[]): void {
  if (editions.length !== adminStoryEditionKeys.length) throw new Error("Invalid admin response");
  for (let i = 0; i < adminStoryEditionKeys.length; i += 1) if (editions[i]?.editionKey !== adminStoryEditionKeys[i]) throw new Error("Invalid admin response");
}
function parseAdminEditionSummaries(value: unknown): AdminEditionSummary[] {
  if (!Array.isArray(value)) throw new Error("Invalid admin response");
  const editions = value.map(parseAdminEditionSummary); assertCanonicalEditionOrder(editions); return editions;
}
function assertCanonicalEditionSubset(editions: readonly { editionKey: AdminStoryEditionKey }[]): void {
  if (editions.length < 1 || editions.length > adminStoryEditionKeys.length) throw new Error("Invalid admin response");
  let previousIndex = -1;
  const ids = new Set<AdminStoryEditionKey>();
  for (const edition of editions) {
    const index = adminStoryEditionKeys.indexOf(edition.editionKey);
    if (index <= previousIndex || ids.has(edition.editionKey)) throw new Error("Invalid admin response");
    previousIndex = index;
    ids.add(edition.editionKey);
  }
}
export function parseAdminReleaseSummary(value: unknown): AdminReleaseSummary {
  const record = adminRecord(value);
  if (!isPositiveSafeInteger(record.release) || !isRFC3339Timestamp(record.createdAt) || !Array.isArray(record.editions)) throw new Error("Invalid admin response");
  const editions = record.editions.map((item) => {
    const member = adminRecord(item);
    if (!isPositiveSafeInteger(member.version)) throw new Error("Invalid admin response");
    return {
      editionKey: parseAdminEditionKey(member.editionKey),
      versionId: parseAdminUUID(member.versionId),
      version: member.version,
    };
  });
  assertCanonicalEditionSubset(editions);
  if (new Set(editions.map((edition) => edition.versionId)).size !== editions.length) throw new Error("Invalid admin response");
  return { release: record.release, createdAt: record.createdAt, editions };
}
function parseAdminCurrentRelease(value: unknown): AdminReleaseSummary | null {
  return value === null ? null : parseAdminReleaseSummary(value);
}
function parseAdminEditionDetail(value: unknown): AdminEditionDetail {
  const record = adminRecord(value); const summary = parseAdminEditionSummary(record);
  if (!Array.isArray(record.versions)) throw new Error("Invalid admin response");
  const versions = record.versions.map(parseAdminVersionSummary);
  if (versions.length !== summary.versionCount) throw new Error("Invalid admin response");
  const ids = new Set<string>(); let previous = Number.POSITIVE_INFINITY;
  for (const version of versions) {
    if (version.editionKey !== summary.editionKey || ids.has(version.versionId) || version.version >= previous) throw new Error("Invalid admin response");
    ids.add(version.versionId); previous = version.version;
  }
  for (const pointer of [summary.draftVersion, summary.publishedVersion]) if (pointer !== null && !ids.has(pointer.versionId)) throw new Error("Invalid admin response");
  return { ...summary, versions };
}
function parseAdminEditionDetails(value: unknown): AdminEditionDetail[] {
  if (!Array.isArray(value)) throw new Error("Invalid admin response");
  const editions = value.map(parseAdminEditionDetail); assertCanonicalEditionOrder(editions); return editions;
}
export function parseAdminStorySummary(value: unknown): AdminStoryListItem {
  const record = adminRecord(value);
  if (!isNonNegativeInteger(record.versionCount) || !isNonNegativeInteger(record.releaseCount) || !isRFC3339Timestamp(record.updatedAt)) throw new Error("Invalid admin response");
  const status = parseAdminStatus(record.status);
  const publishedVersion = parseAdminPointer(record.publishedVersion);
  const currentRelease = parseAdminCurrentRelease(record.currentRelease);
  if (status !== "repair_required") {
    if (currentRelease === null && publishedVersion !== null) throw new Error("Invalid admin response");
    if (currentRelease !== null && (publishedVersion === null || !currentRelease.editions.some((edition) => edition.versionId === publishedVersion.versionId && edition.version === publishedVersion.version))) throw new Error("Invalid admin response");
  }
  if (currentRelease !== null && record.releaseCount < currentRelease.release) throw new Error("Invalid admin response");
  return {
    slug: parseAdminSlug(record.slug),
    ...parseAdminMetadata(record),
    status,
    publishedVersion,
    draftVersion: parseAdminPointer(record.draftVersion),
    versionCount: record.versionCount,
    updatedAt: record.updatedAt,
    source: parseAdminSourceSummary(record.source),
    editions: parseAdminEditionSummaries(record.editions),
    currentRelease,
    releaseCount: record.releaseCount,
  };
}
export function parseAdminStoriesListResponse(value: unknown): AdminStoriesListResponse {
  const record = adminRecord(value); if (!Array.isArray(record.items)) throw new Error("Invalid admin response");
  const items = record.items.map(parseAdminStorySummary); if (new Set(items.map((item) => item.slug)).size !== items.length) throw new Error("Invalid admin response"); return { items };
}
export function parseAdminStoryDetail(value: unknown): AdminStoryDetail {
  const record = adminRecord(value); const summary = parseAdminStorySummary(record);
  if (!isRFC3339Timestamp(record.createdAt) || !Array.isArray(record.releases)) throw new Error("Invalid admin response");
  const releases = record.releases.map(parseAdminReleaseSummary);
  if (releases.length !== summary.releaseCount) throw new Error("Invalid admin response");
  let previous = Number.POSITIVE_INFINITY;
  for (const release of releases) {
    if (release.release >= previous) throw new Error("Invalid admin response");
    previous = release.release;
  }
  if (summary.currentRelease !== null && !releases.some((release) => release.release === summary.currentRelease?.release && release.createdAt === summary.currentRelease.createdAt)) throw new Error("Invalid admin response");
  return { ...summary, createdAt: record.createdAt, editions: parseAdminEditionDetails(record.editions), releases };
}
export function parseAdminVersionSource(value: unknown): AdminVersionSource {
  const record = adminRecord(value, ["markdown", "renderedhtml"]);
  if (!isPositiveSafeInteger(record.version) || !isRFC3339Timestamp(record.createdAt) || typeof record.markdown !== "string" || typeof record.renderedHtml !== "string" || typeof record.isDraft !== "boolean" || typeof record.isPublished !== "boolean" || !isNonNegativeInteger(record.segmentCount) || !isNonNegativeInteger(record.wordCount) || !isNonNegativeInteger(record.chapterCount)) throw new Error("Invalid admin response");
  return { slug: parseAdminSlug(record.slug), editionKey: parseAdminEditionKey(record.editionKey), versionId: parseAdminUUID(record.versionId), version: record.version, ...parseAdminMetadata(record), markdown: record.markdown, renderedHtml: parseSafeRenderedStoryHTML(record.renderedHtml), segmentCount: record.segmentCount, wordCount: record.wordCount, chapterCount: record.chapterCount, createdAt: record.createdAt, isDraft: record.isDraft, isPublished: record.isPublished, health: parseAdminHealth(record.health) };
}
function parseAdminSourceVersionSummary(value: unknown): AdminSourceVersionSummary {
  const record = adminRecord(value);
  if (!isPositiveSafeInteger(record.version) || !isRFC3339Timestamp(record.createdAt) || typeof record.isCurrent !== "boolean") throw new Error("Invalid admin response");
  return { versionId: parseAdminUUID(record.versionId), version: record.version, ...parseAdminMetadata(record), createdAt: record.createdAt, isCurrent: record.isCurrent };
}
export function parseAdminSourceDetail(value: unknown): AdminSourceDetail {
  const record = adminRecord(value); const summary = parseAdminSourceSummary(record);
  if (summary.status === "missing" || !Array.isArray(record.versions) || !isNonNegativeInteger(record.versionCount)) throw new Error("Invalid admin response");
  const versions = record.versions.map(parseAdminSourceVersionSummary);
  if (versions.length !== record.versionCount) throw new Error("Invalid admin response");
  const ids = new Set<string>(); let previous = Number.POSITIVE_INFINITY;
  for (const version of versions) { if (ids.has(version.versionId) || version.version >= previous) throw new Error("Invalid admin response"); ids.add(version.versionId); previous = version.version; }
  if (summary.currentVersion !== null && !versions.some((version) => version.versionId === summary.currentVersion?.versionId && version.version === summary.currentVersion.version && version.isCurrent)) throw new Error("Invalid admin response");
  return { slug: parseAdminSlug(record.slug), ...summary, versions };
}
export function parseAdminSourceVersion(value: unknown): AdminSourceVersion {
  const record = adminRecord(value, ["sourcetext"]);
  if (
    typeof record.sourceText !== "string" ||
    record.sourceText.trim().length === 0 ||
    !isPositiveSafeInteger(record.version) ||
    !isRFC3339Timestamp(record.createdAt) ||
    typeof record.isCurrent !== "boolean"
  ) {
    throw new Error("Invalid admin response");
  }
  return {
    slug: parseAdminSlug(record.slug),
    versionId: parseAdminUUID(record.versionId),
    version: record.version,
    ...parseAdminMetadata(record),
    createdAt: record.createdAt,
    isCurrent: record.isCurrent,
    sourceText: record.sourceText,
  };
}
function parseAdminIssue(value: unknown): AdminValidationIssue {
  const record = adminRecord(value); return { field: requiredAdminString(record, "field"), code: requiredAdminString(record, "code"), message: requiredAdminString(record, "message") };
}
export function getAdminValidationIssues(error: unknown): AdminValidationIssue[] | null {
  if (!(error instanceof Error)) return null; const body = (error as APIError).body;
  if (!isJsonObject(body) || !isJsonObject(body.error) || !Array.isArray(body.error.issues)) return null;
  try { return body.error.issues.map(parseAdminIssue); } catch { return null; }
}
export function parseAdminPreviewResponse(value: unknown): AdminPreviewResponse {
  const record = adminRecord(value, ["renderedhtml"]);
  if (typeof record.renderedHtml !== "string" || !isNonNegativeInteger(record.segmentCount) || !isNonNegativeInteger(record.wordCount) || !isNonNegativeInteger(record.chapterCount) || !Array.isArray(record.warnings)) throw new Error("Invalid admin response");
  return { slug: parseAdminSlug(record.slug), ...parseAdminMetadata(record), renderedHtml: parseSafeRenderedStoryHTML(record.renderedHtml), segmentCount: record.segmentCount, wordCount: record.wordCount, chapterCount: record.chapterCount, warnings: record.warnings.map(parseAdminIssue) };
}
export function parseAdminDraftUpsertResponse(value: unknown): AdminDraftUpsertResponse {
  const record = adminRecord(value, ["renderedhtml"]);
  if (!isPositiveSafeInteger(record.version) || !isNonNegativeInteger(record.segmentCount) || !isNonNegativeInteger(record.wordCount) || !isNonNegativeInteger(record.chapterCount) || typeof record.renderedHtml !== "string" || typeof record.outcome !== "string" || !adminDraftOutcomes.has(record.outcome as AdminDraftOutcome)) throw new Error("Invalid admin response");
  return { slug: parseAdminSlug(record.slug), editionKey: parseAdminEditionKey(record.editionKey), versionId: parseAdminUUID(record.versionId), version: record.version, segmentCount: record.segmentCount, wordCount: record.wordCount, chapterCount: record.chapterCount, renderedHtml: parseSafeRenderedStoryHTML(record.renderedHtml), outcome: record.outcome as AdminDraftOutcome };
}
function parseAdminEditionBundleResult(value: unknown): AdminEditionBundleResult {
  const record = adminRecord(value);
  if (!isPositiveSafeInteger(record.version) || !isNonNegativeInteger(record.segmentCount) || !isNonNegativeInteger(record.wordCount) || !isNonNegativeInteger(record.chapterCount) || typeof record.outcome !== "string" || !adminEditionIngestOutcomes.has(record.outcome as AdminEditionIngestOutcome)) throw new Error("Invalid admin response");
  return { editionKey: parseAdminEditionKey(record.editionKey), versionId: parseAdminUUID(record.versionId), version: record.version, segmentCount: record.segmentCount, wordCount: record.wordCount, chapterCount: record.chapterCount, outcome: record.outcome as AdminEditionIngestOutcome };
}
export function parseAdminEditionBundleUpsertResponse(value: unknown): AdminEditionBundleUpsertResponse {
  const record = adminRecord(value);
  if (!Array.isArray(record.results)) throw new Error("Invalid admin response");
  const results = record.results.map(parseAdminEditionBundleResult);
  assertCanonicalEditionOrder(results);
  if (new Set(results.map((item) => item.versionId)).size !== results.length) throw new Error("Invalid admin response");
  return { slug: parseAdminSlug(record.slug), results };
}
export function parseAdminCreateReleaseResponse(value: unknown): AdminCreateReleaseResponse {
  const record = adminRecord(value);
  if (typeof record.outcome !== "string" || !adminReleaseOutcomes.has(record.outcome as AdminReleaseOutcome)) throw new Error("Invalid admin response");
  return {
    slug: parseAdminSlug(record.slug),
    outcome: record.outcome as AdminReleaseOutcome,
    release: parseAdminReleaseSummary(record.release),
  };
}
export function parseAdminStoryStatusResponse(value: unknown): AdminStoryStatusResponse {
  const record = adminRecord(value);
  if (!isNonNegativeInteger(record.versionCount) || !isNonNegativeInteger(record.releaseCount) || !isRFC3339Timestamp(record.updatedAt)) throw new Error("Invalid admin response");
  const status = parseAdminStatus(record.status);
  const publishedVersion = parseAdminPointer(record.publishedVersion);
  const currentRelease = parseAdminCurrentRelease(record.currentRelease);
  if (status !== "repair_required") {
    if (currentRelease === null && publishedVersion !== null) throw new Error("Invalid admin response");
    if (currentRelease !== null && (publishedVersion === null || !currentRelease.editions.some((edition) => edition.versionId === publishedVersion.versionId && edition.version === publishedVersion.version))) throw new Error("Invalid admin response");
  }
  return {
    slug: parseAdminSlug(record.slug),
    status,
    publishedVersion,
    draftVersion: parseAdminPointer(record.draftVersion),
    versionCount: record.versionCount,
    updatedAt: record.updatedAt,
    currentRelease,
    releaseCount: record.releaseCount,
  };
}
export function parseAdminSourceUpsertResponse(value: unknown): AdminSourceUpsertResponse {
  const record = adminRecord(value); if (!isPositiveSafeInteger(record.version) || typeof record.outcome !== "string" || !adminSourceOutcomes.has(record.outcome as AdminSourceOutcome)) throw new Error("Invalid admin response");
  return { slug: parseAdminSlug(record.slug), versionId: parseAdminUUID(record.versionId), version: record.version, outcome: record.outcome as AdminSourceOutcome };
}
export async function adminPreview(payload: AdminPreviewRequest, signal?: AbortSignal): Promise<AdminPreviewResponse> {
  return parseAdminPreviewResponse(await request<unknown>("/api/v1/admin/preview", { method: "POST", body: JSON.stringify(payload), signal }));
}
export async function adminDraftUpsertStory(payload: AdminDraftUpsertRequest): Promise<AdminDraftUpsertResponse> {
  return parseAdminDraftUpsertResponse(await request<unknown>("/api/v1/admin/stories/draft", { method: "POST", body: JSON.stringify(payload) }));
}
export async function adminIngestEditionBundle(payload: AdminEditionBundleUpsertRequest): Promise<AdminEditionBundleUpsertResponse> {
  return parseAdminEditionBundleUpsertResponse(await request<unknown>("/api/v1/admin/stories/editions/ingest", { method: "POST", body: JSON.stringify(payload) }));
}
export async function adminListStories(signal?: AbortSignal): Promise<AdminStoriesListResponse> {
  return parseAdminStoriesListResponse(await request<unknown>("/api/v1/admin/stories", { signal }));
}
export async function adminGetStory(slug: string, signal?: AbortSignal): Promise<AdminStoryDetail> {
  return parseAdminStoryDetail(await request<unknown>(`/api/v1/admin/stories/${encodeURIComponent(slug)}`, { signal }));
}
export async function adminGetEditionVersionSource(slug: string, editionKey: AdminStoryEditionKey, versionId: string, signal?: AbortSignal): Promise<AdminVersionSource> {
  const parsed = parseAdminVersionSource(await request<unknown>(`/api/v1/admin/stories/${encodeURIComponent(slug)}/editions/${encodeURIComponent(editionKey)}/versions/${encodeURIComponent(versionId)}`, { signal }));
  if (parsed.editionKey !== editionKey) throw new Error("Invalid admin response"); return parsed;
}
export async function adminGetVersionSource(slug: string, versionId: string, signal?: AbortSignal): Promise<AdminVersionSource> {
  return adminGetEditionVersionSource(slug, "classic", versionId, signal);
}
export async function adminGetSource(slug: string, signal?: AbortSignal): Promise<AdminSourceDetail> {
  return parseAdminSourceDetail(await request<unknown>(`/api/v1/admin/stories/${encodeURIComponent(slug)}/source`, { signal }));
}
export async function adminGetSourceVersion(slug: string, versionId: string, signal?: AbortSignal): Promise<AdminSourceVersion> {
  return parseAdminSourceVersion(await request<unknown>(`/api/v1/admin/stories/${encodeURIComponent(slug)}/source/versions/${encodeURIComponent(versionId)}`, { signal }));
}
export async function adminUpsertSource(slug: string, payload: AdminSourceUpsertRequest): Promise<AdminSourceUpsertResponse> {
  return parseAdminSourceUpsertResponse(await request<unknown>(`/api/v1/admin/stories/${encodeURIComponent(slug)}/source`, { method: "PUT", body: JSON.stringify(payload) }));
}
export async function adminCreateRelease(slug: string, payload: AdminCreateReleaseRequest): Promise<AdminCreateReleaseResponse> {
  return parseAdminCreateReleaseResponse(await request<unknown>(`/api/v1/admin/stories/${encodeURIComponent(slug)}/releases`, { method: "POST", body: JSON.stringify(payload) }));
}
export async function adminUnpublishStory(slug: string): Promise<AdminStoryStatusResponse> {
  return parseAdminStoryStatusResponse(await request<unknown>(`/api/v1/admin/stories/${encodeURIComponent(slug)}/unpublish`, { method: "POST" }));
}

/* ---------------------------- Progress -------------------------- */

export type ProgressState = {
  version: number;
  locator: ReaderLocatorV2;
  percent: number;
};

export type ProgressResponse = {
  progress: ProgressState | null;
};

export function parseProgressResponse(value: unknown): ProgressResponse {
  if (!isRecord(value) || !hasExactKeys(value, ["progress"])) {
    throw new Error("Invalid progress response");
  }
  if (value.progress === null) return { progress: null };
  if (
    !isRecord(value.progress) ||
    !hasExactKeys(value.progress, ["version", "locator", "percent"]) ||
    !isPositiveInteger(value.progress.version) ||
    typeof value.progress.percent !== "number" ||
    !Number.isFinite(value.progress.percent) ||
    value.progress.percent < 0 ||
    value.progress.percent > 1
  ) {
    throw new Error("Invalid progress response");
  }
  return {
    progress: {
      version: value.progress.version,
      locator: parseReaderLocatorV2(value.progress.locator),
      percent: value.progress.percent,
    },
  };
}

export async function getProgress(
  slug: string,
  profileID: string,
): Promise<ProgressResponse> {
  const data = await profileScopedRequest<unknown>(
    `/api/v1/progress/${encodeURIComponent(slug)}`,
    profileID,
  );
  return parseProgressResponse(data);
}

export async function saveProgress(
  slug: string,
  profileID: string,
  version: number,
  locator: ReaderLocatorV2,
  percent: number,
  options: { keepalive?: boolean } = {},
): Promise<void> {
  const result = await profileScopedRequest<unknown>(
    `/api/v1/progress/${encodeURIComponent(slug)}`,
    profileID,
    {
      method: "PUT",
      body: JSON.stringify({ version, locator, percent }),
      keepalive: options.keepalive,
    },
  );
  if (!isRecord(result) || result.ok !== true) {
    throw new Error("Invalid progress-save response");
  }
}

/* ------------------------- Continue / Recent -------------------- */

export type ContinueItem = {
  slug: string;
  percent: number;
  updatedAt: string;
};

export async function getContinue(
  profileID: string,
  limit = 3,
): Promise<{ items: ContinueItem[] }> {
  const data = await profileScopedRequest<{ items?: ContinueItem[] }>(
    `/api/v1/continue?limit=${limit}`,
    profileID,
  );
  return { items: Array.isArray(data.items) ? data.items : [] };
}
