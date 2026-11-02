import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createRuntime, interactionId, light, PAGE_TEXT } from '../index.js'
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
