/**
 * avatar.js — the canonical avatar-hash palette for Vulos Talk.
 *
 * The one place hand-picked hex is allowed to live (per the design contract):
 * a small, deliberately muted set of identity tints. A stable string hash maps
 * any account id / display name onto the same swatch everywhere — message
 * avatars, the presence roster and the mention picker — so a person reads as
 * "the same colour" across every surface. Use this instead of ad-hoc hsl() or
 * first-character indexing.
 */

// Muted identity tints — aligned with the presence/accent family, no neon.
export const AVATAR_PALETTE = [
  '#0f6a6c', '#4f7a4d', '#c08436', '#b8453a', '#4a6b8a',
  '#6e5b8a', '#7a5a3d', '#3d6b5a', '#6a3d6a', '#8a6a2a',
]

/** stable djb2-style hash over the whole string (not just the first char). */
function hashString(s) {
  let h = 0
  for (let i = 0; i < s.length; i++) h = ((h << 5) - h + s.charCodeAt(i)) | 0
  return Math.abs(h)
}

/** avatarColor — deterministic palette swatch for an id/name. */
export function avatarColor(id) {
  if (!id) return AVATAR_PALETTE[0]
  return AVATAR_PALETTE[hashString(String(id)) % AVATAR_PALETTE.length]
}
