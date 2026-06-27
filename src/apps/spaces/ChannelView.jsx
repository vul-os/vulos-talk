/**
 * ChannelView — message stream + composer for a single channel/DM.
 *
 * Slack/Google-Chat-class features:
 *   - Author-grouped, day-separated message stream (MessageList)
 *   - Hover toolbar, reactions, threads, pins, search, mentions (existing)
 *   - /slash-command autocomplete in the composer (GET /api/spaces/commands)
 *   - @mention autocomplete (MentionPicker)
 *   - Best-effort typing indicators (cross-tab BroadcastChannel)
 *   - Read/unread with a "New" divider + jump-to-unread button
 *   - Best-effort link previews, copy-message-link
 *   - Huddle button → /meet/:id (see TODO(seam-C))
 *   - Thread panel with an "also send to channel" checkbox
 *   - Responsive: 3-pane desktop, full-screen composer + overlay thread on mobile
 */
import { useEffect, useRef, useState, useCallback } from 'react'
import {
  Send, Hash, Lock, AtSign, X, MessageSquare, ChevronRight, Search,
  Pin, Bell, UserPlus, AlignLeft, Eye, Headphones, ArrowDown, ArrowLeft, Smile, Slash,
} from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import MessageList from './MessageList.jsx'
import MentionPicker, { parseMentionQuery, insertMention } from './MentionPicker.jsx'
import SlashCommandPicker, { parseSlashQuery, completeSlash } from './SlashCommandPicker.jsx'
import SearchBar from './SearchBar.jsx'
import PinnedPanel from './PinnedPanel.jsx'
import { FileUploadZone, PendingFileList } from './FileUpload.jsx'
import NotifPrefsPopover, { useNotifPref } from './NotifPrefs.jsx'
import RichMessage from './RichMessage.jsx'
import EmojiPicker from './EmojiPicker.jsx'
import { useTyping } from './typing.js'
import { api } from '../../lib/api.js'
import { toast } from '../../lib/toast.jsx'
import { getDefaultStore, STATE_DELETED } from '../../lib/crdt/messages.js'
import { PresenceDot } from '../../components/PresenceBar.jsx'
import { IconButton, Input, Modal, Topbar, Button } from '../../components/ui'

const POLL_INTERVAL_MS = 3000
const AUTO_AWAY_MS = 10 * 60 * 1000

function ChannelIcon({ type, size = 15 }) {
  if (type === 'dm') return <AtSign size={size} className="text-accent" />
  if (type === 'private') return <Lock size={size} className="text-warning" />
  return <Hash size={size} className="text-ink-faint" />
}

