import { currentAccountContext, type AccountContext } from "./account-context";
import { listReaderProfiles, type ReaderProfile } from "./api";
import {
  clearSelectedReaderProfile,
  reconcileReaderProfileSelection,
  selectReaderProfile,
  selectedReaderProfileID,
} from "./reader-profile-selection";

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
  let profiles: readonly ReaderProfile[];
  try {
    profiles = await listReaderProfiles(account);
  } catch {
    throw new ProfileContextError("unavailable");
  }
  const selection = reconcileReaderProfileSelection(
    selectedReaderProfileID(),
    profiles,
  );
  if (selection === null) {
    clearSelectedReaderProfile();
    throw new ProfileContextError("profile_selection_required");
  }
  selectReaderProfile(selection.id);
  const profile = profiles.find((candidate) => candidate.id === selection.id);
  if (!profile) {
    clearSelectedReaderProfile();
    throw new ProfileContextError("profile_selection_required");
  }
  return { account, profile };
}
