import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const source = await readFile(new URL('../src/lib/supabase-auth.ts', import.meta.url), 'utf8')
const router = await readFile(new URL('../src/router.ts', import.meta.url), 'utf8')
const dockerfile = await readFile(new URL('../Dockerfile', import.meta.url), 'utf8')

test('official client owns PKCE persistence and callback exchange', () => {
  assert.match(source, /createClient/)
  assert.match(source, /flowType: 'pkce'/)
  assert.match(source, /persistSession: true/)
  assert.match(source, /autoRefreshToken: true/)
  assert.match(source, /detectSessionInUrl: false/)
  assert.match(source, /exchangeCodeForSession\(code\)/)
  assert.doesNotMatch(source, /localStorage\.|sessionStorage\./)
  assert.doesNotMatch(source, /setItem\(|getItem\(/)
})

test('bearer client is limited to the new auth family and omits cookies', () => {
  assert.match(source, /identityRequest\('\/api\/auth\/onboard', 'POST'/)
  assert.match(source, /identityRequest\('\/api\/auth\/me', 'GET'/)
  assert.match(source, /Authorization: `Bearer \$\{accessToken\}`/)
  assert.match(source, /credentials: 'omit'/)
  assert.doesNotMatch(source, /\/api\/v1\//)
  assert.doesNotMatch(source, /pp_session|pp_unlocked|pp_aid/)
})

test('identity routes and protected routes use explicit account context', () => {
  for (const path of ['/account/login', '/auth/callback', '/account']) {
    assert.match(router, new RegExp(`path: ["']${path.replace('/', '\\/')}["']`))
  }
  assert.doesNotMatch(router, /unlock|auth\/status|auth\/logout|requiresUnlock/)
  assert.match(router, /path: ["']\/library["'][\s\S]*requiresAccount/)
  assert.match(router, /path: ["']\/journey["'][\s\S]*requiresAccount/)
  assert.match(router, /currentAccountContext\(\)/)
})

test('CSP and browser configuration share one exact project origin', () => {
  assert.match(source, /parsed\.origin !== supabaseURL/)
  assert.match(source, /destination\.origin !== origin/)
  assert.match(source, /destination\.pathname !== '\/auth\/v1\/authorize'/)
  assert.ok(source.includes('/^sb_publishable_[A-Za-z0-9_-]{20,}$/'))
  assert.ok(dockerfile.includes('^sb_publishable_[A-Za-z0-9_-]{20,}$'))
})
