/**
 * toast.jsx — tiny dependency-free toast utility.
 *
 * Usage:
 *   import { toast, Toaster } from '../lib/toast.jsx'
 *   toast.success('Message sent')
 *   toast.error('Send failed')
 *   // render <Toaster /> once near the app root.
 *
 * A module-level pub/sub lets any component fire a toast without prop drilling
 * or context. Tokens-only styling; respects prefers-reduced-motion via the
 * shared animation utilities.
 */
import { useEffect, useState } from 'react'
import { Check, AlertTriangle, Info, X } from 'lucide-react'

let listeners = []
let counter = 0

function emit(message, opts = {}) {
  const t = {
    id: ++counter,
    message: String(message),
    type: opts.type || 'info',
    duration: opts.duration ?? 3200,
  }
  listeners.forEach((l) => l(t))
  return t.id
}

export function toast(message, opts) {
  return emit(message, opts)
}
toast.success = (m, o = {}) => emit(m, { ...o, type: 'success' })
toast.error = (m, o = {}) => emit(m, { ...o, type: 'error' })
toast.info = (m, o = {}) => emit(m, { ...o, type: 'info' })

const ICONS = {
  success: Check,
  error: AlertTriangle,
  info: Info,
}

export function Toaster() {
  const [toasts, setToasts] = useState([])

  useEffect(() => {
    function onToast(t) {
      setToasts((prev) => [...prev, t])
      if (t.duration > 0) {
        setTimeout(() => {
          setToasts((prev) => prev.filter((x) => x.id !== t.id))
        }, t.duration)
      }
    }
    listeners.push(onToast)
    return () => {
      listeners = listeners.filter((l) => l !== onToast)
    }
  }, [])

  function dismiss(id) {
    setToasts((prev) => prev.filter((x) => x.id !== id))
  }

  if (toasts.length === 0) return null

  return (
    <div
      className="fixed bottom-4 right-4 z-[100] flex flex-col gap-2 max-w-[calc(100vw-2rem)]"
      role="status"
      aria-live="polite"
    >
      {toasts.map((t) => {
        const Icon = ICONS[t.type] || Info
        const tone =
          t.type === 'success'
            ? 'text-success'
            : t.type === 'error'
              ? 'text-danger'
              : 'text-accent-press'
        return (
          <div
            key={t.id}
            className="flex items-center gap-2.5 bg-paper border border-line rounded-md shadow-e3 pl-3 pr-2 py-2 min-w-[220px] animate-rise-in"
          >
            <Icon size={15} className={`flex-shrink-0 ${tone}`} />
            <span className="text-sm text-ink tracking-tightish flex-1 min-w-0">
              {t.message}
            </span>
            <button
              type="button"
              onClick={() => dismiss(t.id)}
              aria-label="Dismiss notification"
              className="p-1 rounded-sm text-ink-faint hover:text-ink hover:bg-accent-tint transition-colors flex-shrink-0"
            >
              <X size={13} />
            </button>
          </div>
        )
      })}
    </div>
  )
}

export default toast
