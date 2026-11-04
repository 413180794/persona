import { test } from 'node:test'
import assert from 'node:assert/strict'
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { startFakeNode, until } from '../../runtime-sdk/testing/fake-node.js'
import { createPaseoBridge, uuidOf } from '../lib/bridge.js'
import { PaseoClient } from '../lib/paseo.js'
import { agentSnapshot, entry, startFakePaseo } from './fake-paseo.js'

const SID = 'aaaaaaaa-0000-4000-8000-000000000001'
const SID2 = 'aaaaaaaa-0000-4000-8000-000000000002'
const FEATURES = ['prefer_running', 'resume_argv', 'handover', 'transcript', 'launchers', 'start_params', 'modes', 'resume']

const PROVIDERS = { claude: { enabled: false }, 'my-claude': { extends: 'claude', label: 'My Claude' }, 'my-codex': { extends: 'codex' }, grok: { extends: 'acp', label: 'Grok' } }
const ENTRIES = [
  { provider: 'claude', enabled: false, status: 'unavailable', models: [] },
  { provider: 'my-claude', enabled: true, status: 'ready', label: 'My Claude', models: [
    { id: 'fable[1m]', label: 'Fable', isDefault: true, thinkingOptions: [{ id: 'low' }, { id: 'high' }, { id: 'max' }], defaultThinkingOptionId: 'max' },
  ] },
  { provider: 'my-codex', enabled: true, status: 'ready', label: 'My Codex', models: [{ id: 'gpt', label: 'GPT', isDefault: true }] },
  { provider: 'grok', enabled: true, status: 'ready', label: 'Grok', models: [] },
]
const PROJECT = { projectKey: 'prj_1', projectName: 'proj', workspaceName: '主线', checkout: { cwd: '/tmp', isGit: false } }

function agents() {
  const claude = agentSnapshot({ provider: 'my-claude', cwd: '/tmp', title: 'A', status: 'idle', lastUserMessageAt: '2026-11-04T00:00:00Z',
    persistence: { provider: 'my-claude', sessionId: SID, metadata: { provider: 'claude' } }, runtimeInfo: { provider: 'claude', sessionId: SID },
    availableModes: [{ id: 'default' }, { id: 'acceptEdits' }, { id: 'plan' }, { id: 'bypassPermissions' }], model: 'fable[1m]' })
  const codex = agentSnapshot({ provider: 'my-codex', cwd: '/tmp', title: 'B', status: 'running', persistence: { provider: 'my-codex', sessionId: 'th-1', metadata: { provider: 'codex' } } })
  const grok = agentSnapshot({ provider: 'grok', cwd: '/tmp', title: 'C', status: 'closed' })
  const fresh = agentSnapshot({ provider: 'my-claude', cwd: '/tmp', title: null })
  const labeled = agentSnapshot({ provider: 'my-claude', cwd: '/tmp', title: 'E', labels: { 'persona.session': 'paseo-e1' },
    persistence: { provider: 'my-claude', sessionId: SID2, metadata: { provider: 'claude' } } })
  return { claude, codex, grok, fresh, labeled }
}

async function setup(t, o = {}) {
  const a = agents()
  const fake = await startFakePaseo(t, { providers: PROVIDERS, entries: ENTRIES, agents: Object.values(a).map((agent) => ({ agent, project: PROJECT })), ...o })
  const node = await startFakeNode(t, { features: FEATURES })
  const home = mkdtempSync('/tmp/ppaseo-')
  t.after(() => rmSync(home, { recursive: true, force: true }))
  // Where Paseo's own CLI finds its daemon: the address in the home's paseo.pid.
  writeFileSync(join(home, 'paseo.pid'), JSON.stringify({ pid: process.pid, listen: `127.0.0.1:${fake.port}` }))
  const logs = []
  const paseo = new PaseoClient({ home, retryMin: 30, retryMax: 100, log: (m) => logs.push(m) })
  const bridge = createPaseoBridge({ paseo, log: (m) => logs.push(m) },
    { socketPath: node.path, retryMs: 30, pushDelayMs: 5, refreshMs: 0, awayGrace: o.awayGrace ?? 400, callWait: 1500 })
  bridge.start()
  t.after(() => bridge.stop())
  await node.ready
  await until('the first list', () => node.last('runtime.sessions')?.sessions?.length >= 4)
  return { fake, node, bridge, paseo, logs, a, rows: () => node.last('runtime.sessions').sessions }
}

