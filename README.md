# Vulos Talk

Team chat for Vulos: **channels / "Spaces", DMs, threads, reactions, pins,
search, presence**, and real-time **huddles/meetings** (WebRTC with a lobby,
signed join tokens, and recordings).

Vulos Talk is a standalone product extracted from Vulos Office. It runs
completely on its own — a Go backend (gin) that serves the meeting + spaces API
and embeds the built React SPA via `//go:embed dist`. The Vulos Workspace shell
surfaces it as the "Talk" app alongside Mail, Office, and Calendar.

## Architecture

- **Backend** (`backend/`, module `vulos-talk`): gin HTTP API for Spaces
  (channels/DMs/threads/messages/reactions/pins/status/search/presence) and
  Meetings (rooms, lobby, signed tokens, TURN credentials, recordings).
- **Frontend** (`src/`): Vite + React 18. `TalkShell` is the app shell;
  `src/apps/spaces/*` is the chat + calls UI. Shared UI primitives
  (`components/ui`), the design tokens (`src/design`), and the CRDT message
  store (`src/lib/crdt`) were carried over from Office.
- **Integration seam** (`backend/seam`): Talk runs standalone by default —
  identity is verified against a local JWT secret, entitlements are unlimited
  (self-host), and usage metering is a no-op. The vulos-cloud control plane is
  optional and engaged only when `VULOS_CP_BASE_URL` is set.

## Develop

```sh
npm install
npm run build      # vite build → dist/, then go build -o vulos-talk .
./vulos-talk       # serves API + embedded SPA on :8080
```

For a live frontend with API proxy: `npm run dev:web` (Vite on :5175 proxying
`/api` → :8080).

## Test

```sh
npm test           # vitest (frontend)
go test ./...      # backend
```

## Auth

Talk does not host its own login UI. The `RequireAuth` boundary redirects an
unauthenticated user to the central identity surface (`app.vulos.org/login`) and
relies on the shared `vulos.org` session cookie. When `auth.enabled` is false
(the self-host default) every request is the single-user `self` identity.

## Configuration & self-host

Talk runs fully self-hosted with zero cloud configuration — the defaults in
`config.yaml` are production-safe for a single-tenant deployment. Key knobs:

- `config.yaml` — server address, `data_dir`/`uploads_dir`, auth toggle, and the
  storage backend (`local` SQLite by default, or `postgres`).
- `VULOS_TALK_JWT_SECRET` — HS256 signing secret. **Required when `auth.enabled`
  is true** (the server fails closed at startup without it). It must match the
  secret used by the central identity issuer so Talk accepts the shared session.
- `VULOS_TALK_DEV=1` — permits an insecure built-in dev secret for local work.
  Never set in production.
- `VULOS_TALK_CORS_ORIGINS` — comma-separated origin allowlist (enables
  credentialed CORS). Unset = allow all origins without credentials (same-origin).
- `VULOS_LOBBY_DB` — path to the durable lobby/meeting SQLite store
  (default `<data_dir>/lobby.db`).
- `VULOS_CP_BASE_URL` — reserved seam for the optional vulos-cloud control plane.
  The cloud adapter ships as a separate package; the standalone build here never
  imports it and always runs with the permissive self-host provider.

## Roadmap

`// TODO(seam-C): route huddle video through vulos-meet` — Talk currently hosts
its own WebRTC meeting/lobby/TURN backend (carried from Office). The product map
consolidates real-time video into the dedicated **vulos-meet** product; when
that lands, the `/meetings` + `/meet` + `/turn` surface here will be replaced by
a seam-C handoff to vulos-meet, leaving Talk focused on chat + Spaces.
