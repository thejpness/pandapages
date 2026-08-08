import { computed, readonly, ref } from "vue";

// Reader mode is deliberately memory-only UX state. It is not an
// authorization boundary and is never persisted with a PIN or a durable
// "verified" marker. A refresh therefore returns to parent mode and asks for
// a protected profile PIN again before entering child mode.
const childProfileID = ref<string | null>(null);

export const readerMode = computed<"parent" | "child">(() =>
  childProfileID.value === null ? "parent" : "child",
);
export const activeChildProfileID = readonly(childProfileID);

export function enterChildMode(profileID: string): boolean {
  if (!profileID || profileID.trim() !== profileID) return false;
  childProfileID.value = profileID;
  return true;
}

export function leaveChildMode(): void {
  childProfileID.value = null;
}

export function isChildMode(): boolean {
  return childProfileID.value !== null;
}

export function isChildModeFor(profileID: string): boolean {
  return childProfileID.value === profileID;
}
