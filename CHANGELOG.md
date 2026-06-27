# Changelog

All notable changes to Vulos Talk are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **Seam-C: huddles → Vulos Meet.** Talk no longer hosts real-time audio/video.
  Starting a huddle in a channel derives a deterministic Meet room, mints a
  `VULOS-MEET/1` join token (locally via `VULOS_MEET_API_KEY`/`VULOS_MEET_API_SECRET`,
  or brokered via `VULOS_CP_BASE_URL`), and embeds the Vulos Meet web client in an
  iframe (`HuddlePanel`) — with Meet's in-call chat pointed back at the originating
  channel (`talkChannel`/`talkBase`/`talkToken`) so the conversation persists.
  New endpoints `GET /api/meet/config` and `POST /api/spaces/channels/:id/huddle`
  (membership-checked). Meet is an **optional** dependency: with `VULOS_MEET_URL`
  unset the Huddle action degrades to a "video not configured" state and Talk
  standalone (chat + Spaces) is fully functional.
- **Slack/Google-Chat-class UI** — reshaped the Spaces experience into a
  three-pane desktop layout (sidebar · stream · thread) with a workspace header,
  prominent compose, ⌘K quick-switcher, collapsible Channels / DMs / Threads /
  Activity sections, unread bolding + mention badges, presence dots, and a `?`
  keyboard-shortcut overlay.
- **Dense message stream** — author grouping, day separators, a per-message hover
  toolbar (react · reply-in-thread · edit · delete · pin · copy-link), link
  previews, jump-to-unread, and typing indicators.
- **Composer** — Markdown + preview, emoji picker, `@mention` autocomplete, and
  **`/slash-command` autocomplete**, with Shift+Enter newline / Enter send.
- **Bot framework + API** — bot/app model with hashed bot tokens, signing
  secrets, and scopes; a scoped REST API under `/api/bot/v1/`; **signed event
  webhooks** (`message.created`, `app_mention`, `member_joined`, `slash_command`)
  with `X-Vulos-Signature` HMAC-SHA256; an SSE event stream (socket-mode style);
  slash commands; per-bot incoming webhooks; and a self-hostable **Apps & Bots**
  admin console.
- **Bot registry seam** — `bots.Registry` interface with a standalone
  SQLite-backed default and a documented Vulos Cloud control-plane hook (core
  never imports the cloud adapter). Example bot in `examples/echo-bot/`, full
  reference in `docs/BOT-API.md`.
- Responsive single-column mobile layout (channel drawer, bottom nav,
  full-screen composer) and expanded demo seed + screenshot gallery.

## [1.0.0] — 2026-06-26

First standalone release of **Vulos Talk**, extracted from Vulos Office into its
own product and conformed to the VulOS product standard.

### Added
- Standalone team-chat product: a single Go binary serving the API and the
  embedded React SPA (`//go:embed dist`).
- **Spaces** — public/private channels and DMs backed by a durable CRDT message
  store (SQLite): messages, threads, reactions, pins, status, presence.
- Per-channel SQLite FTS5 full-text search.
- **Huddles** — hand off to the dedicated Vulos Meet product (seam-C; see the
  Added entry at the top of Unreleased).
- Integration seam (Identity / Entitlements / Usage) with standalone defaults;
  optional vulos-cloud control plane engaged only when configured.
- Playwright demo screenshotter (`npm run screenshots`) with zero-setup seeded
  data, plus `docs/` (ARCHITECTURE, CONFIGURATION, API, SCREENSHOTS).

### Changed
- Rebranded from Vulos Office to Vulos Talk across the shell, PWA manifests,
  service worker, observability service/metric names, and `config.yaml`.
- Renamed environment variables `VULOS_OFFICE_*` → `VULOS_TALK_*`
  (`VULOS_TALK_JWT_SECRET`, `VULOS_TALK_DEV`).
- The huddles surface is now labelled **Huddles** (was "Meet") to avoid
  collision with the separate Vulos Meet product.

### Removed
- Talk's carried-over real-time video backend: the in-process WebRTC mesh,
  meeting lobby, TURN/ICE credential issuance, recordings, and the
  `/api/meetings`, `/api/meet/*`, and `/api/turn/*` endpoints (with their
  `services/meeting` lobby/token/turn code and the meeting/recording storage).
  Real-time A/V now lives in the dedicated Vulos Meet product (seam-C above).

### Fixed
- Spaces routing: `SpacesApp` now reads the `:id` route param and navigates to
  the `/channels/:id` and `/dm/:id` routes declared by `TalkShell` (the previous
  `/spaces/:channelId` mismatch caused a redirect loop), and selecting the first
  channel on the bare route no longer rewrites the URL.

### Roadmap
- Seam-C huddle video → **Vulos Meet** is implemented (see Added/Removed above).
  Meet remains an optional dependency; no hard cross-repo dependency exists.
