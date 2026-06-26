# API

All endpoints are under `/api`. When `auth.enabled` is true, every route except
the auth-status surface and the public meeting/lobby join requires a valid HS256
session (sent via the `Authorization: Bearer <jwt>` header or the `session`
cookie). When auth is disabled, the caller is the single-user `self` identity.

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

## Huddles — meetings, lobby, TURN, recordings

> `TODO(seam-C)`: this surface is planned to move to **Vulos Meet**. See
> [ARCHITECTURE.md](ARCHITECTURE.md).

| Method | Path | Notes |
|--------|------|-------|
| `POST`/`GET` | `/api/meetings` · `GET`/`PUT`/`DELETE` `/api/meetings/:id` | Huddle room CRUD |
| `GET`  | `/api/meetings/:id/join` | **Public** — external invitees follow a bare link |
| `POST` | `/api/meet/:roomId/token` | **Public** — issue a signed join token |
| `POST` | `/api/meet/:roomId/lobby/enter` | **Public** — request lobby entry |
| `GET`  | `/api/meet/:roomId/lobby` · `POST` `/admit` · `/admit-all` · `/deny` | Organizer-only lobby control |
| `GET`  | `/api/turn/credentials` | Short-lived TURN/ICE credentials |
| `POST`/`GET` | `/api/meet/:roomId/recordings` · `GET`/`DELETE` `/recordings/:rid` | Membership-checked recording storage |

## Observability

| Method | Path | Notes |
|--------|------|-------|
| `GET`  | `/metrics` | Prometheus metrics (`vulos_talk_*`) |
| `GET`  | `/version` | Build version |
