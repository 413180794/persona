import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createRuntime, interactionId, light, PAGE_TEXT, RpcError } from '../index.js'
import { startFakeNode, until } from '../testing/fake-node.js'
import { createEchoRuntime } from '../examples/echo-runtime.mjs'

const minimal = (extra = {}) => ({ listSessions: async () => [{ session_id: 's1', status: 'idle' }], items: async () => ({ items: [], to_seq: 0 }), ...extra })

test('hello names the runtime and declares only the caps whose driver methods exist', async (t) => {
  const n = await startFakeNode(t)
  const rt = createRuntime(minimal({ send: async () => {} }), { runtime: 'toy', name: 'Toy', agent: 'claude', launcher: 'toy', socketPath: n.path, retryMs: 20 })
  t.after(() => rt.stop())
  rt.start()
  await n.ready
  assert.equal(n.hello.runtime, 'toy')
  assert.equal(n.hello.name, 'Toy')
  assert.equal(n.hello.agent, 'claude')
  assert.deepEqual(n.hello.caps, { start: false, send: true, interrupt: false, rename: false, close: false })
  assert.equal(n.hello.launcher, undefined, 'no launcher without start')
  await until('list', () => n.last('runtime.sessions'))
  await n.peer.call('session.send', { session_id: 's1', text: 'x' })
  await assert.rejects(n.peer.call('session.interrupt', { session_id: 's1' }), /unsupported/)
  assert.throws(() => createRuntime(minimal(), {}), /runtime id/)
})

test('appended content goes out as continuous batches, and a gap says where it starts', async (t) => {
  const n = await startFakeNode(t)
  const rt = createRuntime(minimal({ cursor: async () => 10 }), { runtime: 'toy', socketPath: n.path, retryMs: 20 })
  t.after(() => rt.stop())
  rt.start()
  await n.ready
  rt.append('s1', { items: [{ id: 'x' }], fromSeq: 9, toSeq: 10 }) // not watched: nothing is pushed
  await n.peer.call('session.watch', { session_id: 's1', since_seq: 0 })
  for (const seq of [11, 12]) rt.append('s1', { items: [{ id: `i${seq}`, seq }], fromSeq: seq - 1, toSeq: seq, history: 'h' })
  rt.append('s1', { items: [], fromSeq: 12, toSeq: 13, history: 'h' }) // a step the chat does not show still moves the seq
  await until('first batch', () => n.all('session.items').length === 1)
  assert.deepEqual(n.all('session.items')[0], { session_id: 's1', items: [{ id: 'i11', seq: 11 }, { id: 'i12', seq: 12 }], from_seq: 10, to_seq: 13, history: 'h' })
  rt.append('s1', { items: [{ id: 'i20', seq: 20 }], fromSeq: 19, toSeq: 20, history: 'h' })
  await until('gap batch', () => n.all('session.items').length === 2)
  assert.equal(n.all('session.items')[1].from_seq, 19)
})

test('content committed while a watch catches up is held, then sent once without repeats', async (t) => {
  const n = await startFakeNode(t)
  let release
  const gate = new Promise((r) => { release = r })
  const rt = createRuntime(minimal({
    items: async (p) => {
      await gate
      return { items: [{ id: 'c', seq: 3 }], history: 'h', from_seq: p.after_seq, to_seq: 3 }
    },
  }), { runtime: 'toy', socketPath: n.path, retryMs: 20 })
  t.after(() => rt.stop())
  rt.start()
  await n.ready
  const watching = n.peer.call('session.watch', { session_id: 's1', since_seq: 2 })
  await until('watching', () => rt.watching('s1'))
  rt.append('s1', { items: [{ id: 'c', seq: 3 }], fromSeq: 2, toSeq: 3, history: 'h' }) // also in the catch-up page
  rt.append('s1', { items: [{ id: 'd', seq: 4 }], fromSeq: 3, toSeq: 4, history: 'h' })
  release()
  await watching
  await until('two batches', () => n.all('session.items').length === 2)
  const [catchUp, live] = n.all('session.items')
  assert.deepEqual([catchUp.from_seq, catchUp.to_seq, live.from_seq, live.to_seq], [2, 3, 3, 4])
  assert.deepEqual(live.items.map((i) => i.id), ['d'])
})

