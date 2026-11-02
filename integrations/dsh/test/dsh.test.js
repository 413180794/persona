import { test } from 'node:test'
import assert from 'node:assert/strict'
import { startFakeNode, until } from '../../runtime-sdk/testing/fake-node.js'
import { createDshRuntime } from '../lib/dsh.js'

/** A Cordis-like event bus: emit for emit events, waterfall for the answerer chains (registration order, prepend first). */
function bus() {
  const listeners = new Map()
  return {
    on(name, fn, opts = {}) {
      const list = listeners.get(name) ?? []
      if (opts.prepend) list.unshift(fn)
      else list.push(fn)
      listeners.set(name, list)
      return () => listeners.set(name, (listeners.get(name) ?? []).filter((f) => f !== fn))
    },
    emit(name, ...args) {
      for (const fn of listeners.get(name) ?? []) fn(...args)
    },
    waterfall(name, payload, last) {
      const list = listeners.get(name) ?? []
      const run = (i) => (i < list.length ? Promise.resolve(list[i](payload, () => run(i + 1))) : Promise.resolve(last()))
      return run(0)
    },
  }
}

/** A sessionController over in-memory logs. */
function controller() {
  const sessions = new Map()
  const c = {
    sessions,
    prompts: [],
    cancels: [],
    renames: [],
    add(id, summary, events = []) {
      sessions.set(id, { summary: { sessionId: id, updatedAt: Date.now(), running: false, blank: false, agentAvailable: true, ...summary }, events })
    },
    async list() {
      return { items: [...sessions.values()].map((s) => s.summary) }
    },
    async projections({ sessionId }) {
      const s = sessions.get(sessionId)
      return s ? { asOfSeq: s.events.at(-1)?.seq ?? -1, values: {} } : null
    },
    async page({ address, throughSeq, beforeSeq, maxMessages }) {
      const s = sessions.get(address.sessionId)
      const inRange = s.events.filter((e) => e.seq <= throughSeq && (beforeSeq === undefined || e.seq < beforeSeq))
      const page = inRange.slice(-maxMessages)
      return { records: page.map((event) => ({ type: 'event', event })), hasMore: page.length < inRange.length }
    },
    async prompt(req) {
      c.prompts.push(req)
      return { accepted: true }
    },
    cancel(req) {
      c.cancels.push(req)
      return { accepted: true }
    },
    async create({ cwd }) {
      c.add('session-new', { cwd, blank: true })
      return { sessionId: 'session-new' }
    },
    async rename(req) {
      c.renames.push(req)
      return { title: req.title }
    },
    async attachment() {
      return { attachment: { mediaType: 'image/png' }, data: 'iVBORw0KGgo=' }
    },
  }
  return c
}

const text = (seq, role, body) => role === 'user'
  ? { type: 'user/message', seq, time: 0, surfaceOp: 'append', data: { role: 'user', id: `u${seq}`, content: [{ type: 'text', text: body }], source: { kind: 'user' } } }
  : { type: 'assistant/message', seq, time: 0, surfaceOp: 'append', data: { message: { role: 'assistant', content: [{ type: 'text', text: body }] } } }

async function setup(t, opts = {}) {
  const n = await startFakeNode(t, { tools: opts.tools })
  const ctl = controller()
  const ev = bus()
  const answered = []
  const rt = createDshRuntime({
    controller: ctl,
    on: ev.on,
    userQuestions: { answer: (agent, callId, answer) => { answered.push({ callId, answer }); return true } },
    hasLiveClient: () => opts.liveClient ?? true,
    workspace: opts.workspace,
    registerTool: opts.registerTool,
  }, { socketPath: n.path, pushDelayMs: 5, retryMs: 20, interactions: opts.interactions, maxSessions: opts.maxSessions,
    runtime: opts.runtime, name: opts.name, launcher: opts.launcher })
  t.after(() => rt.stop())
  return { n, ctl, ev, rt, answered }
}

