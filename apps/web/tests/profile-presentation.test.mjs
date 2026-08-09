import assert from 'node:assert/strict'
import test from 'node:test'

import { loadTypeScript } from './helpers/typescript-module.mjs'

async function presentationModule() {
  return loadTypeScript('../src/lib/profile-presentation.ts', import.meta.url)
}

test('profile presentation v1 is stable for the same immutable profile ID', async () => {
  const { module } = await presentationModule()
  const profileID = 'b9a819be-102a-4e47-a826-1cf6931b0ac9'

  assert.deepEqual(module.profilePresentationV1(profileID), module.profilePresentationV1(profileID))
  assert.equal(module.profilePresentationV1.length, 1)
})

test('representative profile IDs retain their deterministic v1 slots', async () => {
  const { module } = await presentationModule()

  assert.equal(module.profilePresentationV1('b9a819be-102a-4e47-a826-1cf6931b0ac9').key, 'coral')
  assert.equal(module.profilePresentationV1('4b0b8a2d-9758-41f6-afb7-33697ac7e4e0').key, 'moss')
  assert.equal(module.profilePresentationV1('reader-profile-42').key, 'coral')
})

test('presentation resolution depends only on its profile ID argument', async () => {
  const { module, source } = await presentationModule()
  const profileID = 'b9a819be-102a-4e47-a826-1cf6931b0ac9'

  assert.equal(module.profilePresentationV1.length, 1)
  assert.deepEqual(module.profilePresentationV1(profileID), module.profilePresentationV1(profileID))
  assert.doesNotMatch(source, /\bname\b/ui)
})

test('the frozen v1 registry has a permanent count and ordering', async () => {
  const { module } = await presentationModule()

  assert.equal(module.PROFILE_PRESENTATION_V1_SLOT_COUNT, 8)
  assert.equal(module.PROFILE_PRESENTATION_V1.length, module.PROFILE_PRESENTATION_V1_SLOT_COUNT)
  assert.deepEqual([...module.PROFILE_PRESENTATION_V1_SLOT_KEYS], [
    'marigold', 'moss', 'river', 'coral', 'plum', 'midnight', 'sky', 'cedar',
  ])
  assert.ok(Object.isFrozen(module.PROFILE_PRESENTATION_V1))
  assert.ok(module.PROFILE_PRESENTATION_V1.every((presentation) => Object.isFrozen(presentation)))
})

test('resolver returns only immutable curated v1 presentations without mutation', async () => {
  const { module, source } = await presentationModule()
  const profileID = 'reader-profile-42'
  const resolved = module.profilePresentationV1(profileID)

  assert.ok(module.PROFILE_PRESENTATION_V1.includes(resolved))
  assert.equal(Reflect.set(resolved, 'key', 'changed'), false)
  assert.deepEqual(module.profilePresentationV1(profileID), resolved)
  assert.doesNotMatch(source, /Math\.random/)
})
