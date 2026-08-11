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
  isResolvedVersion: boolean;
};

export type LibraryProgressAvailability = "available" | "unavailable";

export type LibraryEditionSummary = {
  editionKey: ReaderEditionKey;
  version: number;
  wordCount: number;
  chapterCount: number;
};

export type LibraryStory = {
  slug: string;
  title: string;
  author: string | null;
  language: string;
  state: "selected";
  eligibleEditions: LibraryEditionSummary[];
  selectedEdition: ReaderEditionKey;
  progress: LibraryProgress | null;
  progressAvailability: LibraryProgressAvailability;
};

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
  "state",
  "eligibleEditions",
  "selectedEdition",
] as const;

const libraryEditionRequiredKeys = [
  "editionKey",
  "version",
  "wordCount",
  "chapterCount",
] as const;

const libraryProgressKeys = [
  "version",
  "percent",
  "updatedAt",
  "isResolvedVersion",
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
  "iscurrentversion",
  "locator",
  "markdown",
  "profile",
  "profiledata",
  "profileid",
  "profiles",
  "prompt",
  "publishedversion",
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

function parseLibraryEditionSummaries(value: unknown): LibraryEditionSummary[] {
  if (!Array.isArray(value) || value.length === 0) {
    return invalidLibraryResponse();
  }

  const editions: LibraryEditionSummary[] = [];
  let previousIndex = -1;
  for (const candidate of value) {
    if (
      !isRecord(candidate) ||
      !libraryEditionRequiredKeys.every((key) => Object.hasOwn(candidate, key)) ||
      !isReaderEditionKey(candidate.editionKey) ||
      !isPositiveSafeInteger(candidate.version) ||
      !isNonNegativeInteger(candidate.wordCount) ||
      !isNonNegativeInteger(candidate.chapterCount)
    ) {
      return invalidLibraryResponse();
    }
    const index = readerEditionKeys.indexOf(candidate.editionKey);
    if (index <= previousIndex) return invalidLibraryResponse();
    previousIndex = index;
    editions.push({
      editionKey: candidate.editionKey,
      version: candidate.version,
      wordCount: candidate.wordCount,
      chapterCount: candidate.chapterCount,
    });
  }
  return editions;
}

function parseLibraryProgress(
  value: unknown,
  selectedEdition: ReaderEditionKey,
  eligibleEditions: readonly LibraryEditionSummary[],
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
    typeof value.isResolvedVersion !== "boolean"
  ) {
    return { progress: null, progressAvailability: "unavailable" };
  }

  if (value.isResolvedVersion) {
    const selected = eligibleEditions.find(
      (edition) => edition.editionKey === selectedEdition,
    );
    if (selected === undefined || selected.version !== value.version) {
      return { progress: null, progressAvailability: "unavailable" };
    }
  }

  return {
    progress: {
      version: value.version,
      percent: value.percent,
      updatedAt: value.updatedAt,
      isResolvedVersion: value.isResolvedVersion,
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
    value.state !== "selected" ||
    Object.hasOwn(value, "wordCount") ||
    Object.hasOwn(value, "chapterCount")
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

  const eligibleEditions = parseLibraryEditionSummaries(value.eligibleEditions);
  if (
    !isReaderEditionKey(value.selectedEdition) ||
    !eligibleEditions.some(
      (edition) => edition.editionKey === value.selectedEdition,
    )
  ) {
    return invalidLibraryResponse();
  }
  const selectedEdition = value.selectedEdition;

  const parsedProgress = Object.hasOwn(value, "progress")
    ? parseLibraryProgress(
        value.progress,
        selectedEdition,
        eligibleEditions,
      )
    : { progress: null, progressAvailability: "unavailable" as const };

  return {
    slug: value.slug,
    title: value.title,
    author,
    language: value.language,
    state: "selected",
    eligibleEditions,
    selectedEdition,
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

export async function getLibrary(profileID: string): Promise<LibraryResponse> {
  const data = await profileScopedRequest<unknown>("/api/v1/library", profileID);
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

export type ReaderResolvedStoryPayload = ReaderStoryPayload & {
  editionKey: ReaderEditionKey;
};

export type ReaderResolutionPayload = {
  state: "selected";
  eligibleEditions: ReaderEditionKey[];
  story: ReaderResolvedStoryPayload;
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

function parseReaderEditionSubset(value: unknown): ReaderEditionKey[] {
  if (!Array.isArray(value) || value.length === 0) {
    throw new Error("Invalid Reader resolution editions");
  }

  const editions: ReaderEditionKey[] = [];
  let previousIndex = -1;
  for (const candidate of value) {
    if (!isReaderEditionKey(candidate)) {
      throw new Error("Invalid Reader resolution edition");
    }
    const index = readerEditionKeys.indexOf(candidate);
    if (index <= previousIndex || editions.includes(candidate)) {
      throw new Error("Invalid Reader resolution edition order");
    }
    editions.push(candidate);
    previousIndex = index;
  }
  return editions;
}

function parseReaderResolvedStory(value: unknown): ReaderResolvedStoryPayload {
  if (
    !isRecord(value) ||
    !hasExactKeys(value, [
      "slug",
      "title",
      "author",
      "language",
      "version",
      "segments",
      "editionKey",
    ]) ||
    !isReaderEditionKey(value.editionKey)
  ) {
    throw new Error("Invalid Reader resolution story");
  }

  const story = parseReaderStoryPayload({
    slug: value.slug,
    title: value.title,
    author: value.author,
    language: value.language,
    version: value.version,
    segments: value.segments,
  });
  return { ...story, editionKey: value.editionKey };
}

export function parseReaderResolutionPayload(
  value: unknown,
): ReaderResolutionPayload {
  if (
    !isRecord(value) ||
    !hasExactKeys(value, ["state", "eligibleEditions", "story"])
  ) {
    throw new Error("Invalid Reader resolution response");
  }

  const eligibleEditions = parseReaderEditionSubset(value.eligibleEditions);
  if (value.state !== "selected") {
    throw new Error("Invalid Reader resolution state");
  }
  const story = parseReaderResolvedStory(value.story);
  if (!eligibleEditions.includes(story.editionKey)) {
    throw new Error("Reader selected an ineligible edition");
  }
  return { state: "selected", eligibleEditions, story };
}

function parseReaderEditionMutation(value: unknown): void {
  if (!isRecord(value) || !hasExactKeys(value, ["ok"]) || value.ok !== true) {
    throw new Error("Invalid Reader edition response");
  }
}

export async function getReaderStory(
  slug: string,
  profileID: string,
  signal?: AbortSignal,
): Promise<ReaderResolutionPayload> {
  const data = await profileScopedRequest<unknown>(
    `/api/v1/reader-resolution/${encodeURIComponent(slug)}`,
    profileID,
    { signal },
  );
  return parseReaderResolutionPayload(data);
}

export async function setReaderStoryEdition(
  slug: string,
  profileID: string,
  editionKey: ReaderEditionKey,
  signal?: AbortSignal,
): Promise<void> {
  const data = await profileScopedRequest<unknown>(
    `/api/v1/reader-edition/${encodeURIComponent(slug)}`,
    profileID,
    {
      method: "PUT",
      body: JSON.stringify({ editionKey }),
      signal,
    },
  );
  parseReaderEditionMutation(data);
}

export async function clearReaderStoryEdition(
  slug: string,
  profileID: string,
  signal?: AbortSignal,
): Promise<void> {
  const data = await profileScopedRequest<unknown>(
    `/api/v1/reader-edition/${encodeURIComponent(slug)}`,
    profileID,
    { method: "DELETE", signal },
  );
  parseReaderEditionMutation(data);
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

export type AdminSourceProviderID = "project-gutenberg";
export type AdminSourceProviderContributor = { name: string; role: string };
export type AdminSourceProviderRepresentation = {
  label: string;
  mediaType: string;
  url: string;
  sizeBytes?: number;
};
export type AdminSourceProviderWork = {
  provider: AdminSourceProviderID;
  externalId: string;
  title: string;
  contributors: AdminSourceProviderContributor[];
  languages: string[];
  landingUrl: string;
  providerRights?: string;
  representations: AdminSourceProviderRepresentation[];
};
export type AdminSourceProviderSearchResponse = {
  provider: AdminSourceProviderID;
  results: AdminSourceProviderWork[];
};
export type AdminSourceQualityStatus = "pending" | "approved" | "rejected";
export type AdminSourceQualityReview = { status: AdminSourceQualityStatus; note: string | null; reviewedAt: string | null };
export type AdminCopyrightFactState = "none_confirmed" | "present" | "unknown";
export type AdminCopyrightEvidenceReference = { source: string; fact: string; locator?: string; identifier?: string; digest?: string };
export type AdminCopyrightFactEvidence = { state: AdminCopyrightFactState; references: AdminCopyrightEvidenceReference[] };
export type AdminSourceEligibilityHumanEvidence = { workCategory?: "ordinary_literary" | "unknown"; workCategoryReferences?: AdminCopyrightEvidenceReference[]; authorDeathYear?: number; authorDeathReferences?: AdminCopyrightEvidenceReference[]; firstPublicationYear?: number; firstPublicationReferences?: AdminCopyrightEvidenceReference[]; translation?: AdminCopyrightFactEvidence; additionalTextualContribution?: AdminCopyrightFactEvidence; specialCategory?: AdminCopyrightFactEvidence; unpublishedAtEnd1988?: AdminCopyrightFactEvidence };
export type AdminCopyrightContributorEvidence = { name: string; role: string; birthYear?: number; deathYear?: number };
export type AdminCopyrightReason =
  | "us_provider_public_domain_confirmed" | "us_provider_restricted" | "us_provider_rights_missing" | "us_provider_rights_conflict" | "us_header_rights_conflict" | "us_header_rights_unknown"
  | "uk_ordinary_literary_term_expired" | "uk_ordinary_literary_term_active" | "uk_evaluation_date_invalid" | "uk_work_category_unsupported" | "uk_work_category_evidence_missing" | "uk_joint_authorship_unsupported" | "uk_anonymous_authorship_unsupported" | "uk_pseudonymous_authorship_unsupported" | "uk_authorship_unsupported" | "uk_authorship_evidence_missing" | "uk_author_identity_missing" | "uk_author_death_unknown" | "uk_author_evidence_missing" | "uk_publication_evidence_missing" | "uk_publication_posthumous_unsupported" | "uk_translation_present" | "uk_translation_unknown" | "uk_translation_evidence_missing" | "uk_additional_contribution_present" | "uk_additional_contribution_unknown" | "uk_additional_contribution_evidence_missing" | "uk_special_category_unsupported" | "uk_special_category_evidence_missing" | "uk_unpublished_history_unsupported" | "uk_unpublished_history_evidence_missing" | "uk_author_death_invalid" | "uk_author_death_future" | "uk_publication_year_invalid" | "uk_publication_year_future" | "overall_eligible" | "overall_blocked";
export type AdminCopyrightJurisdiction = { status: "eligible" | "ineligible" | "indeterminate"; reason: AdminCopyrightReason };
export type AdminSourceEligibilityEffectiveUK = { workCategory: string; workCategoryReferences: AdminCopyrightEvidenceReference[]; authorship: string; authorshipReferences: AdminCopyrightEvidenceReference[]; authorName: string; authorDeathYear: number; authorReferences: AdminCopyrightEvidenceReference[]; firstPublicationYear: number; firstPublicationReferences: AdminCopyrightEvidenceReference[]; translation: AdminCopyrightFactEvidence; additionalTextualContribution: AdminCopyrightFactEvidence; specialCategory: AdminCopyrightFactEvidence; unpublishedAtEnd1988: AdminCopyrightFactEvidence };
export type AdminSourceEligibility = { policyVersion: "panda-pages-copyright-v1"; evaluationDate: string; evaluatedAt: string; us: AdminCopyrightJurisdiction; uk: AdminCopyrightJurisdiction; overall: "eligible" | "blocked"; overallReason: AdminCopyrightReason; opdsRights: "public_domain" | "restricted" | "unknown"; rdfRights: "public_domain" | "restricted" | "unknown"; headerRights: "public_domain" | "restricted" | "no_classification" | "conflicting"; providerTitle: string; contributors: AdminCopyrightContributorEvidence[]; rdfDigest: string; effectiveUkEvidence: AdminSourceEligibilityEffectiveUK; assessmentHash?: string };
export type AdminSourceAcquisitionRepresentation = {
  label: string | null;
  mediaType: string;
  providerUrl: string;
  sizeBytes: number | null;
};
export type AdminSourceAcquisitionSummary = {
  id: string;
  provider: AdminSourceProviderID;
  externalId: string;
  title: string;
  contributors: AdminSourceProviderContributor[];
  languages: string[];
  landingUrl: string;
  providerRights: string | null;
  selectedRepresentation: AdminSourceAcquisitionRepresentation;
  normalisationVersion: string;
  retrievedContentHash: string;
  normalisedContentHash: string;
  snapshotHash: string;
  createdAt: string;
  eligibility: AdminSourceEligibility | null;
  sourceQuality: AdminSourceQualityReview;
};
export type AdminSourceAcquisitionDetail = AdminSourceAcquisitionSummary & {
  sourceText: string;
};
export type AdminSourceAcquisitionPersistResponse = {
  outcome: "created" | "reused";
  acquisition: AdminSourceAcquisitionSummary;
};
export type AdminSourceAcquisitionListResponse = {
  items: AdminSourceAcquisitionSummary[];
};
export type AdminSourceQualityReviewUpdateRequest = {
  status: AdminSourceQualityStatus;
  note: string;
};

const adminStoryStatuses = new Set<AdminStoryStatus>(["draft_only", "published", "published_with_draft", "unpublished", "repair_required"]);
const adminEditionStatuses = new Set<AdminEditionStatus>(["empty", "draft_only", "published", "published_with_draft", "unpublished", "repair_required"]);
const adminSourceStatuses = new Set<AdminSourceStatus>(["missing", "ready", "repair_required"]);
const adminVersionHealthValues = new Set<AdminVersionHealth>(["ready", "repair_required", "unavailable"]);
const adminDraftOutcomes = new Set<AdminDraftOutcome>(["created_story", "created_version", "reused"]);
const adminEditionIngestOutcomes = new Set<AdminEditionIngestOutcome>(["created", "reused"]);
const adminReleaseOutcomes = new Set<AdminReleaseOutcome>(["created", "reused_current"]);
const adminSourceOutcomes = new Set<AdminSourceOutcome>(["created_source", "created_version", "reused"]);
const adminSourceProviderIDs = new Set<AdminSourceProviderID>(["project-gutenberg"]);
const adminSourceQualityStatuses = new Set<AdminSourceQualityStatus>(["pending", "approved", "rejected"]);
const adminCopyrightFactStates = new Set<AdminCopyrightFactState>(["none_confirmed", "present", "unknown"]);
const adminCopyrightJurisdictionStatuses = new Set<AdminCopyrightJurisdiction["status"]>(["eligible", "ineligible", "indeterminate"]);
const adminCopyrightReasons = new Set<AdminCopyrightReason>(["us_provider_public_domain_confirmed", "us_provider_restricted", "us_provider_rights_missing", "us_provider_rights_conflict", "us_header_rights_conflict", "us_header_rights_unknown", "uk_ordinary_literary_term_expired", "uk_ordinary_literary_term_active", "uk_evaluation_date_invalid", "uk_work_category_unsupported", "uk_work_category_evidence_missing", "uk_joint_authorship_unsupported", "uk_anonymous_authorship_unsupported", "uk_pseudonymous_authorship_unsupported", "uk_authorship_unsupported", "uk_authorship_evidence_missing", "uk_author_identity_missing", "uk_author_death_unknown", "uk_author_evidence_missing", "uk_publication_evidence_missing", "uk_publication_posthumous_unsupported", "uk_translation_present", "uk_translation_unknown", "uk_translation_evidence_missing", "uk_additional_contribution_present", "uk_additional_contribution_unknown", "uk_additional_contribution_evidence_missing", "uk_special_category_unsupported", "uk_special_category_evidence_missing", "uk_unpublished_history_unsupported", "uk_unpublished_history_evidence_missing", "uk_author_death_invalid", "uk_author_death_future", "uk_publication_year_invalid", "uk_publication_year_future", "overall_eligible", "overall_blocked"]);
const adminSourceAcquisitionOutcomes = new Set<AdminSourceAcquisitionPersistResponse["outcome"]>(["created", "reused"]);
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

function parseAdminSourceProviderID(value: unknown): AdminSourceProviderID {
  if (typeof value !== "string" || !adminSourceProviderIDs.has(value as AdminSourceProviderID)) {
    throw new Error("Invalid admin response");
  }
  return value as AdminSourceProviderID;
}

function parseAdminURL(value: unknown): string {
  const raw = requiredAdminString({ value }, "value");
  let parsed: URL;
  try {
    parsed = new URL(raw);
  } catch {
    throw new Error("Invalid admin response");
  }
  if (parsed.protocol !== "https:" || parsed.username || parsed.password) {
    throw new Error("Invalid admin response");
  }
  return raw;
}

function optionalAdminString(record: Record<string, unknown>, key: string): string | null {
  if (!(key in record) || record[key] === null) return null;
  return requiredAdminString(record, key);
}

function parseAdminBoundedSize(value: unknown): number | null {
  if (value === undefined || value === null) return null;
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0) throw new Error("Invalid admin response");
  return value;
}

function parseAdminSourceProviderContributors(value: unknown): AdminSourceProviderContributor[] {
  if (!Array.isArray(value)) throw new Error("Invalid admin response");
  return value.map((item) => {
    const record = adminRecord(item);
    return { name: requiredAdminString(record, "name"), role: requiredAdminString(record, "role") };
  });
}

function parseAdminLanguages(value: unknown): string[] {
  if (!Array.isArray(value)) throw new Error("Invalid admin response");
  return value.map((item) => {
    if (typeof item !== "string" || item.trim().length === 0) throw new Error("Invalid admin response");
    return item;
  });
}

function parseAdminSourceProviderRepresentation(value: unknown): AdminSourceProviderRepresentation {
  const record = adminRecord(value);
  const sizeBytes = parseAdminBoundedSize(record.sizeBytes);
  if (typeof record.label !== "string") throw new Error("Invalid admin response");
  const result: AdminSourceProviderRepresentation = {
    label: record.label,
    mediaType: requiredAdminString(record, "mediaType"),
    url: parseAdminURL(record.url),
  };
  if (sizeBytes !== null) result.sizeBytes = sizeBytes;
  return result;
}

export function parseAdminSourceProviderWork(value: unknown): AdminSourceProviderWork {
  const record = adminRecord(value);
  if (!Array.isArray(record.representations)) throw new Error("Invalid admin response");
  const providerRights = optionalAdminString(record, "providerRights");
  return {
    provider: parseAdminSourceProviderID(record.provider),
    externalId: requiredAdminString(record, "externalId"),
    title: requiredAdminString(record, "title"),
    contributors: parseAdminSourceProviderContributors(record.contributors),
    languages: parseAdminLanguages(record.languages),
    landingUrl: parseAdminURL(record.landingUrl),
    ...(providerRights === null ? {} : { providerRights }),
    representations: record.representations.map(parseAdminSourceProviderRepresentation),
  };
}

export function parseAdminSourceProviderSearchResponse(value: unknown): AdminSourceProviderSearchResponse {
  const record = adminRecord(value);
  if (!Array.isArray(record.results)) throw new Error("Invalid admin response");
  const provider = parseAdminSourceProviderID(record.provider);
  const results = record.results.map(parseAdminSourceProviderWork);
  if (results.some((work) => work.provider !== provider)) throw new Error("Invalid admin response");
  return { provider, results };
}

function parseAdminSourceQualityReview(value: unknown): AdminSourceQualityReview {
  const record = adminRecord(value);
  if (typeof record.status !== "string" || !adminSourceQualityStatuses.has(record.status as AdminSourceQualityStatus)) throw new Error("Invalid admin response");
  const status = record.status as AdminSourceQualityStatus;
  const note = optionalAdminString(record, "note");
  const reviewedAt = record.reviewedAt === undefined || record.reviewedAt === null ? null : parseAdminNullableTimestamp(record.reviewedAt);
  if ((status === "pending" && (note !== null || reviewedAt !== null)) || (status !== "pending" && (note === null || reviewedAt === null))) throw new Error("Invalid admin response");
  return { status, note, reviewedAt };
}

function parseAdminEvidenceReferences(value: unknown): AdminCopyrightEvidenceReference[] {
  if (!Array.isArray(value) || value.length > 8) throw new Error("Invalid admin response");
  return value.map((item) => {
    // A citation locator is display-only evidence metadata. It is deliberately
    // parsed as a small closed object rather than becoming a Reader locator or
    // a URL the browser/application will fetch.
    if (!isRecord(item) || !isJsonObject(item) || Object.keys(item).some((key) => !["source", "fact", "locator", "identifier", "digest"].includes(key))) throw new Error("Invalid admin response");
    const record = item;
    const locator = optionalAdminString(record, "locator");
    const identifier = optionalAdminString(record, "identifier");
    const digest = optionalAdminString(record, "digest");
    if (digest !== null) parseAdminSHA256(digest);
    return { source: requiredAdminString(record, "source"), fact: requiredAdminString(record, "fact"), ...(locator === null ? {} : { locator }), ...(identifier === null ? {} : { identifier }), ...(digest === null ? {} : { digest }) };
  });
}

function parseAdminFactEvidence(value: unknown): AdminCopyrightFactEvidence {
  const record = adminRecord(value);
  if (typeof record.state !== "string" || !adminCopyrightFactStates.has(record.state as AdminCopyrightFactState)) throw new Error("Invalid admin response");
  return { state: record.state as AdminCopyrightFactState, references: parseAdminEvidenceReferences(record.references) };
}

export function parseAdminEligibility(value: unknown): AdminSourceEligibility {
  const record = adminRecord(value);
  const reason = (item: unknown): AdminCopyrightReason => { if (typeof item !== "string" || !adminCopyrightReasons.has(item as AdminCopyrightReason)) throw new Error("Invalid admin response"); return item as AdminCopyrightReason; };
  const jurisdiction = (item: unknown): AdminCopyrightJurisdiction => { const itemRecord = adminRecord(item); if (typeof itemRecord.status !== "string" || !adminCopyrightJurisdictionStatuses.has(itemRecord.status as AdminCopyrightJurisdiction["status"])) throw new Error("Invalid admin response"); return { status: itemRecord.status as AdminCopyrightJurisdiction["status"], reason: reason(itemRecord.reason) }; };
  const evidenceRecord = adminRecord(record.effectiveUkEvidence);
  const workCategory = evidenceRecord.workCategory; const authorship = evidenceRecord.authorship; const authorName = evidenceRecord.authorName;
  if ((workCategory !== "ordinary_literary" && workCategory !== "unknown") || typeof authorship !== "string" || !["single_known", "joint", "anonymous", "pseudonymous", "unknown"].includes(authorship) || typeof authorName !== "string") throw new Error("Invalid admin response");
  const effectiveUkEvidence: AdminSourceEligibilityEffectiveUK = { workCategory, workCategoryReferences: parseAdminEvidenceReferences(evidenceRecord.workCategoryReferences), authorship, authorshipReferences: parseAdminEvidenceReferences(evidenceRecord.authorshipReferences), authorName, authorDeathYear: parseAdminInteger(evidenceRecord.authorDeathYear), authorReferences: parseAdminEvidenceReferences(evidenceRecord.authorReferences), firstPublicationYear: parseAdminInteger(evidenceRecord.firstPublicationYear), firstPublicationReferences: parseAdminEvidenceReferences(evidenceRecord.firstPublicationReferences), translation: parseAdminFactEvidence(evidenceRecord.translation), additionalTextualContribution: parseAdminFactEvidence(evidenceRecord.additionalTextualContribution), specialCategory: parseAdminFactEvidence(evidenceRecord.specialCategory), unpublishedAtEnd1988: parseAdminFactEvidence(evidenceRecord.unpublishedAtEnd1988) };
  if (!Array.isArray(record.contributors)) throw new Error("Invalid admin response");
  const contributors = record.contributors.map((item) => { const itemRecord = adminRecord(item); const birthYear = itemRecord.birthYear === undefined ? undefined : parseAdminInteger(itemRecord.birthYear); const deathYear = itemRecord.deathYear === undefined ? undefined : parseAdminInteger(itemRecord.deathYear); return { name: requiredAdminString(itemRecord, "name"), role: requiredAdminString(itemRecord, "role"), ...(birthYear === undefined ? {} : { birthYear }), ...(deathYear === undefined ? {} : { deathYear }) }; });
  const overall = record.overall; if (overall !== "eligible" && overall !== "blocked") throw new Error("Invalid admin response");
  const assessmentHash = optionalAdminString(record, "assessmentHash");
  if (record.policyVersion !== "panda-pages-copyright-v1" || (record.opdsRights !== "public_domain" && record.opdsRights !== "restricted" && record.opdsRights !== "unknown") || (record.rdfRights !== "public_domain" && record.rdfRights !== "restricted" && record.rdfRights !== "unknown") || (record.headerRights !== "public_domain" && record.headerRights !== "restricted" && record.headerRights !== "no_classification" && record.headerRights !== "conflicting")) throw new Error("Invalid admin response");
  const evaluatedAt = parseAdminNullableTimestamp(record.evaluatedAt); if (evaluatedAt === null) throw new Error("Invalid admin response");
  return { policyVersion: record.policyVersion, evaluationDate: requiredAdminString(record, "evaluationDate"), evaluatedAt, us: jurisdiction(record.us), uk: jurisdiction(record.uk), overall, overallReason: reason(record.overallReason), opdsRights: record.opdsRights, rdfRights: record.rdfRights, headerRights: record.headerRights, providerTitle: requiredAdminString(record, "providerTitle"), contributors, rdfDigest: parseAdminSHA256(record.rdfDigest), effectiveUkEvidence, ...(assessmentHash === null ? {} : { assessmentHash: parseAdminSHA256(assessmentHash) }) };
}

function parseAdminInteger(value: unknown): number { if (typeof value !== "number" || !Number.isSafeInteger(value)) throw new Error("Invalid admin response"); return value; }

function parseAdminSourceAcquisitionRepresentation(value: unknown): AdminSourceAcquisitionRepresentation {
  const record = adminRecord(value);
  return {
    label: optionalAdminString(record, "label"),
    mediaType: requiredAdminString(record, "mediaType"),
    providerUrl: parseAdminURL(record.providerUrl),
    sizeBytes: parseAdminBoundedSize(record.sizeBytes),
  };
}

function parseAdminSHA256(value: unknown): string {
  if (typeof value !== "string" || !/^[a-f0-9]{64}$/.test(value)) throw new Error("Invalid admin response");
  return value;
}

export function parseAdminSourceAcquisitionSummary(value: unknown): AdminSourceAcquisitionSummary {
  const record = adminRecord(value);
  if ("sourceText" in record) throw new Error("Invalid admin response");
  const createdAt = parseAdminNullableTimestamp(record.createdAt);
  if (createdAt === null) throw new Error("Invalid admin response");
  return {
    id: parseAdminUUID(record.id),
    provider: parseAdminSourceProviderID(record.provider),
    externalId: requiredAdminString(record, "externalId"),
    title: requiredAdminString(record, "title"),
    contributors: parseAdminSourceProviderContributors(record.contributors),
    languages: parseAdminLanguages(record.languages),
    landingUrl: parseAdminURL(record.landingUrl),
    providerRights: optionalAdminString(record, "providerRights"),
    selectedRepresentation: parseAdminSourceAcquisitionRepresentation(record.selectedRepresentation),
    normalisationVersion: requiredAdminString(record, "normalisationVersion"),
    retrievedContentHash: parseAdminSHA256(record.retrievedContentHash),
    normalisedContentHash: parseAdminSHA256(record.normalisedContentHash),
    snapshotHash: parseAdminSHA256(record.snapshotHash),
    createdAt,
    eligibility: record.eligibility === undefined || record.eligibility === null ? null : parseAdminEligibility(record.eligibility),
    sourceQuality: parseAdminSourceQualityReview(record.sourceQuality),
  };
}

export function parseAdminSourceAcquisitionDetail(value: unknown): AdminSourceAcquisitionDetail {
  const record = adminRecord(value, ["sourcetext"]);
  if (typeof record.sourceText !== "string" || record.sourceText.length === 0) throw new Error("Invalid admin response");
  const summaryRecord = { ...record };
  delete summaryRecord.sourceText;
  return { ...parseAdminSourceAcquisitionSummary(summaryRecord), sourceText: record.sourceText };
}

export function parseAdminSourceAcquisitionListResponse(value: unknown): AdminSourceAcquisitionListResponse {
  const record = adminRecord(value);
  if (!Array.isArray(record.items)) throw new Error("Invalid admin response");
  const items = record.items.map(parseAdminSourceAcquisitionSummary);
  if (new Set(items.map((item) => item.id)).size !== items.length) throw new Error("Invalid admin response");
  return { items };
}

export function parseAdminSourceAcquisitionPersistResponse(value: unknown): AdminSourceAcquisitionPersistResponse {
  const record = adminRecord(value);
  if (typeof record.outcome !== "string" || !adminSourceAcquisitionOutcomes.has(record.outcome as AdminSourceAcquisitionPersistResponse["outcome"])) {
    throw new Error("Invalid admin response");
  }
  return { outcome: record.outcome as AdminSourceAcquisitionPersistResponse["outcome"], acquisition: parseAdminSourceAcquisitionSummary(record.acquisition) };
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

export async function adminSearchSourceProvider(
  provider: AdminSourceProviderID,
  query: string,
  signal?: AbortSignal,
): Promise<AdminSourceProviderSearchResponse> {
  const params = new URLSearchParams({ q: query });
  return parseAdminSourceProviderSearchResponse(await request<unknown>(
    `/api/v1/admin/source-providers/${encodeURIComponent(provider)}/search?${params}`,
    { signal },
  ));
}

export async function adminGetSourceProviderWork(
  provider: AdminSourceProviderID,
  externalID: string,
  signal?: AbortSignal,
): Promise<AdminSourceProviderWork> {
  return parseAdminSourceProviderWork(await request<unknown>(
    `/api/v1/admin/source-providers/${encodeURIComponent(provider)}/works/${encodeURIComponent(externalID)}`,
    { signal },
  ));
}

export async function adminPersistSourceAcquisition(
  provider: AdminSourceProviderID,
  externalID: string,
  payload: AdminSourceEligibilityHumanEvidence,
): Promise<AdminSourceAcquisitionPersistResponse> {
  return parseAdminSourceAcquisitionPersistResponse(await request<unknown>(
    "/api/v1/admin/source-providers/" + encodeURIComponent(provider) + "/works/" + encodeURIComponent(externalID) + "/acquisitions",
    { method: "POST", body: JSON.stringify(payload) },
  ));
}

export async function adminCheckSourceEligibility(
  provider: AdminSourceProviderID,
  externalID: string,
  payload: AdminSourceEligibilityHumanEvidence,
  signal?: AbortSignal,
): Promise<AdminSourceEligibility> {
  return parseAdminEligibility(await request<unknown>(
    "/api/v1/admin/source-providers/" + encodeURIComponent(provider) + "/works/" + encodeURIComponent(externalID) + "/copyright-eligibility",
    { method: "POST", body: JSON.stringify(payload), signal },
  ));
}

export async function adminListSourceAcquisitions(signal?: AbortSignal): Promise<AdminSourceAcquisitionListResponse> {
  return parseAdminSourceAcquisitionListResponse(await request<unknown>("/api/v1/admin/source-acquisitions", { signal }));
}

export async function adminGetSourceAcquisition(id: string, signal?: AbortSignal): Promise<AdminSourceAcquisitionDetail> {
  return parseAdminSourceAcquisitionDetail(await request<unknown>(`/api/v1/admin/source-acquisitions/${encodeURIComponent(id)}`, { signal }));
}

export async function adminUpdateSourceAcquisitionSourceQualityReview(
  id: string,
  payload: AdminSourceQualityReviewUpdateRequest,
): Promise<AdminSourceAcquisitionSummary> {
  return parseAdminSourceAcquisitionSummary(await request<unknown>(
    "/api/v1/admin/source-acquisitions/" + encodeURIComponent(id) + "/source-quality-review",
    { method: "PUT", body: JSON.stringify(payload) },
  ));
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
