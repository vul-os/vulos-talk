/**
 * src/shells/TalkShell.jsx — talk.vulos.org standalone shell (Spaces)
 *
 * Spaces experience: channels, DMs, threads, and huddles. Real-time A/V is NOT
 * hosted here — a huddle hands off to the dedicated vulos-meet product, embedded
 * in an iframe within the channel (seam-C). See ChannelView's HuddlePanel.
 * Routes: / /channels/:id /dm/:id
 *
 * Wrapped in RequireAuth — redirects to app.vulos.org/login on 401.
 *
 * Deploy: dist-talk/  SPA fallback — server must serve index.html for all
 * unmatched paths.
 */

import { lazy, Suspense } from 'react'
import { Routes, Route, Navigate } from 'react-router-dom'
import RequireAuth from './RequireAuth.jsx'
import { Toaster } from '../lib/toast.jsx'

const SpacesApp = lazy(() => import('../apps/spaces/SpacesApp.jsx'))
const BotsApp   = lazy(() => import('../apps/bots/BotsApp.jsx'))

function Loading() {
  return (
    <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', background: 'var(--bg, #0f0f0f)' }}>
      <div className="w-7 h-7 border-2 border-accent border-t-transparent rounded-full animate-spin" />
    </div>
  )
}

export default function TalkShell() {
  return (
    <RequireAuth>
      <div style={{ display: 'flex', flexDirection: 'column', height: '100vh', background: 'var(--bg, #0f0f0f)' }}>
        <Suspense fallback={<Loading />}>
          <Routes>
            <Route path="/" element={<SpacesApp />} />
            <Route path="/channels/:id" element={<SpacesApp />} />
            <Route path="/dm/:id" element={<SpacesApp />} />
            <Route path="/apps" element={<BotsApp />} />
            <Route path="/settings/bots" element={<BotsApp />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </Suspense>
        <Toaster />
      </div>
    </RequireAuth>
  )
}
