<div align="center">

# Vulos Talk

**Team chat for your own server — channels, DMs, threads, and huddles in a single Go binary.**

Spaces (channels / DMs) · Threads · Reactions · Pins · Search · Presence · Huddles

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Version](https://img.shields.io/badge/version-1.0.0-informational)](CHANGELOG.md)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://golang.org)
[![React](https://img.shields.io/badge/React-18-61DAFB?logo=react&logoColor=black)](https://react.dev)

<sub><img src="docs/assets/vulos-logo.png" height="14" alt="VulOS"> Part of <strong><a href="https://vulos.org">VulOS</a></strong> — the open, self-hostable web OS &amp; app suite. Runs standalone, or combined under one login by <a href="https://vulos.org">Vulos Workspace</a>.</sub>

<br>

<img src="docs/screenshots/hero.png" alt="Vulos Talk — #general with a seeded team conversation" width="900">

</div>

---

## What is Vulos Talk?

Vulos Talk is a self-hostable **team chat** application that ships as **one
self-contained Go binary** with the entire React frontend embedded via
`//go:embed`. It gives a team real-time **Spaces** (public/private channels and
DMs), threaded conversations, emoji reactions, pinned messages, full-text
search, presence, and **huddles** (WebRTC meetings with a lobby, signed join
tokens, and recordings). Drop the binary next to a `config.yaml` and it runs —
SQLite by default, no external services, no telemetry.

It is **independently self-hostable**: with zero configuration it runs as a
single-user, local-storage app. Everything that *could* tie it to an external
service lives behind a small, clean **seam**, so the core never imports cloud
code — remove the (separate) cloud adapter and the standalone build still
compiles and runs.

## Part of VulOS

[VulOS](https://vulos.org) is an open, self-hostable web OS + app suite. Each
product is self-hostable on its own and can be combined under one login by
**Vulos Workspace**:

- **Vulos Mail** — mail + calendar + contacts (engine: lilmail; UI: `@vulos/mail-ui`; server: vulos-mail)
- **Vulos Talk** — team chat + channels/Spaces + huddles
- **Vulos Meet** — video meetings (LiveKit SFU)
- **Vulos Office** — documents: docs, sheets, slides, PDF
- **Vulos Relay** — sovereign connectivity fabric (`@vulos/relay-client`)
- **Vulos Workspace** — the open suite shell (one login, app switcher, admin)
- **Vulos OS** — the web-native desktop

**Vulos Talk** is the **team-chat product** of the suite: Spaces, DMs, threads,
and huddles. It runs standalone **and** is surfaced by **Vulos Workspace** as
the "Talk" app alongside Mail, Office, and Meet. Workspace links/embeds Talk
through clean seams; products never import one another's code.

> **Roadmap — `TODO(seam-C)`:** Talk currently hosts its own WebRTC
> meeting/lobby/TURN backend (carried over from Vulos Office). The product map
> consolidates real-time video into the dedicated **Vulos Meet** product; when
> that lands, the `/meetings` + `/meet` + `/turn` surface here will be replaced
> by a seam-C handoff to vulos-meet, leaving Talk focused on chat + Spaces.
> This is a documented future seam — no cross-repo dependency exists today.

## Features

- **Spaces** — public & private channels plus direct messages, all backed by a
  CRDT message store that survives restarts (durable SQLite).
- **Threads** — reply in a side-panel thread without derailing the channel.
- **Reactions & pins** — emoji reactions and per-channel pinned messages.
- **Search** — SQLite FTS5 full-text search within a channel.
- **Presence** — REST/poll heartbeat + roster with custom status.
- **Rich messages** — Markdown rendering, `@mentions`, and file uploads.
- **Huddles** — WebRTC meetings with a lobby, organizer-only admit/deny,
  short-lived signed join tokens, TURN/ICE credentials, and recordings.
- **Single binary** — Go backend + embedded React SPA; SQLite by default,
  optional PostgreSQL. Zero telemetry.
- **Standalone-first** — runs with no cloud; an optional control-plane seam is
  engaged only when explicitly configured.

## Screenshots

Generated from a seeded demo backend with `npm run screenshots`
(see [docs/SCREENSHOTS.md](docs/SCREENSHOTS.md)).

| Spaces (`#general`) | Threaded reply |
|---|---|
| ![Spaces](docs/screenshots/hero.png) | ![Thread](docs/screenshots/thread.png) |

| Direct message | Huddles |
|---|---|
| ![DM](docs/screenshots/dm.png) | ![Huddles](docs/screenshots/huddles.png) |

## Quick start (standalone)

Vulos Talk runs entirely on its own — no other Vulos product is required.

### Build from source

```sh
git clone https://github.com/vul-os/vulos-talk.git
cd vulos-talk

npm install          # frontend deps
npm run build        # vite build → dist/, then go build -o vulos-talk .
./vulos-talk         # serves the API + embedded SPA on :8080
```

Open <http://localhost:8080>. With the default config, auth is disabled and
every request is the single-user `self` identity — ideal for a personal or
LAN deployment.

### Run with Go directly

```sh
npm run build:frontend   # produce dist/ (embedded by the binary)
go run .                 # start the server on :8080
```

### Configure

Copy and edit [`config.yaml`](config.yaml) (server address, data dir, auth,
storage backend). The full reference — including the JWT signing secret,
CORS allowlist, and the optional cloud seam — is in
[docs/CONFIGURATION.md](docs/CONFIGURATION.md).

### Live frontend (development)

```sh
npm run dev:web   # Vite on :5175 proxying /api → :8080
```

## Architecture

```
                ┌────────────────────────── vulos-talk (one Go binary) ──────────────────────────┐
   Browser ──▶  │  gin HTTP server                                                                │
   (React SPA)  │   ├── /api/auth/*        minimal status/me (no login UI; redirects to identity) │
                │   ├── /api/spaces/*      channels · DMs · messages · threads · reactions · pins  │
                │   │                      · status · search · presence  ──▶  CRDT SpacesStore     │
                │   ├── /api/meetings/*    huddle rooms (CRUD)                  (SQLite)            │
                │   ├── /api/meet/*        lobby · signed join tokens · admit/deny · recordings    │
                │   ├── /api/turn/*        short-lived TURN/ICE credentials                        │
                │   ├── /metrics /version  Prometheus + build info                                 │
                │   └── //go:embed dist    serves the built React SPA (history-API fallback)       │
                │                                                                                   │
                │  seam/  ── standalone by default (local JWT, unlimited entitlements, no-op usage) │
                │            optional vulos-cloud control plane (separate adapter; never imported)  │
                └───────────────────────────────────────────────────────────────────────────────┘
```

Full detail and the `TODO(seam-C)` huddle/video plan: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Configuration

Talk is configured via `config.yaml` plus a few environment variables (JWT
secret, lobby DB path, CORS allowlist, optional cloud base URL). See
[docs/CONFIGURATION.md](docs/CONFIGURATION.md) for the complete reference.

Talk does not host its own login UI: the `RequireAuth` boundary redirects an
unauthenticated user to the central identity surface (`app.vulos.org/login`) and
relies on the shared `vulos.org` session cookie. When `auth.enabled` is `false`
(the self-host default) every request is the single-user `self` identity.

## Documentation

| Document | What's in it |
|----------|--------------|
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Components, request flow, the integration seam, and the `seam-C` roadmap |
| [docs/CONFIGURATION.md](docs/CONFIGURATION.md) | `config.yaml` reference + environment variables |
| [docs/API.md](docs/API.md) | REST API surface (Spaces, Meetings, Huddles, presence) |
| [docs/SCREENSHOTS.md](docs/SCREENSHOTS.md) | How the demo screenshotter works + how to regenerate |
| [CHANGELOG.md](CHANGELOG.md) | Release history (Keep a Changelog) |

## Development

```sh
npm install            # install frontend deps
npm run build          # vite build + go build -o vulos-talk .
npm test               # frontend unit tests (vitest)
go test ./...          # backend tests
npm run screenshots    # regenerate docs/screenshots/ from seeded demo data
```

## Contributing

Issues and pull requests are welcome. Please keep changes conservative and make
sure `npm run build`, `go build ./...`, `go test ./...`, and `npm test` all pass
before opening a PR.

## License

[MIT](LICENSE) © Imran Paruk
