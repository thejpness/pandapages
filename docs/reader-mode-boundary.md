# Reader mode boundary

Status: accepted current product boundary
Date: 2026-08-08

## Decision

Reader mode is a **UX containment boundary**, not an adult authentication or
authorization boundary.

While reader mode is active, Panda Pages keeps adult/account surfaces out of the
reader experience:

- `/profiles` remains available as the reader switcher; it keeps parent-management actions hidden while reader mode is active;
- `/admin` and Story Studio routes return to `/library`;
- `/account` returns to `/library`.

The account chooser remains a route that does not require an already-selected
account, because its purpose is to establish that selection. It is nevertheless
parent-only while reader mode is active.

Public and authentication infrastructure routes are different concerns. The
public site remains public, and `/account/login` plus `/auth/callback` remain
available so an expired or absent adult session can recover normally.

## Leaving reader mode

The current product deliberately leaves **Parent controls** ungated. Activating
it clears the in-memory reader-mode state and opens the parent profile surface.

This is a product/UX choice for the current release, not a claim that Panda Pages
prevents a child with physical access to the device from reaching the Parent
Hub.

A hard parental escape gate is deferred until the product requires unattended
child-device containment.

Reader-mode state is deliberately memory-only. Router containment therefore
applies while the current application session remains mounted. A full document
reload or address-bar navigation starts a fresh frontend session without
reader-mode state. This is another reason reader mode must not be described as a
child-device security boundary.

## Security invariants

The following must remain true regardless of the reader-mode UX:

- Supabase is the external adult identity provider.
- Adult authority comes from the authenticated adult plus explicit Panda Pages
  account membership.
- `owner` / `adult` membership controls adult capabilities.
- Server authorization remains authoritative; router containment is not a
  substitute for backend authorization.
- Reader profiles are reading identities, not adult principals.
- Reader PINs are optional reader-entry gates only.
- A reader PIN must never become an account, parent, owner, Story Studio, or
  administration credential.
- Reader PIN verification must not mint a child bearer token, custom child
  session, or administration session.
- Reader-mode state remains frontend memory-only.
- Progress remains profile-scoped.
- Settings remain account-scoped.

## Rejected shortcut

Reusing a reader's four-digit PIN as a "parent PIN" is explicitly rejected.

That would invert the meaning of the credential: a secret intended to protect
entry *into a reader* would become authority to leave that reader and reach
adult controls. It would also couple reader management to adult authorization.

## Future hard-gate options

If Panda Pages later requires a child-safe device mode, implement that as a
separate product/security feature. The viable directions are:

1. require fresh Supabase adult reauthentication before leaving reader mode; or
2. introduce a distinct account-level Parent PIN with its own storage, rate
   limiting, reset/recovery rules, and threat model.

That decision must be made explicitly. It must not be inferred from, or
implemented by reusing, reader PINs.

## Consequence

Current reader-mode routing prevents accidental or ordinary navigation into
adult/account surfaces, while the backend remains the real security boundary.
The explicit escape control is intentionally a UX transition rather than an
authentication challenge.
