# Changelog

All notable changes to Vulos Talk are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] — 2026-06-26

First standalone release of **Vulos Talk**, extracted from Vulos Office into its
own product and conformed to the VulOS product standard.

### Added
- Standalone team-chat product: a single Go binary serving the API and the
  embedded React SPA (`//go:embed dist`).
- **Spaces** — public/private channels and DMs backed by a durable CRDT message
  store (SQLite): messages, threads, reactions, pins, status, presence.
- Per-channel SQLite FTS5 full-text search.
- **Huddles** — WebRTC meetings with a lobby, organizer-only admit/deny,
  short-lived signed join tokens, TURN/ICE credentials, and recordings.
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

### Fixed
- Spaces routing: `SpacesApp` now reads the `:id` route param and navigates to
  the `/channels/:id` and `/dm/:id` routes declared by `TalkShell` (the previous
  `/spaces/:channelId` mismatch caused a redirect loop), and selecting the first
  channel on the bare route no longer rewrites the URL.

### Roadmap
- `TODO(seam-C)`: route huddle video through the dedicated **Vulos Meet**
  product. No cross-repo dependency exists today.