test('asking the phone marks the session as needing you, and a withdrawn ask takes the card off', async (t) => {
  const n = await startFakeNode(t)
  const rt = createRuntime(minimal({ hasLocalClient: () => false }), { runtime: 'toy', socketPath: n.path, retryMs: 20, pushDelayMs: 5 })
  t.after(() => rt.stop())
  rt.start()
  await n.ready
  await until('listed', () => n.last('runtime.sessions'))
  assert.equal(rt.phoneFirst('s1'), true, 'no local UI open: the phone first')
  assert.equal(rt.phoneFirst('unlisted'), false)
  let answer
  n.resolve = () => new Promise((r) => { answer = r })
  const id = interactionId('toy', 's1', 'permission', 'call-1')
  const asking = rt.ask({ id, kind: 'permission', session_id: 's1', tool_name: 'Bash' })
  await until('blocked', () => n.last('runtime.sessions')?.sessions[0].status === 'blocked')
  answer({ allow: true })
  assert.deepEqual(await asking, { allow: true })
  await until('back to idle', () => n.last('runtime.sessions')?.sessions[0].status === 'idle')

  n.resolve = () => new Promise(() => {}) // never answered
  const ac = new AbortController()
  const withdrawn = rt.ask({ id: 'toy-2', kind: 'question', session_id: 's1' }, { signal: ac.signal })
  await until('asked', () => n.asked.length === 2)
  ac.abort(new Error('gone'))
  await assert.rejects(withdrawn, /gone/)
  await until('cancelled', () => n.last('interaction.cancel')?.id === 'toy-2')
  assert.equal(interactionId('toy', 's1', 'permission', 'call-1'), id, 'stable ids')
})

test('live text is throttled to the latest whole text, and ends empty', async (t) => {
  const n = await startFakeNode(t)
  const rt = createRuntime(minimal(), { runtime: 'toy', socketPath: n.path, retryMs: 20 })
  t.after(() => rt.stop())
  rt.start()
  await n.ready
  rt.stream('s1', 'ignored: not watched')
  await n.peer.call('session.watch', { session_id: 's1', since_seq: 0 })
  for (const text of ['H', 'He', 'Hel', 'Hell', 'Hello']) rt.stream('s1', text)
  await until('the latest text', () => n.last('session.delta')?.text === 'Hello', 1000)
  assert.ok(n.all('session.delta').length <= 2, 'updates within one window are merged')
  assert.ok(!n.all('session.delta').some((d) => d.text.startsWith('ignored')), 'unwatched sessions get no deltas')
  rt.streamEnd('s1')
  await until('end', () => n.last('session.delta')?.text === '')
})

test('item helpers cut long text the way the apps expect', () => {
  const it = light({ kind: 'text', text: 'a'.repeat(PAGE_TEXT + 10) })
  assert.equal(it.full_size, PAGE_TEXT + 10)
  assert.match(it.text, /已截断/)
})

test('the echo example is a complete runtime: start, send, stream, ask, page', async (t) => {
  const n = await startFakeNode(t)
  const { runtime } = createEchoRuntime({ socketPath: n.path, retryMs: 20, pushDelayMs: 5, answerFirst: 'phone' })
  t.after(() => runtime.stop())
  runtime.start()
  await n.ready
  assert.equal(n.hello.launcher, 'echo')
  const { session } = await n.peer.call('session.start', { cwd: '/w/demo' })
  await n.peer.call('session.watch', { session_id: session.session_id, since_seq: 0 })
  n.resolve = () => ({ allow: true })
  await n.peer.call('session.send', { session_id: session.session_id, text: 'please ask first', client_msg_id: 'm1' })
  await until('reply', () => n.all('session.items').flatMap((b) => b.items).some((i) => i.text === 'echo: please ask first'), 3000)
  assert.equal(n.asked[0].kind, 'permission')
  assert.ok(n.all('session.delta').some((d) => d.text.startsWith('echo')))
  const page = await n.peer.call('session.items', { session_id: session.session_id, limit: 10 })
  assert.deepEqual(page.items.map((i) => i.role), ['user', 'assistant'])
  const batches = n.all('session.items')
  assert.deepEqual(batches.slice(1).filter((b, i) => b.from_seq !== batches[i].to_seq), [])
})

test('without a hub on the node the phone is not asked first; it is again once the hub is back', async (t) => {
  const n = await startFakeNode(t, { hub: false })
  const rt = createRuntime(minimal(), { runtime: 'toy', socketPath: n.path, retryMs: 20, pushDelayMs: 5, answerFirst: 'phone' })
  t.after(() => rt.stop())
  rt.start()
  await n.ready
  await until('listed', () => n.last('runtime.sessions'))
  assert.equal(rt.hubConnected, false)
  assert.equal(rt.phoneFirst('s1'), false, 'no hub: the local UI asks, even with answerFirst phone')
  n.setHub(true)
  await until('hub back', () => rt.hubConnected)
  assert.equal(rt.phoneFirst('s1'), true)
  n.setHub(false)
  await until('hub gone', () => !rt.hubConnected)
  assert.equal(rt.phoneFirst('s1'), false)
})

