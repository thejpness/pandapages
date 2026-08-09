import { currentAccountContext, type AccountContext } from "./account-context";
import { listReaderProfiles, type ReaderProfile } from "./api";
import {
  resolvePersistedReaderProfileSelection,
  selectedReaderProfileID,
} from "./reader-profile-selection";
import { invalidateReaderProfileSession } from "./profile-session";

export type ReaderProfileContext = Readonly<{
  account: AccountContext;
  profile: ReaderProfile;
}>;

export class ProfileContextError extends Error {
  kind: "profile_selection_required" | "unavailable";

  constructor(kind: "profile_selection_required" | "unavailable") {
    super(kind);
    this.kind = kind;
  }
}

export async function currentReaderProfileContext(): Promise<ReaderProfileContext> {
  const account = await currentAccountContext();
  const persistedID = selectedReaderProfileID();
  let profiles: readonly ReaderProfile[];
  try {
    profiles = await listReaderProfiles(account);
  } catch {
    throw new ProfileContextError("unavailable");
  }
  const selection = resolvePersistedReaderProfileSelection(
    persistedID,
    profiles,
  );
  if (selection === null) {
    invalidateReaderProfileSession(persistedID ?? undefined);
    throw new ProfileContextError("profile_selection_required");
  }
  const profile = profiles.find((candidate) => candidate.id === selection.id);
  if (!profile) {
    invalidateReaderProfileSession(persistedID ?? undefined);
    throw new ProfileContextError("profile_selection_required");
  }
  return { account, profile };
}
