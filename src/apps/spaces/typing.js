/**
 * typing.js — best-effort typing indicators.
 *
 * There is no realtime typing channel in the REST/poll backend, so we use a
 * BroadcastChannel (with a localStorage fallback) keyed per channel. Each open
 * tab announces "<who> is typing in <channel>" on keypress; other tabs collect
 * announcements newer than TYPING_TTL and render them. This is genuinely live
 * across multiple windows of the same self-hosted instance — a believable
 * best-effort without inventing a backend route.
 */
import { useEffect, useRef, useState } from 'react'

const TYPING_TTL = 4000
const CHANNEL_NAME = 'vulos-talk-typing'

// Per-tab identity so two tabs (even as the same account) see each other type.
const TAB_ID = Math.random().toString(36).slice(2, 10)

function makeBus() {
  if (typeof BroadcastChannel !== 'undefined') {
    try { return new BroadcastChannel(CHANNEL_NAME) } catch { /* fall through */ }
  }
  // Fallback: a no-op bus (single-tab / unsupported env). Typing simply won't
  // surface, which is fine — the UI degrades gracefully.
  return { postMessage() {}, close() {}, onmessage: null }
}

export function useTyping(channelId, currentUserLabel = 'Someone') {
  const [typing, setTyping] = useState([]) // [{ tabId, label, ts }]
  const busRef = useRef(null)
  const lastSentRef = useRef(0)

  useEffect(() => {
    const bus = makeBus()
    busRef.current = bus
    function onMsg(e) {
      const d = e.data
      if (!d || d.channelId !== channelId || d.tabId === TAB_ID) return
      setTyping((prev) => {
        const others = prev.filter((p) => p.tabId !== d.tabId)
        return [...others, { tabId: d.tabId, label: d.label, ts: Date.now() }]
      })
    }
    bus.onmessage = onMsg
    bus.addEventListener?.('message', onMsg)

    const sweep = setInterval(() => {
      setTyping((prev) => prev.filter((p) => Date.now() - p.ts < TYPING_TTL))
    }, 1000)

    return () => {
      clearInterval(sweep)
      bus.onmessage = null
      bus.removeEventListener?.('message', onMsg)
      bus.close?.()
    }
  }, [channelId])

  // Clear stale entries when switching channels.
  useEffect(() => { setTyping([]) }, [channelId])

  function notifyTyping() {
    const now = Date.now()
    if (now - lastSentRef.current < 1200) return // throttle
    lastSentRef.current = now
    busRef.current?.postMessage({ channelId, tabId: TAB_ID, label: currentUserLabel })
  }

  const labels = [...new Set(typing.map((t) => t.label))]
  return { typingLabels: labels, notifyTyping }
}
