#!/usr/bin/env node
/**
 * Vulos Talk — demo data seeder
 *
 * Seeds a running Talk backend with a realistic, multi-author team chat via the
 * REST API: public channels ("Spaces"), DMs, a threaded conversation, emoji
 * reactions, a pinned message, and a few scheduled huddles.
 *
 * Multi-author messages are produced by running the backend with auth ENABLED
 * and a known JWT secret, then minting a short-lived HS256 token per author and
 * sending each message with that bearer token. The backend derives the message
 * author from the verified token subject — never from a client header — so this
 * is the only honest way to seed messages attributed to different people.
 *
 * Usage (standalone — server must already be running with auth enabled and the
 * matching VULOS_TALK_JWT_SECRET):
 *   VULOS_TALK_JWT_SECRET=<secret> node scripts/seed-demo.mjs --base-url http://localhost:8083
 *
 * The screenshotter (scripts/screenshots.mjs) calls seedViaAPI() automatically.
 *
 * Data dir: /tmp/vulos-talk-demo-data  (never touches ./data)
 */

import crypto from 'node:crypto'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

export const DEMO_DATA_DIR = '/tmp/vulos-talk-demo-data'

// A fixed, clearly-labelled secret used ONLY for local demo/screenshot seeding.
// The screenshotter starts the backend with this same value.
export const DEMO_SECRET =
  process.env.VULOS_TALK_JWT_SECRET || 'vulos-talk-demo-screenshot-secret-do-not-use-in-prod'

// Demo cast. Keys are account ids (rendered as the message author handle); the
// value is the friendly name used for DMs / display.
export const AUTHORS = {
  you:     'You',
  amara:   'Amara Diallo',
  sipho:   'Sipho Ndlovu',
  kefilwe: 'Kefilwe Mthembu',
  zanele:  'Zanele Khumalo',
  tendai:  'Tendai Moyo',
}

const BASE_URL =
  process.env.BASE_URL ??
  process.argv.find((a) => a.startsWith('http')) ??
  'http://localhost:8083'

// ── tiny HS256 JWT signer (no external deps) ──────────────────────────────────
function b64url(input) {
  return Buffer.from(input).toString('base64url')
}

export function mintToken(sub, secret = DEMO_SECRET, ttlSec = 86400) {
  const header = b64url(JSON.stringify({ alg: 'HS256', typ: 'JWT' }))
  const now = Math.floor(Date.now() / 1000)
  const payload = b64url(JSON.stringify({ sub, iat: now, exp: now + ttlSec }))
  const data = `${header}.${payload}`
  const sig = crypto.createHmac('sha256', secret).update(data).digest('base64url')
  return `${data}.${sig}`
}

// ── HTTP helpers ──────────────────────────────────────────────────────────────
async function post(baseURL, p, body, token) {
  const headers = { 'Content-Type': 'application/json' }
  if (token) headers.Authorization = `Bearer ${token}`
  const r = await fetch(`${baseURL}/api${p}`, {
    method: 'POST',
    headers,
    body: JSON.stringify(body ?? {}),
  })
  if (!r.ok) {
    const t = await r.text()
    throw new Error(`POST ${p} → ${r.status}: ${t}`)
  }
  return r.json().catch(() => ({}))
}

async function tryPost(baseURL, p, body, token) {
  try { return await post(baseURL, p, body, token) }
  catch (e) { console.warn(`  [warn] seed: ${e.message}`); return null }
}

