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
import { useMemo, useEffect } from 'react'
import { BoardApp, createBoardDoc, createWebsocketProvider } from '@vulos/board-ui'
import '@vulos/board-ui/style.css'
import { avatarColor } from './avatar.js'

// Board sync server. Override per-deploy with VITE_BOARD_WS_URL.
const BOARD_WS_URL = import.meta.env?.VITE_BOARD_WS_URL || 'wss://board.vulos.org/ws'

export default function ChannelBoard({ channelId, currentUser, displayName }) {
  // One Y.Doc per channel — re-created when the channel changes.
  const ydoc = useMemo(() => createBoardDoc(), [channelId]) // eslint-disable-line react-hooks/exhaustive-deps

  // Connect a transport for this channel's board. The provider exposes
  // `.awareness` (cursors/presence), which BoardApp consumes.
  //
  // PROD: no auth token is sent yet. createWebsocketProvider() accepts an
  // optional `token` (forwarded as `?token=`), but Talk has no board-scoped
  // credential to mint today, so the board server must currently authorize the
  // room by other means (e.g. network boundary). Before this ships to prod,
  // pass a channel-scoped token here so the board server can authn/authz the
  // room — otherwise anyone who can reach BOARD_WS_URL can join any room.
  const provider = useMemo(
    () => createWebsocketProvider({ url: BOARD_WS_URL, room: channelId, doc: ydoc }),
    [channelId, ydoc],
  )

  useEffect(() => () => { try { provider.destroy() } catch {} }, [provider])

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
        awareness={provider.awareness}
        user={user}
        boardId={channelId}
      />
    </div>
  )
}