test('lists DSH sessions to the node after hello', async (t) => {
  const { n, ctl, rt } = await setup(t)
  ctl.add('session-a', { cwd: '/w/app', running: true, projections: { kind: 'cached', asOfSeq: 41, values: { title: 'Fix the build' } } })
  ctl.add('session-blank', { cwd: '/w/app', blank: true })
  ctl.add('session-child', { cwd: '/w/app', origin: 'subagent' })
  ctl.add('session-legacy-child', { cwd: '/w/app', parentSessionId: 'session-a' }) // older DSH: a parent, no origin
  ctl.add('session-old', { cwd: '/w/app', updatedAt: Date.now() - 30 * 86_400_000 })
  rt.start()
  await n.ready
  assert.equal(n.hello.runtime, 'dsh')
  assert.equal(n.hello.name, 'DSH')
  assert.equal(n.hello.launcher, 'dsh')
  assert.equal(n.hello.caps.close, false)
  assert.equal(n.hello.caps.send, true)
  await until('runtime.sessions', () => n.last('runtime.sessions'))
  const list = n.last('runtime.sessions').sessions
  assert.deepEqual(list.map((s) => s.session_id), ['session-a'])
  assert.equal(list[0].status, 'working')
  assert.equal(list[0].title, 'Fix the build')
  assert.equal(list[0].space_name, 'app')
  assert.equal(list[0].rev, 42)
})

test('a session the phone sends to stays listed past the count and age limits', async (t) => {
  const { n, ctl, rt } = await setup(t, { maxSessions: 2 })
  ctl.add('session-old', { cwd: '/w/app', updatedAt: Date.now() - 30 * 86_400_000 }, [text(0, 'user', 'long ago')])
  for (const id of ['session-x', 'session-y', 'session-z']) ctl.add(id, { cwd: '/w/app' })
  rt.start()
  await n.ready
  await until('listed', () => n.last('runtime.sessions'))
  assert.equal(n.last('runtime.sessions').sessions.length, 2)
  assert.ok(!n.last('runtime.sessions').sessions.some((s) => s.session_id === 'session-old'))
  await n.peer.call('session.send', { session_id: 'session-old', text: 'still there?' })
  await until('kept in the list', () => n.last('runtime.sessions')?.sessions.some((s) => s.session_id === 'session-old'))
  assert.equal(n.last('runtime.sessions').sessions.length, 2, 'the kept one counts toward the limit')
})

test('pages history back from the end, by turns, before a seq, after a seq and over a range', async (t) => {
  const { n, ctl, rt } = await setup(t)
  ctl.add('session-p', {}, [text(0, 'user', 'one'), text(1, 'assistant', 'a1'), text(2, 'user', 'two'), text(3, 'assistant', 'a2')])
  rt.start()
  await n.ready
  const latest = await n.peer.call('session.items', { session_id: 'session-p', turns: 2 })
  assert.deepEqual(latest.items.map((i) => i.text), ['one', 'a1', 'two', 'a2'])
  assert.equal(latest.history, 'dsh:session-p')
  assert.equal(latest.to_seq, 4)
  const before = await n.peer.call('session.items', { session_id: 'session-p', before_seq: 3, limit: 1 })
  assert.deepEqual(before.items.map((i) => i.text), ['a1'])
  assert.equal(before.has_more, true)
  const after = await n.peer.call('session.items', { session_id: 'session-p', after_seq: 2 })
  assert.deepEqual(after.items.map((i) => i.text), ['two', 'a2'])
  assert.equal(after.from_seq, 2)
  assert.equal(after.to_seq, 4)
  const caughtUp = await n.peer.call('session.items', { session_id: 'session-p', after_seq: 4 })
  assert.deepEqual(caughtUp.items, [])
  const range = await n.peer.call('session.items', { session_id: 'session-p', from_seq: 2, through_seq: 3 })
  assert.deepEqual(range.items.map((i) => i.text), ['a1', 'two'])
  await assert.rejects(n.peer.call('session.items', { session_id: 'nope' }), /没有会话/)
})

test('sends with the client message id, queued when idle and steered while working', async (t) => {
  const { n, ctl, ev, rt } = await setup(t)
  ctl.add('session-s', {}, [text(0, 'user', 'hi')])
  rt.start()
  await n.ready
  await n.peer.call('session.send', { session_id: 'session-s', text: 'go on', client_msg_id: 'm-1', images: [{ mime: 'image/png', data: 'AAAA' }] })
  assert.equal(ctl.prompts[0].requestId, 'm-1')
  assert.equal(ctl.prompts[0].mode, 'queue')
  assert.deepEqual(ctl.prompts[0].content, [{ type: 'text', text: 'go on' }, { type: 'image', mediaType: 'image/png', data: 'AAAA', name: 'phone' }])
  ev.emit('session/event', { id: 'session-s' }, { type: 'turn/start', seq: 1, data: { turn: 1 } })
  await n.peer.call('session.send', { session_id: 'session-s', text: 'also this' })
  assert.equal(ctl.prompts[1].mode, 'steer')
  await n.peer.call('session.interrupt', { session_id: 'session-s' })
  assert.deepEqual(ctl.cancels, [{ sessionId: 'session-s' }])
  await assert.rejects(n.peer.call('session.send', { session_id: 'session-s', text: '  ' }), /空/)
})

