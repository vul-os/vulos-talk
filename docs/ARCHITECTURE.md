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
│   ├── handlers/            gin HTTP handlers (auth, spaces, presence, huddle handoff, bots/apps)
│   ├── middleware/          JWT auth middleware + signing-secret resolver (fail-closed)
│   ├── spaces/              CRDT-backed Spaces store (channels, messages, threads, reactions, pins, FTS5 search, presence)
│   ├── meet/                seam-C: derive Meet room + mint/broker VULOS-MEET/1 join token
│   ├── storage/             file/object storage (local + postgres) and per-account S3 backend resolver
│   ├── models/              shared request/response + domain types
│   ├── billing/             entitlement/usage enforcement over the seam
│   ├── seam/                integration seam (Identity / Entitlements / Usage) — standalone defaults
│   ├── fileacl/             attachment access-control store
│   └── obs/                 Prometheus metrics + optional OTel tracing
└── src/                     React SPA (Vite)
    ├── entries/talk.jsx     SPA entry — mounts TalkShell under BrowserRouter
    ├── shells/              TalkShell (routes) + RequireAuth (auth boundary)
    ├── apps/spaces/         the chat UI (SpacesApp, ChannelView, MessageList, …) + HuddlePanel (embedded Vulos Meet)
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
   ├── /api/meet/config  ──▶ meet seam                     whether Vulos Meet is configured
   ├── /api/spaces/channels/:id/huddle ──▶ meet seam       seam-C: mint VULOS-MEET/1 join (handoff to Vulos Meet)
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

## Seam-C — huddle video → Vulos Meet

Talk hosts **no** real-time audio/video. Real-time A/V is consolidated into the
dedicated **Vulos Meet** product; Talk focuses on chat + Spaces and hands huddles
off across **seam-C**:

1. A member starts a huddle in a channel (`POST /api/spaces/channels/:id/huddle`,
   membership-checked).
2. `backend/meet` derives a deterministic Meet room from the channel id
   (`talk-<sha256(channel)>`), qualified with the workspace tenant
   (`<tenant>:<room>`) so every member lands in the same room.
3. It obtains a short-lived `VULOS-MEET/1` join token — signed locally with the
   shared LiveKit `(api_key, api_secret)` pair (`VULOS_MEET_API_KEY` /
   `VULOS_MEET_API_SECRET`), or **brokered** from the control plane when
   `VULOS_CP_BASE_URL` is set (the cloud is the sole issuer, MEET-CP-01). The
   token bytes are byte-compatible with what `vulos-meet`'s `wrap.Validator` and
   LiveKit Server verify (same `livekit/protocol/auth` claim set).
4. The SPA embeds the Meet web client (`VULOS_MEET_URL`) in an iframe
   (`HuddlePanel`), deep-linked with `room` + `token` + `talkChannel`/`talkBase`/
   `talkToken` so Meet's in-call chat reads/writes **this** Talk channel and the
   conversation persists after the call.

Meet is an **optional** dependency — there is no hard cross-repo dependency. With
no Meet configured, `GET /api/meet/config` reports `enabled:false`, the Huddle
action shows a "video not configured" state, and Talk standalone (chat + Spaces)
is fully functional. The core never imports a cloud package; only the
composition root reads the seam config (`meet.FromEnv`).
