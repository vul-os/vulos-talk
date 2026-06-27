/**
 * ChannelBoard — per-channel collaborative whiteboard surface.
 *
 * Mounts the @vulos/board-ui editor (Excalidraw + Yjs CRDT core) for a single
 * channel. One board per channel: boardId === channelId, so everyone viewing a
 * channel's "Board" tab edits the same shared document.
 *
 * Transport — websocket (board sync server), see Step-3 note below.
 * ───────────────────────────────────────────────────────────────────────────
 * TODO(seam): route board collab over @vulos/relay-client.
 *   The intended end-state (per board-ui's own provider.ts note) is for Talk to
 *   own the transport and pump Y.Doc diffs over the Vulos Relay peer-fabric,
 *   the same fabric calls/presence ride on — i.e. a custom Y.Doc provider that:
 *     • outbound: doc.on('update', (u, origin) => { if (origin!=='remote') relaySend(u) })
 *     • inbound:  relay 'message' bytes → Y.applyUpdate(doc, bytes, 'remote')
 *     • late-join: on a new peer, broadcast Y.encodeStateAsUpdate(doc)
 *   That requires standing up a relay FabricClient for a `board:<channelId>`
 *   session (signaling URL + identity/auth + ICE), which Talk does not wire for
 *   channels today (presence is REST-poll; calls use media-only createCall).
 *   Until that fabric path exists, we use the board sync server below, which
 *   gives a fully-working collaborative board now.
 */
import { useMemo, useEffect, useState } from 'react'
import { BoardApp, createBoardDoc, createWebsocketProvider } from '@vulos/board-ui'
import '@vulos/board-ui/style.css'
import { avatarColor } from './avatar.js'
import { api } from '../../lib/api.js'

// Board sync server. Override per-deploy with VITE_BOARD_WS_URL.
const BOARD_WS_URL = import.meta.env?.VITE_BOARD_WS_URL || 'wss://board.vulos.org/ws'

export default function ChannelBoard({ channelId, currentUser, displayName }) {
  // One Y.Doc per channel — re-created when the channel changes.
  const ydoc = useMemo(() => createBoardDoc(), [channelId]) // eslint-disable-line react-hooks/exhaustive-deps

  // Connect a transport for this channel's board. The provider exposes
  // `.awareness` (cursors/presence), which BoardApp consumes.
  //
  // AUTH: the websocket board server authorizes connections with an HMAC
  // ?token=. We fetch a channel-scoped token from the control plane
  // (GET /api/board/token?room=<channelId>) BEFORE connecting and forward it to
  // createWebsocketProvider({ token }). Because the fetch is async the provider
  // is created in an effect (not useMemo). Talk standalone may not implement the
  // endpoint and dev/open mode returns an empty token — both degrade to a
  // token-less connect so the board keeps working. BoardApp tolerates a
  // not-yet-ready provider (awareness is optional).
  const [provider, setProvider] = useState(null)

  useEffect(() => {
    let alive = true
    let p = null
    ;(async () => {
      let token
      try {
        const r = await api.boardToken(channelId)
        // Empty token (dev/open mode) → connect anyway.
        token = r && r.token ? r.token : undefined
      } catch (e) {
        // No /api/board/token (Talk standalone / no auth): log and connect
        // token-less so the board still works.
        console.warn('[board] token fetch failed; connecting without token', e)
      }
      if (!alive) return
      p = createWebsocketProvider({ url: BOARD_WS_URL, room: channelId, doc: ydoc, token })
      setProvider(p)
    })()
    // Tear down on channel switch / unmount — never leak a socket.
    return () => {
      alive = false
      if (p) { try { p.destroy() } catch { /* already gone */ } }
      setProvider(null)
    }
  }, [channelId, ydoc])

  // Map Talk's current user onto board-ui's BoardUser shape.
  const user = useMemo(() => ({
    id: currentUser || 'me',
    name: displayName || currentUser || 'You',
    color: avatarColor(currentUser || 'me'),
  }), [currentUser, displayName])

  return (
    <div className="flex-1 min-h-0 bg-bg">
      <BoardApp
        ydoc={ydoc}
        awareness={provider?.awareness}
        user={user}
        boardId={channelId}
      />
    </div>
  )
}