test('starts a session from the phone and lists it before its first prompt', async (t) => {
  const { n, ctl, rt } = await setup(t)
  rt.start()
  await n.ready
  const res = await n.peer.call('session.start', { cwd: '/w/new', prompt: 'hello' })
  assert.equal(res.session.session_id, 'session-new')
  assert.equal(ctl.prompts[0].sessionId, 'session-new')
  await until('listed', () => n.last('runtime.sessions')?.sessions.some((s) => s.session_id === 'session-new'))
})

test('a watched session gets its new events as continuous items and the live text as deltas', async (t) => {
  const { n, ctl, ev, rt } = await setup(t)
  ctl.add('session-w', {}, [text(0, 'user', 'one'), text(1, 'assistant', 'a1')])
  rt.start()
  await n.ready
  await n.peer.call('session.watch', { session_id: 'session-w', since_seq: 1 })
  await until('catch-up', () => n.all('session.items').length === 1)
  const catchUp = n.all('session.items')[0]
  assert.deepEqual(catchUp.items.map((i) => i.text), ['a1'])
  assert.equal(catchUp.from_seq, 1)
  assert.equal(catchUp.to_seq, 2)

  const next = [text(2, 'user', 'two'), { type: 'step/start', seq: 3, data: {} }, text(4, 'assistant', 'a2')]
  for (const e of next) {
    ctl.sessions.get('session-w').events.push(e)
    ev.emit('session/event', { id: 'session-w' }, e)
  }
  await until('live batch', () => n.all('session.items').length === 2)
  const live = n.all('session.items')[1]
  assert.deepEqual(live.items.map((i) => i.text), ['two', 'a2'])
  assert.equal(live.from_seq, 2)
  assert.equal(live.to_seq, 5)

  const agent = { session: { id: 'session-w' } }
  ev.emit('agent/assistant-stream', { agent, frame: { type: 'start' } })
  ev.emit('agent/assistant-stream', { agent, frame: { type: 'chunk', chunk: { type: 'text-delta', text: 'Hel' } } })
  ev.emit('agent/assistant-stream', { agent, frame: { type: 'chunk', chunk: { type: 'text-delta', text: 'lo' } } })
  await until('delta', () => n.last('session.delta')?.text === 'Hello')
  ev.emit('agent/assistant-stream', { agent, frame: { type: 'end' } })
  await until('end of the delta', () => n.last('session.delta')?.text === '')

  // A missed event: the batch says where it starts, so the phone fills the gap.
  ev.emit('session/event', { id: 'session-w' }, text(9, 'assistant', 'later'))
  await until('gap batch', () => n.all('session.items').length === 3)
  assert.equal(n.all('session.items')[2].from_seq, 9)
})

test('approvals of a session the phone drives go to the phone first; a dismissed card goes to the browser', async (t) => {
  const { n, ctl, ev, rt } = await setup(t)
  ctl.add('session-x', {}, [text(0, 'user', 'deploy')])
  rt.start()
  await n.ready
  await until('listed', () => n.last('runtime.sessions'))
  const agent = { session: { id: 'session-x' } }
  ev.emit('session/event', { id: 'session-x' }, { type: 'assistant/message', seq: 1, data: { message: { content: [
    { type: 'tool-call', id: 'call_1', name: 'bash', arguments: '{"command":"rm -rf build"}' }] } } })

  // Not driven from the phone and a browser is open: the browser decides, nothing reaches the phone.
  let browser = 0
  const fromBrowser = () => { browser++; return 'rejected' }
  assert.equal(await ev.waterfall('approval/request', { agent, toolName: 'bash', callId: 'call_1' }, fromBrowser), 'rejected')
  assert.equal(n.asked.length, 0)

  await n.peer.call('session.send', { session_id: 'session-x', text: 'yes, deploy' })
  n.resolve = () => ({ allow: true })
  assert.equal(await ev.waterfall('approval/request', { agent, toolName: 'bash', callId: 'call_1' }, fromBrowser), 'allowed-once')
  assert.equal(n.asked.length, 1)
  assert.equal(n.asked[0].kind, 'permission')
  assert.equal(n.asked[0].tool_name, 'Bash')
  assert.deepEqual(n.asked[0].tool_input, { command: 'rm -rf build' })
  assert.equal(n.asked[0].summary, 'rm -rf build')
  assert.equal(browser, 1)

  n.resolve = () => ({ dismiss: true })
  assert.equal(await ev.waterfall('approval/request', { agent, toolName: 'bash', callId: 'call_2' }, fromBrowser), 'rejected')
  assert.equal(browser, 2)

  // DSH's reason goes beside the command; a refusal with words rejects, then steers the words into the turn.
  n.resolve = () => ({ allow: false, message: 'not on Friday' })
  const before = ctl.prompts.length
  assert.equal(await ev.waterfall('approval/request', { agent, toolName: 'bash', callId: 'call_1', reason: 'deletes files' }, fromBrowser), 'rejected')
  const card = n.asked.at(-1)
  assert.equal(card.reason, 'deletes files')
  assert.equal(card.summary, 'rm -rf build')
  await until('the reason steered in', () => ctl.prompts.length === before + 1)
  const steer = ctl.prompts.at(-1)
  assert.equal(steer.mode, 'steer')
  assert.equal(steer.sessionId, 'session-x')
  assert.match(steer.content[0].text, /拒绝了 Bash）not on Friday$/)
  assert.equal(steer.requestId, `${card.id}-reason`)
})

