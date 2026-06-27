# API

All endpoints are under `/api`. When `auth.enabled` is true, every route except
the auth-status surface requires a valid HS256 session (sent via the
`Authorization: Bearer <jwt>` header or the `session` cookie). When auth is
disabled, the caller is the single-user `self` identity.

Identity is always derived from the verified token subject — a client-supplied
`X-Account-ID` header is ignored unless the caller is a verified admin.

## Auth (status only — no login UI)

| Method | Path | Notes |
|--------|------|-------|
| `GET`  | `/api/auth/status` | Whether auth is enforced and whether the caller is signed in |
| `GET`  | `/api/auth/me`     | `401` when auth is enforced and unauthenticated; `self` identity when auth is disabled |

## Spaces — channels, DMs, messages

| Method | Path |
|--------|------|
| `GET`  | `/api/spaces/channels` |
| `POST` | `/api/spaces/channels` |
| `POST` | `/api/spaces/channels/:channelId/join` |
| `GET`  | `/api/spaces/channels/:channelId/members` |
| `POST` | `/api/spaces/channels/:channelId/members` |
| `PUT`  | `/api/spaces/channels/:channelId/members/me/name` |
| `GET`  | `/api/spaces/channels/:channelId/messages` |
| `POST` | `/api/spaces/channels/:channelId/messages` |
| `PUT`  | `/api/spaces/channels/:channelId/messages/:msgId` |
| `DELETE` | `/api/spaces/channels/:channelId/messages/:msgId` |
| `POST` | `/api/spaces/channels/:channelId/read` · `GET` `/read` |

### Threads, reactions, pins, status, search

| Method | Path |
|--------|------|
| `GET`  | `/api/spaces/channels/:channelId/threads/:parentId` |
| `POST` | `/api/spaces/channels/:channelId/threads/:parentId/reply` |
| `GET`  | `/api/spaces/channels/:channelId/reactions` |
| `POST` / `DELETE` | `/api/spaces/messages/:msgId/react` |
| `GET`  | `/api/spaces/channels/:channelId/pins` |
| `POST` | `/api/spaces/channels/:channelId/pins` · `DELETE` `/pins/:msgId` |
| `PUT`  | `/api/spaces/users/me/status` · `GET` `/api/spaces/users/:userId/status` |
| `GET`  | `/api/spaces/channels/:channelId/search?q=...` |

### CRDT sync & presence

| Method | Path |
|--------|------|
| `GET`  | `/api/spaces/channels/:channelId/ops` · `POST` `/api/spaces/ops` |
| `POST` | `/api/spaces/presence/heartbeat` · `GET` `/api/spaces/presence/roster` |

## Huddles — handoff to Vulos Meet (seam-C)

Talk hosts no real-time audio/video. A huddle hands off to the dedicated
**Vulos Meet** product: Talk derives a deterministic Meet room from the channel,
mints a `VULOS-MEET/1` join token (locally, or brokered via the control plane),
and the SPA embeds the Meet web client in an iframe with Meet's in-call chat
pointed back at the channel. Meet is optional — when it is not configured the
endpoints report `enabled:false` and the SPA disables the Huddle action. See
[ARCHITECTURE.md](ARCHITECTURE.md) and `vulos-meet/spec/TOKEN.md`.

| Method | Path | Notes |
|--------|------|-------|
| `GET`  | `/api/meet/config` | Reports `{enabled}` — whether video (Vulos Meet) is configured |
| `POST` | `/api/spaces/channels/:channelId/huddle` | Membership-checked; mints a Meet join and returns `{enabled, join_url, meet_url, room, token, talk_base, talk_channel, talk_token, expires_at}` (or `{enabled:false, reason}`) |

## Observability

| Method | Path | Notes |
|--------|------|-------|
| `GET`  | `/metrics` | Prometheus metrics (`vulos_talk_*`) |
| `GET`  | `/version` | Build version |
