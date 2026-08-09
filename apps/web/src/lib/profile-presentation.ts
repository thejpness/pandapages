export type ProfilePresentationPattern =
  | 'dots'
  | 'arches'
  | 'rays'
  | 'checks'
  | 'rings'
  | 'steps'
  | 'leaves'
  | 'waves'

export type ProfilePresentationV1Key =
  | 'marigold'
  | 'moss'
  | 'river'
  | 'coral'
  | 'plum'
  | 'midnight'
  | 'sky'
  | 'cedar'

export type ProfilePresentation = Readonly<{
  version: 1
  key: ProfilePresentationV1Key
  pattern: ProfilePresentationPattern
  surface: string
  accent: string
  ink: string
}>

/**
 * Compatibility contract: these eight v1 slots are permanent. Do not reorder,
 * remove, or add slots. A future visual system must introduce a new version
 * rather than remapping existing profile IDs.
 */
export const PROFILE_PRESENTATION_V1_SLOT_COUNT = 8 as const

const registry = [
  { version: 1, key: 'marigold', pattern: 'dots', surface: '#F1D79B', accent: '#9A5B00', ink: '#17130D' },
  { version: 1, key: 'moss', pattern: 'arches', surface: '#D7E4CF', accent: '#35654C', ink: '#101712' },
  { version: 1, key: 'river', pattern: 'rays', surface: '#D5E4EA', accent: '#23627A', ink: '#0C171B' },
  { version: 1, key: 'coral', pattern: 'checks', surface: '#F0D5C7', accent: '#9A4936', ink: '#21110D' },
  { version: 1, key: 'plum', pattern: 'rings', surface: '#E0D8E8', accent: '#624C7B', ink: '#1B1420' },
  { version: 1, key: 'midnight', pattern: 'steps', surface: '#282B33', accent: '#E9C866', ink: '#FFFDF6' },
  { version: 1, key: 'sky', pattern: 'leaves', surface: '#D9E6F5', accent: '#3D6198', ink: '#101925' },
  { version: 1, key: 'cedar', pattern: 'waves', surface: '#E4D7C5', accent: '#755238', ink: '#1A120D' },
] as const satisfies readonly ProfilePresentation[]

export const PROFILE_PRESENTATION_V1: readonly ProfilePresentation[] = Object.freeze(
  registry.map((presentation) => Object.freeze({ ...presentation })),
)

export const PROFILE_PRESENTATION_V1_SLOT_KEYS: readonly ProfilePresentationV1Key[] =
  Object.freeze(PROFILE_PRESENTATION_V1.map((presentation) => presentation.key))

if (PROFILE_PRESENTATION_V1.length !== PROFILE_PRESENTATION_V1_SLOT_COUNT) {
  throw new Error('Profile presentation v1 registry must retain its fixed slot count.')
}

function stableProfileIDHashV1(profileID: string): number {
  let hash = 0x811c9dc5
  for (let index = 0; index < profileID.length; index += 1) {
    hash ^= profileID.charCodeAt(index)
    hash = Math.imul(hash, 0x01000193)
  }
  return hash >>> 0
}

/**
 * Resolves immutable profile IDs into the frozen v1 visual registry only.
 */
export function profilePresentationV1(profileID: string): ProfilePresentation {
  const slot = stableProfileIDHashV1(profileID) % PROFILE_PRESENTATION_V1_SLOT_COUNT
  return PROFILE_PRESENTATION_V1[slot] ?? PROFILE_PRESENTATION_V1[0]
}
