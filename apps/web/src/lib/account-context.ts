import {
  loadIdentity,
  onboardIdentity,
  restoreSupabaseSession,
  type AuthenticatedIdentity,
  type IdentityMembership,
} from "./supabase-auth";

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
function selectedAccountID(): string | null {
  const value = window.localStorage.getItem(selectedAccountStorageKey);
  return value && value.trim() === value ? value : null;
}
export function selectAccount(accountID: string): void {
  window.localStorage.setItem(selectedAccountStorageKey, accountID);
}
export function clearSelectedAccount(): void {
  window.localStorage.removeItem(selectedAccountStorageKey);
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
  const membership = saved
    ? identity.memberships.find((item) => item.accountId === saved)
    : identity.memberships.length === 1
      ? identity.memberships[0]
      : undefined;
  if (!membership) throw new AccountContextError("account_selection_required");
  return { accessToken: session.access_token, identity, membership };
}