test('while the socket file is there (a node restarting) it retries every second, not on the long back-off', async (t) => {
  const { mkdtempSync, rmSync, writeFileSync } = await import('node:fs')
  const { join } = await import('node:path')
  const dir = mkdtempSync('/tmp/jrt-')
  t.after(() => rmSync(dir, { recursive: true, force: true }))
  const path = join(dir, 'runtime.sock')
  writeFileSync(path, '') // what a node that went away leaves: the file, nobody listening
  const rt = createRuntime(minimal(), { runtime: 'toy', socketPath: path, retryMs: 8000 }) // a long back-off otherwise
  t.after(() => rt.stop())
  rt.start()
  await new Promise((r) => setTimeout(r, 300))
  rmSync(path)
  const started = Date.now()
  const n = await startFakeNode(t, { path }) // the node is back
  await n.ready
  assert.ok(Date.now() - started < 2000, `reconnected after ${Date.now() - started} ms`)
})

test('page: from the end by turns or limit, back from a seq, forward with a cut, and a span', async () => {
  const { page } = await import('../index.js')
  const it = (seq, role, kind = 'text', text = `${role}${seq}`) => ({ id: `${seq}#0`, seq, role, kind, text })
  const all = [it(1, 'user'), it(2, 'assistant'), it(3, 'user'), it(4, 'assistant'), { ...it(4, 'assistant', 'tool_use'), id: '4#1' }, it(5, 'user'), it(6, 'assistant')]
  const at = { history: 'h', toSeq: 9 }
  const last = page(all, { turns: 2 }, at)
  assert.deepEqual(last.items.map((i) => i.seq), [3, 4, 4, 5, 6])
  assert.equal(last.has_more, true)
  assert.equal(last.to_seq, 9)
  const back = page(all, { before_seq: 5, limit: 1 }, at)
  assert.deepEqual(back.items.map((i) => i.id), ['4#0', '4#1'], 'a record is not split')
  assert.equal(back.to_seq, 4)
  const fwd = page(all, { after_seq: 4 }, at)
  assert.deepEqual([fwd.from_seq, fwd.to_seq, fwd.more_after, fwd.items.length], [4, 9, false, 2])
  const span = page(all, { from_seq: 2, through_seq: 3 }, at)
  assert.deepEqual(span.items.map((i) => i.seq), [2, 3])
  const many = Array.from({ length: 600 }, (_, i) => it(i + 1, 'assistant'))
  const cut = page(many, { after_seq: 0 }, { history: 'h', toSeq: 600 })
  assert.deepEqual([cut.items.length, cut.more_after, cut.to_seq], [500, true, 500])
})

test('a runtime that requires a node feature stays off an older node until a new one replaces the socket', async (t) => {
  const { mkdtempSync, rmSync } = await import('node:fs')
  const { join } = await import('node:path')
  const dir = mkdtempSync('/tmp/jrt-')
  t.after(() => rmSync(dir, { recursive: true, force: true }))
  const path = join(dir, 'runtime.sock')
  const old = await startFakeNode(t, { path, features: [] })
  const rt = createRuntime(minimal(), { runtime: 'toy', socketPath: path, retryMs: 20, pushDelayMs: 5, requires: ['prefer_running'] })
  t.after(() => rt.stop())
  rt.start()
  await old.ready
  await new Promise((r) => setTimeout(r, 1500))
  assert.equal(rt.connected, false)
  assert.equal(old.all('runtime.sessions').length, 0, 'nothing listed to an older node')
  assert.equal(old.hellos, 1, 'not reconnecting to the older node meanwhile')
  old.close() // the node restarts as a newer one: a new socket file at the same path
  rmSync(path, { force: true })
  const fresh = await startFakeNode(t, { path })
  await fresh.ready
  await until('listed to the new node', () => fresh.last('runtime.sessions'), 10_000)
  assert.equal(rt.connected, true)
})

test('a send is made once per client_msg_id: a retry gets the first answer, a failed one may go again', async (t) => {
  const n = await startFakeNode(t)
  let calls = 0
  let fail = false
  const rt = createRuntime({ ...minimal(), send: async () => { calls++; if (fail) throw new Error('down') } }, { runtime: 'toy', socketPath: n.path, retryMs: 20, pushDelayMs: 5 })
  t.after(() => rt.stop())
  rt.start()
  await n.ready
  await Promise.all([n.peer.call('session.send', { session_id: 's1', text: 'a', client_msg_id: 'm1' }), n.peer.call('session.send', { session_id: 's1', text: 'a', client_msg_id: 'm1' })])
  await n.peer.call('session.send', { session_id: 's1', text: 'a', client_msg_id: 'm1' })
  assert.equal(calls, 1)
  fail = true
  await assert.rejects(n.peer.call('session.send', { session_id: 's1', text: 'b', client_msg_id: 'm2' }))
  fail = false
  await n.peer.call('session.send', { session_id: 's1', text: 'b', client_msg_id: 'm2' })
  assert.equal(calls, 3)
  assert.equal(rt.touched('s1'), true)
  assert.equal(rt.touched('s2'), false)
})

