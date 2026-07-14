import type { AuthState } from './auth-state'

const fallbackPath = '/library'

const fixedDestinations = new Set([
  '/library',
  '/journey',
  '/admin',
  '/admin/upload',
  '/admin/ai',
])

const storySlugPattern = /^[a-z0-9]+(?:-[a-z0-9]+)*$/

export function safeNextPath(value: unknown): string {
  if (typeof value !== 'string' || value.length === 0 || value.trim() !== value) {
    return fallbackPath
  }

  if (value.includes('\\') || value.includes('?') || value.includes('#')) {
    return fallbackPath
  }

  if (fixedDestinations.has(value)) return value

  if (!value.startsWith('/read/') || value.startsWith('//')) return fallbackPath

  const encodedSlug = value.slice('/read/'.length)
  if (!encodedSlug || encodedSlug.includes('/')) return fallbackPath

  try {
    const slug = decodeURIComponent(encodedSlug)
    if (!storySlugPattern.test(slug)) return fallbackPath
    if (encodeURIComponent(slug) !== encodedSlug) return fallbackPath
    return `/read/${encodedSlug}`
  } catch {
    return fallbackPath
  }
}

export type AuthNavigationDecision =
  | true
  | { path: string; query?: { next: string } }

export function protectedRouteDecision(
  state: AuthState,
  requestedPath: unknown
): AuthNavigationDecision {
  const next = safeNextPath(requestedPath)
  if (state === 'unlocked') return true
  if (state === 'locked') return { path: '/unlock', query: { next } }
  return { path: '/session-unavailable', query: { next } }
}

export function unlockRouteDecision(
  state: AuthState,
  requestedPath: unknown
): AuthNavigationDecision {
  const next = safeNextPath(requestedPath)
  if (state === 'unlocked') return { path: next }
  if (state === 'locked') return true
  return { path: '/session-unavailable', query: { next } }
}