function formatTime(ts) {
  if (!ts) return ''
  return new Date(ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

// ---- local reactions store ---------------------------------------------------

function mergeReactions(current, msgId, emoji, currentUser, toggle) {
  const bucket = { ...(current[msgId] || {}) }
  const existing = bucket[emoji] || { count: 0, userIds: [] }
  if (toggle) {
    if (existing.userIds.includes(currentUser)) {
      const userIds = existing.userIds.filter((u) => u !== currentUser)
      if (userIds.length === 0) {
        const { [emoji]: _, ...rest } = bucket
        return { ...current, [msgId]: rest }
      }
      return { ...current, [msgId]: { ...bucket, [emoji]: { count: userIds.length, userIds } } }
    }
    const userIds = [...existing.userIds, currentUser]
    return { ...current, [msgId]: { ...bucket, [emoji]: { count: userIds.length, userIds } } }
  }
  if (!existing.userIds.includes(currentUser)) {
    const userIds = [...existing.userIds, currentUser]
    return { ...current, [msgId]: { ...bucket, [emoji]: { count: userIds.length, userIds } } }
  }
  return current
}

// ---- TypingIndicator ---------------------------------------------------------

function TypingIndicator({ labels = [] }) {
  if (labels.length === 0) return null
  const text =
    labels.length === 1 ? `${labels[0]} is typing…`
      : labels.length === 2 ? `${labels[0]} and ${labels[1]} are typing…`
        : 'Several people are typing…'
  return (
    <div className="px-4 h-5 flex items-center gap-1.5 text-2xs text-ink-faint" aria-live="polite">
      <span className="flex items-end gap-[3px] pb-0.5">
        <span className="typing-dot" />
        <span className="typing-dot" />
        <span className="typing-dot" />
      </span>
      <span className="italic">{text}</span>
    </div>
  )
}

// ---- ThreadPanel -------------------------------------------------------------

function ThreadPanel({ root, replies = [], onSend, onClose, currentUser, mobile = false }) {
  const [body, setBody] = useState('')
  const [alsoSend, setAlsoSend] = useState(false)
  const [sending, setSending] = useState(false)
  const bottomRef = useRef(null)

  useEffect(() => { bottomRef.current?.scrollIntoView({ block: 'end' }) }, [replies.length])
  if (!root) return null

  const handleSend = async () => {
    const text = body.trim()
    if (!text || sending) return
    setSending(true)
    try {
      await onSend(text, root.id, alsoSend)
      setBody('')
    } finally {
      setSending(false)
    }
  }

  return (
    <aside
      className={[
        'flex flex-col overflow-hidden bg-bg-elev2 animate-slide-in-right',
        mobile
          ? 'fixed inset-0 z-40'
          : 'w-80 flex-shrink-0 border-l border-line',
      ].join(' ')}
    >
      <div className="flex items-center justify-between px-3 h-11 border-b border-line bg-paper flex-shrink-0">
        <div className="flex items-center gap-2 min-w-0">
          {mobile && (
            <IconButton size="sm" title="Back" onClick={onClose}><ArrowLeft size={15} /></IconButton>
          )}
          <MessageSquare size={14} className="text-ink-muted" />
          <span className="text-sm font-semibold text-ink tracking-tightish">Thread</span>
          {replies.length > 0 && (
            <span className="text-2xs bg-bg-elev2 text-ink-faint rounded-pill px-1.5 py-0.5 font-medium">{replies.length}</span>
          )}
        </div>
        <IconButton size="sm" title="Close thread" onClick={onClose}><X size={14} /></IconButton>
      </div>

      <div className="px-3 py-3 bg-paper border-b border-line flex-shrink-0">
        <div className="flex items-baseline gap-2 mb-1">
          <span className="text-xs font-semibold text-ink tracking-tightish">{root.author_id}</span>
          <span className="text-2xs text-ink-faint">{formatTime(root.created_at)}</span>
        </div>
        <RichMessage body={root.body} />
      </div>

      <div className="flex-1 overflow-y-auto px-3 py-3 space-y-3">
        {replies.length === 0 && (
          <p className="text-xs text-ink-faint text-center py-6 font-serif italic">No replies yet. Start the thread.</p>
        )}
        {replies.map((r) => {
          const isOwn = r.author_id === currentUser
          const isDeleted = r.state === STATE_DELETED
          return (
            <div key={r.id} className="flex flex-col gap-0.5 animate-rise-in">
              <div className="flex items-baseline gap-2">
                <span className={`text-xs font-semibold tracking-tightish ${isOwn ? 'text-accent-press' : 'text-ink'}`}>{r.author_id}</span>
                <span className="text-2xs text-ink-faint">{formatTime(r.created_at)}</span>
              </div>
              {isDeleted
                ? <p className="text-xs text-ink-faint italic">This message was deleted.</p>
                : <RichMessage body={r.body} />}
            </div>
          )
        })}
        <div ref={bottomRef} />
      </div>

      <div className="p-3 border-t border-line bg-paper flex-shrink-0 space-y-2">
        <textarea
          value={body}
          onChange={(e) => setBody(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); handleSend() } }}
          rows={2}
          placeholder="Reply in thread…"
          className="w-full text-sm bg-bg-elev2 border border-line rounded-sm px-2 py-1.5 resize-none outline-none focus:border-accent focus:shadow-focus focus:bg-paper transition-colors text-ink placeholder:text-ink-faint"
        />
        <label className="flex items-center gap-2 text-2xs text-ink-muted cursor-pointer select-none">
          <input type="checkbox" checked={alsoSend} onChange={(e) => setAlsoSend(e.target.checked)} className="accent-[var(--accent)]" />
          Also send to channel
        </label>
        <button
          type="button"
          onClick={handleSend}
          disabled={!body.trim() || sending}
          className="w-full h-7 text-xs font-medium bg-accent text-white rounded-sm hover:bg-accent-hover disabled:opacity-50 transition-colors tracking-tightish"
        >
          {sending ? 'Sending…' : 'Reply'}
        </button>
      </div>
    </aside>
  )
}

