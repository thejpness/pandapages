import assert from 'node:assert/strict'
import test from 'node:test'
import { loadTypeScript } from './helpers/typescript-module.mjs'

async function apiModule() {
  return loadTypeScript(
    '../src/lib/api.ts',
    import.meta.url,
    (source) => source.replaceAll('import.meta.env.VITE_API_BASE', "''"),
  )
}

function settings(overrides = {}) {
  return {
    child: {
      id: 'child-id',
      name: 'Mina',
      ageMonths: 72,
      interests: ['Stars'],
      sensitivities: ['Loud storms'],
      ...overrides.child,
    },
    prompt: {
      id: 'prompt-id',
      name: 'Default prompt v1',
      schemaVersion: 1,
      rules: { tone: 'cosy', readingTimeMinutes: 8 },
      ...overrides.prompt,
    },
  }
}

function responseError(status) {
  const error = new Error(`Request failed: ${status}`)
  error.status = status
  return error
}

test('settings parser accepts the current payload and copies mutable arrays', async () => {
  const { module: api } = await apiModule()
  const input = settings()
  const parsed = api.parseSettingsResponse(input)

  assert.deepEqual(parsed, input)
  assert.notEqual(parsed.child.interests, input.child.interests)
  assert.notEqual(parsed.child.sensitivities, input.child.sensitivities)
})

test('settings parser accepts the repository-native empty success shape', async () => {
  const { module: api } = await apiModule()
  const empty = {
    child: {
      name: '',
      ageMonths: 0,
      interests: null,
      sensitivities: null,
    },
    prompt: {
      name: '',
      schemaVersion: 0,
      rules: null,
    },
  }

  assert.deepEqual(api.parseSettingsResponse(empty), {
    child: {
      id: undefined,
      name: '',
      ageMonths: 0,
      interests: [],
      sensitivities: [],
    },
    prompt: {
      id: undefined,
      name: '',
      schemaVersion: 0,
      rules: {},
    },
  })
})

test('settings parser accepts independently absent child and prompt records', async () => {
  const { module: api } = await apiModule()
  const absentChild = {
    name: '',
    ageMonths: 0,
    interests: null,
    sensitivities: null,
  }
  const absentPrompt = {
    name: '',
    schemaVersion: 0,
    rules: null,
  }

  assert.deepEqual(
    api.parseSettingsResponse({
      child: absentChild,
      prompt: settings().prompt,
    }),
    {
      child: {
        id: undefined,
        name: '',
        ageMonths: 0,
        interests: [],
        sensitivities: [],
      },
      prompt: settings().prompt,
    },
  )
  assert.deepEqual(
    api.parseSettingsResponse({
      child: settings().child,
      prompt: absentPrompt,
    }),
    {
      child: settings().child,
      prompt: { id: undefined, name: '', schemaVersion: 0, rules: {} },
    },
  )
})

test('settings parser rejects malformed success bodies instead of inventing defaults', async () => {
  const { module: api } = await apiModule()
  const malformed = [
    null,
    {},
    { child: {}, prompt: {} },
    settings({ child: { interests: null } }),
    settings({ child: { sensitivities: ['valid', 3] } }),
    settings({ child: { ageMonths: -1 } }),
    settings({ child: { ageMonths: 1.5 } }),
    settings({ child: { id: null } }),
    settings({ prompt: { schemaVersion: -1 } }),
    settings({ prompt: { rules: [] } }),
    {
      child: {
        name: 'Mina',
        ageMonths: 72,
        interests: [],
        sensitivities: [],
      },
      prompt: settings().prompt,
    },
  ]

  for (const value of malformed) {
    assert.throws(
      () => api.parseSettingsResponse(value),
      (error) => api.isInvalidSettingsResponseError(error),
    )
  }
})

test('settings failure classification keeps auth, validation and availability distinct', async () => {
  const { module: api } = await apiModule()

  assert.equal(api.classifySettingsRequestFailure(responseError(401), 'load'), 'unauthorized')
  assert.equal(api.classifySettingsRequestFailure(responseError(401), 'save'), 'unauthorized')
  assert.equal(api.classifySettingsRequestFailure(responseError(400), 'save'), 'validation')
  assert.equal(api.classifySettingsRequestFailure(responseError(413), 'save'), 'validation')
  assert.equal(api.classifySettingsRequestFailure(responseError(400), 'load'), 'unavailable')
  assert.equal(api.classifySettingsRequestFailure(responseError(503), 'load'), 'unavailable')
  assert.equal(api.classifySettingsRequestFailure(responseError(500), 'save'), 'unavailable')
  assert.equal(api.classifySettingsRequestFailure(new TypeError('network failed'), 'load'), 'unavailable')
  assert.equal(api.classifySettingsRequestFailure(new Error('Invalid settings response'), 'save'), 'unavailable')
})

test('settings requests use the fixed credentialed endpoint and validate both success bodies', async (t) => {
  const originalFetch = globalThis.fetch
  t.after(() => {
    globalThis.fetch = originalFetch
  })

  const { module: api } = await apiModule()
  const requests = []
  globalThis.fetch = async (url, init) => {
    requests.push({ url, init })
    return new Response(JSON.stringify(settings()), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  }

  assert.deepEqual(await api.getSettings(), settings())
  assert.deepEqual(await api.saveSettings(settings()), settings())
  assert.deepEqual(
    requests.map(({ url, init }) => [url, init.method ?? 'GET', init.credentials]),
    [
      ['/api/v1/settings', 'GET', 'include'],
      ['/api/v1/settings', 'PUT', 'include'],
    ],
  )

  globalThis.fetch = async () =>
    new Response('{', {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  await assert.rejects(api.getSettings, (error) => api.isInvalidSettingsResponseError(error))
  await assert.rejects(
    () => api.saveSettings(settings()),
    (error) => api.isInvalidSettingsResponseError(error),
  )
})
