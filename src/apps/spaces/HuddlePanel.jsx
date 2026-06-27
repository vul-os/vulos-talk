/**
 * HuddlePanel — seam-C embed of the dedicated vulos-meet product.
 *
 * Talk does NOT host real-time audio/video. When a member starts a huddle in a
 * channel, the backend (POST /api/spaces/channels/:id/huddle) derives a
 * deterministic Meet room, mints a VULOS-MEET/1 join token, and returns a deep
 * link to the Meet web client. This component embeds that web client in an
 * iframe overlay. The deep link carries talkChannel/talkBase/talkToken so Meet's
 * in-call chat is Talk-backed and the conversation persists in this channel.
 *
 * Closing the panel simply unmounts the iframe (leaving the call). Media
 * permissions are granted to the Meet origin via the iframe `allow` attribute.
 */
import { useEffect, useRef } from 'react'
import { X, Video } from 'lucide-react'
import { IconButton } from '../../components/ui'

export default function HuddlePanel({ joinUrl, channelName, onClose }) {
  const closeRef = useRef(null)

  // Esc leaves the huddle; focus the close control on open for keyboard users.
  useEffect(() => {
    closeRef.current?.focus()
    function onKey(e) { if (e.key === 'Escape') onClose?.() }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  if (!joinUrl) return null

  return (
    <div
      className="fixed inset-0 z-[60] flex flex-col bg-ink/95 backdrop-blur-sm animate-fade-in"
      role="dialog"
      aria-modal="true"
      aria-label={`Huddle in ${channelName || 'channel'}`}
    >
      <div className="flex items-center justify-between h-12 px-4 bg-paper border-b border-line flex-shrink-0">
        <span className="flex items-center gap-2 min-w-0">
          <Video size={15} className="text-accent" />
          <span className="text-sm font-semibold text-ink tracking-tightish truncate">
            Huddle{channelName ? ` · ${channelName}` : ''}
          </span>
          <span className="text-2xs text-ink-faint hidden sm:inline">powered by Vulos Meet</span>
        </span>
        <IconButton ref={closeRef} size="sm" title="Leave huddle" aria-label="Leave huddle" onClick={onClose}>
          <X size={15} />
        </IconButton>
      </div>
      <iframe
        title="Vulos Meet huddle"
        src={joinUrl}
        className="flex-1 w-full border-0 bg-ink"
        allow="camera; microphone; display-capture; autoplay; fullscreen; speaker-selection"
        allowFullScreen
      />
    </div>
  )
}
