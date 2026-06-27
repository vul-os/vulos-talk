# Vulos Talk — Bot Framework & API

> **Migration note.** Talk's bots now run on the **shared Vulos Apps & Bots
> platform** (`github.com/vul-os/vulos-apps`, package `appsplatform`). A bot **is
> an app**. The new canonical management + runtime surface is **`/api/apps`**
> (see `vulos-apps/docs/APPS-PLATFORM.md`) — and it is the contract **Vulos
> Workspace** aggregates via `GET /api/apps`. The legacy surface documented
> below (`/api/bots`, `/api/bot/v1`, `/api/bot/hooks/:id`) is preserved as a
> **compatibility shim over the same registry**, so existing bots and the
> `examples/echo-bot` keep working unchanged. What changed in substance: app
> tokens are now prefixed **`vat_`** (signing secrets **`vas_`**) and a bot
> posts/acts as the synthetic account **`app:<id>`** (was `bot:<id>`).

This document describes the Vulos Talk bot/app framework: how to register a bot,
authenticate, call the REST API, receive signed outbound events, register slash
commands, post via incoming webhooks, and consume the socket-mode-style SSE
event stream. It also explains the **registry seam** (standalone vs. a
Cloud-managed control plane).

All paths are relative to the Talk server origin (e.g. `http://localhost:8080`).

---

## Concepts

A **bot** (a.k.a. app) has:

| Field                 | Meaning                                                              |
| --------------------- | ------------------------------------------------------------------- |
| `id`                  | Server-assigned id. The bot posts/acts as account `app:<id>`.       |
| `name`                | Display name. Used for `@name` mentions.                            |
| `owner_id`            | The account that created the bot (admin API is owner-scoped).       |
| `org_id`              | Tenant id. Empty in OSS / standalone.                              |
| `scopes`              | Permissions (see below).                                            |
| `event_url`           | Optional outbound webhook URL for signed events.                   |
| `incoming_webhook_id` | Random id used in the (unauthenticated) incoming-webhook URL.      |
| `slash_commands`      | `[{name, description}]` — names stored without the leading slash.  |
| `created_at`          | Creation time.                                                     |

### Secrets

- **Bot token** — a Bearer secret (prefix `vat_`). **Only its sha256 hash is
  stored at rest.** The plaintext is shown **once** at create / rotate time and
  can never be recovered — rotate to get a new one.
- **Signing secret** — used to sign outbound events (prefix `vas_`). It is
  stored **as-is** (not hashed) because Talk must reproduce it to compute the
  HMAC on every outbound event. Treat the bot record as sensitive at rest.

### Scopes