test('with no browser open the phone gets approvals; the browser policy never asks it', async (t) => {
  const { n, ctl, ev, rt } = await setup(t, { liveClient: false })
  ctl.add('session-y', {})
  rt.start()
  await n.ready
  await until('listed', () => n.last('runtime.sessions'))
  n.resolve = () => ({ allow: false })
  assert.equal(await ev.waterfall('approval/request', { agent: { session: { id: 'session-y' } }, toolName: 'write' }, () => 'allowed-once'), 'rejected')
  assert.equal(n.asked.length, 1)
})

test('plan reviews and questions go to the phone; a timed question answered late goes in through userQuestions.answer', async (t) => {
  const { n, ctl, ev, rt, answered } = await setup(t, { interactions: 'phone' })
  ctl.add('session-q', {})
  rt.start()
  await n.ready
  await until('listed', () => n.last('runtime.sessions'))
  const agent = { session: { id: 'session-q' } }
  const plan = { id: 'review', question: 'Approve?', detail: '# Plan\n1. do it', options: [{ label: 'Approve' }, { label: 'Keep planning' }], intent: { kind: 'plan-review', approve: 'Approve' } }
  n.resolve = () => ({ approve: true })
  const res = await ev.waterfall('user-questions/request', { agent, questions: [plan] }, () => { throw new Error('no answerer') })
  assert.deepEqual(res, { answers: [{ id: 'review', selected: ['Approve'] }] })
  assert.equal(n.asked[0].kind, 'plan')
  assert.equal(n.asked[0].plan, '# Plan\n1. do it')

  // The timed wait ends before the phone answers: DSH gets the timeout, the phone's answer arrives later.
  let release
  n.resolve = () => new Promise((r) => { release = r })
  const wait = new AbortController()
  const q = { id: 'q1', question: 'Which db?', options: [{ label: 'pg' }, { label: 'sqlite' }] }
  const pending = ev.waterfall('user-questions/request', { agent, questions: [q], signal: wait.signal, wait: { callId: 'call_q', timed: true } }, () => { throw new Error('no answerer') })
  await until('asked', () => n.asked.length === 2)
  assert.equal(n.asked[1].kind, 'question')
  assert.equal(n.asked[1].questions[0].question, 'Which db?')
  wait.abort(new Error('ASK_TIMED_OUT'))
  await assert.rejects(pending, /ASK_TIMED_OUT/)
  release({ answers: { 'Which db?': 'pg' } })
  await until('late answer', () => answered.length === 1)
  assert.deepEqual(answered[0], { callId: 'call_q', answer: { answers: [{ id: 'q1', selected: ['pg'] }] } })
})

test('reconnects when the node goes away and comes back', async (t) => {
  const { n, ctl, rt } = await setup(t)
  ctl.add('session-r', {})
  rt.start()
  await n.ready
  await until('connected', () => rt.connected)
  await until('first list', () => n.all('runtime.sessions').length === 1)
  n.peer.close(new Error('node restarted'))
  await until('reconnected', () => rt.connected && n.all('runtime.sessions').length >= 2, 5000)
})

