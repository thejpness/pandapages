import { readerEditionKeys, type ReaderEditionKey } from './api'

export const readerEditionOrder = readerEditionKeys

export const readerEditionAgeGuideNote =
  'Ages are only a guide — choose the reading level that feels right.'

type ReaderEditionPresentation = {
  label: string
  description: string
  approxAge: string
  stage: string
}

const presentations: Record<ReaderEditionKey, ReaderEditionPresentation> = {
  classic: {
    label: 'Classic',
    description: 'The fullest version of the story.',
    approxAge: '11+',
    stage: 'Fullest reading experience',
  },
  'confident-readers': {
    label: 'Confident Readers',
    description: 'A substantial edition for confident independent reading.',
    approxAge: '9–11',
    stage: 'Fluent readers',
  },
  'growing-readers': {
    label: 'Growing Readers',
    description: 'A supported edition with reduced narrative complexity.',
    approxAge: '7–9',
    stage: 'Independent reading',
  },
  'story-explorers': {
    label: 'Story Explorers',
    description: 'A shorter edition focused on the core story journey.',
    approxAge: '5–7',
    stage: 'Early readers',
  },
  'little-listeners': {
    label: 'Little Listeners',
    description: 'The most compact read-aloud edition with the clearest narrative line.',
    approxAge: '3–5',
    stage: 'Read together',
  },
}

export function readerEditionLabel(key: ReaderEditionKey): string {
  return presentations[key].label
}

export function readerEditionDescription(key: ReaderEditionKey): string {
  return presentations[key].description
}

export function readerEditionAgeStage(key: ReaderEditionKey): string {
  const { approxAge, stage } = presentations[key]
  return `Approx. ages ${approxAge} · ${stage}`
}

export function readerEditionOptionLabel(key: ReaderEditionKey): string {
  return `${readerEditionLabel(key)} — ${readerEditionAgeStage(key)}`
}
