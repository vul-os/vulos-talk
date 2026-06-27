/**
 * SpacesApp — Vulos Spaces surface (Slack / Google-Chat-class layout).
 * Routes (mounted by TalkShell): /  /channels/:id  /dm/:id
 *
 *   - Left sidebar: workspace header, prominent Compose, global search/jump,
 *     collapsible sections (Channels with unread bold + mention badges, Direct
 *     Messages with presence dots), plus Threads and Activity entries, and an
 *     Apps & Bots / settings affordance.
 *   - Center pane: ChannelView, or the Threads / Activity views.
 *   - Responsive: 3-pane desktop; ≤768px single column with a channel drawer,
 *     back nav, full-screen composer and a bottom nav.
 *   - Keyboard: ⌘K quick switcher, ? help overlay, Esc to close.
 *
 * Backed by the CRDT message store; REST/poll presence via useRestPresence.
 */
import { useEffect, useRef, useState, useCallback } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import {
  Hash, Lock, AtSign, Plus, Users, Search, ChevronDown, ChevronRight,
  Pencil, MessageSquare, Bell, Bot, HelpCircle, X, Menu, Home, User,
} from 'lucide-react'
import ChannelView from './ChannelView.jsx'
import ActivityView from './ActivityView.jsx'
import { QuickSwitcher, ShortcutsHelp } from './Shortcuts.jsx'
import { api } from '../../lib/api.js'
import { toast } from '../../lib/toast.jsx'
import { STATUS_ONLINE } from '@vulos/relay-client/presence'
import { PresenceDot, StatusPicker } from '../../components/PresenceBar.jsx'
import { Button, IconButton, Input, Modal, Sidebar, LoadingState, ThemeSwitch } from '../../components/ui'
import { avatarColor } from './avatar.js'
import { TalkMark } from '../../components/TalkLogo.jsx'

// ---------------------------------------------------------------------------
// useRestPresence (unchanged) — OFFICE-62 REST/poll presence.
// ---------------------------------------------------------------------------

function useRestPresence() {
  const [roster, setRoster] = useState([])
  const statusRef = useRef({ status: 'online', text: '' })

  const doHeartbeat = useCallback(async () => {
    try { await api.spacesHeartbeat(statusRef.current.status, statusRef.current.text, '') } catch {}
  }, [])

  const doRoster = useCallback(async () => {
    try {
      const entries = await api.spacesGetRoster()
      setRoster((entries || []).map((e) => ({
        accountId: e.user_id,
        displayName: e.display_name || e.user_id,
        status: e.status || 'online',
        statusText: e.status_text || '',
        color: avatarColor(e.user_id),
        online: true,
      })))
    } catch {}
  }, [])

  useEffect(() => {
    doHeartbeat(); doRoster()
    const hb = setInterval(doHeartbeat, 15000)
    const rs = setInterval(doRoster, 15000)
    return () => { clearInterval(hb); clearInterval(rs) }
  }, [doHeartbeat, doRoster])

  const setStatus = useCallback((status, text = '') => {
    statusRef.current = { status, text }
    api.spacesHeartbeat(status, text, '').catch(() => {})
  }, [])

  return { roster, setStatus }
}

// ---------------------------------------------------------------------------
// Seen-state (best-effort unread tracking via localStorage).
// ---------------------------------------------------------------------------

const SEEN_KEY = 'spaces_seen'
function loadSeen() { try { return JSON.parse(localStorage.getItem(SEEN_KEY) || '{}') } catch { return {} } }
function channelActivityTs(ch) { return ch.last_message_at || ch.last_activity || ch.updated_at || null }
function mentionCount(ch) { return ch.unread_mentions ?? ch.mention_count ?? 0 }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function ChannelIcon({ type, size = 14, className = '' }) {
  if (type === 'dm') return <AtSign size={size} className={`text-accent ${className}`} />
  if (type === 'private') return <Lock size={size} className={`text-warning ${className}`} />
  return <Hash size={size} className={`text-ink-faint ${className}`} />
}

// ---------------------------------------------------------------------------
// CreateChannelModal
// ---------------------------------------------------------------------------