const call = (node, method, params) => node.peer.call(method, params)

test("Paseo's agents are the engine's sessions: Claude Code's by its session and transcript, the others by agent", async (t) => {
  const { node, a, rows } = await setup(t)
  assert.equal(node.hello.runtime, 'paseo')
  assert.equal(node.hello.name, 'Paseo')
  assert.equal(node.hello.version, '0.14.0', "Paseo's version")
  assert.deepEqual(node.hello.caps, { start: true, send: true, interrupt: true, rename: true, close: true, assistant: false, modes: true, resume: true })
  assert.deepEqual(node.hello.launchers.map((l) => [l.name, l.agent]), [['my-claude', 'claude'], ['my-codex', 'codex'], ['grok', 'paseo']])
  assert.deepEqual(node.hello.launchers[0].models, [{ id: 'fable[1m]', name: 'Fable', default: true, efforts: ['low', 'high', 'max'], default_effort: 'max' }])
  const byId = Object.fromEntries(rows().map((r) => [r.session_id, r]))
  assert.deepEqual(Object.keys(byId).sort(), [SID, 'paseo-e1', `paseo-${a.codex.id}`, `paseo-${a.grok.id}`].sort(), 'a Claude agent without a session yet is not listed')
  assert.deepEqual([byId[SID].agent, byId[SID].transcript, byId[SID].status, byId[SID].space_name, byId[SID].launcher], ['claude', SID, 'done', '主线', 'my-claude'])
  assert.deepEqual([byId['paseo-e1'].transcript, byId['paseo-e1'].title], [SID2, 'E'])
  assert.deepEqual([byId[`paseo-${a.codex.id}`].agent, byId[`paseo-${a.codex.id}`].status, byId[`paseo-${a.codex.id}`].transcript], ['codex', 'working', undefined])
  assert.deepEqual([byId[`paseo-${a.grok.id}`].agent_label, byId[`paseo-${a.grok.id}`].status], ['Grok', 'offline'])
})

test('changes in Paseo move the list: a new agent, a status, one archived', async (t) => {
  const { fake, node, a, rows } = await setup(t)
  const n = agentSnapshot({ provider: 'grok', cwd: '/tmp', title: 'new', status: 'running' })
  fake.upsert(n, PROJECT)
  await until('the new agent listed', () => rows().some((r) => r.session_id === `paseo-${n.id}` && r.status === 'working'))
  fake.update(a.codex.id, { status: 'idle', lastUserMessageAt: '2026-11-04T01:00:00Z' })
  await until('the codex agent done', () => rows().find((r) => r.session_id === `paseo-${a.codex.id}`)?.status === 'done')
  fake.update(a.fresh.id, { runtimeInfo: { provider: 'claude', sessionId: 'bbbbbbbb-0000-4000-8000-000000000003' } })
  await until('the Claude agent listed once it has a session', () => rows().some((r) => r.session_id === 'bbbbbbbb-0000-4000-8000-000000000003' && !r.transcript))
  await call(node, 'session.close', { session_id: `paseo-${n.id}` })
  assert.equal(fake.requestsOf('archive_agent_request').at(-1).agentId, n.id)
  await until('the archived agent gone', () => !rows().some((r) => r.session_id === `paseo-${n.id}`))
})

