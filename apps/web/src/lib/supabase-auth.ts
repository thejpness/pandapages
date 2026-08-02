import { createClient, type Session, type SupabaseClient } from '@supabase/supabase-js'

const supabaseURL = (import.meta.env.VITE_SUPABASE_URL || '').trim()
const publishableKey = (import.meta.env.VITE_SUPABASE_PUBLISHABLE_KEY || '').trim()
const apiBase = (import.meta.env.VITE_API_BASE || '').trim().replace(/\/$/, '')

let browserClient: SupabaseClient | undefined

export type IdentityMembership = Readonly<{
  accountId: string
  accountName: string
  role: 'owner' | 'adult'
}>

export type AuthenticatedIdentity = Readonly<{
  authenticated: true
  created?: boolean
  principal: Readonly<{
    id: string
    displayName: string
  }>
  memberships: readonly IdentityMembership[]
}>

function configuredOrigin(): string {
  if (!supabaseURL || !publishableKey) {
    throw new Error('Supabase identity foundation is not configured')
  }
  let parsed: URL
  try {
    parsed = new URL(supabaseURL)
  } catch {
    throw new Error('Supabase identity foundation is not configured')
  }
  if (
    parsed.protocol !== 'https:' ||
    parsed.username ||
    parsed.password ||
    parsed.pathname !== '/' ||
    parsed.search ||
    parsed.hash ||
    parsed.origin !== supabaseURL
  ) {
    throw new Error('Supabase identity foundation is not configured')
  }
  if (!/^sb_publishable_[A-Za-z0-9_-]{20,}$/.test(publishableKey)) {
    throw new Error('Supabase identity foundation is not configured')
  }
  return parsed.origin
}

export function supabaseClient(): SupabaseClient {
  if (browserClient) return browserClient
  browserClient = createClient(configuredOrigin(), publishableKey, {
    auth: {
      flowType: 'pkce',
      persistSession: true,
      autoRefreshToken: true,
      detectSessionInUrl: false,
    },
  })
  return browserClient
}

function callbackURL(): string {
  return new URL('/auth/callback', window.location.origin).toString()
}

export async function startSupabaseLogin(): Promise<void> {
  const origin = configuredOrigin()
  const { data, error } = await supabaseClient().auth.signInWithOAuth({
    provider: 'google',
    options: {
      redirectTo: callbackURL(),
      skipBrowserRedirect: true,
    },
  })
  if (error || !data.url) throw new Error('Could not start secure sign-in')

  let destination: URL
  try {
    destination = new URL(data.url)
  } catch {
    throw new Error('Could not start secure sign-in')
  }
  if (destination.origin !== origin || destination.pathname !== '/auth/v1/authorize') {
    throw new Error('Could not start secure sign-in')
  }
  window.location.assign(destination.toString())
}

export async function completeSupabaseCallback(search: string): Promise<Session> {
  const parameters = new URLSearchParams(search)
  const code = parameters.get('code')
  if (!code || code.length > 2048 || /\s/.test(code)) {
    throw new Error('The sign-in callback is invalid')
  }
  const { data, error } = await supabaseClient().auth.exchangeCodeForSession(code)
  if (error || !data.session?.access_token) {
    throw new Error('Could not complete secure sign-in')
  }
  return data.session
}

export async function restoreSupabaseSession(): Promise<Session | null> {
  const { data, error } = await supabaseClient().auth.getSession()
  if (error) throw new Error('Could not restore secure sign-in')
  return data.session
}

export async function signOutSupabaseSession(): Promise<void> {
  const { error } = await supabaseClient().auth.signOut()
  if (error) throw new Error('Could not sign out')
}

export async function onboardIdentity(accessToken: string): Promise<AuthenticatedIdentity> {
  return identityRequest('/api/auth/onboard', 'POST', accessToken)
}

export async function loadIdentity(accessToken: string): Promise<AuthenticatedIdentity> {
  return identityRequest('/api/auth/me', 'GET', accessToken)
}

async function identityRequest(path: string, method: 'GET' | 'POST', accessToken: string): Promise<AuthenticatedIdentity> {
  if (!accessToken || accessToken.length > 16 * 1024 || /\s/.test(accessToken)) {
    throw new Error('Secure sign-in is unavailable')
  }
  const response = await fetch(`${apiBase}${path}`, {
    method,
    headers: { Authorization: `Bearer ${accessToken}` },
    credentials: 'omit',
    cache: 'no-store',
    redirect: 'error',
  })
  const body: unknown = await response.json().catch(() => undefined)
  if (!response.ok) {
    throw new Error(identityErrorMessage(response.status, body))
  }
  return parseIdentity(body)
}

function identityErrorMessage(status: number, body: unknown): string {
  const code = isRecord(body) && typeof body.error === 'string' ? body.error : ''
  if (status === 401 || code === 'invalid_bearer' || code === 'bearer_required') {
    return 'Secure sign-in has expired'
  }
  if (code === 'onboarding_required') return 'Panda Pages setup is required'
  return 'Panda Pages identity is temporarily unavailable'
}

function parseIdentity(value: unknown): AuthenticatedIdentity {
  if (!isRecord(value) || value.authenticated !== true || !isRecord(value.principal) || !Array.isArray(value.memberships)) {
    throw new Error('Panda Pages returned an invalid identity response')
  }
  const id = requiredString(value.principal.id, 128)
  const displayName = requiredString(value.principal.displayName, 120)
  if (value.created !== undefined && typeof value.created !== 'boolean') {
    throw new Error('Panda Pages returned an invalid identity response')
  }
  const memberships = value.memberships.map((membership): IdentityMembership => {
    if (!isRecord(membership) || membership.role !== 'owner' && membership.role !== 'adult') {
      throw new Error('Panda Pages returned an invalid identity response')
    }
    return Object.freeze({
      accountId: requiredString(membership.accountId, 128),
      accountName: requiredString(membership.accountName, 120),
      role: membership.role,
    })
  })
  if (memberships.length === 0) {
    throw new Error('Panda Pages returned an invalid identity response')
  }
  return Object.freeze({
    authenticated: true,
    ...(value.created === undefined ? {} : { created: value.created }),
    principal: Object.freeze({ id, displayName }),
    memberships: Object.freeze(memberships),
  })
}

function requiredString(value: unknown, maximum: number): string {
  if (typeof value !== 'string' || value.length === 0 || value.length > maximum || value.trim() !== value) {
    throw new Error('Panda Pages returned an invalid identity response')
  }
  return value
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
