/**
 * BotsApp — Talk's "Apps & Bots" manage surface (routes: /apps, /settings/bots).
 *
 * MIGRATION: this surface is now backed by the shared @vulos/apps-ui
 * <AppsAndBots/> component in `mode="product"`, which talks to Talk's mounted
 * Apps & Bots platform at `/api/apps` (install / configure / rotate / delete,
 * with one-time token + signing-secret reveal). It replaces the old bespoke
 * Bots UI; the same surface is what Vulos Workspace aggregates across products.
 *
 * Auth: Talk authenticates with an httpOnly `session` cookie, so we inject a
 * fetcher that sends `credentials: 'include'` (and routes through the endpoint-
 * failover base) instead of the apps-ui default bearer-token fetch.
 */
import { useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { ArrowLeft } from 'lucide-react'
import { selectEndpoint } from '@vulos/relay-client/endpoints'
import AppsAndBots from '@vulos/apps-ui'
import '@vulos/apps-ui/styles.css'
import { useTheme } from '../../components/ui/useTheme'
import { IconButton } from '../../components/ui'

// resolveTheme maps Talk's tri-state theme ('light' | 'dark' | 'system') to the
// two-state theme @vulos/apps-ui expects, honoring the OS preference for system.
function resolveTheme(theme) {
  if (theme === 'light' || theme === 'dark') return theme
  if (typeof window !== 'undefined' && window.matchMedia) {
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  }
  return 'dark'
}

export default function BotsApp() {
  const navigate = useNavigate()
  const { theme } = useTheme()

  // Cookie-authed fetcher: prepend the (failover-aware) endpoint origin and send
  // the session cookie. apps-ui builds same-origin paths under `/api/apps`.
  const fetcher = useCallback(async (input, init = {}) => {
    const base = await selectEndpoint()
    return fetch(base + input, { ...init, credentials: 'include' })
  }, [])

  return (
    <div className="flex-1 min-h-0 overflow-y-auto bg-bg">
      <header className="flex items-center gap-2.5 h-14 px-4 bg-paper border-b border-line sticky top-0 z-10">
        <IconButton title="Back to Talk" onClick={() => navigate('/')}>
          <ArrowLeft size={16} />
        </IconButton>
        <h1 className="text-sm font-semibold text-ink tracking-tightish">Apps &amp; Bots</h1>
      </header>

      <div className="max-w-3xl mx-auto px-4 py-6">
        <AppsAndBots
          mode="product"
          product="talk"
          basePath="/api/apps"
          theme={resolveTheme(theme)}
          fetcher={fetcher}
        />
      </div>
    </div>
  )
}
