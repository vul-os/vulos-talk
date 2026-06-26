#!/usr/bin/env node
/**
 * Vulos Talk — Playwright screenshotter
 *
 * Captures the main Talk surfaces at 1440×900 into docs/screenshots/ using a
 * fully seeded, zero-setup demo backend (no real accounts or cloud needed).
 *
 * Default (local) mode:
 *   1. Builds the frontend (dist/) if missing and the Go binary.
 *   2. Starts the binary on port 8083 with auth ENABLED and a known demo JWT
 *      secret, pointed at a throwaway data dir (/tmp/vulos-talk-demo-data).
 *   3. Seeds Spaces channels, a threaded multi-author conversation, reactions,
 *      a pinned message, a DM and scheduled huddles via the REST API.
 *   4. Injects a demo session cookie and captures the screens.
 *   5. Stops the server.
 *
 * Usage:
 *   npm run screenshots
 *   BASE_URL=https://talk.example.com npm run screenshots   # against a live instance
 *
 * Prerequisites:
 *   npm install && npx playwright install chromium
 *
 * Limitations: live huddle video (WebRTC) and real-time websocket presence
 * cannot be seeded headlessly, so the huddles screen shows the scheduling
 * dashboard rather than an in-call view. See docs/SCREENSHOTS.md.
 */