function CreateChannelModal({ open, onClose, onCreated }) {
  const [name, setName] = useState('')
  const [type, setType] = useState('public')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(null)

  async function submit(e) {
    e.preventDefault()
    const n = name.trim().toLowerCase().replace(/\s+/g, '-')
    if (!n) return
    setLoading(true); setError(null)
    try {
      const ch = await api.spacesCreateChannel(n, type)
      toast.success(`#${n} created`)
      onCreated(ch); onClose(); setName('')
    } catch (err) { setError(err.message || 'Create failed') }
    finally { setLoading(false) }
  }

  return (
    <Modal open={open} onClose={onClose} title="Create a channel">
      <form onSubmit={submit}>
        <Modal.Body className="space-y-4">
          {error && <p className="text-xs text-danger bg-danger-bg rounded-sm px-3 py-2">{error}</p>}
          <Input label="Name" placeholder="e.g. team-design" value={name} onChange={(e) => setName(e.target.value)} leading={<Hash size={13} />} autoFocus />
          <div>
            <label className="block text-xs text-ink-muted font-medium mb-1.5 tracking-tightish">Type</label>
            <div className="flex gap-2">
              {[{ v: 'public', label: 'Public', hint: 'Anyone can join' }, { v: 'private', label: 'Private', hint: 'Invite only' }].map((o) => (
                <button key={o.v} type="button" onClick={() => setType(o.v)} className={['flex-1 text-left rounded-md border px-3 py-2 transition-colors duration-fast ease-out', type === o.v ? 'border-accent bg-accent-tint text-ink' : 'border-line hover:border-line-strong text-ink-muted'].join(' ')}>
                  <div className="text-sm font-medium tracking-tightish">{o.label}</div>
                  <div className="text-2xs text-ink-faint">{o.hint}</div>
                </button>
              ))}
            </div>
          </div>
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" onClick={onClose} type="button">Cancel</Button>
          <Button variant="primary" type="submit" disabled={loading || !name.trim()}>{loading ? 'Creating…' : 'Create channel'}</Button>
        </Modal.Footer>
      </form>
    </Modal>
  )
}

// ---------------------------------------------------------------------------
// NewConversationModal — direct message OR group DM (multiple recipients).
// ---------------------------------------------------------------------------

