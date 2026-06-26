/**
 * LinkPreview.jsx — best-effort inline link card.
 *
 * No network fetch (offline-first / zero backend awareness): we parse the first
 * http(s) URL out of a message body and render a tidy card showing the domain
 * and a humanised path as a stand-in title. The host app can later swap in real
 * unfurl metadata without changing this contract.
 */
import { Link2 } from 'lucide-react'

const URL_RE = /(https?:\/\/[^\s<>"')]+)/i

/** firstUrl — return the first http(s) URL in a string, or null. */
export function firstUrl(text = '') {
  const m = String(text).match(URL_RE)
  return m ? m[1] : null
}

/** describeUrl — derive { domain, title, href } for a card without fetching. */
export function describeUrl(raw) {
  try {
    const u = new URL(raw)
    const domain = u.hostname.replace(/^www\./, '')
    const path = decodeURIComponent(u.pathname).replace(/\/$/, '')
    const slug = path.split('/').filter(Boolean).pop() || ''
    const title = slug
      ? slug.replace(/[-_]+/g, ' ').replace(/\.\w+$/, '').replace(/\b\w/g, (c) => c.toUpperCase())
      : domain
    return { domain, title, href: u.href }
  } catch {
    return null
  }
}

export default function LinkPreview({ body = '' }) {
  const url = firstUrl(body)
  if (!url) return null
  const info = describeUrl(url)
  if (!info) return null

  return (
    <a
      href={info.href}
      target="_blank"
      rel="noopener noreferrer"
      className="mt-1.5 flex items-start gap-2.5 max-w-md bg-bg-elev2 border border-line rounded-md px-3 py-2 hover:border-line-strong transition-colors group"
    >
      <span className="mt-0.5 flex h-7 w-7 items-center justify-center rounded-md bg-accent-tint flex-shrink-0">
        <Link2 size={14} className="text-accent-press" />
      </span>
      <span className="min-w-0">
        <span className="block text-2xs uppercase tracking-eyebrow text-ink-faint truncate">
          {info.domain}
        </span>
        <span className="block text-sm text-ink tracking-tightish truncate group-hover:text-accent-press transition-colors">
          {info.title}
        </span>
      </span>
    </a>
  )
}