| Scope             | Grants                                              |
| ----------------- | --------------------------------------------------- |
| `chat:write`      | Post messages (and reactions endpoints' write path).|
| `history:read`    | Read channel message history.                       |
| `channels:read`   | List visible channels.                              |
| `members:read`    | List channel members.                               |
| `reactions:write` | Add / remove reactions.                             |

A bot with **no scopes** can only call `auth.test`. Unknown scope strings are
rejected at create/update time.

### Channel visibility (message-post scoping)

- **Public** channels: any bot may read and post.
- **Private / DM** channels: the bot must be a **member** (membership account id
  `app:<id>`). Otherwise reads/posts return **403**, and the channel is not
  delivered in events. Add a bot to a private channel via the normal Spaces
  member-invite flow using account id `app:<id>`.

---

## Admin API (session / cookie authed)

Base: `/api/bots`. Authenticated with the normal Talk session (JWT Bearer or
`session` cookie). **Owner-scoped**: a caller sees and manages only bots whose
`owner_id` equals their account id; an admin sees and manages all bots. A
cross-owner access returns `404` (no existence leak).

`BotSummary` JSON (never includes secrets):

```json
{
  "id": "…", "name": "…", "scopes": ["chat:write"],
  "event_url": "", "slash_commands": [{"name":"deploy","description":"…"}],
  "owner_id": "alice", "incoming_webhook_id": "…",
  "incoming_webhook_url": "/api/bot/hooks/…", "created_at": "…"
}
```

| Method & path                     | Body                                                     | Response                                                                 |
| --------------------------------- | -------------------------------------------------------- | ----------------------------------------------------------------------- |
| `GET /api/bots`                   | —                                                        | `[BotSummary]`                                                           |
| `POST /api/bots`                  | `{name, scopes, event_url, slash_commands, default_channel?}` | `{bot, token, signing_secret, incoming_webhook_url}` (secrets shown once)|
| `GET /api/bots/:id`               | —                                                        | `BotSummary`                                                             |
| `PUT /api/bots/:id`               | `{name?, scopes?, event_url?, slash_commands?, default_channel?}` | `BotSummary`                                                             |
| `DELETE /api/bots/:id`            | —                                                        | `{ok:true}`                                                             |
| `POST /api/bots/:id/rotate-token` | —                                                        | `{token}` (shown once)                                                   |
| `POST /api/bots/:id/rotate-secret`| —                                                        | `{signing_secret}` (shown once)                                          |

`PUT` treats absent JSON fields as "leave unchanged" (only the keys present in
the body are updated).

### Slash-command catalog

`GET /api/spaces/commands` (session-authed) returns every registered slash
command for the composer autocomplete:

```json
[{"name":"deploy","description":"ship it","bot_id":"…"}]
```

---

## Bot REST API (Bearer bot-token authed)

Base: `/api/bot/v1`. Authenticate with the **bot token**:

```
Authorization: Bearer vat_xxxxxxxx…
```

`BotAuth` looks the token up by its sha256 hash; an unknown/missing token →
`401`. Each route enforces the scope it needs → `403` when missing.

| Method & path                                  | Scope             | Notes                                                              |
| ---------------------------------------------- | ----------------- | ----------------------------------------------------------------- |
| `GET /api/bot/v1/auth.test`                    | none              | `{bot_id, name, scopes}`                                          |
| `POST /api/bot/v1/messages`                    | `chat:write`      | Body `{channel_id, text, thread_parent?}` → the created message. Author = `app:<id>`. Channel-access enforced. |
| `GET /api/bot/v1/channels`                     | `channels:read`   | Public channels + private/DM channels the bot is a member of.    |
| `GET /api/bot/v1/channels/:channelId/history`  | `history:read`    | `?limit=N` (default 50, cap 200); most-recent N, chronological.  |
| `GET /api/bot/v1/channels/:channelId/members`  | `members:read`    | `[Membership]`. Channel-access enforced.                         |
| `POST /api/bot/v1/reactions`                   | `reactions:write` | Body `{channel_id, message_id, emoji}` → adds reaction `app:<id>`.|
| `DELETE /api/bot/v1/reactions`                 | `reactions:write` | Same body → removes it.                                           |
| `GET /api/bot/v1/events`                       | none              | SSE event stream (see below).                                     |

---

## Incoming webhooks (simplest integration)

`POST /api/bot/hooks/:webhookId` — **no auth header**; the `webhookId` in the
URL is itself the secret. Body:

```json
{ "text": "deploy finished ✅", "channel_id": "general" }
```

`channel_id` is optional: it falls back to the bot's `default_channel`, then to
`general`. The message is posted as `app:<id>`. Unknown webhook id → `404`.

```bash
curl -X POST http://localhost:8080/api/bot/hooks/$WEBHOOK_ID \
  -H 'Content-Type: application/json' \
  -d '{"text":"hello from CI"}'
```

---

## Outbound events (signed webhooks)

When a bot has `event_url` set, Talk POSTs JSON events to it (fire-and-forget,
~5s client timeout, best-effort, never blocking the originating request).

### Headers

```
Content-Type: application/json
X-Vulos-Request-Timestamp: <unix seconds>
X-Vulos-Signature: v0=<hex hmac-sha256>
```

The signature is computed over the exact basestring:

```
basestring = "<timestamp>" + "." + <raw request body bytes>
signature  = "v0=" + hex( HMAC_SHA256(signing_secret, basestring) )
```

This is Slack-compatible. **Verify** by recomputing the signature over the
timestamp + the raw body you received and comparing in constant time; reject
stale timestamps (the example bot uses a 5-minute window) to blunt replay.

#### Worked verification example (pseudocode)

```
ts   = header["X-Vulos-Request-Timestamp"]
sig  = header["X-Vulos-Signature"]
if abs(now() - int(ts)) > 300: reject            # replay window
mine = "v0=" + hex(hmac_sha256(secret, ts + "." + rawBody))
if not constant_time_equals(mine, sig): reject    # forged / wrong secret
```

#### Go snippet

```go
func verify(ts string, body []byte, secret, sig string) bool {
    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write([]byte(ts)); mac.Write([]byte(".")); mac.Write(body)
    expected := "v0=" + hex.EncodeToString(mac.Sum(nil))
    return hmac.Equal([]byte(expected), []byte(sig))
}
```

A complete, dependency-free verifier + echo bot lives in
[`examples/echo-bot/main.go`](../examples/echo-bot/main.go).

### Event envelope

After the platform migration the envelope carries `app_id` + `product` (the old
`bot_id` + `team` fields are gone — a consumer that read only `type` + `event`,
like the example echo-bot, is unaffected):

```json
{
  "type": "app_mention",
  "app_id": "…",
  "product": "talk",
  "event": { /* per-type payload */ },
  "event_time": 1700000000
}
```

### Event types

| `type`            | `event` payload                                                         | Delivered when                                                            |
| ----------------- | ---------------------------------------------------------------------- | ------------------------------------------------------------------------ |
| `message.created` | `{channel_id, message_id, author_id, text, thread_parent}`            | A new top-level/thread message in a channel the bot can see. A bot never receives its **own** messages. |
| `app_mention`     | same as `message.created`                                              | The message text mentions the bot (`@<name>` or `<@app:<id>>`).          |
| `member_joined`   | `{channel_id, account_id}`                                            | Someone joins a channel the bot is in.                                    |
| `slash_command`   | `{command, text, channel_id, user_id}`                               | A user invokes a slash command the bot registered (see below).           |

---

## Slash commands

A bot registers command names (without the leading slash) in `slash_commands`.

Dispatch happens on the Spaces message-send path: when a user sends a message
whose body starts with `/` and whose first token matches a registered command,
Talk does **not** store it as a channel message. Instead it emits a
`slash_command` event to the owning bot and responds to the sender with:

```json
{ "slash": true, "command": "deploy", "dispatched": true }   // HTTP 200
```

If the first token matches no registered command, the message is stored
normally (HTTP 201).

---

## Socket-mode style event stream (SSE)

For bots behind NAT (no public `event_url`), open:

```
GET /api/bot/v1/events
Authorization: Bearer vat_…
```

The response is `text/event-stream`. Talk pushes the **same event JSON objects**
as `data:` frames as they occur. The stream is authenticated by the bot token
(over TLS), so events on the stream are not separately HMAC-signed. Subscriptions
are tracked per-bot in memory and cleaned up on disconnect. A slow consumer that
falls behind simply drops events (the stream never blocks delivery to others).

```bash
curl -N http://localhost:8080/api/bot/v1/events -H "Authorization: Bearer $TOKEN"
# data: {"type":"app_mention","app_id":"…","product":"talk","event":{…},"event_time":…}
```

---

## The registry seam (standalone vs. Cloud-managed control plane)

The bot **Registry** is an interface (`backend/bots/registry.go`) — the same
"seam" pattern as `backend/seam`:

- **Standalone default** (`backend/bots/store_standalone.go`,
  `StandaloneRegistry`) lives in-repo and is backed by a pure-Go modernc SQLite
  database (env `VULOS_BOTS_DB`, default `<DataDir>/bots.db`), with an in-memory
  fallback (`NewMemoryRegistry`) for tests / degraded boot. Bot tokens are
  stored as sha256 hashes; signing secrets are stored as-is.

- **Cloud control plane** — a Vulos Cloud "developer console" would implement
  the **same `bots.Registry` interface** in a **separate package** (e.g.
  `backend/integration/cloud`) that the **core never imports**. Only the
  composition root (`main.go`) decides which implementation to wire, and only
  when explicitly selected. Removing the cloud package never breaks the core
  build. In that mode, bot creation/rotation/listing would be brokered by the
  control plane (org-scoped, centrally audited), while the runtime data plane
  (REST API, events, webhooks, SSE) is unchanged — it depends only on the
  interface.

The dispatcher (`backend/bots/dispatcher.go`) depends only on the `Registry`
interface and a minimal `Spaces` view, so it is agnostic to which registry
backs it.
