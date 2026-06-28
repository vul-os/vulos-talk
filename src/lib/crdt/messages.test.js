/**
 * messages.test.js — convergence + rapid-update tests for the browser-side CRDT
 * MessageStore. Mirrors the Go store_test.go convergence suite so the two
 * replicas (browser + server) agree on the merge function: append idempotency,
 * edit LWW, terminal tombstones, and commutative merges under reordering.
 */
import { describe, it, expect } from 'vitest'
import {
  MessageStore,
  OP_APPEND,
  OP_EDIT,
  OP_TOMBSTONE,
  STATE_ACTIVE,
  STATE_EDITED,
  STATE_DELETED,
} from './messages.js'

const CH = 'general'

describe('MessageStore — local mutations', () => {
  it('send appends an active message with a non-empty clock', () => {
    const s = new MessageStore('node-A')
    const m = s.send(CH, 'alice', 'hello')
    expect(m.state).toBe(STATE_ACTIVE)
    expect(m.seq_clock).toBeTruthy()
    expect(s.listMessages(CH)).toHaveLength(1)
  })

  it('edit marks the message edited and changes the body (LWW clock advances)', () => {
    const s = new MessageStore('node-A')
    const m = s.send(CH, 'alice', 'v1')
    const e = s.edit(CH, m.id, 'v2')
    expect(e.body).toBe('v2')
    expect(e.state).toBe(STATE_EDITED)
    expect(e.seq_clock > m.seq_clock).toBe(true)
  })

  it('delete tombstones terminally — a later edit throws', () => {
    const s = new MessageStore('node-A')
    const m = s.send(CH, 'alice', 'secret')
    s.delete(CH, m.id)
    const got = s.listMessages(CH)[0]
    expect(got.state).toBe(STATE_DELETED)
    expect(got.body).toBe('')
    expect(() => s.edit(CH, m.id, 'resurrect')).toThrow()
  })
})

describe('MessageStore — rapid updates', () => {
  it('hands out strictly increasing clocks for a burst of sends', () => {
    const s = new MessageStore('node-A')
    const clocks = []
    for (let i = 0; i < 500; i++) clocks.push(s.send(CH, 'alice', `m${i}`).seq_clock)
    const sorted = [...clocks].sort()
    expect(clocks).toEqual(sorted) // monotonic in creation order
    expect(new Set(clocks).size).toBe(clocks.length) // all unique
  })

  it('listMessages stays sorted by clock under interleaved send/edit', () => {
    const s = new MessageStore('node-A')
    const ids = []
    for (let i = 0; i < 50; i++) ids.push(s.send(CH, 'u', `m${i}`).id)
    // Edit a scattering of messages — order must remain clock-sorted.
    for (let i = 0; i < 50; i += 7) s.edit(CH, ids[i], `edited-${i}`)
    const list = s.listMessages(CH)
    for (let i = 1; i < list.length; i++) {
      expect(list[i - 1].seq_clock <= list[i].seq_clock).toBe(true)
    }
  })
})

describe('MessageStore — merge convergence', () => {
  // Build a canonical op log from a source replica.
  function sourceOps() {
    const src = new MessageStore('src')
    const a = src.send(CH, 'alice', 'first')
    const b = src.send(CH, 'bob', 'second')
    src.edit(CH, a.id, 'first (edited)')
    src.delete(CH, b.id)
    return src.exportOps(CH)
  }

  it('mergeOps is idempotent — applying twice equals applying once', () => {
    const ops = sourceOps()
    const once = new MessageStore('r1')
    once.mergeOps(ops)
    const twice = new MessageStore('r2')
    twice.mergeOps(ops)
    twice.mergeOps(ops) // redundant replay
    expect(index(twice.listMessages(CH))).toEqual(index(once.listMessages(CH)))
  })

  it('mergeOps is commutative — reversed order converges to the same state', () => {
    const ops = sourceOps()
    const fwd = new MessageStore('r1')
    fwd.mergeOps(ops)
    const rev = new MessageStore('r2')
    rev.mergeOps([...ops].reverse())
    expect(index(rev.listMessages(CH))).toEqual(index(fwd.listMessages(CH)))
  })

  it('edit LWW: a higher-clock edit wins regardless of arrival order', () => {
    const base = new MessageStore('src')
    const m = base.send(CH, 'alice', 'v0')
    const editLow = { op: OP_EDIT, channel_id: CH, msg: { ...m, body: 'low', state: STATE_EDITED, seq_clock: '00000000000000000001-0000000000-x' } }
    const editHigh = { op: OP_EDIT, channel_id: CH, msg: { ...m, body: 'high', state: STATE_EDITED, seq_clock: '99999999999999999999-0000000000-y' } }
    const append = { op: OP_APPEND, channel_id: CH, msg: m }

    const r = new MessageStore('r')
    r.mergeOps([append, editHigh, editLow]) // high arrives before low
    expect(r.listMessages(CH)[0].body).toBe('high')
  })

  it('tombstone always wins even if it arrives before the append', () => {
    const m = new MessageStore('src').send(CH, 'alice', 'doomed')
    const append = { op: OP_APPEND, channel_id: CH, msg: m }
    const tomb = { op: OP_TOMBSTONE, channel_id: CH, msg: { ...m, body: '', state: STATE_DELETED, seq_clock: '00000000000000000001-0000000000-x' } }

    const r = new MessageStore('r')
    r.mergeOps([tomb, append]) // tombstone first, then a stale append
    const got = r.listMessages(CH)[0]
    expect(got.state).toBe(STATE_DELETED)
    expect(got.body).toBe('')
  })

  it('exportOps(afterClock) returns only newer ops for cold-joiner catch-up', () => {
    const s = new MessageStore('src')
    const m1 = s.send(CH, 'a', 'one')
    s.send(CH, 'a', 'two')
    const after = s.exportOps(CH, m1.seq_clock)
    expect(after.every((op) => op.msg.seq_clock > m1.seq_clock)).toBe(true)
    expect(after).toHaveLength(1)
  })
})

function index(msgs) {
  const out = {}
  for (const m of msgs) out[m.id] = `${m.state}:${m.body}`
  return out
}
