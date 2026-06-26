# Architecture

Vulos Talk is a single Go binary that serves a JSON API **and** the compiled
React single-page app (embedded with `//go:embed dist`). There is no separate
frontend server to deploy and, by default, no external services — durable state
lives in local SQLite files.

## Components

```
repo
├── main.go                  composition root: config, routes, embedded SPA, seam wiring
├── backend/
│   ├── config/              config.yaml loader + defaults
│   ├── handlers/            gin HTTP handlers (auth, spaces, meetings, meet, turn, presence, recordings)
│   ├── middleware/          JWT auth middleware + signing-secret resolver (fail-closed)
│   ├── spaces/              CRDT-backed Spaces store (channels, messages, threads, reactions, pins, FTS5 search, presence)
│   ├── services/meeting/    durable lobby/meeting store + room-id + audit log
│   ├── storage/             file/object storage (local + postgres) and per-account S3 backend resolver
│   ├── models/              shared request/response + domain types
│   ├── billing/             entitlement/usage enforcement over the seam
│   ├── seam/                integration seam (Identity / Entitlements / Usage) — standalone defaults
│   ├── fileacl/             attachment access-control store
│   └── obs/                 Prometheus metrics + optional OTel tracing
└── src/                     React SPA (Vite)
    ├── entries/talk.jsx     SPA entry — mounts TalkShell under BrowserRouter
    ├── shells/              TalkShell (routes) + RequireAuth (auth boundary)
    ├── apps/spaces/         the chat + huddles UI (SpacesApp, ChannelView, MessageList, Meetings, Room, CallView, …)
    ├── components/ui/       shared UI primitives (Sidebar, Button, Modal, Topbar, …)
    ├── design/              design tokens (warm-paper surface, single teal accent)
    └── lib/                 api client, CRDT message store, sanitisation
```

## Request flow

```
Browser (React SPA, same origin)
   │  fetch /api/... (credentials: include)
   ▼
gin router (main.go)
   ├── CORS (allow-all without credentials, or explicit allowlist when VULOS_TALK_CORS_ORIGINS is set)
   ├── /api/auth/status, /api/auth/me        (unauthenticated status surface)
   ├── middleware.Auth (only when auth.enabled)   ──▶ verifies HS256 JWT (Authorization header or `session` cookie)
   │        sets the verified account id from the token subject (never a client header)
   ├── /api/spaces/*     ──▶ spaces.Store (SQLite)         channels, messages, threads, reactions, pins, search, presence
   ├── /api/meetings/*   ──▶ meeting store (SQLite)        huddle room CRUD
   ├── /api/meet/*       ──▶ meeting store                 lobby, signed join tokens, admit/deny, recordings
   ├── /api/turn/*       ──▶ TURN handler                  short-lived ICE credentials
   └── NoRoute           ──▶ embedded dist/ (SPA, history-API fallback to index.html)
```

The SPA shell (`TalkShell`) wraps everything in `RequireAuth`, which probes
`GET /api/auth/me`. When auth is enabled and no valid session is present the
shell redirects to the central identity surface (`app.vulos.org/login`); when
auth is disabled every request is the single-user `self` identity.

## Spaces store (CRDT)

Channel messages are modelled as an append/edit/tombstone op-log so clients can
merge concurrent edits and the server can expose ops for sync
(`/spaces/channels/:id/ops`, `/spaces/ops`). State is persisted to SQLite so
messages, reactions, and pins survive a restart. Per-channel search uses SQLite
FTS5. Presence is REST/poll based (`/spaces/presence/heartbeat` + `/roster`).

## The integration seam

`backend/seam` defines three small interfaces — **Identity**, **Entitlements**,
and **Usage** — with standalone defaults that need no cloud:

- **Identity** validates a locally-signed HS256 JWT (or returns the single-user
  `self` identity when auth is disabled).
- **Entitlements** returns unlimited quotas (self-host is not metered).
- **Usage** is a no-op (or Prometheus-only).

The composition root (`main.go`) wires the standalone provider. An optional
vulos-cloud control-plane adapter implements the same interfaces in a **separate
package** that the core never imports — so removing it never breaks the core.
It is engaged only when explicitly configured (`VULOS_CP_BASE_URL`).

## Roadmap — `TODO(seam-C)`: huddle video → Vulos Meet

Talk currently hosts its own WebRTC huddle backend (`/meetings`, `/meet`,
`/turn`, recordings), carried over from Vulos Office. The VulOS product map
consolidates real-time video into the dedicated **Vulos Meet** product. The
planned **seam-C** handoff will replace Talk's meeting/lobby/TURN surface with a
call into vulos-meet, leaving Talk focused on chat + Spaces. This is a
documented future seam only — there is **no cross-repo dependency today**, and
the marker lives in `main.go` and the meeting routes.
