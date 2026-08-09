import { readerEditionKeys, type ReaderEditionKey } from './api'

export const readerEditionOrder = readerEditionKeys

const labels: Record<ReaderEditionKey, string> = {
  classic: 'Classic',
  'confident-readers': 'Confident Readers',
  'growing-readers': 'Growing Readers',
  'story-explorers': 'Story Explorers',
  'little-listeners': 'Little Listeners',
}

const descriptions: Record<ReaderEditionKey, string> = {
  classic: 'The fullest version of the story.',
  'confident-readers': 'A substantial edition for confident independent reading.',
  'growing-readers': 'A supported edition with reduced narrative complexity.',
  'story-explorers': 'A shorter edition focused on the core story journey.',
  'little-listeners': 'The most compact read-aloud edition with the clearest narrative line.',
}

export function readerEditionLabel(key: ReaderEditionKey): string {
  return labels[key]
}

export function readerEditionDescription(key: ReaderEditionKey): string {
  return descriptions[key]
}
