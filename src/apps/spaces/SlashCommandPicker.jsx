/**
 * SlashCommandPicker.jsx — /slash-command autocomplete popup for the composer.
 *
 * Mirrors MentionPicker's keyboard model (↑/↓ navigate, Tab/Enter complete,
 * Esc dismiss). The popup only appears when the composer text starts with "/"
 * and the cursor is still inside the leading command token.
 *
 * Commands are fetched from GET /api/spaces/commands (bot-registered) — the
 * caller passes them in as `commands: [{ name, description }]`.
 */
import { useEffect, useRef, useState } from 'react'
import { Slash } from 'lucide-react'

/**
 * parseSlashQuery — detect a leading "/command" being typed.
 * Returns { query } when the value starts with "/" and the cursor is within the
 * first whitespace-delimited token; otherwise null.
 */
export function parseSlashQuery(value, cursorPos) {
  if (!value.startsWith('/')) return null
  // Only while typing the command word itself (before the first space).
  const firstSpace = value.indexOf(' ')
  const tokenEnd = firstSpace === -1 ? value.length : firstSpace
  if (cursorPos > tokenEnd) return null
  return { query: value.slice(1, tokenEnd) }
}

/**
 * completeSlash — replace the leading "/query" token with "/name " keeping any
 * trailing text after the first space.
 */
export function completeSlash(value, name) {
  const firstSpace = value.indexOf(' ')
  const rest = firstSpace === -1 ? '' : value.slice(firstSpace)
  return `/${name}${rest || ' '}`
}

export default function SlashCommandPicker({ commands = [], query = '', onSelect, onClose }) {
  const [idx, setIdx] = useState(0)
  const ref = useRef(null)

  const q = query.toLowerCase()
  const filtered = commands
    .filter((c) => (c.name || '').toLowerCase().includes(q))
    .slice(0, 8)

  useEffect(() => { setIdx(0) }, [query])

  useEffect(() => {
    function onKey(e) {
      if (!filtered.length) return
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        setIdx((i) => (i + 1) % filtered.length)
      } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        setIdx((i) => (i - 1 + filtered.length) % filtered.length)
      } else if (e.key === 'Tab' || e.key === 'Enter') {
        e.preventDefault()
        onSelect(filtered[idx]?.name || '')
      } else if (e.key === 'Escape') {
        onClose()
      }
    }
    window.addEventListener('keydown', onKey, true)
    return () => window.removeEventListener('keydown', onKey, true)
  }, [filtered, idx, onSelect, onClose])

  if (filtered.length === 0) return null

  return (
    <div
      ref={ref}
      className="absolute bottom-full mb-1 left-0 z-50 bg-paper border border-line rounded-lg shadow-e3 py-1 w-72 animate-scale-in"
      role="listbox"
      aria-label="Slash command suggestions"
    >
      {filtered.map((c, i) => (
        <button
          key={c.name}
          type="button"
          role="option"
          aria-selected={i === idx}
          onMouseEnter={() => setIdx(i)}
          onClick={() => onSelect(c.name)}
          className={[
            'w-full flex items-center gap-2.5 px-3 py-1.5 text-sm transition-colors duration-fast text-left',
            i === idx ? 'bg-accent-tint text-ink' : 'text-ink-muted hover:bg-bg-elev2',
          ].join(' ')}
        >
          <Slash size={13} className="text-accent flex-shrink-0" />
          <span className="flex-1 min-w-0">
            <span className="block font-medium tracking-tightish truncate">/{c.name}</span>
            {c.description && (
              <span className="block text-2xs text-ink-faint truncate">{c.description}</span>
            )}
          </span>
        </button>
      ))}
      <div className="px-3 pt-1.5 pb-0.5 border-t border-line mt-1">
        <p className="text-2xs text-ink-faint">Tab/Enter to complete · Esc to dismiss</p>
      </div>
    </div>
  )
}