// ---- InviteMemberModal -------------------------------------------------------

function InviteMemberModal({ open, onClose, channelId, roster = [], onInvited }) {
  const [accountId, setAccountId] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(null)
  const [success, setSuccess] = useState(null)

  function handleAccountIdChange(e) {
    setAccountId(e.target.value)
    setError(null); setSuccess(null)
  }
  function applySuggestion(peer) {
    setAccountId(peer.accountId)
    setDisplayName(peer.displayName || '')
  }
  async function submit(e) {
    e.preventDefault()
    const id = accountId.trim()
    if (!id) return
    setLoading(true); setError(null); setSuccess(null)
    try {
      await api.spacesInviteMember(channelId, id, displayName.trim())
      setSuccess(`${displayName.trim() || id} added to the channel.`)
      setAccountId(''); setDisplayName('')
      onInvited?.()
    } catch (err) {
      if (err.message?.includes('409') || err.message?.includes('already')) setError(`${id} is already a member of this channel.`)
      else setError(err.message || 'Invite failed. Please try again.')
    } finally { setLoading(false) }
  }
  function handleClose() {
    setAccountId(''); setDisplayName(''); setError(null); setSuccess(null); onClose()
  }

  const rosterSuggestions = accountId.trim().length > 0
    ? roster.filter((p) =>
        (p.accountId?.toLowerCase().includes(accountId.trim().toLowerCase()) ||
         p.displayName?.toLowerCase().includes(accountId.trim().toLowerCase())) &&
        p.accountId !== accountId.trim()).slice(0, 5)
    : []

  return (
    <Modal open={open} onClose={handleClose} title="Add members">
      <form onSubmit={submit}>
        <Modal.Body className="space-y-4">
          {error && <p role="alert" className="text-xs text-danger bg-danger-bg rounded-sm px-3 py-2">{error}</p>}
          {success && <p role="status" className="text-xs text-success bg-success-bg rounded-sm px-3 py-2">{success}</p>}
          <div>
            <Input label="Account ID" placeholder="e.g. alice or alice@vulos.org" value={accountId} onChange={handleAccountIdChange} leading={<AtSign size={13} />} autoFocus />
            {rosterSuggestions.length > 0 && (
              <ul role="listbox" aria-label="Suggested members" className="mt-1 border border-line rounded-md bg-paper shadow-e2 overflow-hidden">
                {rosterSuggestions.map((p) => (
                  <li key={p.accountId}>
                    <button type="button" onClick={() => applySuggestion(p)} className="w-full flex items-center gap-2 px-3 py-2 text-sm text-left hover:bg-accent-tint transition-colors">
                      <PresenceDot status={p.status} size={7} />
                      <span className="font-medium text-ink tracking-tightish">{p.displayName || p.accountId}</span>
                      {p.displayName && <span className="text-ink-faint text-2xs ml-auto">{p.accountId}</span>}
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>
          <Input label="Their name (optional)" placeholder="e.g. Jane Doe" value={displayName} onChange={(e) => setDisplayName(e.target.value)} leading={<UserPlus size={13} />} />
          <p className="text-2xs text-ink-faint leading-relaxed">They will be added immediately and can read the channel's history.</p>
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" type="button" onClick={handleClose}>Cancel</Button>
          <Button variant="primary" type="submit" disabled={loading || !accountId.trim()}>{loading ? 'Adding…' : 'Add to channel'}</Button>
        </Modal.Footer>
      </form>
    </Modal>
  )
}

// ---- ChannelView -------------------------------------------------------------

export default function ChannelView({ channel, currentUser, roster = [], onStatusChange, onMobileBack }) {
  const navigate = useNavigate()
  const [messages, setMessages] = useState([])
  const [body, setBody] = useState('')
  const [sending, setSending] = useState(false)
  const [replyTo, setReplyTo] = useState(null)
  const [threadRoot, setThreadRoot] = useState(null)
  const [error, setError] = useState(null)
  const [showSearch, setShowSearch] = useState(false)
  const [highlightId, setHighlightId] = useState(null)
  const [showPinned, setShowPinned] = useState(false)
  const [pinnedMsgs, setPinnedMsgs] = useState([])
  const [pinnedIds, setPinnedIds] = useState(new Set())
  const [reactions, setReactions] = useState({})
  const [showNotifPrefs, setShowNotifPrefs] = useState(false)
  const [pendingFiles, setPendingFiles] = useState([])
  const [previewMode, setPreviewMode] = useState(false)
  const [members, setMembers] = useState([])
  const [showInvite, setShowInvite] = useState(false)
  const [showComposerEmoji, setShowComposerEmoji] = useState(false)
  const [mentionQuery, setMentionQuery] = useState(null)
  const [slashQuery, setSlashQuery] = useState(null)
  const [commands, setCommands] = useState([])
  const [lastReadClock, setLastReadClock] = useState(null)

  const bottomRef = useRef(null)
  const pollRef = useRef(null)
  const composeRef = useRef(null)
  const awayTimerRef = useRef(null)
  const crdtStore = getDefaultStore()

  const selfLabel = (roster.find((p) => p.accountId === currentUser)?.displayName) || currentUser || 'Someone'
  const { typingLabels, notifyTyping } = useTyping(channel?.id || '', selfLabel)

  const { pref: notifPref, setPref: setNotifPref } = useNotifPref(
    channel?.id || '', channel?.type || 'public', members.length,
  )

  function resetAwayTimer() {
    if (awayTimerRef.current) clearTimeout(awayTimerRef.current)
    awayTimerRef.current = setTimeout(() => { onStatusChange?.('away', '') }, AUTO_AWAY_MS)
  }

  useEffect(() => {
    const handler = () => resetAwayTimer()
    window.addEventListener('mousemove', handler)
    window.addEventListener('keydown', handler)
    resetAwayTimer()
    return () => {
      window.removeEventListener('mousemove', handler)
      window.removeEventListener('keydown', handler)
      if (awayTimerRef.current) clearTimeout(awayTimerRef.current)
    }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  // Slash commands (feature-detect; 404 → empty).
  useEffect(() => {
    let alive = true
    api.spacesListCommands().then((cmds) => { if (alive) setCommands(cmds || []) }).catch(() => { if (alive) setCommands([]) })
    return () => { alive = false }
  }, [])

  const loadMessages = useCallback(async () => {
    if (!channel) return
    try {
      const msgs = await api.spacesListMessages(channel.id)
      crdtStore.mergeOps(msgs.map((m) => ({
        op: m.state === 'deleted' ? 'tombstone' : m.state === 'edited' ? 'edit' : 'append',
        channel_id: m.channel_id, msg: m, applied_at: m.updated_at,
      })))
      setMessages(crdtStore.listMessages(channel.id))
    } catch (e) {
      console.warn('[ChannelView] poll error', e)
    }
  }, [channel, crdtStore])

  const loadMembers = useCallback(async () => {
    if (!channel) return
    try { setMembers((await api.spacesListMembers(channel.id)) || []) } catch {}
  }, [channel])

  const loadPins = useCallback(async () => {
    if (!channel) return
    try {
      const pins = await api.spacesPinsList(channel.id)
      setPinnedMsgs(pins || [])
      setPinnedIds(new Set((pins || []).map((p) => p.message_id)))
    } catch {}
  }, [channel])

  const loadReactions = useCallback(async () => {
    if (!channel) return
    try {
      const rxns = await api.spacesListReactions(channel.id)
      const byMsg = {}
      for (const r of rxns || []) {
        if (!byMsg[r.message_id]) byMsg[r.message_id] = {}
        if (!byMsg[r.message_id][r.emoji]) byMsg[r.message_id][r.emoji] = { count: 0, userIds: [] }
        if (!byMsg[r.message_id][r.emoji].userIds.includes(r.user_id)) {
          byMsg[r.message_id][r.emoji].userIds.push(r.user_id)
          byMsg[r.message_id][r.emoji].count++
        }
      }
      setReactions(byMsg)
    } catch {}
  }, [channel])

  const loadReadState = useCallback(async () => {
    if (!channel) return
    try {
      const rs = await api.spacesGetReadState(channel.id)
      setLastReadClock(rs?.clock || rs?.seq_clock || null)
    } catch { setLastReadClock(null) }
  }, [channel])

  useEffect(() => {
    setMessages([]); setError(null); setThreadRoot(null); setShowSearch(false)
    setShowPinned(false); setPendingFiles([]); setBody(''); setMentionQuery(null); setSlashQuery(null)
    if (!channel) return
    loadMessages(); loadMembers(); loadPins(); loadReactions(); loadReadState()
    pollRef.current = setInterval(() => { loadMessages(); loadReactions() }, POLL_INTERVAL_MS)
    return () => clearInterval(pollRef.current)
  }, [channel?.id, loadMessages, loadMembers, loadPins, loadReactions, loadReadState])

  useEffect(() => { bottomRef.current?.scrollIntoView({ behavior: 'smooth' }) }, [messages.length])

  function markReadLatest() {
    const last = messages[messages.length - 1]
    if (last) {
      api.spacesMarkRead(channel.id, last.seq_clock).catch(() => {})
      setLastReadClock(last.seq_clock)
    }
  }

  // ---- Send -------------------------------------------------------------------
  async function send() {
    const text = body.trim()
    if (!text || sending) return
    setSending(true); setError(null)
    try {
      await api.spacesSendMessage(channel.id, text, replyTo?.id || '')
      setBody(''); setReplyTo(null); setMentionQuery(null); setSlashQuery(null)
      if (composeRef.current) composeRef.current.style.height = 'auto'
      await loadMessages()
      markReadLatest()
    } catch (e) {
      setError(e.message || 'Send failed'); toast.error('Message failed to send')
    } finally { setSending(false) }
  }

  async function sendThreadReply(text, parentId, alsoSend = false) {
    setError(null)
    await api.spacesReplyThread(channel.id, parentId, text)
    if (alsoSend) { try { await api.spacesSendMessage(channel.id, text, '') } catch {} }
    await loadMessages()
  }

  async function handleEdit(msgId, newBody) {
    try { await api.spacesEditMessage(channel.id, msgId, newBody); await loadMessages() }
    catch (e) { setError(e.message || 'Edit failed') }
  }
  async function handleDelete(msgId) {
    try { await api.spacesDeleteMessage(channel.id, msgId); await loadMessages() }
    catch (e) { setError(e.message || 'Delete failed') }
  }

  async function handleReact(msgId, emoji) {
    const mine = reactions[msgId]?.[emoji]?.userIds.includes(currentUser)
    setReactions((r) => mergeReactions(r, msgId, emoji, currentUser, true))
    try {
      if (mine) await api.spacesUnreact(channel.id, msgId, emoji)
      else await api.spacesReact(channel.id, msgId, emoji)
    } catch {
      setReactions((r) => mergeReactions(r, msgId, emoji, currentUser, true))
    }
    loadReactions()
  }

  async function handlePin(msg) {
    try { await api.spacesPinMessage(channel.id, msg.id); loadPins(); toast.success('Pinned to channel') }
    catch (e) { setError(e.message || 'Pin failed') }
  }
  async function handleUnpin(msgId) {
    try { await api.spacesUnpinMessage(channel.id, msgId); loadPins() }
    catch (e) { setError(e.message || 'Unpin failed') }
  }

  function copyLink(msg) {
    const prefix = channel.type === 'dm' ? '/dm' : '/channels'
    const url = `${window.location.origin}${prefix}/${channel.id}#msg-${msg.id}`
    navigator.clipboard?.writeText(url).then(
      () => toast.success('Link copied'),
      () => toast.error('Copy failed'),
    )
  }

  function jumpToMessage(msg) {
    setShowSearch(false); setShowPinned(false)
    setHighlightId(msg.message_id || msg.id)
    setTimeout(() => {
      const el = document.querySelector(`[data-msg-id="${msg.message_id || msg.id}"]`)
      el?.scrollIntoView({ behavior: 'smooth', block: 'center' })
      setTimeout(() => setHighlightId(null), 1500)
    }, 100)
  }

  function jumpToUnread() {
    const el = document.querySelector('[data-testid="unread-divider"]')
    el?.scrollIntoView({ behavior: 'smooth', block: 'center' })
  }

  function handleDropFiles(files) { setPendingFiles((p) => [...p, ...files]) }

  async function uploadAndSend() {
    if (!body.trim() && pendingFiles.length === 0) return
    setSending(true); setError(null)
    try {
      for (const file of pendingFiles) {
        try { await api.uploadImage(file) } catch {}
        await api.spacesSendMessage(channel.id, body.trim() || `[file: ${file.name}]`, replyTo?.id || '')
      }
      if (pendingFiles.length === 0 && body.trim()) await api.spacesSendMessage(channel.id, body.trim(), replyTo?.id || '')
      setBody(''); setReplyTo(null); setPendingFiles([])
      if (composeRef.current) composeRef.current.style.height = 'auto'
      await loadMessages()
    } catch (e) { setError(e.message || 'Send failed') }
    finally { setSending(false) }
  }

  // ---- composer change: detect @mention + /slash ------------------------------
  function handleComposeChange(e) {
    const val = e.target.value
    setBody(val)
    const cursor = e.target.selectionStart
    setMentionQuery(parseMentionQuery(val, cursor))
    setSlashQuery(commands.length > 0 ? parseSlashQuery(val, cursor) : null)
    notifyTyping()
    e.target.style.height = 'auto'
    e.target.style.height = e.target.scrollHeight + 'px'
  }

  function handleMentionSelect(accountId) {
    if (!mentionQuery) return
    const cursor = composeRef.current?.selectionStart || body.length
    const mention = accountId === 'channel' ? '@channel' : `<@${accountId}>`
    setBody(insertMention(body, mentionQuery.atStart, cursor, mention))
    setMentionQuery(null)
    composeRef.current?.focus()
  }

  function handleSlashSelect(name) {
    setBody(completeSlash(body, name))
    setSlashQuery(null)
    composeRef.current?.focus()
  }

  function insertEmoji(emoji) {
    setBody((b) => b + emoji)
    setShowComposerEmoji(false)
    composeRef.current?.focus()
  }

  function startHuddle() {
    // TODO(seam-C): route huddle audio/video through vulos-meet rather than the
    // standalone Meetings flow — for now we navigate to the existing /meet/:id
    // room keyed by the channel so a huddle reuses the call surface.
    toast.info('Starting huddle…')
    navigate(`/meet/${channel.id}`)
  }

  // ---- mention/slash roster ---------------------------------------------------
  const displayRoster = (() => {
    const byId = new Map()
    for (const m of members) byId.set(m.account_id, { accountId: m.account_id, displayName: m.display_name || m.account_id, status: 'offline' })
    for (const p of roster) {
      const existing = byId.get(p.accountId) || {}
      byId.set(p.accountId, {
        ...existing, ...p,
        displayName: existing.displayName && existing.displayName !== p.accountId ? existing.displayName : (p.displayName || existing.displayName || p.accountId),
      })
    }
    return Array.from(byId.values())
  })()
  const mentionMembers = displayRoster.map((p) => ({ accountId: p.accountId, displayName: p.displayName, status: p.status }))

  function openThread(msg) { setThreadRoot(msg); setReplyTo(null) }

  if (!channel) {
    return (
      <div className="flex-1 flex items-center justify-center bg-bg px-6">
        <div className="text-center">
          <MessageSquare size={30} className="text-ink-faint mx-auto mb-3" />
          <p className="text-ink-faint text-sm font-serif italic">Select a channel or DM to start messaging.</p>
        </div>
      </div>
    )
  }

  const isDM = channel.type === 'dm'
  const liveThreadRoot = threadRoot ? messages.find((m) => m.id === threadRoot.id) || threadRoot : null
  const threadReplies = liveThreadRoot ? messages.filter((m) => m.thread_parent === liveThreadRoot.id) : []
  const desc = channel.description || ''
  const hasUnread = !!lastReadClock && messages.some((m) => !m.thread_parent && m.author_id !== currentUser && m.seq_clock > lastReadClock)

  const headerActionBtn = (active) => [
    'p-1.5 rounded-sm transition-colors',
    active ? 'bg-accent-tint text-accent-press' : 'text-ink-faint hover:text-ink hover:bg-accent-tint',
  ].join(' ')

  return (
    <div className="flex-1 flex min-h-0 bg-bg">
      <FileUploadZone onFiles={handleDropFiles}>
        <div className="flex-1 flex flex-col min-h-0">
          <Topbar
            leading={
              <span className="flex items-center gap-2 px-1 min-w-0">
                {onMobileBack && (
                  <IconButton size="sm" className="md:hidden" title="Back to channels" onClick={onMobileBack}>
                    <ArrowLeft size={16} />
                  </IconButton>
                )}
                <ChannelIcon type={channel.type} size={15} />
                <span className="font-semibold text-ink tracking-tightish text-sm truncate">{channel.name}</span>
                {desc && <span className="text-2xs text-ink-faint hidden md:inline truncate max-w-[200px]">— {desc}</span>}
                <span className="text-2xs text-ink-faint hidden sm:inline">{members.length > 0 && `${members.length} members`}</span>
              </span>
            }
            title={<span />}
            actions={
              <div className="flex items-center gap-1">
                <button type="button" onClick={startHuddle} className={headerActionBtn(false)} title="Start a huddle" aria-label="Start a huddle">
                  <Headphones size={14} />
                </button>
                <button type="button" onClick={() => setShowSearch((v) => !v)} className={headerActionBtn(showSearch)} title="Search in channel" aria-label="Search in channel">
                  <Search size={14} />
                </button>
                <button type="button" onClick={() => setShowPinned((v) => !v)} className={headerActionBtn(showPinned)} title="Pinned messages" aria-label="Pinned messages">
                  <Pin size={14} />
                  {pinnedMsgs.length > 0 && <span className="ml-0.5 text-2xs tabular-nums">{pinnedMsgs.length}</span>}
                </button>
                {channel.type === 'private' && (
                  <button type="button" onClick={() => setShowInvite(true)} className={headerActionBtn(false)} title="Add members" aria-label="Add members to channel">
                    <UserPlus size={14} />
                  </button>
                )}
                <div className="relative">
                  <button type="button" onClick={() => setShowNotifPrefs((v) => !v)} className={headerActionBtn(showNotifPrefs)} title={`Notifications: ${notifPref}`} aria-label="Notification preferences">
                    <Bell size={14} />
                  </button>
                  {showNotifPrefs && <NotifPrefsPopover pref={notifPref} onChange={setNotifPref} onClose={() => setShowNotifPrefs(false)} />}
                </div>
              </div>
            }
          />

          {showSearch && <SearchBar messages={messages} onJump={jumpToMessage} onClose={() => setShowSearch(false)} />}

          {error && (
            <div className="px-4 py-2 bg-danger-bg border-b border-line text-xs text-danger flex items-center justify-between">
              {error}
              <IconButton size="sm" onClick={() => setError(null)} title="Dismiss"><X size={12} /></IconButton>
            </div>
          )}

          <div className="relative flex-1 flex flex-col min-h-0">
            <MessageList
              messages={messages}
              onReply={openThread}
              onEdit={handleEdit}
              onDelete={handleDelete}
              onPin={handlePin}
              onUnpin={handleUnpin}
              onReact={handleReact}
              onCopyLink={copyLink}
              currentUser={currentUser || 'me'}
              roster={roster}
              pinnedIds={pinnedIds}
              reactions={reactions}
              highlightId={highlightId}
              lastReadClock={lastReadClock}
            />
            {hasUnread && (
              <button
                type="button"
                onClick={jumpToUnread}
                className="absolute top-2 left-1/2 -translate-x-1/2 z-10 flex items-center gap-1.5 bg-accent text-white text-2xs font-medium rounded-pill px-3 py-1 shadow-e2 hover:bg-accent-hover transition-colors animate-rise-in"
              >
                <ArrowDown size={12} /> New messages
              </button>
            )}
          </div>

          <div ref={bottomRef} />

          <TypingIndicator labels={typingLabels} />

          {/* Composer */}
          <div className="px-4 py-3 border-t border-line bg-paper flex-shrink-0">
            {replyTo && (
              <div className="mb-2 flex items-center gap-2 text-2xs text-ink-muted bg-accent-tint border border-line rounded-sm px-3 py-1.5">
                <ChevronRight size={11} className="text-accent" />
                <span>Replying to <span className="font-semibold text-ink">{replyTo.author_id}</span></span>
                <button type="button" onClick={() => setReplyTo(null)} className="ml-auto text-ink-faint hover:text-ink"><X size={11} /></button>
              </div>
            )}

            <PendingFileList files={pendingFiles} onRemove={(i) => setPendingFiles((f) => f.filter((_, idx) => idx !== i))} />

            <div className="flex items-center gap-1 mb-1.5">
              <button
                type="button"
                onClick={() => setPreviewMode((v) => !v)}
                className={['text-2xs px-2 py-0.5 rounded-sm border transition-colors', previewMode ? 'border-accent bg-accent-tint text-accent-press' : 'border-transparent text-ink-faint hover:text-ink'].join(' ')}
                title={previewMode ? 'Edit markdown' : 'Preview'}
              >
                {previewMode
                  ? <span className="flex items-center gap-1"><AlignLeft size={11} /> Edit</span>
                  : <span className="flex items-center gap-1"><Eye size={11} /> Preview</span>}
              </button>
              <span className="text-2xs text-ink-faint hidden sm:inline">**bold** _italic_ `code` · @ mention · / commands</span>
            </div>

            {previewMode ? (
              <div className="bg-bg-elev2 border border-line rounded-md px-3 py-2 min-h-[40px] text-sm text-ink mb-2">
                {body.trim() ? <RichMessage body={body} members={mentionMembers} /> : <span className="text-ink-faint italic text-xs">Nothing to preview.</span>}
              </div>
            ) : (
              <div className="relative flex gap-1 items-end bg-paper border border-line rounded-md focus-within:border-accent focus-within:shadow-focus transition-[border-color,box-shadow] duration-fast ease-out">
                {mentionQuery !== null && (
                  <div className="absolute bottom-full left-0 mb-1 z-50">
                    <MentionPicker members={mentionMembers} query={mentionQuery.query} onSelect={handleMentionSelect} onClose={() => setMentionQuery(null)} />
                  </div>
                )}
                {slashQuery !== null && (
                  <div className="absolute bottom-full left-0 mb-1 z-50">
                    <SlashCommandPicker commands={commands} query={slashQuery.query} onSelect={handleSlashSelect} onClose={() => setSlashQuery(null)} />
                  </div>
                )}

                <textarea
                  ref={composeRef}
                  className="flex-1 bg-transparent outline-none px-3 py-2 text-sm resize-none max-h-40 text-ink placeholder:text-ink-faint"
                  rows={1}
                  placeholder={`Message ${isDM ? '' : '#'}${channel.name}…`}
                  value={body}
                  onChange={handleComposeChange}
                  onFocus={markReadLatest}
                  onKeyDown={(e) => {
                    if (mentionQuery !== null || slashQuery !== null) {
                      if (['ArrowUp', 'ArrowDown', 'Tab', 'Enter', 'Escape'].includes(e.key)) return
                    }
                    if (e.key === 'Enter' && !e.shiftKey) {
                      e.preventDefault()
                      pendingFiles.length > 0 ? uploadAndSend() : send()
                    }
                  }}
                />

                <div className="relative flex items-center">
                  <button type="button" onClick={() => setShowComposerEmoji((v) => !v)} className="m-1 p-1.5 rounded-sm text-ink-faint hover:text-ink hover:bg-accent-tint transition-colors" title="Emoji" aria-label="Insert emoji">
                    <Smile size={16} />
                  </button>
                  {showComposerEmoji && (
                    <div className="absolute right-0 bottom-full mb-1 z-50">
                      <EmojiPicker onPick={insertEmoji} onClose={() => setShowComposerEmoji(false)} />
                    </div>
                  )}
                </div>

                <button
                  type="button"
                  onClick={pendingFiles.length > 0 ? uploadAndSend : send}
                  disabled={(!body.trim() && pendingFiles.length === 0) || sending}
                  title="Send"
                  aria-label="Send"
                  className="m-1 inline-flex items-center justify-center h-8 w-8 rounded-sm bg-accent text-white shadow-e1 hover:bg-accent-hover disabled:opacity-40 disabled:cursor-not-allowed transition-[background,opacity] duration-fast ease-out flex-shrink-0"
                >
                  <Send size={14} />
                </button>
              </div>
            )}
          </div>
        </div>
      </FileUploadZone>

      {liveThreadRoot && !showPinned && (
        <>
          <div className="hidden md:flex">
            <ThreadPanel root={liveThreadRoot} replies={threadReplies} onSend={sendThreadReply} onClose={() => setThreadRoot(null)} currentUser={currentUser || 'me'} />
          </div>
          <div className="md:hidden">
            <ThreadPanel root={liveThreadRoot} replies={threadReplies} onSend={sendThreadReply} onClose={() => setThreadRoot(null)} currentUser={currentUser || 'me'} mobile />
          </div>
        </>
      )}

      {showPinned && <PinnedPanel pinnedMsgs={pinnedMsgs} onJump={jumpToMessage} onUnpin={handleUnpin} onClose={() => setShowPinned(false)} />}

      <InviteMemberModal open={showInvite} onClose={() => setShowInvite(false)} channelId={channel.id} roster={displayRoster} onInvited={loadMembers} />
    </div>
  )
}
