import assert from 'node:assert/strict'
import test from 'node:test'

import { loadTypeScript } from './helpers/typescript-module.mjs'

const profile = {
  id: '123e4567-e89b-42d3-a456-426614174300',
  name: 'Mina',
  pin_enabled: false,
  reading_level: 'classic',
}

async function apiModule() {
  return loadTypeScript(
    '../src/lib/api.ts',
    import.meta.url,
    (source) => source.replaceAll('import.meta.env.VITE_API_BASE', ),
  )
}

test('profile management stays account scoped and sends bearer plus account context', async () => {
  const { module: api } = await apiModule()
  const requests = []
  const originalFetch = globalThis.fetch
  globalThis.fetch = async (input, init) => {
    const headers = new Headers(init.headers)
    requests.push({
      path: String(input),
      method: init.method ?? 'GET',
      authorization: headers.get('authorization'),
      account: headers.get('x-pp-account-id'),
      profile: headers.get('x-pp-profile-id'),
      body: init.body,
    })
    const method = init.method ?? 'GET'
    const body = method === 'GET' ? { profiles: [profile] } : profile
    if (method === 'DELETE') return new Response(null, { status: 204 })
    return new Response(JSON.stringify(body), {
      headers: { 'content-type': 'application/json' },
    })
  }
  try {
    await api.listReaderProfiles()
    await api.createReaderProfile('Mina', 'little-listeners')
    await api.renameReaderProfile(profile.id, 'Mina Panda', 'classic')
    await api.deleteReaderProfile(profile.id)
  } finally {
    globalThis.fetch = originalFetch
  }

  assert.equal(requests.length, 4)
  for (const request of requests) {
    assert.equal(request.authorization, 'Bearer test-access-token')
    assert.equal(request.account, '11111111-1111-4111-8111-111111111111')
    assert.equal(request.profile, null)
  }
  assert.equal(requests[0].path, '/api/v1/profiles')
  assert.equal(requests[1].method, 'POST')
  assert.deepEqual(JSON.parse(requests[1].body), {
    name: 'Mina',
    readingLevel: 'little-listeners',
  })
  assert.equal(requests[2].method, 'PATCH')
  assert.deepEqual(JSON.parse(requests[2].body), {
    name: 'Mina Panda',
    readingLevel: 'classic',
  })
  assert.equal(requests[3].method, 'DELETE')
})

test('profile PIN operations remain account scoped and never require a profile header', async () => {
  const { module: api } = await apiModule()
  const requests = []
  const originalFetch = globalThis.fetch
  globalThis.fetch = async (input, init) => {
    const headers = new Headers(init.headers)
    requests.push({ path: String(input), method: init.method, headers, body: init.body })
    const verified = init.method === 'POST'
    const enabled = init.method !== 'DELETE'
    return new Response(JSON.stringify(verified ? { verified: true } : { pin_enabled: enabled }), {
      headers: { 'content-type': 'application/json' },
    })
  }
  try {
    await api.setReaderProfilePIN(profile.id, '1234')
    await api.verifyReaderProfilePIN(profile.id, '1234')
    await api.removeReaderProfilePIN(profile.id)
  } finally {
    globalThis.fetch = originalFetch
  }
  assert.equal(requests.length, 3)
  for (const request of requests) {
    assert.equal(request.headers.get('authorization'), 'Bearer test-access-token')
    assert.equal(request.headers.get('x-pp-account-id'), '11111111-1111-4111-8111-111111111111')
    assert.equal(request.headers.get('x-pp-profile-id'), null)
  }
  assert.equal(requests[0].method, 'PUT')
  assert.equal(requests[1].method, 'POST')
  assert.equal(requests[2].method, 'DELETE')
})

test('future profile-scoped calls opt in to the profile header explicitly', async () => {
  const { module: api } = await apiModule()
  const originalFetch = globalThis.fetch
  let headers
  globalThis.fetch = async (_input, init) => {
    headers = new Headers(init.headers)
    return new Response(JSON.stringify({ ok: true }), {
      headers: { 'content-type': 'application/json' },
    })
  }
  try {
    await api.profileScopedRequest('/api/v1/future-reader-endpoint', profile.id)
  } finally {
    globalThis.fetch = originalFetch
  }
  assert.equal(headers.get('x-pp-profile-id'), profile.id)
})
