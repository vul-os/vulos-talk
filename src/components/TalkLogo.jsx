/**
 * TalkLogo.jsx — the Vulos Talk brand mark.
 *
 * Renders the real product logo (`/vulos-talk.png`) as a square, rounded tile.
 * The source is a high-resolution square image, so it stays crisp when scaled
 * down to rail/sidebar sizes. If the image ever fails to load we fall back to
 * the original hand-drawn geometric SVG mark so the lockup never breaks.
 *
 * All colour in the fallback resolves through design tokens (the gradient +
 * dots use the teal accent vars), so it inverts cleanly across light/dark.
 *
 *   <TalkLogo size={28} />            → just the tile mark
 *   <TalkLogo size={28} withWordmark/> → mark + "Talk" lockup
 */
import { useId, useState } from 'react'

// Matches the original SVG tile rounding (rx 8.5 on a 32 viewBox ≈ 0.266).
const RADIUS_RATIO = 8.5 / 32

function FallbackMark({ size = 28, className = '' }) {
  const gid = useId().replace(/:/g, '')
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 32 32"
      role="img"
      aria-label="Vulos Talk"
      className={`text-white ${className}`}
      style={{ display: 'block', flexShrink: 0 }}
    >
      <defs>
        <linearGradient id={`tl-${gid}`} x1="0" y1="0" x2="1" y2="1">
          <stop offset="0%" stopColor="var(--teal-300)" />
          <stop offset="55%" stopColor="var(--teal-600)" />
          <stop offset="100%" stopColor="var(--teal-800)" />
        </linearGradient>
      </defs>
      {/* Tile */}
      <rect x="0" y="0" width="32" height="32" rx="8.5" fill={`url(#tl-${gid})`} />
      {/* Top sheen — a hairline of brightness across the tile shoulder */}
      <rect x="0" y="0" width="32" height="14" rx="8.5" fill="currentColor" opacity="0.10" />
      {/* Conversation bubble + tail (the tail points down to form a soft V) */}
      <path
        d="M7 8.5h18a2.5 2.5 0 0 1 2.5 2.5v7a2.5 2.5 0 0 1-2.5 2.5h-9.4L11 25.5a.7.7 0 0 1-1.2-.5V20.9A2.6 2.6 0 0 1 7 18.4V11a2.5 2.5 0 0 1 2.5-2.5Z"
        fill="currentColor"
      />
      {/* Three talk dots */}
      <circle cx="12" cy="14.5" r="1.7" fill="var(--teal-700)" />
      <circle cx="17" cy="14.5" r="1.7" fill="var(--teal-600)" />
      <circle cx="22" cy="14.5" r="1.7" fill="var(--teal-500)" />
    </svg>
  )
}

export function TalkMark({ size = 28, className = '' }) {
  const [failed, setFailed] = useState(false)
  if (failed) return <FallbackMark size={size} className={className} />
  return (
    <img
      src="/vulos-talk.png"
      width={size}
      height={size}
      alt="Vulos Talk"
      onError={() => setFailed(true)}
      className={className}
      style={{
        display: 'block',
        flexShrink: 0,
        width: size,
        height: size,
        borderRadius: Math.round(size * RADIUS_RATIO),
        objectFit: 'cover',
      }}
    />
  )
}

export default function TalkLogo({ size = 28, withWordmark = false, className = '' }) {
  if (!withWordmark) return <TalkMark size={size} className={className} />
  return (
    <span className={`inline-flex items-center gap-2.5 ${className}`}>
      <TalkMark size={size} />
      <span className="flex flex-col -space-y-0.5 leading-none">
        <span className="text-sm font-semibold tracking-tightish text-ink">Vulos</span>
        <span className="font-mono text-[10px] uppercase tracking-wider text-accent-press">Talk</span>
      </span>
    </span>
  )
}
