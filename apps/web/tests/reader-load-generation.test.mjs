import assert from 'node:assert/strict'
import test from 'node:test'
import { loadTypeScript } from './helpers/typescript-module.mjs'

async function loadGenerationModule() {
  return (
    await loadTypeScript(
      '../src/lib/reader-load-generation.ts',
      import.meta.url,
    )
  ).module
}

test('a newer Reader route aborts and invalidates the stale request', async () => {
  const module = await loadGenerationModule()
  const loads = module.createReaderLoadGeneration()
  const first = loads.begin()
  const second = loads.begin()

  assert.equal(first.signal.aborted, true)
  assert.equal(loads.isCurrent(first.generation), false)
  assert.equal(second.signal.aborted, false)
  assert.equal(loads.isCurrent(second.generation), true)
})

test('disposal invalidates the active Reader request', async () => {
  const module = await loadGenerationModule()
  const loads = module.createReaderLoadGeneration()
  const active = loads.begin()
  loads.cancel()

  assert.equal(active.signal.aborted, true)
  assert.equal(loads.isCurrent(active.generation), false)
})
