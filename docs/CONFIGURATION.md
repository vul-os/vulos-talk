# Configuration

Vulos Talk reads `config.yaml` from its working directory at startup (falling
back to safe defaults if absent), plus a few environment variables for secrets
and deployment-specific wiring. It runs fully self-hosted with **zero**
configuration; the settings below are for when you want to lock things down.

## `config.yaml`

```yaml
server:
  addr: ":8080"             # listen address
  data_dir: "./data"        # SQLite + durable state
  uploads_dir: "./uploads"  # uploaded attachments

auth:
  enabled: false            # true = require a verified session (see JWT secret below)
  password: "changeme"      # only used if enabled
  max_attempts: 5           # lockout after N failed attempts
  lockout_minutes: 15
  session_hours: 24

storage:
  type: "local"             # "local" (SQLite/disk) or "postgres"
  postgres:                 # only used when type: "postgres"
    host: "localhost"
    port: 5432
    user: "postgres"
    password: ""
    database: "vulos_talk"
    sslmode: "disable"
```

With `auth.enabled: false` (the default) every request is the single-user
`self` identity — ideal for a personal or trusted-LAN deployment.

## Environment variables

| Variable | Purpose |
|----------|---------|
| `VULOS_TALK_JWT_SECRET` | HS256 signing secret. **Required when `auth.enabled` is true** — the server fails closed at startup without it. Must match the secret used by the central identity issuer so Talk accepts the shared `vulos.org` session. |
| `VULOS_TALK_DEV` | Set to `1`/`true` to permit an insecure built-in dev secret when `VULOS_TALK_JWT_SECRET` is unset. **Never set in production.** |
| `VULOS_TALK_CORS_ORIGINS` | Comma-separated origin allowlist; enables credentialed CORS. Unset = allow all origins **without** credentials (same-origin SPA). |
| `VULOS_MEET_URL` | Public base URL of the **Vulos Meet** deployment (signal gate + web client, e.g. `https://meet.vulos.org`) the huddle iframe is deep-linked against. Unset disables huddles (the action shows a "video not configured" state). |
| `VULOS_MEET_API_KEY` / `VULOS_MEET_API_SECRET` | Shared LiveKit credential pair, identical to the Meet deployment's. Required for **local** `VULOS-MEET/1` token minting. Omit when brokering via the control plane. |
| `VULOS_MEET_TENANT` | Tenant audience stamped on every Meet token and used as the room-id prefix — all members of a workspace share one tenant so they land in the same room. Default: `VULOS_ORG_ID`, then `talk`. Must not contain the separator byte. |
| `VULOS_MEET_TENANT_SEP` | Tenant/room separator byte; must match the Meet deployment. Default `:`. |
| `VULOS_MEET_TOKEN_TTL` | Join-token validity (Go duration). Default `2h`, hard-capped at `6h`. |
| `VULOS_TALK_PUBLIC_URL` | Public Talk origin Meet's in-call chat calls back to (`talkBase`). Falls back to the incoming request origin (honouring `X-Forwarded-Proto`). |
| `VULOS_CP_BASE_URL` | Optional vulos-cloud control plane. Enables the integration seam (entitlements/usage) **and** brokers Meet token minting (the cloud is the sole token issuer; Talk holds no `api_secret`). Authenticated with `VULOS_CP_TOKEN` (`X-Relay-Auth`). The cloud adapter is a separate package the standalone build never imports. |
| `VITE_BOARD_WS_URL` | Build-time (Vite) URL of the board sync server backing the per-channel **Board** tab (`@vulos/board-ui` whiteboard). Default: `wss://board.vulos.org/ws`. Note: Talk does not yet send a board-scoped auth token on this connection — the board server must authorize rooms by other means until that token is wired. The longer-term direction is to pump board CRDT diffs over the Vulos Relay fabric (see the seam note in `ChannelBoard.jsx`). |

## Authentication model

Talk does **not** host its own login UI. The SPA's `RequireAuth` boundary calls
`GET /api/auth/me`:

- **auth disabled** → returns the single-user `self` identity; the app is fully
  usable with no login.
- **auth enabled** → a valid HS256 session (presented via the `Authorization:
  Bearer` header or the `session` cookie) is required; otherwise the shell
  redirects to the central identity surface (`app.vulos.org/login`) carrying a
  `next` parameter. The shared `vulos.org` session cookie means a user already
  signed in at the identity surface passes transparently.

The signing secret is shared between the issuer and Talk; the identity (account
id) is always derived from the verified token subject, never from a
client-supplied header.

## Storage

- **local** (default) — SQLite + on-disk attachments under `data_dir` /
  `uploads_dir`. Nothing else to run.
- **postgres** — set `storage.type: "postgres"` and fill the `postgres` block
  for a shared/multi-instance deployment.