function NewConversationModal({ open, onClose, onCreated, roster = [] }) {
  const [recipients, setRecipients] = useState([]) // [{ id, name }]
  const [input, setInput] = useState('')
  const [name, setName] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(null)

  function reset() { setRecipients([]); setInput(''); setName(''); setError(null) }
  function handleClose() { reset(); onClose() }

  function addRecipient(id, displayName = '') {
    const v = id.trim()
    if (!v || recipients.some((r) => r.id === v)) return
    setRecipients((r) => [...r, { id: v, name: displayName.trim() }])
    setInput(''); setName('')
  }
  function removeRecipient(id) { setRecipients((r) => r.filter((x) => x.id !== id)) }

  async function submit(e) {
    e.preventDefault()
    const all = input.trim() ? [...recipients, { id: input.trim(), name: name.trim() }] : recipients
    const ids = [...new Set(all.map((r) => r.id).filter(Boolean))]
    if (ids.length === 0) return
    setLoading(true); setError(null)
    try {
      const members = ['me', ...ids]
      const dmName = members.slice().sort().join('-')
      const memberNames = all.reduce((acc, r) => { if (r.name) acc[r.id] = r.name; return acc }, {})
      const ch = await api.spacesCreateChannel(dmName, 'dm', members, Object.keys(memberNames).length ? memberNames : null)
      toast.success(ids.length > 1 ? 'Group DM opened' : 'Direct message opened')
      onCreated(ch); handleClose()
    } catch (err) { setError(err.message || 'Create failed') }
    finally { setLoading(false) }
  }

  const suggestions = input.trim()
    ? roster.filter((p) => !recipients.some((r) => r.id === p.accountId) && p.accountId !== 'me' &&
        (p.accountId.toLowerCase().includes(input.toLowerCase()) || p.displayName?.toLowerCase().includes(input.toLowerCase()))).slice(0, 5)
    : []

  const isGroup = recipients.length + (input.trim() ? 1 : 0) > 1

  return (
    <Modal open={open} onClose={handleClose} title={isGroup ? 'New group message' : 'New message'}>
      <form onSubmit={submit}>
        <Modal.Body className="space-y-4">
          {error && <p className="text-xs text-danger bg-danger-bg rounded-sm px-3 py-2">{error}</p>}
          {recipients.length > 0 && (
            <div className="flex flex-wrap gap-1.5">
              {recipients.map((r) => (
                <span key={r.id} className="inline-flex items-center gap-1 bg-accent-tint border border-accent-tint-2 rounded-pill pl-2 pr-1 py-0.5 text-xs text-ink">
                  {r.name || r.id}
                  <button type="button" onClick={() => removeRecipient(r.id)} className="text-ink-faint hover:text-ink" aria-label={`Remove ${r.id}`}><X size={11} /></button>
                </span>
              ))}
            </div>
          )}
          <div>
            <Input
              label="To"
              placeholder="account id — add several for a group"
              value={input}
              onChange={(e) => { setInput(e.target.value); setError(null) }}
              onKeyDown={(e) => { if ((e.key === 'Enter' || e.key === ',') && input.trim()) { e.preventDefault(); addRecipient(input, name) } }}
              leading={<AtSign size={13} />}
              autoFocus
            />
            {suggestions.length > 0 && (
              <ul role="listbox" className="mt-1 border border-line rounded-md bg-paper shadow-e2 overflow-hidden">
                {suggestions.map((p) => (
                  <li key={p.accountId}>
                    <button type="button" onClick={() => addRecipient(p.accountId, p.displayName)} className="w-full flex items-center gap-2 px-3 py-2 text-sm text-left hover:bg-accent-tint transition-colors">
                      <PresenceDot status={p.status} size={7} />
                      <span className="font-medium text-ink tracking-tightish">{p.displayName || p.accountId}</span>
                      {p.displayName && <span className="text-ink-faint text-2xs ml-auto">{p.accountId}</span>}
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>
          <Input label="Their name (optional)" placeholder="e.g. Jane Doe" value={name} onChange={(e) => setName(e.target.value)} leading={<Users size={13} />} />
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" onClick={handleClose} type="button">Cancel</Button>
          <Button variant="primary" type="submit" disabled={loading || (recipients.length === 0 && !input.trim())}>
            {loading ? 'Opening…' : isGroup ? 'Start group' : 'Open DM'}
          </Button>
        </Modal.Footer>
      </form>
    </Modal>
  )
}

// ---------------------------------------------------------------------------
// DisplayNameModal
// ---------------------------------------------------------------------------

function DisplayNameModal({ open, onClose, channelId, onSaved }) {
  const [displayName, setDisplayName] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(null)

  async function submit(e) {
    e.preventDefault()
    if (!channelId) return
    setLoading(true); setError(null)
    try { await api.spacesSetMyName(channelId, displayName.trim()); onSaved?.(); onClose() }
    catch (err) { setError(err.message || 'Save failed') }
    finally { setLoading(false) }
  }

  return (
    <Modal open={open} onClose={onClose} title="Your display name">
      <form onSubmit={submit}>
        <Modal.Body className="space-y-4">
          {error && <p className="text-xs text-danger bg-danger-bg rounded-sm px-3 py-2">{error}</p>}
          <p className="text-2xs text-ink-faint">How you appear to others in this channel. Leave blank to show your account id.</p>
          <Input label="Display name" placeholder="e.g. Jane Doe" value={displayName} onChange={(e) => setDisplayName(e.target.value)} leading={<Users size={13} />} autoFocus />
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" onClick={onClose} type="button">Cancel</Button>
          <Button variant="primary" type="submit" disabled={loading || !channelId}>{loading ? 'Saving…' : 'Save name'}</Button>
        </Modal.Footer>
      </form>
    </Modal>
  )
}

// ---------------------------------------------------------------------------
// SectionToggle + ChannelRow
// ---------------------------------------------------------------------------

function SectionToggle({ label, open, onToggle, onAdd, addTitle }) {
  return (
    <div className="flex items-center justify-between pl-2 pr-1 pt-1 pb-1 group">
      <button onClick={onToggle} className="flex items-center gap-1 text-2xs font-semibold text-ink-faint uppercase tracking-eyebrow hover:text-ink-muted transition-colors">
        {open ? <ChevronDown size={11} /> : <ChevronRight size={11} />}
        {label}
      </button>
      {onAdd && (
        <button onClick={onAdd} title={addTitle} aria-label={addTitle} className="opacity-0 group-hover:opacity-100 rounded-xs p-0.5 text-ink-faint hover:text-ink hover:bg-accent-tint transition-[opacity,background,color] duration-fast">
          <Plus size={12} />
        </button>
      )}
    </div>
  )
}

function ChannelRow({ channel, isActive, unread, mentions, dmPeer, onSelect }) {
  return (
    <button
      type="button"
      onClick={() => onSelect(channel)}
      aria-current={isActive ? 'true' : undefined}
      className={[
        'relative w-full flex items-center gap-2 h-7 pl-3 pr-2 rounded-md text-left transition-colors duration-fast ease-out',
        isActive ? 'bg-paper text-ink shadow-e1' : unread ? 'text-ink hover:bg-accent-tint' : 'text-ink-muted hover:bg-accent-tint hover:text-ink',
      ].join(' ')}
    >
      <span aria-hidden className={['absolute left-0 top-1 bottom-1 w-[2px] rounded-r-full', isActive ? 'bg-accent' : 'bg-transparent'].join(' ')} />
      <span className="relative flex-shrink-0">
        <ChannelIcon type={channel.type} />
        {dmPeer && <span className="absolute -bottom-0.5 -right-0.5"><PresenceDot status={dmPeer.status} size={6} /></span>}
      </span>
      <span className={['truncate text-sm tracking-tightish flex-1', unread && !isActive ? 'font-semibold' : ''].join(' ')}>{channel.name}</span>
      {mentions > 0 && (
        <span className="flex-shrink-0 min-w-[18px] h-[18px] px-1 inline-flex items-center justify-center rounded-pill bg-danger text-white text-[10px] font-bold tabular-nums">{mentions > 99 ? '99+' : mentions}</span>
      )}
      {unread && mentions === 0 && !isActive && <span className="flex-shrink-0 w-1.5 h-1.5 rounded-full bg-accent-press" />}
    </button>
  )
}

// ---------------------------------------------------------------------------
// SpacesSidebar
// ---------------------------------------------------------------------------

function SpacesSidebar({
  channels, activeId, view, onSelect, onSetView, onRefresh, seenMap,
  roster, localStatus, localStatusText, onSetStatus, onOpenQuick, onOpenHelp,
}) {
  const navigate = useNavigate()
  const [showCreateChannel, setShowCreateChannel] = useState(false)
  const [showNewConvo, setShowNewConvo] = useState(false)
  const [showDisplayName, setShowDisplayName] = useState(false)
  const [showCompose, setShowCompose] = useState(false)
  const [channelsOpen, setChannelsOpen] = useState(true)
  const [dmsOpen, setDmsOpen] = useState(true)
  const [showStatusPicker, setShowStatusPicker] = useState(false)

  const publicChannels = channels.filter((c) => c.type !== 'dm')
  const dms = channels.filter((c) => c.type === 'dm')

  function isUnread(ch) {
    if (ch.id === activeId) return false
    const ts = channelActivityTs(ch)
    if (!ts) return false
    const seen = seenMap[ch.id]
    return !seen || new Date(ts) > new Date(seen)
  }

  const peersOnline = roster.filter((p) => !p.isSelf)

  return (
    <Sidebar collapsed={false} className="!w-full md:!w-60 h-full">
      {/* Workspace header */}
      <button
        type="button"
        onClick={() => navigate('/')}
        className="group flex items-center justify-between h-14 px-3 border-b border-line flex-shrink-0 hover:bg-bg-hover transition-colors text-left"
      >
        <span className="flex items-center gap-2.5 min-w-0">
          <TalkMark size={28} />
          <span className="flex flex-col min-w-0 -space-y-0.5">
            <span className="text-sm font-semibold text-ink tracking-tightish truncate leading-tight">Vulos Talk</span>
            <span className="flex items-center gap-1 font-mono text-[10px] uppercase tracking-wider text-ink-faint leading-tight">
              <span className="inline-block w-1.5 h-1.5 rounded-full bg-success" /> workspace
            </span>
          </span>
        </span>
        <ChevronDown size={14} className="text-ink-faint group-hover:text-ink-muted transition-colors flex-shrink-0" />
      </button>

      {/* Compose + search */}
      <div className="px-2 pt-2.5 pb-2 space-y-1.5 border-b border-line flex-shrink-0">
        <div className="relative">
          <Button variant="primary" fullWidth size="sm" onClick={() => setShowCompose((v) => !v)}>
            <Pencil size={13} /> New message
          </Button>
          {showCompose && (
            <div className="absolute top-full left-0 right-0 mt-1 z-50 bg-paper border border-line rounded-md shadow-e3 py-1 animate-scale-in" onMouseLeave={() => setShowCompose(false)}>
              <button type="button" onClick={() => { setShowCompose(false); setShowNewConvo(true) }} className="w-full flex items-center gap-2 px-3 py-1.5 text-sm text-ink-muted hover:bg-accent-tint hover:text-ink transition-colors text-left">
                <AtSign size={13} /> Message someone
              </button>
              <button type="button" onClick={() => { setShowCompose(false); setShowCreateChannel(true) }} className="w-full flex items-center gap-2 px-3 py-1.5 text-sm text-ink-muted hover:bg-accent-tint hover:text-ink transition-colors text-left">
                <Hash size={13} /> Create channel
              </button>
            </div>
          )}
        </div>
        <button
          type="button"
          onClick={onOpenQuick}
          className="w-full flex items-center gap-2 h-8 px-2.5 rounded-md bg-bg-sunk border border-line text-ink-faint hover:text-ink-muted hover:border-line-strong transition-colors text-left"
        >
          <Search size={13} className="flex-shrink-0" />
          <span className="text-xs tracking-tightish flex-1">Jump to…</span>
          <kbd className="text-[10px] font-mono border border-line rounded-xs px-1 py-px">⌘K</kbd>
        </button>
      </div>

      {/* Nav list */}
      <div className="flex-1 overflow-y-auto py-2 px-1.5 space-y-0.5">
        {/* Threads + Activity entries */}
        <button type="button" onClick={() => onSetView('threads')} className={['w-full flex items-center gap-2 h-7 pl-3 pr-2 rounded-md text-left transition-colors duration-fast', view === 'threads' ? 'bg-paper text-ink shadow-e1' : 'text-ink-muted hover:bg-accent-tint hover:text-ink'].join(' ')}>
          <MessageSquare size={14} className="text-ink-faint" /><span className="text-sm tracking-tightish">Threads</span>
        </button>
        <button type="button" onClick={() => onSetView('activity')} className={['w-full flex items-center gap-2 h-7 pl-3 pr-2 rounded-md text-left transition-colors duration-fast', view === 'activity' ? 'bg-paper text-ink shadow-e1' : 'text-ink-muted hover:bg-accent-tint hover:text-ink'].join(' ')}>
          <Bell size={14} className="text-ink-faint" /><span className="text-sm tracking-tightish">Activity</span>
        </button>

        <div className="mt-2" />
        <SectionToggle label="Channels" open={channelsOpen} onToggle={() => setChannelsOpen(!channelsOpen)} onAdd={() => setShowCreateChannel(true)} addTitle="Create channel" />
        {channelsOpen && publicChannels.map((ch) => (
          <ChannelRow key={ch.id} channel={ch} isActive={ch.id === activeId && view === 'chat'} unread={isUnread(ch)} mentions={mentionCount(ch)} onSelect={onSelect} />
        ))}
        {channelsOpen && publicChannels.length === 0 && <p className="text-2xs text-ink-faint px-3 py-1 italic">No channels yet.</p>}

        <div className="mt-3" />
        <SectionToggle label="Direct Messages" open={dmsOpen} onToggle={() => setDmsOpen(!dmsOpen)} onAdd={() => setShowNewConvo(true)} addTitle="New message" />
        {dmsOpen && dms.map((ch) => {
          const dmPeer = roster.find((p) => !p.isSelf && ch.name.includes(p.displayName || p.accountId))
          return <ChannelRow key={ch.id} channel={ch} isActive={ch.id === activeId && view === 'chat'} unread={isUnread(ch)} mentions={mentionCount(ch)} dmPeer={dmPeer} onSelect={onSelect} />
        })}
        {dmsOpen && dms.length === 0 && <p className="text-2xs text-ink-faint px-3 py-1 italic">No direct messages.</p>}
      </div>

      {/* Footer */}
      <Sidebar.Footer>
        {peersOnline.length > 0 && (
          <div className="flex items-center gap-1.5 px-2 py-1 text-2xs text-ink-faint">
            <Users size={11} />
            <span className="font-medium text-ink-muted">{peersOnline.length} online</span>
          </div>
        )}
        <div className="relative">
          <button type="button" onClick={() => setShowStatusPicker((v) => !v)} className="w-full flex items-center gap-2 h-8 px-3 rounded-md text-ink-muted hover:bg-accent-tint hover:text-ink transition-colors duration-fast ease-out">
            <PresenceDot status={localStatus} size={8} />
            <span className="text-xs truncate tracking-tightish">{localStatusText || localStatus || STATUS_ONLINE}</span>
          </button>
          {showStatusPicker && <StatusPicker currentStatus={localStatus} currentText={localStatusText} onStatusChange={onSetStatus} onClose={() => setShowStatusPicker(false)} />}
        </div>
        <button type="button" onClick={() => setShowDisplayName(true)} disabled={!activeId} title={activeId ? 'Set your display name' : 'Open a channel first'} className="w-full flex items-center gap-2 h-8 px-3 rounded-md text-ink-muted hover:bg-accent-tint hover:text-ink transition-colors duration-fast ease-out disabled:opacity-40 disabled:cursor-not-allowed">
          <User size={13} /><span className="text-xs truncate tracking-tightish">Set your name</span>
        </button>
        <button type="button" onClick={onOpenHelp} className="w-full flex items-center gap-2 h-8 px-3 rounded-md text-ink-muted hover:bg-accent-tint hover:text-ink transition-colors duration-fast ease-out">
          <HelpCircle size={13} /><span className="text-xs truncate tracking-tightish">Keyboard shortcuts</span>
        </button>
        <div className="px-2 pt-1"><ThemeSwitch /></div>
      </Sidebar.Footer>

      <CreateChannelModal open={showCreateChannel} onClose={() => setShowCreateChannel(false)} onCreated={(ch) => { onRefresh(); onSelect(ch) }} />
      <NewConversationModal open={showNewConvo} onClose={() => setShowNewConvo(false)} onCreated={(ch) => { onRefresh(); onSelect(ch) }} roster={roster} />
      <DisplayNameModal open={showDisplayName} onClose={() => setShowDisplayName(false)} channelId={activeId} onSaved={onRefresh} />
    </Sidebar>
  )
}

// ---------------------------------------------------------------------------
// WorkspaceRail — the far-left global rail (Slack/Discord-class).
// Workspace mark on top, primary destinations in the middle, self-presence +
// theme at the foot. Desktop only; mobile uses the bottom nav.
// ---------------------------------------------------------------------------

function RailButton({ icon: Icon, label, active, badge, onClick }) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={label}
      aria-label={label}
      aria-current={active ? 'true' : undefined}
      className="group relative w-full flex items-center justify-center h-11"
    >
      <span
        aria-hidden
        className={[
          'absolute left-0 top-1.5 bottom-1.5 w-[3px] rounded-r-full transition-all duration-base ease-spring',
          active ? 'bg-accent-press opacity-100 scale-y-100' : 'bg-accent-press opacity-0 scale-y-50 group-hover:opacity-40 group-hover:scale-y-90',
        ].join(' ')}
      />
      <span
        className={[
          'relative flex items-center justify-center h-9 w-9 rounded-lg transition-[background,color,transform] duration-fast ease-out',
          active
            ? 'bg-accent-tint text-accent-press border border-accent-tint-2'
            : 'text-ink-faint border border-transparent hover:bg-bg-hover hover:text-ink active:scale-95',
        ].join(' ')}
      >
        <Icon size={18} strokeWidth={active ? 2.1 : 1.8} />
        {badge > 0 && (
          <span className="absolute -top-1 -right-1 min-w-[15px] h-[15px] px-1 inline-flex items-center justify-center rounded-pill bg-danger text-white text-[9px] font-bold tabular-nums ring-2 ring-bg-elev2">
            {badge > 9 ? '9+' : badge}
          </span>
        )}
      </span>
    </button>
  )
}

function WorkspaceRail({ view, activeChat, mentions, onHome, onActivity, navigate, localStatus, onOpenStatus }) {
  return (
    <nav
      aria-label="Workspaces"
      className="hidden md:flex flex-col items-center w-[60px] flex-shrink-0 bg-bg-sunk border-r border-line py-2"
    >
      <div className="flex flex-col w-full">
        <RailButton icon={Home} label="Channels" active={view === 'chat' && activeChat} onClick={onHome} />
        <RailButton icon={Bell} label="Activity" active={view === 'activity'} badge={mentions} onClick={onActivity} />
        <RailButton icon={Bot} label="Apps & Bots" onClick={() => navigate('/apps')} />
      </div>

      <div className="mt-auto flex flex-col items-center gap-2 w-full pt-2">
        <div className="w-7 h-px bg-line" />
        <button
          type="button"
          onClick={onOpenStatus}
          title="You — set status"
          aria-label="Set your status"
          className="relative group"
        >
          <span className="flex items-center justify-center h-8 w-8 rounded-lg bg-accent text-white text-xs font-semibold tracking-tightish transition-transform duration-fast group-hover:scale-105 active:scale-95">
            ME
          </span>
          <span className="absolute -bottom-0.5 -right-0.5"><PresenceDot status={localStatus} size={9} /></span>
        </button>
      </div>
    </nav>
  )
}

// ---------------------------------------------------------------------------
// SpacesApp — root
// ---------------------------------------------------------------------------

function channelPath(ch) {
  return `${ch.type === 'dm' ? '/dm' : '/channels'}/${ch.id}`
}

export default function SpacesApp() {
  const { id: channelId } = useParams()
  const navigate = useNavigate()
  const [channels, setChannels] = useState([])
  const [activeChannel, setActiveChannel] = useState(null)
  const [view, setView] = useState('chat') // 'chat' | 'threads' | 'activity'
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [seenMap, setSeenMap] = useState(loadSeen)
  const [mobilePane, setMobilePane] = useState('list') // 'list' | 'main'
  const [quickOpen, setQuickOpen] = useState(false)
  const [helpOpen, setHelpOpen] = useState(false)
  const [railStatusOpen, setRailStatusOpen] = useState(false)

  const { roster, setStatus: setRestStatus } = useRestPresence()
  const [localStatus, setLocalStatus] = useState(STATUS_ONLINE)
  const [localStatusText, setLocalStatusText] = useState('')

  function handleSetStatus(status, text) {
    setLocalStatus(status); setLocalStatusText(text); setRestStatus(status, text)
  }

  const currentUser = 'me'

  const loadChannels = useCallback(async () => {
    try {
      const chs = await api.spacesListChannels()
      setChannels(chs || [])
      return chs || []
    } catch (e) { setError(e.message || 'Failed to load channels'); return [] }
    finally { setLoading(false) }
  }, [])

  useEffect(() => {
    loadChannels().then((chs) => {
      if (channelId) {
        const found = chs.find((c) => c.id === channelId)
        if (found) { setActiveChannel(found); setMobilePane('main') }
      } else if (chs.length > 0) {
        setActiveChannel(chs[0])
      }
    })
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  function markSeen(ch) {
    setSeenMap((prev) => {
      const next = { ...prev, [ch.id]: new Date().toISOString() }
      try { localStorage.setItem(SEEN_KEY, JSON.stringify(next)) } catch {}
      return next
    })
  }

  function selectChannel(ch) {
    setActiveChannel(ch); setView('chat'); setMobilePane('main'); markSeen(ch)
    navigate(channelPath(ch))
  }

  // Global keyboard shortcuts
  useEffect(() => {
    function onKey(e) {
      const mod = e.metaKey || e.ctrlKey
      if (mod && (e.key === 'k' || e.key === 'K')) { e.preventDefault(); setQuickOpen(true); return }
      const typing = ['INPUT', 'TEXTAREA'].includes(document.activeElement?.tagName) || document.activeElement?.isContentEditable
      if (!typing && e.key === '?') { e.preventDefault(); setHelpOpen(true) }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  if (loading) {
    return <div className="flex-1 flex items-center justify-center bg-bg"><LoadingState label="Loading Spaces…" /></div>
  }
  if (error) {
    return (
      <div className="flex-1 flex flex-col items-center justify-center gap-3 text-sm bg-bg">
        <p className="text-danger">{error}</p>
        <Button variant="secondary" size="sm" onClick={() => { setError(null); setLoading(true); loadChannels() }}>Retry</Button>
      </div>
    )
  }

  const mainPane = view === 'chat'
    ? <ChannelView channel={activeChannel} currentUser={currentUser} roster={roster} onStatusChange={handleSetStatus} onMobileBack={() => setMobilePane('list')} />
    : <ActivityView mode={view} channels={channels} currentUser={currentUser} onOpenChannel={selectChannel} />

  const totalMentions = channels.reduce((n, ch) => n + (mentionCount(ch) || 0), 0)

  return (
    <div className="flex flex-1 min-h-0 bg-bg">
      {/* Far-left global rail (desktop) */}
      <div className="relative">
        <WorkspaceRail
          view={view}
          activeChat={!!activeChannel}
          mentions={totalMentions}
          onHome={() => { setView('chat'); setMobilePane('main'); if (activeChannel) navigate(channelPath(activeChannel)); else navigate('/') }}
          onActivity={() => { setView('activity'); setMobilePane('main') }}
          navigate={navigate}
          localStatus={localStatus}
          onOpenStatus={() => setRailStatusOpen((v) => !v)}
        />
        {railStatusOpen && (
          <div className="hidden md:block absolute left-[52px] bottom-3 z-50">
            <StatusPicker currentStatus={localStatus} currentText={localStatusText} onStatusChange={handleSetStatus} onClose={() => setRailStatusOpen(false)} />
          </div>
        )}
      </div>

      {/* Sidebar — drawer on mobile (clears the fixed bottom nav) */}
      <div className={['md:flex md:w-auto pb-14 md:pb-0', mobilePane === 'list' ? 'flex w-full' : 'hidden'].join(' ')}>
        <SpacesSidebar
          channels={channels}
          activeId={activeChannel?.id}
          view={view}
          onSelect={selectChannel}
          onSetView={(v) => { setView(v); setMobilePane('main') }}
          onRefresh={loadChannels}
          seenMap={seenMap}
          roster={roster}
          localStatus={localStatus}
          localStatusText={localStatusText}
          onSetStatus={handleSetStatus}
          onOpenQuick={() => setQuickOpen(true)}
          onOpenHelp={() => setHelpOpen(true)}
        />
      </div>

      {/* Main pane — reserve the bottom-nav height on mobile so the composer
          is never hidden behind the fixed nav (md+ has no bottom nav). */}
      <div className={['flex-1 min-h-0 flex-col pb-14 md:pb-0', mobilePane === 'main' ? 'flex' : 'hidden md:flex'].join(' ')}>
        {mainPane}
      </div>

      {/* Mobile bottom nav */}
      <nav className="md:hidden fixed bottom-0 inset-x-0 z-30 bg-paper border-t border-line flex items-stretch h-14" aria-label="Primary">
        {[
          { key: 'list', label: 'Channels', Icon: Home, onClick: () => setMobilePane('list') },
          { key: 'dms', label: 'DMs', Icon: AtSign, onClick: () => { setMobilePane('list') } },
          { key: 'activity', label: 'Activity', Icon: Bell, onClick: () => { setView('activity'); setMobilePane('main') } },
          { key: 'you', label: 'You', Icon: User, onClick: () => setMobilePane('list') },
        ].map(({ key, label, Icon, onClick }) => (
          <button key={key} type="button" onClick={onClick} className="flex-1 flex flex-col items-center justify-center gap-0.5 text-ink-faint hover:text-ink active:bg-accent-tint transition-colors min-w-[44px]">
            <Icon size={18} />
            <span className="text-[10px] tracking-tightish">{label}</span>
          </button>
        ))}
      </nav>

      <QuickSwitcher open={quickOpen} channels={channels} onClose={() => setQuickOpen(false)} onSelect={selectChannel} />
      <ShortcutsHelp open={helpOpen} onClose={() => setHelpOpen(false)} />
    </div>
  )
}