/** A workspaceRegistry: its archive set and the archive calls. */
function workspace(archived = []) {
  const w = {
    archivedSessionIds: [...archived],
    calls: [],
    async archiveSession(id, o) {
      w.calls.push({ id, ...o })
      w.archivedSessionIds = [...w.archivedSessionIds, id]
    },
  }
  return w
}

test('archived sessions are not listed, and closing one from the phone archives it and stops its work', async (t) => {
  const ws = workspace(['session-gone'])
  const { n, ctl, rt } = await setup(t, { workspace: ws })
  ctl.add('session-gone', { cwd: '/w/app' })
  ctl.add('session-live', { cwd: '/w/app', running: true })
  rt.start()
  await n.ready
  assert.equal(n.hello.caps.close, true)
  await until('listed', () => n.last('runtime.sessions'))
  assert.deepEqual(n.last('runtime.sessions').sessions.map((s) => s.session_id), ['session-live'])
  await n.peer.call('session.close', { session_id: 'session-live' })
  assert.deepEqual(ws.calls, [{ id: 'session-live', stopActivity: true }])
  await until('gone from the list', () => n.last('runtime.sessions')?.sessions.length === 0)
})

test('another runtime id, name and launcher: in hello, the cards\' ids and the rows\' label', async (t) => {
  const { n, ctl, ev, rt } = await setup(t, { runtime: 'dsh-work', name: 'DSH (work)', launcher: 'dsh-work', interactions: 'phone' })
  ctl.add('session-w', { cwd: '/w/app' })
  rt.start()
  await n.ready
  assert.equal(n.hello.runtime, 'dsh-work')
  assert.equal(n.hello.name, 'DSH (work)')
  assert.equal(n.hello.launcher, 'dsh-work')
  assert.equal(n.hello.agent, 'dsh', 'it still runs DSH\'s agent')
  await until('listed', () => n.last('runtime.sessions'))
  assert.equal(n.last('runtime.sessions').sessions[0].agent_label, 'DSH (work)')
  n.resolve = () => ({ allow: true })
  const outcome = await ev.waterfall('approval/request', { agent: { session: { id: 'session-w' } }, toolName: 'bash', callId: 'c1' }, () => 'unavailable')
  assert.equal(outcome, 'allowed-once')
  assert.ok(n.asked[0].id.includes('dsh-work'), n.asked[0].id)
})

test('the hub\'s tools become DSH tools; a call goes through the node with the calling session', async (t) => {
  const registered = new Map()
  const registerTool = (def) => {
    registered.set(def.name, def)
    return () => registered.delete(def.name)
  }
  const { n, rt } = await setup(t, { registerTool, tools: [
    { name: 'phone_calendar_events', description: 'Calendar', input_schema: { type: 'object', properties: { day: { type: 'string' } } } },
    { name: 'mail_search', description: 'Mail', input_schema: { type: 'object' } },
  ] })
  rt.start()
  await n.ready
  await until('registered', () => registered.size === 2)
  const def = registered.get('mcp__jarvis__phone_calendar_events')
  assert.equal(def.description, 'Calendar')
  assert.deepEqual(def.parameters.properties.day, { type: 'string' })
  const value = await def.execute({ day: 'today' }, { agent: { session: { id: 'session-c' } }, signal: AbortSignal.timeout(5000) })
  assert.deepEqual(n.toolCalls.at(-1), { name: 'phone_calendar_events', args: { day: 'today' }, session_id: 'session-c' })
  assert.deepEqual(def.output.render({}, value), [{ type: 'text', text: 'phone_calendar_events ran' }])
  n.callTool = () => ({ content: [{ type: 'text', text: 'iPhone 不在线，请打开 Jarvis' }], is_error: true })
  await assert.rejects(def.execute({}, { agent: { session: { id: 'session-c' } } }), /iPhone 不在线/)

  // The same tools: nothing re-registered; a changed list replaces them; the hub away keeps them.
  await rt.refreshTools()
  assert.equal(registered.size, 2)
  n.tools = [{ name: 'phone_reminders', description: 'Reminders', input_schema: { type: 'object' } }]
  await rt.refreshTools()
  assert.deepEqual([...registered.keys()], ['mcp__jarvis__phone_reminders'])
  n.setHub(false)
  await new Promise((r) => setTimeout(r, 50))
  assert.equal(registered.size, 1)
  rt.stop()
  assert.equal(registered.size, 0, 'stopping takes them off')
})
