/**
 * ActivityView — sidebar-reachable "Threads" and "Activity / Mentions" panes.
 *
 * Both are best-effort: they scan the in-browser CRDT message store for every
 * channel the user has opened this session (the store accumulates messages as
 * channels are visited). No extra backend route is required.
 *
 *   - mode="threads"  → top-level messages with replies the user is part of.
 *   - mode="activity" → messages that @-mention the user or @channel.
 */
import { Hash, Lock, AtSign, MessageSquare, Bell } from 'lucide-react'
import { getDefaultStore } from '../../lib/crdt/messages.js'
import RichMessage from './RichMessage.jsx'

function ChannelGlyph({ type }) {
  if (type === 'dm') return <AtSign size={13} className="text-accent" />
  if (type === 'private') return <Lock size={13} className="text-warning" />
  return <Hash size={13} className="text-ink-faint" />
}

function collectThreads(channels, currentUser, store) {
  const out = []
  for (const ch of channels) {
    const msgs = store.listMessages(ch.id)
    if (!msgs.length) continue
    const replyMap = {}
    for (const m of msgs) if (m.thread_parent) (replyMap[m.thread_parent] ||= []).push(m)
    for (const root of msgs) {
      if (root.thread_parent) continue
      const replies = replyMap[root.id]
      if (!replies?.length) continue
      const involved = root.author_id === currentUser || replies.some((r) => r.author_id === currentUser)
      if (!involved) continue
      out.push({ channel: ch, root, replyCount: replies.length, last: replies[replies.length - 1] })
    }
  }
  return out.sort((a, b) => (a.last.created_at < b.last.created_at ? 1 : -1))
}

function collectActivity(channels, currentUser, store) {
  const out = []
  const needle = `<@${currentUser}>`
  for (const ch of channels) {
    for (const m of store.listMessages(ch.id)) {
      if (m.state === 'deleted' || m.author_id === currentUser) continue
      const b = m.body || ''
      if (b.includes(needle) || b.includes('@channel') || b.includes('<@channel>')) {
        out.push({ channel: ch, msg: m })
      }
    }
  }
  return out.sort((a, b) => (a.msg.created_at < b.msg.created_at ? 1 : -1))
}

export default function ActivityView({ mode = 'activity', channels = [], currentUser = 'me', onOpenChannel }) {
  const store = getDefaultStore()
  const isThreads = mode === 'threads'
  const items = isThreads
    ? collectThreads(channels, currentUser, store)
    : collectActivity(channels, currentUser, store)

  return (
    <div className="flex-1 flex flex-col min-h-0 bg-bg">
      <header className="flex items-center gap-2 h-11 px-4 bg-paper border-b border-line flex-shrink-0">
        {isThreads ? <MessageSquare size={15} className="text-ink-muted" /> : <Bell size={15} className="text-ink-muted" />}
        <span className="text-sm font-semibold text-ink tracking-tightish">{isThreads ? 'Threads' : 'Activity'}</span>
        <span className="text-2xs text-ink-faint">{items.length}</span>
      </header>

      <div className="flex-1 overflow-y-auto">
        {items.length === 0 ? (
          <div className="h-full flex items-center justify-center px-6 text-center">
            <p className="text-sm text-ink-faint font-serif italic">
              {isThreads
                ? 'No threads you follow yet. Reply in a thread to see it here.'
                : 'No mentions yet. Messages that @-mention you will appear here.'}
            </p>
          </div>
        ) : (
          <ul className="divide-y divide-line">
            {items.map((it, i) => {
              const m = isThreads ? it.root : it.msg
              return (
                <li key={`${it.channel.id}-${m.id}-${i}`}>
                  <button
                    type="button"
                    onClick={() => onOpenChannel(it.channel)}
                    className="w-full text-left px-4 py-3 hover:bg-bg-elev2 transition-colors"
                  >
                    <div className="flex items-center gap-1.5 mb-1">
                      <ChannelGlyph type={it.channel.type} />
                      <span className="text-2xs text-ink-faint tracking-tightish">{it.channel.name}</span>
                      <span className="text-2xs text-ink-faint ml-auto">{m.author_id}</span>
                    </div>
                    <RichMessage body={m.body} />
                    {isThreads && (
                      <p className="mt-1 text-2xs text-accent-press flex items-center gap-1">
                        <MessageSquare size={11} /> {it.replyCount} {it.replyCount === 1 ? 'reply' : 'replies'}
                      </p>
                    )}
                  </button>
                </li>
              )
            })}
          </ul>
        )}
      </div>
    </div>
  )
}
