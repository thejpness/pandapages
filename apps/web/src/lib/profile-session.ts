import {
  clearSelectedReaderProfile,
  selectReaderProfile,
  selectedReaderProfileID,
} from "./reader-profile-selection";
import { enterChildMode, isChildModeFor, leaveChildMode } from "./reader-mode";
import type { APIError } from "./api";

// A reader session becomes active only after an explicit successful entry.
// Callers must perform PIN verification before calling this function.
export function enterReaderProfileSession(profileID: string): boolean {
  if (!selectReaderProfile(profileID)) return false;
  return enterChildMode(profileID);
}

// Delete and profile-invalid paths must never transfer session authority to a
// different reader. Omit profileID only when the currently persisted profile
// itself has already been proven invalid.
export function invalidateReaderProfileSession(profileID?: string): void {
  if (profileID === undefined || selectedReaderProfileID() === profileID) {
    clearSelectedReaderProfile();
  }
  if (profileID === undefined || isChildModeFor(profileID)) {
    leaveChildMode();
  }
}

const invalidProfileErrorCodes = new Set([
  "profile_required",
  "invalid_profile",
  "profile_forbidden",
]);

export function isProfileSessionInvalidError(error: unknown): boolean {
  if (!(error instanceof Error)) return false;
  const code = (error as APIError).code;
  return typeof code === "string" && invalidProfileErrorCodes.has(code);
}