test("an agent's timeline as items, its live text, and what it writes next while watched", async (t) => {
  const { fake, node, a } = await setup(t)
  const id = `paseo-${a.codex.id}`
  fake.setTimeline(a.codex.id, [
    entry(1, { type: 'user_message', text: '跑测试', messageId: 'u1' }),
    entry(2, { type: 'tool_call', callId: 'c1', name: 'shell', status: 'running', error: null, detail: { type: 'shell', command: 'go test ./...' } }),
  ])
  const p = await call(node, 'session.items', { session_id: id, turns: 3 })
  assert.deepEqual(p.items.map((i) => [i.id, i.kind]), [['1#0', 'text'], ['2#0', 'tool_use']])
  assert.equal(p.items[1].tool_name, 'Bash')
  assert.equal(p.history, `paseo:${a.codex.id}:ep1`)

  await call(node, 'session.watch', { session_id: id, since_seq: 0 })
  await until('the timeline followed', () => fake.requestsOf('agent.timeline.set_subscription.request').some((r) => r.agentIds[0] === a.codex.id))
  await until('the subscription made', () => [...fake.subs.values()].some((s) => s.kind === 'timeline'))
  fake.stream(a.codex.id, { type: 'timeline', provider: 'codex', turnId: 't1', item: { type: 'assistant_message', text: '测试', messageId: 'm1' } }, 5)
  fake.stream(a.codex.id, { type: 'timeline', provider: 'codex', turnId: 't1', item: { type: 'assistant_message', text: '都过了', messageId: 'm1' } }, 6)
  await until('the live text', () => node.all('session.delta').some((d) => d.session_id === id && d.text === '测试都过了'))

  // The tool finished and the text was written: the next read pushes them, from where the watch began.
  fake.setTimeline(a.codex.id, [
    entry(1, { type: 'user_message', text: '跑测试', messageId: 'u1' }),
    entry(2, { type: 'tool_call', callId: 'c1', name: 'shell', status: 'completed', error: null, detail: { type: 'shell', command: 'go test ./...', output: 'ok', exitCode: 0 } }, 4),
    entry(5, { type: 'assistant_message', text: '测试都过了' }, 6),
  ])
  fake.update(a.codex.id, { status: 'idle', lastUserMessageAt: '2026-11-04T01:00:00Z' })
  fake.stream(a.codex.id, { type: 'turn_completed', provider: 'codex', turnId: 't1' })
  const pushed = await until('the new items', () => node.all('session.items').filter((x) => x.session_id === id).flatMap((x) => x.items).length >= 2 &&
    node.all('session.items').filter((x) => x.session_id === id))
  const all = pushed.flatMap((x) => x.items)
  assert.deepEqual(all.map((i) => [i.id, i.kind]), [['4#0', 'tool_result'], ['5#0', 'text']])
  assert.equal(pushed[0].from_seq, 2, 'continuous with where the watch began')
  assert.equal(pushed.at(-1).to_seq, 6)
  assert.ok(node.all('session.delta').some((d) => d.session_id === id && d.text === ''), 'the live text ends')
  await call(node, 'session.unwatch', { session_id: id })
  await until('the subscription released', () => fake.requestsOf('subscription.release.request').length > 0)
})

test("a Claude Code session's watch is its live text only: the node reads the transcript", async (t) => {
  const { fake, node, a } = await setup(t)
  await call(node, 'session.watch', { session_id: SID, since_seq: 0, transcript: true })
  await until('the subscription made', () => [...fake.subs.values()].some((s) => s.kind === 'timeline' && s.agentIds[0] === a.claude.id))
  fake.stream(a.claude.id, { type: 'timeline', provider: 'claude', turnId: 't', item: { type: 'assistant_message', text: '好的', messageId: 'msg_1' } }, 10)
  await until('the live text', () => node.all('session.delta').some((d) => d.session_id === SID && d.text === '好的'))
  fake.stream(a.claude.id, { type: 'timeline', provider: 'claude', turnId: 't', item: { type: 'tool_call', callId: 'toolu_1', name: 'Bash', status: 'running', error: null, detail: { type: 'shell', command: 'ls' } } }, 11)
  await until('it ends at the tool call', () => node.all('session.delta').some((d) => d.session_id === SID && d.text === ''))
  await new Promise((r) => setTimeout(r, 300))
  assert.equal(node.all('session.items').filter((x) => x.session_id === SID).length, 0, 'no items: the transcript has them')
  assert.equal(fake.requestsOf('fetch_agent_timeline_request').filter((r) => r.agentId === a.claude.id).length, 0, 'its timeline is not read')
})

