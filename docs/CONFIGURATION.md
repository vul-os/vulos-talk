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
| `VULOS_LOBBY_DB` | Path to the durable lobby/meeting SQLite store. Default: `<data_dir>/lobby.db`. |
| `VULOS_CP_BASE_URL` | Reserved seam for the optional vulos-cloud control plane. The cloud adapter ships as a separate package; the standalone build never imports it and always runs with the permissive self-host provider. |
| `VITE_BOARD_WS_URL` | Build-time (Vite) URL of the board sync server backing the per-channel **Board** tab (`@vulos/board-ui` whiteboard). Default: `wss://board.vulos.org/ws`. Note: Talk does not yet send a board-scoped auth token on this connection — the board server must authorize rooms by other means until that token is wired. The longer-term plan is to drop this server and pump board CRDT diffs over the Vulos Relay fabric (see `ChannelBoard.jsx` `TODO(seam)`). |

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

Meeting recordings use an org-bucket object store that boots without any cloud
configuration.
