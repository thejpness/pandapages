const profileDestinationBase = "https://pandapages.invalid";

export const defaultReaderDestination = "/library";

export type ProfileCreateOrigin = "chooser" | "manage";

// Profile choice can only continue a reader journey. Keeping this parser here
// prevents profile surfaces from independently accepting different next values.
export function resolveReaderDestination(value: unknown): string {
  if (
    typeof value !== "string" ||
    !value.startsWith("/") ||
    value.startsWith("//") ||
    value.startsWith("/\\")
  ) {
    return defaultReaderDestination;
  }

  let url: URL;
  try {
    url = new URL(value, profileDestinationBase);
  } catch {
    return defaultReaderDestination;
  }

  if (url.origin !== profileDestinationBase) return defaultReaderDestination;
  if (url.pathname === "/library") return url.pathname + url.search + url.hash;

  const slug = url.pathname.match(/^\/read\/([a-z0-9]+(?:-[a-z0-9]+)*)$/i);
  return slug ? url.pathname + url.search + url.hash : defaultReaderDestination;
}

export function resolveProfileCreateOrigin(value: unknown): ProfileCreateOrigin {
  return value === "manage" ? "manage" : "chooser";
}

export function profileCreateReturnDestination(value: unknown): string {
  return resolveProfileCreateOrigin(value) === "manage"
    ? "/profiles/manage"
    : "/profiles";
}