test('sending: into the running turn, once per message, images as Paseo takes them', async (t) => {
  const { fake, node, a } = await setup(t)
  await call(node, 'session.send', { session_id: SID, text: '继续', client_msg_id: 'cm-1', images: [{ mime: 'image/png', data: 'iVBO' }] })
  await call(node, 'session.send', { session_id: SID, text: '继续', client_msg_id: 'cm-1' })
  const sent = fake.requestsOf('send_agent_message_request')
  assert.equal(sent.length, 1, 'a retry of the same message is not sent again')
  assert.deepEqual({ ...sent[0], requestId: undefined }, { type: 'send_agent_message_request', requestId: undefined, agentId: a.claude.id, text: '继续',
    messageId: uuidOf(`persona:${a.claude.id}:cm-1`), activeTurnBehavior: 'steer', images: [{ data: 'iVBO', mimeType: 'image/png' }] })
  fake.handlers.send_agent_message_request = () => ({ payload: { agentId: a.claude.id, accepted: false, error: 'Agent is archived' } })
  await assert.rejects(call(node, 'session.send', { session_id: SID, text: 'x', client_msg_id: 'cm-2' }), /Paseo 没有收下这条消息：Agent is archived/)
  await assert.rejects(call(node, 'session.send', { session_id: 'paseo-nope', text: 'x' }), /没有这个会话/)
  // Paseo slow to start the run (a closed agent loading): the send is left with it, not failed.
  fake.handlers.send_agent_message_request = () => null
  const t0 = Date.now()
  await call(node, 'session.send', { session_id: SID, text: '慢', client_msg_id: 'cm-3' })
  assert.ok(Date.now() - t0 >= 1400, 'waited for Paseo first')
})

test('interrupt, rename, permission mode and effort go to the agent', async (t) => {
  const { fake, node, a } = await setup(t)
  await call(node, 'session.interrupt', { session_id: SID })
  assert.equal(fake.requestsOf('cancel_agent_request')[0].agentId, a.claude.id)
  await call(node, 'session.rename', { session_id: SID, title: '新名字' })
  assert.equal(fake.requestsOf('update_agent_request')[0].name, '新名字')
  await call(node, 'session.modes', { session_id: SID, permission_mode: 'plan', effort: 'high' })
  assert.deepEqual([fake.requestsOf('set_agent_mode_request')[0].modeId, fake.requestsOf('set_agent_thinking_request')[0].thinkingOptionId], ['plan', 'high'])
  await assert.rejects(call(node, 'session.modes', { session_id: SID, effort: 'xhigh' }), /没有「xhigh」/)
  await assert.rejects(call(node, 'session.modes', { session_id: SID, permission_mode: 'auto' }), /没有「auto」模式/)
  await assert.rejects(call(node, 'session.modes', { session_id: `paseo-${a.codex.id}`, permission_mode: 'plan' }), /没有 Claude Code 的权限模式/)
})