test('recent: pinned and phone-touched sessions always, the rest by age up to the limit, newest first', async (t) => {
  const n = await startFakeNode(t)
  const rt = createRuntime(minimal({ send: async () => {} }), { runtime: 'toy', socketPath: n.path, retryMs: 20, pushDelayMs: 5 })
  t.after(() => rt.stop())
  rt.start()
  await n.ready
  await n.peer.call('session.send', { session_id: 'old-touched', text: 'x', client_msg_id: 'r1' })
  const day = 86400_000
  const row = (id, ago, running = false) => ({ session_id: id, updated_at: new Date(Date.now() - ago).toISOString(), running })
  const rows = [row('a', 1000), row('b', 2000), row('c', 3000), row('ancient', 30 * day), row('old-running', 40 * day, true), row('old-touched', 50 * day)]
  const got = rt.recent(rows, { days: 7, max: 4, pinned: (r) => r.running })
  assert.deepEqual(got.map((r) => r.session_id), ['a', 'b', 'old-running', 'old-touched'])
  const { appToolName } = await import('../index.js')
  assert.deepEqual([appToolName('bash'), appToolName('find', { find: 'Glob' }), appToolName('mystery')], ['Bash', 'Glob', 'mystery'])
})

test('hello carries one instance per runtime, the same on every reconnect', async (t) => {
  const n = await startFakeNode(t)
  const rt = createRuntime(minimal(), { runtime: 'toy', socketPath: n.path, retryMs: 20 })
  t.after(() => rt.stop())
  rt.start()
  await n.ready
  const first = n.hello.instance
  assert.match(first, /^[0-9a-f-]{36}$/)
  n.peer.close(new Error('node restarts'))
  await until('hello again', () => n.hellos >= 2)
  assert.equal(n.hello.instance, first)
  const other = createRuntime(minimal(), { runtime: 'toy2', socketPath: n.path, retryMs: 20 })
  t.after(() => other.stop())
  other.start()
  await until('the other runtime', () => n.hello.runtime === 'toy2')
  assert.notEqual(n.hello.instance, first)
})

test('a runtime id another process holds is not fought over: it is tried again only every 30 seconds', async (t) => {
  const n = await startFakeNode(t)
  n.conflict()
  const logs = []
  const rt = createRuntime(minimal(), { runtime: 'toy', socketPath: n.path, retryMs: 20, log: (m) => logs.push(m) })
  t.after(() => rt.stop())
  rt.start()
  await until('refused', () => n.hellos >= 1)
  await new Promise((r) => setTimeout(r, 300))
  assert.equal(n.hellos, 1, 'not retried on the short back-off')
  assert.equal(rt.connected, false)
  assert.ok(logs.some((m) => m.includes('another runtime id')), logs.join('\n'))
})

test('the hub\'s tools: listed and called through the node, onHub after hello and as the hub comes and goes', async (t) => {
  const n = await startFakeNode(t, { tools: [{ name: 'phone_contacts', description: 'Contacts', input_schema: { type: 'object' } }] })
  const hubs = []
  const rt = createRuntime(minimal(), { runtime: 'toy', socketPath: n.path, retryMs: 20, onHub: (c) => hubs.push(c) })
  t.after(() => rt.stop())
  assert.deepEqual(await rt.tools(), [], 'no node yet: none')
  rt.start()
  await n.ready
  await until('onHub after hello', () => hubs.length === 1)
  assert.deepEqual(hubs, [true])
  assert.deepEqual((await rt.tools()).map((x) => x.name), ['phone_contacts'])
  const res = await rt.callTool({ name: 'phone_contacts', args: { q: 'li' }, session_id: 's1' })
  assert.equal(res.content[0].text, 'phone_contacts ran')
  assert.deepEqual(n.toolCalls[0], { name: 'phone_contacts', args: { q: 'li' }, session_id: 's1' })
  n.setHub(false)
  await until('onHub: gone', () => hubs.length === 2)
  n.setHub(false) // no change: not called again
  n.setHub(true)
  await until('onHub: back', () => hubs.length === 3)
  assert.deepEqual(hubs, [true, false, true])
})

test('a refusal with data reaches the node with its data (delivered: none)', async (t) => {
  const n = await startFakeNode(t)
  const rt = createRuntime(minimal({ send: async () => { throw new RpcError('handed over', 'handover', { delivered: 'none' }) } }), { runtime: 'toy', socketPath: n.path, retryMs: 20 })
  t.after(() => rt.stop())
  rt.start()
  await n.ready
  await assert.rejects(n.peer.call('session.send', { session_id: 's1', text: 'x' }), (err) => err.code === 'handover' && err.data?.delivered === 'none')
})