import { chromium } from 'playwright'
import { mkdirSync, writeFileSync, existsSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import path from 'node:path'
import { spawn, execSync } from 'node:child_process'

import { seedViaAPI, mintToken, DEMO_DATA_DIR, DEMO_SECRET } from './seed-demo.mjs'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const ROOT = path.resolve(__dirname, '..')
const OUT = path.join(ROOT, 'docs', 'screenshots')

const EXTERNAL_URL = process.env.BASE_URL
const LOCAL_PORT = 8083
const LOCAL_BASE = `http://localhost:${LOCAL_PORT}`
const BASE = EXTERNAL_URL ?? LOCAL_BASE

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

async function waitForHTTP(url, maxMs = 45_000) {
  const deadline = Date.now() + maxMs
  while (Date.now() < deadline) {
    try {
      const r = await fetch(url, { signal: AbortSignal.timeout(1_500) })
      if (r.status < 600) return
    } catch { /* not ready */ }
    await sleep(500)
  }
  throw new Error(`${url} did not become ready within ${maxMs}ms`)
}

let serverProc = null

async function startLocalServer() {
  console.log('\n  setting up demo environment …')

  if (!existsSync(path.join(ROOT, 'dist', 'index.html'))) {
    console.log('  building frontend (dist/) …')
    execSync('npm run build:frontend', { cwd: ROOT, stdio: 'pipe' })
  }

  const binPath = '/tmp/vulos-talk-screenshots-bin'
  console.log('  building Go binary …')
  execSync(`go build -o "${binPath}" .`, { cwd: ROOT, stdio: 'pipe' })

  const tmpWD = '/tmp/vulos-talk-ss-wd'
  mkdirSync(tmpWD, { recursive: true })
  mkdirSync(DEMO_DATA_DIR, { recursive: true })
  // Auth is ENABLED so seeded messages can be attributed to different authors
  // (the backend derives identity from the verified JWT subject only).
  writeFileSync(`${tmpWD}/config.yaml`, [
    'server:',
    `  addr: ":${LOCAL_PORT}"`,
    `  data_dir: "${DEMO_DATA_DIR}"`,
    `  uploads_dir: "${DEMO_DATA_DIR}/uploads"`,
    'auth:',
    '  enabled: true',
    'storage:',
    '  type: "local"',
  ].join('\n') + '\n')

  serverProc = spawn(binPath, [], {
    cwd: tmpWD,
    env: {
      ...process.env,
      VULOS_TALK_JWT_SECRET: DEMO_SECRET,
      VULOS_LOBBY_DB: `${DEMO_DATA_DIR}/lobby.db`,
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  serverProc.stdout.on('data', (d) => process.stdout.write(`  [go] ${d}`))
  serverProc.stderr.on('data', (d) => process.stdout.write(`  [go] ${d}`))

  await waitForHTTP(`${LOCAL_BASE}/version`)
  console.log(`  server ready at ${LOCAL_BASE}`)

  await seedViaAPI(LOCAL_BASE, DEMO_SECRET)
  await sleep(1_000)
}

function stopLocalServer() {
  if (serverProc) { try { serverProc.kill() } catch {} ; serverProc = null }
}

async function shot(page, name, description) {
  const outPath = path.join(OUT, `${name}.png`)
  await page.screenshot({ path: outPath, fullPage: false })
  console.log(`  [ok] ${name}.png — ${description}`)
  return { name, description, status: 'ok' }
}

// Find the seeded channel id for a given name from the API (auth via cookie token).
async function channelIdByName(token, name) {
  try {
    const r = await fetch(`${BASE}/api/spaces/channels`, {
      headers: { Authorization: `Bearer ${token}` },
    })
    const chs = await r.json()
    const ch = (chs || []).find((c) => c.name === name)
    return ch?.id ?? null
  } catch { return null }
}

async function main() {
  mkdirSync(OUT, { recursive: true })
  console.log('\nVulos Talk screenshotter')
  console.log(`  target   : ${BASE}`)
  console.log(`  output   : ${path.relative(ROOT, OUT)}/`)

  if (!EXTERNAL_URL) await startLocalServer()

  const sessionToken = mintToken('you', DEMO_SECRET)

  const browser = await chromium.launch({ headless: true })
  const context = await browser.newContext({
    viewport: { width: 1440, height: 900 },
    colorScheme: 'dark',
    locale: 'en-US',
  })
  // RequireAuth checks /api/auth/me via the `session` cookie; inject it so the
  // SPA renders as the demo user `you` without a login round-trip.
  await context.addCookies([{
    name: 'session',
    value: sessionToken,
    url: BASE,
  }])

  const page = await context.newPage()
  page.on('pageerror', () => {})

  const results = []
  const engId = await channelIdByName(sessionToken, 'engineering')
  const dmId = await channelIdByName(sessionToken, 'amara-you')

  const gotoChannel = async (urlPath) => {
    await page.goto(`${BASE}${urlPath}`, { waitUntil: 'domcontentloaded', timeout: 20_000 })
    await page.waitForSelector('[data-msg-id]', { timeout: 15_000 }).catch(() => {})
    await sleep(1_000)
  }

  try {
    // 1. Hero — #general with the seeded multi-author conversation (deterministic;
    //    the backend auto-creates this channel with id "general").
    await gotoChannel('/channels/general')
    results.push(await shot(page, 'hero', 'Spaces — #general with a seeded team conversation'))

    // 2. A channel with a thread — open the thread side-panel.
    if (engId) {
      await gotoChannel(`/channels/${engId}`)
      // Target the "N replies" affordance (a digit before "repl"), not the
      // hidden per-message "Reply in thread" buttons.
      const repliesBtn = page.locator('button', { hasText: /\d+\s+repl(y|ies)/ }).first()
      if (await repliesBtn.count()) {
        await repliesBtn.click().catch(() => {})
        await sleep(800)
        results.push(await shot(page, 'thread', 'Threaded reply panel in #engineering'))
      } else {
        results.push(await shot(page, 'thread', '#engineering channel (thread affordance not found)'))
      }
    }

    // 3. A direct message.
    if (dmId) {
      await gotoChannel(`/dm/${dmId}`)
      results.push(await shot(page, 'dm', 'Direct message between two teammates'))
    }

    // 4. In-channel search — open the search panel in #general and run a query.
    await gotoChannel('/channels/general')
    const searchBtn = page.locator('button[aria-label="Search in channel"], button[title="Search in channel"]').first()
    if (await searchBtn.count()) {
      await searchBtn.click().catch(() => {})
      await sleep(400)
      const searchInput = page.locator('input[type="search"], input[placeholder*="earch"]').first()
      if (await searchInput.count()) {
        await searchInput.fill('durable').catch(() => {})
        await sleep(900)
      }
      results.push(await shot(page, 'search', 'In-channel search with live results'))
    }

    // 5. Slash-command autocomplete — type "/" in the composer to surface the
    //    registered bot commands (fed by GET /api/spaces/commands).
    await gotoChannel('/channels/general')
    const composer = page.locator('textarea').first()
    if (await composer.count()) {
      await composer.click().catch(() => {})
      await composer.type('/', { delay: 60 }).catch(() => {})
      await sleep(800)
      results.push(await shot(page, 'slash-command', 'Slash-command autocomplete in the composer'))
      await composer.fill('').catch(() => {})
    }

    // 6. Apps & Bots — the standalone bot admin console (route /apps).
    await page.goto(`${BASE}/apps`, { waitUntil: 'domcontentloaded', timeout: 20_000 })
    await sleep(1_500)
    results.push(await shot(page, 'bots', 'Apps & Bots — create and manage bot apps + tokens'))

    // 7. Mobile — single-column layout with bottom nav at 390×844.
    const mobile = await context.newPage()
    mobile.on('pageerror', () => {})
    await mobile.setViewportSize({ width: 390, height: 844 })
    await mobile.goto(`${BASE}/channels/general`, { waitUntil: 'domcontentloaded', timeout: 20_000 })
    await mobile.waitForSelector('[data-msg-id]', { timeout: 15_000 }).catch(() => {})
    await sleep(1_200)
    const mOut = path.join(OUT, 'mobile.png')
    await mobile.screenshot({ path: mOut, fullPage: false })
    console.log('  [ok] mobile.png — single-column mobile layout')
    results.push({ name: 'mobile', description: 'Single-column mobile layout (390×844)', status: 'ok' })
    await mobile.close()

    // 8. Huddles — the meeting/scheduling dashboard (/meet/:id renders it).
    await page.goto(`${BASE}/meet/dashboard`, { waitUntil: 'domcontentloaded', timeout: 20_000 })
    await sleep(1_500)
    results.push(await shot(page, 'huddles', 'Huddles — scheduled meeting rooms with join links'))
  } finally {
    await browser.close()
    if (!EXTERNAL_URL) stopLocalServer()
  }

  // Per-directory manifest.
  const notes = [
    '# docs/screenshots',
    '',
    'Generated by `npm run screenshots` (scripts/screenshots.mjs) against a',
    'seeded demo backend (scripts/seed-demo.mjs). Regenerate with `npm run screenshots`.',
    '',
    '| File | Surface |',
    '|------|---------|',
    ...results.map((r) => `| ${r.name}.png | ${r.description} |`),
    '',
    '## Limitations',
    '',
    'Live huddle video (WebRTC) and real-time websocket presence cannot be seeded',
    'headlessly, so `huddles.png` shows the scheduling dashboard rather than an',
    'in-call view. Everything else is real, seeded data served by the Go binary.',
  ].join('\n')
  writeFileSync(path.join(OUT, 'README.md'), notes + '\n')

  console.log(`\nDone — ${results.length} screenshots in ${path.relative(ROOT, OUT)}/`)
}

main().catch((err) => {
  stopLocalServer()
  console.error('Fatal:', err)
  process.exit(1)
})