test('a new session: an agent of the launcher\'s provider, with the form\'s choices, under an id kept on it', async (t) => {
  const { fake, node, rows } = await setup(t)
  const res = await call(node, 'session.start', { cwd: '/tmp', prompt: '修一下登录', launcher: 'my-claude', model: 'fable[1m]', permission_mode: 'acceptEdits', effort: 'high', workspace_id: 'wks_1' })
  const req = fake.requestsOf('agent.create.request')[0]
  assert.deepEqual(req.config, { provider: 'my-claude', cwd: '/tmp', model: 'fable[1m]', modeId: 'acceptEdits', thinkingOptionId: 'high' })
  assert.equal(req.initialPrompt, '修一下登录')
  assert.equal(req.workspaceId, 'wks_1')
  assert.equal(res.session.session_id, `paseo-${req.agentId}`)
  assert.equal(req.labels['persona.session'], res.session.session_id)
  assert.equal(res.session.agent, 'claude')
  await until('listed', () => rows().some((r) => r.session_id === res.session.session_id))
  // A launcher of the login shell's that Paseo does not have: Paseo's default provider.
  await call(node, 'session.start', { cwd: '/tmp', prompt: 'x', launcher: 'cc_gateway', shell_launcher: true })
  assert.equal(fake.requestsOf('agent.create.request')[1].config.provider, 'my-claude')
  await assert.rejects(call(node, 'session.start', { cwd: '/nonexistent/dir', prompt: 'x' }), /没有这个目录/)
  fake.handlers['agent.create.request'] = () => ({ payload: { agent: null, error: 'Provider unavailable' } })
  await assert.rejects(call(node, 'session.start', { cwd: '/tmp', prompt: 'x', launcher: 'grok' }), /Paseo 没能新建：Provider unavailable/)
})

test('continuing a Claude session: the agent Paseo has, else Paseo imports it under the same id', async (t) => {
  const { fake, node, a } = await setup(t)
  const mine = await call(node, 'session.start', { cwd: '/tmp', resume: SID, open_elsewhere: true })
  assert.equal(mine.session.session_id, SID, "Paseo's own agent: taken as it is, even with its claude running")
  assert.equal(fake.requestsOf('import_agent_request').length, 0)
  const other = 'cccccccc-0000-4000-8000-000000000009'
  const res = await call(node, 'session.start', { cwd: '/tmp', resume: other })
  const imp = fake.requestsOf('import_agent_request')[0]
  assert.deepEqual([imp.provider, imp.sessionId, imp.cwd, imp.labels['persona.session']], [a.claude.provider, other, '/tmp', other])
  assert.equal(res.session.session_id, other)
  assert.equal(res.session.transcript, other)
  const err = await call(node, 'session.start', { cwd: '/tmp', resume: 'dddddddd-0000-4000-8000-000000000004', open_elsewhere: true }).catch((e) => e)
  assert.equal(err.code, 'open_elsewhere', 'a claude Paseo does not run has it: no second writer')
})

