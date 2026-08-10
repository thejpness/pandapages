import {
  loadIdentity,
  onboardIdentity,
  restoreSupabaseSession,
  type AuthenticatedIdentity,
  type IdentityMembership,
} from "./supabase-auth";
import { clearSelectedReaderProfile } from "./reader-profile-selection";
import { leaveChildMode } from "./reader-mode";

const selectedAccountStorageKey = "pandapages.selected-account-id";
export type AccountContext = Readonly<{
  accessToken: string;
  identity: AuthenticatedIdentity;
  membership: IdentityMembership;
}>;
export class AccountContextError extends Error {
  kind: "signed_out" | "account_selection_required" | "unavailable"
  constructor(kind: "signed_out" | "account_selection_required" | "unavailable") {
    super(kind)
    this.kind = kind
  }
}
export function selectedAccountID(): string | null {
  const value = window.localStorage.getItem(selectedAccountStorageKey);
  return value && value.trim() === value ? value : null;
}
export function selectAccount(accountID: string): void {
  if (selectedAccountID() !== accountID) {
    clearSelectedReaderProfile();
    leaveChildMode();
  }
  window.localStorage.setItem(selectedAccountStorageKey, accountID);
}
export function clearSelectedAccount(): void {
  window.localStorage.removeItem(selectedAccountStorageKey);
  clearSelectedReaderProfile();
  leaveChildMode();
}

export function reconcileAccountMembership(
  memberships: readonly IdentityMembership[],
): IdentityMembership | null {
  const saved = selectedAccountID();
  if (saved !== null) {
    const membership = memberships.find((item) => item.accountId === saved);
    if (membership !== undefined) return membership;
    clearSelectedAccount();
  }
  return memberships.length === 1 ? memberships[0] ?? null : null;
}
export async function currentAccountContext(): Promise<AccountContext> {
  let session;
  try {
    session = await restoreSupabaseSession();
  } catch {
    throw new AccountContextError("unavailable");
  }
  if (!session) throw new AccountContextError("signed_out");
  let identity: AuthenticatedIdentity;
  try {
    identity = await loadIdentity(session.access_token);
  } catch (error) {
    if (
      !(error instanceof Error) ||
      !error.message.includes("setup is required")
    )
      throw new AccountContextError("unavailable");
    try {
      identity = await onboardIdentity(session.access_token);
    } catch {
      throw new AccountContextError("unavailable");
    }
  }
  const saved = selectedAccountID();
  const membership = reconcileAccountMembership(identity.memberships);
  if (membership === null) throw new AccountContextError("account_selection_required");
  // An explicit account change goes through selectAccount and clears the
  // reader choice immediately. With a sole membership there may be no stored
  // account ID at all; keep the profile preference until the server profile
  // list reconciles it for that account.
  if (saved !== null && saved !== membership.accountId) {
    clearSelectedReaderProfile();
    leaveChildMode();
  }
  return { accessToken: session.access_token, identity, membership };
}
