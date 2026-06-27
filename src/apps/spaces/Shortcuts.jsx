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

function typeLabel(type) {
  if (type === 'dm') return 'Direct message'
  if (type === 'private') return 'Private channel'
  return 'Channel'
}

// Safe match highlight — splits on the query (no innerHTML), wraps hits in <mark>.
function Highlight({ text, query }) {
  if (!query) return text
  const i = text.toLowerCase().indexOf(query.toLowerCase())
  if (i === -1) return text
  return (
    <>
      {text.slice(0, i)}
      <mark className="bg-transparent text-accent-press font-semibold">{text.slice(i, i + query.length)}</mark>
      {text.slice(i + query.length)}
    </>
  )
}

function KbdHint({ keys, label }) {
  return (
    <span className="inline-flex items-center gap-1">
      {keys.map((k) => (
        <kbd key={k} className="font-mono text-[10px] text-ink-faint border border-line rounded-xs px-1 py-px min-w-[16px] text-center">{k}</kbd>
      ))}
      <span className="text-ink-faint">{label}</span>
    </span>
  )
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
      <div className="flex items-center gap-2.5 px-4 py-3 border-b border-line">
        <Search size={16} className="text-ink-faint flex-shrink-0" />
        <input
          ref={inputRef}
          value={q}
          onChange={(e) => { setQ(e.target.value); setIdx(0) }}
          onKeyDown={onKeyDown}
          placeholder="Jump to a channel or direct message…"
          aria-label="Quick switcher"
          className="flex-1 bg-transparent outline-none text-md text-ink placeholder:text-ink-faint"
        />
        <kbd className="text-2xs font-mono text-ink-faint border border-line rounded-xs px-1.5 py-0.5">esc</kbd>
      </div>
      {!q && (
        <p className="px-4 pt-2.5 pb-1 font-mono text-[10px] font-medium uppercase tracking-wider text-ink-faint select-none">
          Recent
        </p>
      )}
      <ul role="listbox" aria-label="Channels" className="max-h-80 overflow-y-auto pb-1.5 px-1.5">
        {view.length === 0 && (
          <li className="px-4 py-10 text-center">
            <Search size={22} className="text-ink-faint mx-auto mb-2 opacity-60" />
            <p className="text-xs text-ink-faint font-serif italic">No channels or DMs match “{q}”.</p>
          </li>
        )}
        {view.map((ch, i) => {
          const selected = i === idx
          return (
            <li key={ch.id}>
              <button
                type="button"
                role="option"
                aria-selected={selected}
                onMouseEnter={() => setIdx(i)}
                onClick={() => choose(ch)}
                className={[
                  'w-full flex items-center gap-2.5 px-2.5 h-10 rounded-md text-left transition-colors',
                  selected ? 'bg-accent-tint text-ink' : 'text-ink-muted hover:bg-bg-hover',
                ].join(' ')}
              >
                <span className={['flex items-center justify-center w-7 h-7 rounded-md flex-shrink-0 border', selected ? 'bg-paper border-accent-tint-2' : 'bg-bg-elev2 border-line'].join(' ')}>
                  <ChannelGlyph type={ch.type} />
                </span>
                <span className="flex flex-col min-w-0 flex-1 -space-y-0.5">
                  <span className="text-sm tracking-tightish truncate leading-tight"><Highlight text={ch.name} query={q} /></span>
                  <span className="text-[10px] text-ink-faint leading-tight">{typeLabel(ch.type)}</span>
                </span>
                {selected && (
                  <span className="flex items-center gap-1 text-2xs text-ink-faint flex-shrink-0">
                    Jump <CornerDownLeft size={12} />
                  </span>
                )}
              </button>
            </li>
          )
        })}
      </ul>
      <div className="flex items-center gap-4 px-4 h-9 border-t border-line bg-bg-sunk text-2xs">
        <KbdHint keys={['↑', '↓']} label="Navigate" />
        <KbdHint keys={['↵']} label="Open" />
        <KbdHint keys={['esc']} label="Close" />
        <span className="ml-auto text-ink-faint tabular-nums">{view.length} result{view.length !== 1 ? 's' : ''}</span>
      </div>
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