test("cards: Paseo's requests go to the phone and its answers back; one answered in Paseo leaves the phone", async (t) => {
  const { fake, node, a } = await setup(t)
  let answer
  node.resolve = (ix) => answer(ix)
  answer = () => ({ allow: true })
  fake.permission(a.claude.id, { id: 'permission-1', provider: 'claude', name: 'Bash', kind: 'tool', input: { command: 'npm test' }, metadata: { toolUseId: 'toolu_1' } })
  await until('the response', () => fake.requestsOf('agent_permission_response').length === 1)
  assert.deepEqual(node.asked[0].tool_input, { command: 'npm test' })
  assert.deepEqual([node.asked[0].kind, node.asked[0].session_id, node.asked[0].tool_name, node.asked[0].tool_use_id], ['permission', SID, 'Bash', 'toolu_1'])
  const r = fake.requestsOf('agent_permission_response')[0]
  assert.deepEqual([r.agentId, r.requestId, r.response], [a.claude.id, 'permission-1', { behavior: 'allow' }])

  answer = (ix) => ({ answers: { [ix.questions[0].question]: 'B' } })
  fake.permission(a.claude.id, { id: 'permission-2', provider: 'claude', name: 'AskUserQuestion', kind: 'question',
    input: { questions: [{ question: '选哪个？', header: '选择', multiSelect: false, options: [{ label: 'A' }, { label: 'B' }] }] } })
  await until('the answer', () => fake.requestsOf('agent_permission_response').length === 2)
  assert.deepEqual(fake.requestsOf('agent_permission_response')[1].response, { behavior: 'allow', updatedInput: { answers: { '选哪个？': 'B' } } })

  // Nobody answers on the phone; Paseo's own app does: the card is taken off the phone.
  answer = () => new Promise(() => {})
  fake.permission(a.claude.id, { id: 'permission-3', provider: 'claude', name: 'Edit', kind: 'tool', input: { file_path: '/a' } })
  await until('on the phone', () => node.asked.length === 3)
  fake.resolvePermission(a.claude.id, 'permission-3', { behavior: 'allow' })
  await until('withdrawn', () => node.all('interaction.cancel').some((c) => c.id === node.asked[2].id))
  assert.equal(fake.requestsOf('agent_permission_response').length, 2, 'nothing sent for it')

  // Handed back to the computer: left for Paseo's app.
  answer = () => ({ dismiss: true })
  fake.permission(a.claude.id, { id: 'permission-4', provider: 'claude', name: 'ExitPlanMode', kind: 'plan', input: { plan: 'p' }, metadata: { planText: '## 计划' },
    actions: [{ id: 'reject', behavior: 'deny' }, { id: 'implement', behavior: 'allow' }] })
  await until('asked', () => node.asked.length === 4)
  assert.equal(node.asked[3].plan, '## 计划')
  await new Promise((r) => setTimeout(r, 200))
  assert.equal(fake.requestsOf('agent_permission_response').length, 2)
})

test('an approved plan with a mode: implement, then that mode; the plan read from its call', async (t) => {
  const { fake, node, a } = await setup(t)
  node.resolve = () => ({ approve: true, mode: 'bypassPermissions' })
  // Claude Code gives the permission no input for ExitPlanMode: the plan is in the tool call on the timeline.
  fake.setTimeline(a.claude.id, [entry(7, { type: 'tool_call', callId: 'toolu_p', name: 'ExitPlanMode', status: 'running', error: null, detail: { type: 'plan', text: '## 计划\n\n1. 先写测试' } })])
  fake.permission(a.claude.id, { id: 'permission-9', provider: 'claude', name: 'ExitPlanMode', kind: 'plan', input: {}, metadata: { toolUseId: 'toolu_p' },
    actions: [{ id: 'reject', behavior: 'deny' }, { id: 'implement', behavior: 'allow' }] })
  await until('the mode set', () => fake.requestsOf('set_agent_mode_request').length === 1)
  assert.equal(node.asked[0].plan, '## 计划\n\n1. 先写测试')
  assert.deepEqual(fake.requestsOf('agent_permission_response')[0].response, { behavior: 'allow', selectedActionId: 'implement' })
  assert.equal(fake.requestsOf('set_agent_mode_request')[0].modeId, 'bypassPermissions')
})

test('Paseo restarting: the bridge connects again and keeps the sessions; gone for good, it leaves the node', async (t) => {
  const { fake, node, rows, logs } = await setup(t, { awayGrace: 600 })
  const peer = node.peer
  fake.drop()
  await until('connected again', () => fake.hellos.length === 2)
  await until('subscribed again', () => [...fake.subs.values()].some((s) => s.kind === 'agents'))
  assert.equal(node.peer, peer, 'the node connection stayed')
  assert.ok(rows().length >= 4)
  fake.refuse = 4003
  fake.drop()
  await until('left the node', async () => {
    const gone = await Promise.race([peer.done.then(() => true), new Promise((r) => setTimeout(() => r(false), 50))])
    return gone
  }, 5000)
  assert.ok(logs.some((l) => /away/.test(l)))
})
