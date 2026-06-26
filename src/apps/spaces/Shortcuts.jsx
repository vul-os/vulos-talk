/**
 * Shortcuts.jsx — keyboard affordances for the Spaces shell.
 *
 *   - QuickSwitcher  (⌘K / Ctrl-K) — fuzzy channel/DM jumper.
 *   - ShortcutsHelp  (?)            — a help overlay listing shortcuts.
 *
 * Both are built on the design-system Modal so focus-trap + Esc come for free.
 */
import { useEffect, useRef, useState } from 'react'
import { Hash, Lock, AtSign, Search, CornerDownLeft } from 'lucide-react'
import { Modal } from '../../components/ui'

function ChannelGlyph({ type }) {
  if (type === 'dm') return <AtSign size={14} className="text-accent" />
  if (type === 'private') return <Lock size={14} className="text-warning" />
  return <Hash size={14} className="text-ink-faint" />
}

export function QuickSwitcher({ open, channels = [], onClose, onSelect }) {
  const [q, setQ] = useState('')
  const [idx, setIdx] = useState(0)
  const inputRef = useRef(null)

  useEffect(() => {
    if (open) {
      setQ('')
      setIdx(0)
      const id = requestAnimationFrame(() => inputRef.current?.focus())
      return () => cancelAnimationFrame(id)
    }
  }, [open])

  const results = q
    ? channels.filter((c) => c.name.toLowerCase().includes(q.toLowerCase()))
    : channels
  const view = results.slice(0, 12)

  function choose(ch) {
    if (!ch) return
    onSelect(ch)
    onClose()
  }

  function onKeyDown(e) {
    if (e.key === 'ArrowDown') { e.preventDefault(); setIdx((i) => Math.min(i + 1, view.length - 1)) }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setIdx((i) => Math.max(i - 1, 0)) }
    else if (e.key === 'Enter') { e.preventDefault(); choose(view[idx]) }
  }

  if (!open) return null

  return (
    <Modal open={open} onClose={onClose} size="md" className="!p-0">
      <div className="flex items-center gap-2 px-4 h-12 border-b border-line">
        <Search size={15} className="text-ink-faint flex-shrink-0" />
        <input
          ref={inputRef}
          value={q}
          onChange={(e) => { setQ(e.target.value); setIdx(0) }}
          onKeyDown={onKeyDown}
          placeholder="Jump to a channel or DM…"
          aria-label="Quick switcher"
          className="flex-1 bg-transparent outline-none text-sm text-ink placeholder:text-ink-faint"
        />
        <kbd className="text-2xs font-mono text-ink-faint border border-line rounded-xs px-1.5 py-0.5">esc</kbd>
      </div>
      <ul role="listbox" aria-label="Channels" className="max-h-80 overflow-y-auto py-1.5">
        {view.length === 0 && (
          <li className="px-4 py-6 text-center text-xs text-ink-faint font-serif italic">
            No matches.
          </li>
        )}
        {view.map((ch, i) => (
          <li key={ch.id}>
            <button
              type="button"
              role="option"
              aria-selected={i === idx}
              onMouseEnter={() => setIdx(i)}
              onClick={() => choose(ch)}
              className={[
                'w-full flex items-center gap-2.5 px-4 py-2 text-left transition-colors',
                i === idx ? 'bg-accent-tint text-ink' : 'text-ink-muted hover:bg-bg-elev2',
              ].join(' ')}
            >
              <ChannelGlyph type={ch.type} />
              <span className="text-sm tracking-tightish truncate flex-1">{ch.name}</span>
              {i === idx && <CornerDownLeft size={13} className="text-ink-faint flex-shrink-0" />}
            </button>
          </li>
        ))}
      </ul>
    </Modal>
  )
}

const SHORTCUTS = [
  { keys: ['⌘', 'K'], label: 'Open quick switcher' },
  { keys: ['?'], label: 'Show this help' },
  { keys: ['Esc'], label: 'Close panel / dialog' },
  { keys: ['↑', '↓'], label: 'Navigate lists' },
  { keys: ['Enter'], label: 'Send message / confirm' },
  { keys: ['Shift', 'Enter'], label: 'New line in composer' },
  { keys: ['@'], label: 'Mention someone' },
  { keys: ['/'], label: 'Run a slash command' },
]

export function ShortcutsHelp({ open, onClose }) {
  return (
    <Modal open={open} onClose={onClose} title="Keyboard shortcuts" size="md">
      <Modal.Body>
        <ul className="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-2">
          {SHORTCUTS.map((s) => (
            <li key={s.label} className="flex items-center justify-between gap-3">
              <span className="text-sm text-ink-muted tracking-tightish">{s.label}</span>
              <span className="flex items-center gap-1 flex-shrink-0">
                {s.keys.map((k) => (
                  <kbd
                    key={k}
                    className="text-2xs font-mono text-ink border border-line bg-bg-elev2 rounded-xs px-1.5 py-0.5 min-w-[20px] text-center"
                  >
                    {k}
                  </kbd>
                ))}
              </span>
            </li>
          ))}
        </ul>
      </Modal.Body>
    </Modal>
  )
}
