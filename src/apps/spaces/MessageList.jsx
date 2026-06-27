/**
 * MessageList — Slack/Google-Chat-style message stream.
 *
 *   - Messages grouped by author: consecutive messages from the same author
 *     within a few minutes collapse the avatar/name header into a dense row
 *     (timestamp shows in the left gutter on hover).
 *   - Day separators with a hairline rule.
 *   - Per-message hover toolbar: react, reply-in-thread, edit, delete, pin,
 *     copy-link, more-menu.
 *   - Emoji reactions (counts, add, toggle), best-effort link previews.
 *   - A "new messages" unread divider at the first unread message.
 */
import { useState, useRef, useCallback, useEffect } from 'react'
import {
  MoreHorizontal, MessageSquare, Pencil, Trash2, X, Check, Smile, Pin, Link as LinkIcon,
  AtSign,
} from 'lucide-react'
import { STATE_DELETED, STATE_EDITED } from '../../lib/crdt/messages.js'
import { PresenceDot } from '../../components/PresenceBar.jsx'
import RichMessage from './RichMessage.jsx'
import LinkPreview from './LinkPreview.jsx'
import EmojiPicker from './EmojiPicker.jsx'
import { PinBadge } from './PinnedPanel.jsx'
import { avatarColor } from './avatar.js'

const GROUP_WINDOW_MS = 5 * 60 * 1000