// ── seed via REST API ─────────────────────────────────────────────────────────
export async function seedViaAPI(baseURL = BASE_URL, secret = DEMO_SECRET) {
  console.log(`\n  seeding via API → ${baseURL}`)

  const tok = Object.fromEntries(
    Object.keys(AUTHORS).map((id) => [id, mintToken(id, secret)]),
  )

  // Create a public channel as `you`, then have the named members join so the
  // channel roster (header pills) is populated.
  async function publicChannel(name, members = []) {
    const ch = await tryPost(baseURL, '/spaces/channels', { name, type: 'public' }, tok.you)
    if (!ch?.id) return null
    for (const m of members) {
      await tryPost(baseURL, `/spaces/channels/${ch.id}/join`, {}, tok[m])
    }
    return ch
  }

  // Use an already-existing channel by id (the backend auto-creates #general at
  // startup); just have members join it so we don't create a duplicate.
  async function existingChannel(id, members = []) {
    await tryPost(baseURL, `/spaces/channels/${id}/join`, {}, tok.you)
    for (const m of members) {
      await tryPost(baseURL, `/spaces/channels/${id}/join`, {}, tok[m])
    }
    return { id }
  }

  // Post a message as a given author; returns the created message (with id).
  async function say(channelId, author, body) {
    return tryPost(baseURL, `/spaces/channels/${channelId}/messages`, { body }, tok[author])
  }

  async function react(channelId, msgId, author, emoji) {
    if (!msgId) return
    await tryPost(baseURL, `/spaces/messages/${msgId}/react`, { emoji, channel_id: channelId }, tok[author])
  }

  async function pin(channelId, msgId) {
    if (!msgId) return
    await tryPost(baseURL, `/spaces/channels/${channelId}/pins`, { message_id: msgId }, tok.you)
  }

  async function reply(channelId, parentId, author, body) {
    if (!parentId) return
    await tryPost(baseURL, `/spaces/channels/${channelId}/threads/${parentId}/reply`, { body }, tok[author])
  }

  // ── #general (reuse the backend's auto-created channel) ───────────────────────
  const general = await existingChannel('general', ['amara', 'sipho', 'kefilwe', 'zanele', 'tendai'])
  if (general?.id) {
    const welcome = await say(general.id, 'amara',
      'Morning everyone 👋 Welcome to **Vulos Talk** — this is `#general`. Standup huddle at 9:30.')
    await say(general.id, 'sipho',
      'Q3 planning doc is ready for review. Drop comments before Thursday please.')
    await say(general.id, 'kefilwe',
      'New dark-mode palette landed — the warm-paper + single teal accent looks great in the message list.')
    const ship = await say(general.id, 'zanele',
      'Heads up: durable Spaces messages are live. Threads, reactions and pins now survive a restart. 🎉')
    await say(general.id, 'tendai',
      'Maintenance window tonight 18:00–18:10 SAST. No expected downtime for huddles.')
    // Reactions
    await react(general.id, welcome?.id, 'sipho', '👋')
    await react(general.id, welcome?.id, 'kefilwe', '👋')
    await react(general.id, ship?.id, 'amara', '🎉')
    await react(general.id, ship?.id, 'kefilwe', '🎉')
    await react(general.id, ship?.id, 'tendai', '🚀')
    // Pin the announcement
    await pin(general.id, ship?.id)
  }

  // ── #engineering (with a thread) ──────────────────────────────────────────────
  const eng = await publicChannel('engineering', ['amara', 'zanele', 'tendai'])
  if (eng?.id) {
    await say(eng.id, 'amara', 'Pushed the presence-heartbeat fix — roster no longer flaps on reconnect. PR is up.')
    const thread = await say(eng.id, 'zanele',
      'Should huddle signalling move to the dedicated **vulos-meet** service, or stay in Talk for now?')
    await say(eng.id, 'tendai', 'SQLite FTS5 search for Spaces is merged — try the in-channel search bar.')
    // Threaded replies under zanele's question
    await reply(eng.id, thread?.id, 'amara', 'Long term it routes through vulos-meet (seam-C). For now the WebRTC backend stays here.')
    await reply(eng.id, thread?.id, 'tendai', 'Agreed — keep the /meet + /turn surface until the handoff lands so self-host still works standalone.')
    await reply(eng.id, thread?.id, 'zanele', 'Makes sense. I\'ll leave a TODO(seam-C) marker on the meeting routes. 👍')
    await react(eng.id, thread?.id, 'amara', '🤔')
  }

  // ── #design ───────────────────────────────────────────────────────────────────
  const design = await publicChannel('design', ['kefilwe', 'sipho'])
  if (design?.id) {
    await say(design.id, 'kefilwe', 'Sidebar icon sizing pass is done — labels no longer clip at 1280px.')
    await say(design.id, 'sipho', 'Love it. Can we get the same treatment on the huddle controls?')
  }

  // ── #random ────────────────────────────────────────────────────────────────────
  const random = await publicChannel('random', ['amara', 'sipho', 'kefilwe', 'zanele', 'tendai'])
  if (random?.id) {
    const coffee = await say(random.id, 'tendai', 'Coffee huddle in 10? â')
    await say(random.id, 'kefilwe', 'Always.')
    await react(random.id, coffee?.id, 'amara', '☕')
    await react(random.id, coffee?.id, 'zanele', '☕')
  }

  // ── A direct message (you ↔ amara) ──────────────────────────────────────────────
  const dmName = ['you', 'amara'].sort().join('-')
  const dm = await tryPost(baseURL, '/spaces/channels',
    { name: dmName, type: 'dm', members: ['you', 'amara'], member_names: { amara: 'Amara Diallo' } }, tok.you)
  if (dm?.id) {
    await say(dm.id, 'amara', 'Can you review the seam-C write-up before the arch huddle?')
    await say(dm.id, 'you', 'On it — will leave notes in #engineering.')
  }

  // ── Scheduled huddles (meetings dashboard) ──────────────────────────────────────
  const weekStart = new Date()
  weekStart.setHours(0, 0, 0, 0)
  const dow = weekStart.getDay()
  weekStart.setDate(weekStart.getDate() - (dow === 0 ? 6 : dow - 1))
  const at = (days, h, m = 0) => {
    const d = new Date(weekStart)
    d.setDate(d.getDate() + days)
    d.setHours(h, m, 0, 0)
    return d.toISOString()
  }

  const huddles = [
    { title: 'Daily Standup',           scheduled_at: at(0, 9, 30),  duration_min: 15, lobby_required: false, invitees: ['amara@vulos.org', 'sipho@vulos.org', 'zanele@vulos.org'], host_vulos: 'amara@vulos.org' },
    { title: 'Architecture Huddle — seam-C', scheduled_at: at(3, 14, 0), duration_min: 60, lobby_required: true,  invitees: ['zanele@vulos.org', 'tendai@vulos.org'], host_vulos: 'amara@vulos.org' },
    { title: 'Design Review',           scheduled_at: at(1, 11, 0),  duration_min: 45, lobby_required: false, invitees: ['sipho@vulos.org'], host_vulos: 'kefilwe@vulos.org' },
    { title: 'Q3 All-Hands',            scheduled_at: at(4, 15, 0),  duration_min: 90, lobby_required: true,  invitees: ['amara@vulos.org', 'sipho@vulos.org', 'kefilwe@vulos.org', 'zanele@vulos.org', 'tendai@vulos.org'], host_vulos: 'you@vulos.org' },
  ]
  for (const h of huddles) await tryPost(baseURL, '/meetings', h, tok.you)

  console.log('  API seed complete')
}

// ── Main (standalone) ──────────────────────────────────────────────────────────
async function main() {
  console.log('\nVulos Talk — demo seeder')
  console.log(`  api base : ${BASE_URL}`)
  await seedViaAPI(BASE_URL, DEMO_SECRET)
  console.log('\nSeed done.\n')
}

if (process.argv[1] && fileURLToPath(import.meta.url) === path.resolve(process.argv[1])) {
  main().catch((e) => { console.error('Fatal:', e); process.exit(1) })
}