function formatTime(ts) {
  if (!ts) return ''
  return new Date(ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

function formatDate(ts) {
  if (!ts) return ''
  return new Date(ts).toLocaleDateString([], { weekday: 'long', month: 'long', day: 'numeric' })
}

function Avatar({ name, presencePeer, size = 36, onClick }) {
  const initials = (name || '?')[0].toUpperCase()
  const bg = presencePeer?.color || avatarColor(name)
  return (
    <button
      type="button"
      onClick={onClick}
      className="relative flex-shrink-0 rounded-md transition-transform duration-fast ease-out hover:scale-[1.06] focus-visible:outline-none focus-visible:shadow-focus"
      style={{ width: size, height: size }}
      aria-label={`${presencePeer?.displayName || name} — profile`}
    >
      <span
        className="w-full h-full rounded-md flex items-center justify-center text-white text-sm font-semibold tracking-tightish select-none ring-1 ring-inset ring-white/10 shadow-e1"
        style={{ backgroundColor: bg }}
      >
        {initials}
      </span>
      {presencePeer && (
        <span className="absolute -bottom-0.5 -right-0.5">
          <PresenceDot status={presencePeer.status} size={8} />
        </span>
      )}
    </button>
  )
}

// ---- ProfileCard — identity + presence popover ------------------------------
function ProfileCard({ authorId, peer, onClose }) {
  const ref = useRef(null)
  useEffect(() => {
    function onDoc(e) { if (ref.current && !ref.current.contains(e.target)) onClose() }
    function onEsc(e) { if (e.key === 'Escape') onClose() }
    const t = setTimeout(() => document.addEventListener('mousedown', onDoc), 0)
    document.addEventListener('keydown', onEsc)
    return () => { clearTimeout(t); document.removeEventListener('mousedown', onDoc); document.removeEventListener('keydown', onEsc) }
  }, [onClose])
  const name = peer?.displayName || authorId
  const bg = peer?.color || avatarColor(authorId)
  const statusLabel = { online: 'Active', away: 'Away', dnd: 'Do not disturb', 'in-a-call': 'In a call' }[peer?.status] || 'Offline'
  return (
    <div
      ref={ref}
      role="dialog"
      aria-label={`${name} profile`}
      className="absolute left-0 top-7 z-50 w-60 bg-paper border border-line rounded-lg shadow-e3 overflow-hidden animate-scale-in"
    >
      <div className="h-9 bg-gradient-to-r from-accent-tint to-accent-tint-2" />
      <div className="px-3 pb-3 -mt-5">
        <span
          className="flex items-center justify-center w-12 h-12 rounded-lg text-white text-lg font-semibold ring-2 ring-paper shadow-e1 select-none"
          style={{ backgroundColor: bg }}
        >
          {String(name)[0].toUpperCase()}
        </span>
        <p className="mt-2 text-sm font-semibold text-ink tracking-tightish truncate">{name}</p>
        <p className="text-2xs text-ink-faint truncate">@{authorId}</p>
        <div className="mt-2 flex items-center gap-1.5 text-xs text-ink-muted">
          <PresenceDot status={peer?.status || 'offline'} size={8} />
          <span>{statusLabel}</span>
          {peer?.statusText && <span className="text-ink-faint truncate">· {peer.statusText}</span>}
        </div>
      </div>
    </div>
  )
}

// ---- ReactionBar ---------------------------------------------------------------

function ReactionBar({ reactions = {}, currentUser, onToggle, onAdd }) {
  const [whoModal, setWhoModal] = useState(null)
  const entries = Object.entries(reactions)
  if (entries.length === 0 && !onAdd) return null

  return (
    <div className="flex flex-wrap items-center gap-1 mt-1.5">
      {entries.map(([emoji, { count, userIds }]) => {
        const mine = userIds.includes(currentUser)
        return (
          <div key={emoji} className="relative">
            <button
              type="button"
              onClick={() => onToggle(emoji)}
              onMouseEnter={() => setWhoModal(emoji)}
              onMouseLeave={() => setWhoModal(null)}
              className={[
                'flex items-center gap-1 text-xs px-2 py-0.5 rounded-pill border transition-colors duration-fast',
                mine
                  ? 'bg-accent-tint border-accent text-ink'
                  : 'bg-bg-elev2 border-line text-ink-muted hover:border-accent hover:bg-accent-tint',
              ].join(' ')}
              aria-label={`${emoji} reaction, ${count} ${count === 1 ? 'person' : 'people'}`}
            >
              <span>{emoji}</span>
              <span className="tabular-nums font-medium">{count}</span>
            </button>
            {whoModal === emoji && (
              <div className="absolute bottom-full mb-1 left-0 z-50 bg-ink text-paper rounded-sm px-2 py-1 text-2xs shadow-e2 whitespace-nowrap pointer-events-none animate-fade-in">
                {userIds.slice(0, 8).join(', ')}
                {userIds.length > 8 ? ` +${userIds.length - 8} more` : ''}
              </div>
            )}
          </div>
        )
      })}
      <button
        type="button"
        onClick={onAdd}
        className="flex items-center gap-0.5 text-xs px-1.5 py-0.5 rounded-pill border border-transparent text-ink-faint hover:border-line hover:bg-bg-elev2 transition-colors opacity-0 group-hover:opacity-100"
        title="Add reaction"
        aria-label="Add reaction"
      >
        <Smile size={11} />
      </button>
    </div>
  )
}

// ---- MessageItem ---------------------------------------------------------------

function MessageItem({
  msg, replies, onReply, onEdit, onDelete, onPin, onUnpin, onCopyLink,
  currentUser, roster = [], isPinned = false, reactions = {}, onReact,
  grouped = false,
}) {
  const [showMenu, setShowMenu] = useState(false)
  const [showEmojiPicker, setShowEmojiPicker] = useState(false)
  const [editing, setEditing] = useState(false)
  const [editBody, setEditBody] = useState(msg.body)
  const [showProfile, setShowProfile] = useState(false)
  const emojiAnchorRef = useRef(null)

  const isOwn = msg.author_id === currentUser
  const isDeleted = msg.state === STATE_DELETED

  function submitEdit() {
    if (editBody.trim() && editBody.trim() !== msg.body) onEdit(msg.id, editBody.trim())
    setEditing(false)
  }

  const presencePeer = roster.find((p) => p.accountId === msg.author_id || p.displayName === msg.author_id)
  const closeMenu = useCallback(() => setShowMenu(false), [])

  return (
    <div
      data-msg-id={msg.id}
      onContextMenu={(e) => { e.preventDefault(); setShowMenu(true) }}
      className={[
        'group relative flex gap-3 pl-4 pr-4 transition-colors duration-fast ease-out',
        grouped ? 'py-0.5' : 'pt-2 pb-1 mt-1',
        isOwn ? 'hover:bg-accent-tint/40' : 'hover:bg-bg-elev2',
      ].join(' ')}
    >
      <span aria-hidden className="absolute left-0 top-0 bottom-0 w-[2px] bg-accent-press opacity-0 group-hover:opacity-100 transition-opacity duration-fast" />
      {/* Gutter: avatar (group head) or hover-timestamp (grouped follow-on) */}
      {grouped ? (
        <div className="w-9 flex-shrink-0 flex items-start justify-end pr-0.5">
          <span className="text-[10px] text-ink-faint opacity-0 group-hover:opacity-100 transition-opacity tabular-nums mt-0.5">
            {formatTime(msg.created_at)}
          </span>
        </div>
      ) : (
        <div className="relative">
          <Avatar name={msg.author_id} presencePeer={presencePeer} onClick={() => setShowProfile((v) => !v)} />
          {showProfile && <ProfileCard authorId={msg.author_id} peer={presencePeer} onClose={() => setShowProfile(false)} />}
        </div>
      )}

      <div className="flex-1 min-w-0">
        {!grouped && (
          <div className="flex items-baseline gap-2 relative">
            <button
              type="button"
              onClick={() => setShowProfile((v) => !v)}
              className="font-semibold text-sm text-ink tracking-tightish hover:underline decoration-line-emphasis underline-offset-2"
            >
              {msg.author_id}
            </button>
            <span className="text-2xs text-ink-faint tabular-nums">{formatTime(msg.created_at)}</span>
            {msg.state === STATE_EDITED && <span className="text-2xs text-ink-faint italic">edited</span>}
            {isPinned && <PinBadge />}
          </div>
        )}

        {isDeleted ? (
          <p className="text-sm text-ink-faint italic font-serif">This message was deleted.</p>
        ) : editing ? (
          <div className="mt-1 flex gap-2 items-end">
            <textarea
              className="flex-1 bg-paper border border-accent rounded-sm px-2 py-1.5 text-sm resize-none outline-none focus:shadow-focus text-ink"
              rows={2}
              value={editBody}
              onChange={(e) => setEditBody(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); submitEdit() }
                if (e.key === 'Escape') setEditing(false)
              }}
              autoFocus
            />
            <button type="button" onClick={submitEdit} className="p-1.5 rounded-sm bg-accent text-white hover:bg-accent-hover transition-colors" title="Save">
              <Check size={14} />
            </button>
            <button type="button" onClick={() => setEditing(false)} className="p-1.5 rounded-sm bg-bg-elev2 text-ink-muted border border-line hover:bg-paper transition-colors" title="Cancel">
              <X size={14} />
            </button>
          </div>
        ) : (
          <>
            {grouped && msg.state === STATE_EDITED && (
              <span className="float-right text-2xs text-ink-faint italic ml-2">edited</span>
            )}
            <RichMessage body={msg.body} members={roster} />
            <LinkPreview body={msg.body} />
          </>
        )}

        {!isDeleted && (
          <ReactionBar
            reactions={reactions}
            currentUser={currentUser}
            onToggle={(emoji) => onReact(msg.id, emoji)}
            onAdd={() => setShowEmojiPicker(true)}
          />
        )}

        {replies.length > 0 && !isDeleted && (
          <button
            type="button"
            onClick={() => onReply(msg)}
            className="mt-1.5 inline-flex items-center gap-1.5 text-2xs text-accent-press hover:underline font-medium tracking-tightish transition-colors"
          >
            <MessageSquare size={11} />
            {replies.length} {replies.length === 1 ? 'reply' : 'replies'}
          </button>
        )}

        {!isDeleted && replies.length === 0 && (
          <button
            type="button"
            onClick={() => onReply(msg)}
            className="mt-1 inline-flex items-center gap-1 text-2xs text-ink-faint hover:text-accent-press opacity-0 group-hover:opacity-100 transition-[opacity,color] duration-fast ease-out"
          >
            <MessageSquare size={11} /> Reply in thread
          </button>
        )}
      </div>

      {/* Hover action toolbar */}
      {!isDeleted && (
        <div className="absolute right-3 -top-3 flex items-center gap-0.5 bg-paper border border-line rounded-lg shadow-e2 px-0.5 py-0.5 opacity-0 translate-y-0.5 group-hover:opacity-100 group-hover:translate-y-0 transition-[opacity,transform] duration-fast ease-out">
          <div className="relative" ref={emojiAnchorRef}>
            <button type="button" onClick={() => setShowEmojiPicker((v) => !v)} className="p-1 rounded-sm text-ink-faint hover:text-ink hover:bg-accent-tint transition-colors" title="React" aria-label="Add reaction">
              <Smile size={14} />
            </button>
            {showEmojiPicker && (
              <div className="absolute right-0 bottom-full mb-1 z-50">
                <EmojiPicker onPick={(emoji) => { onReact(msg.id, emoji); setShowEmojiPicker(false) }} onClose={() => setShowEmojiPicker(false)} />
              </div>
            )}
          </div>
          <button type="button" onClick={() => onReply(msg)} className="p-1 rounded-sm text-ink-faint hover:text-ink hover:bg-accent-tint transition-colors" title="Reply in thread" aria-label="Reply in thread">
            <MessageSquare size={14} />
          </button>
          <button type="button" onClick={() => onCopyLink?.(msg)} className="p-1 rounded-sm text-ink-faint hover:text-ink hover:bg-accent-tint transition-colors" title="Copy link" aria-label="Copy link to message">
            <LinkIcon size={14} />
          </button>
          <button type="button" onClick={() => setShowMenu((v) => !v)} className="p-1 rounded-sm text-ink-faint hover:text-ink hover:bg-accent-tint transition-colors" title="More" aria-label="Message actions">
            <MoreHorizontal size={14} />
          </button>
          {showMenu && (
            <div className="absolute right-0 top-8 z-10 bg-paper border border-line rounded-md shadow-e2 py-1 min-w-[160px] animate-scale-in" onMouseLeave={closeMenu}>
              {isOwn && (
                <button type="button" onClick={() => { setEditing(true); setEditBody(msg.body); setShowMenu(false) }} className="w-full flex items-center gap-2 px-3 py-1.5 text-xs text-ink-muted hover:bg-accent-tint hover:text-ink transition-colors">
                  <Pencil size={11} /> Edit
                </button>
              )}
              <button type="button" onClick={() => { onCopyLink?.(msg); setShowMenu(false) }} className="w-full flex items-center gap-2 px-3 py-1.5 text-xs text-ink-muted hover:bg-accent-tint hover:text-ink transition-colors">
                <LinkIcon size={11} /> Copy link
              </button>
              {!isPinned ? (
                <button type="button" onClick={() => { onPin(msg); setShowMenu(false) }} className="w-full flex items-center gap-2 px-3 py-1.5 text-xs text-ink-muted hover:bg-accent-tint hover:text-ink transition-colors">
                  <Pin size={11} /> Pin to channel
                </button>
              ) : (
                <button type="button" onClick={() => { onUnpin(msg.id); setShowMenu(false) }} className="w-full flex items-center gap-2 px-3 py-1.5 text-xs text-ink-muted hover:bg-accent-tint hover:text-ink transition-colors">
                  <Pin size={11} /> Unpin
                </button>
              )}
              {isOwn && (
                <button type="button" onClick={() => { onDelete(msg.id); setShowMenu(false) }} className="w-full flex items-center gap-2 px-3 py-1.5 text-xs text-danger hover:bg-danger-bg transition-colors">
                  <Trash2 size={11} /> Delete
                </button>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

// ---- MessageList (exported) ---------------------------------------------------

export default function MessageList({
  messages, onReply, onEdit, onDelete, onPin, onUnpin, onReact, onCopyLink,
  currentUser, roster = [], pinnedIds = new Set(), reactions = {},
  highlightId = null, lastReadClock = null,
}) {
  if (!messages || messages.length === 0) {
    return (
      <div className="flex-1 flex items-center justify-center text-ink-faint text-sm bg-bg px-6">
        <div className="text-center">
          <MessageSquare size={28} className="text-ink-faint mx-auto mb-3" />
          <p className="font-serif italic">No messages yet. Be the first to say something.</p>
        </div>
      </div>
    )
  }

  const topLevel = messages.filter((m) => !m.thread_parent)
  const replyMap = {}
  messages.filter((m) => m.thread_parent).forEach((r) => {
    if (!replyMap[r.thread_parent]) replyMap[r.thread_parent] = []
    replyMap[r.thread_parent].push(r)
  })

  // First unread = first top-level message authored by someone else whose clock
  // is strictly greater than the last-read clock.
  let firstUnreadId = null
  if (lastReadClock) {
    for (const m of topLevel) {
      if (m.author_id !== currentUser && m.seq_clock > lastReadClock) { firstUnreadId = m.id; break }
    }
  }

  let lastDate = null
  let prev = null

  return (
    <div className="flex-1 overflow-y-auto py-2 bg-bg">
      {topLevel.map((msg) => {
        const date = formatDate(msg.created_at)
        const showDate = date !== lastDate
        lastDate = date

        const sameAuthor = prev && prev.author_id === msg.author_id
        const close = prev && (new Date(msg.created_at) - new Date(prev.created_at)) < GROUP_WINDOW_MS
        const grouped = !showDate && sameAuthor && close && msg.author_id !== undefined
        const showUnread = msg.id === firstUnreadId
        prev = msg

        return (
          <div key={msg.id} className={highlightId === msg.id ? 'ring-2 ring-accent ring-inset rounded-sm' : ''}>
            {showDate && (
              <div className="flex items-center gap-3 px-4 py-3">
                <div className="flex-1 h-px bg-line" />
                <span className="font-serif italic text-2xs text-ink-faint uppercase tracking-eyebrow" style={{ fontVariant: 'small-caps' }}>
                  {date}
                </span>
                <div className="flex-1 h-px bg-line" />
              </div>
            )}
            {showUnread && (
              <div className="flex items-center gap-2 px-4 py-1" data-testid="unread-divider">
                <div className="flex-1 h-px bg-danger/60" />
                <span className="text-2xs font-semibold text-danger uppercase tracking-eyebrow">New</span>
              </div>
            )}
            <MessageItem
              msg={msg}
              replies={replyMap[msg.id] || []}
              onReply={onReply}
              onEdit={onEdit}
              onDelete={onDelete}
              onPin={onPin}
              onUnpin={onUnpin}
              onReact={onReact || (() => {})}
              onCopyLink={onCopyLink}
              currentUser={currentUser}
              roster={roster}
              isPinned={pinnedIds.has(msg.id)}
              reactions={reactions[msg.id] || {}}
              grouped={grouped}
            />
          </div>
        )
      })}
    </div>
  )
}
