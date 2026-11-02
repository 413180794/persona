// End-to-end check of the DSH runtime (docs/dsh.md「验证」, the acceptance list of docs/runtimes.md): a real `dsh web`
// (isolated DSH_HOME, the model played by DSH's own llm-mock-server) with this plugin loaded through --patch, a real
// `jarvisd node --stdio` started by the runtime SDK's harness, which plays the hub. It starts a session from the
// "phone", sends to it, and answers an approval and a question.
//
//   DSH_CHECKOUT=<dsh source checkout, built> JARVISD=<jarvisd binary> node integrations/dsh/e2e/run.mjs
//
// Nothing outside fresh temp directories is touched: DSH_HOME, the node's HOME and the workspace all live there.

import { spawn } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { request as httpRequest, createServer } from 'node:http'
import { dirname, join } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { checks, startNode, until } from '../../runtime-sdk/testing/node-harness.mjs'

const here = dirname(fileURLToPath(import.meta.url))
const dshCheckout = process.env.DSH_CHECKOUT
const jarvisd = process.env.JARVISD
if (!dshCheckout || !jarvisd) {
  console.error('set DSH_CHECKOUT (a built dsh source checkout) and JARVISD (a jarvisd binary)')
  process.exit(2)
}
const keep = process.env.KEEP === '1'
const root = mkdtempSync(join('/tmp', 'jdsh-e2e-'))
const dshHome = join(root, 'dsh')
const work = join(root, 'work')
for (const d of [dshHome, work]) mkdirSync(d, { recursive: true })
const { check, summary } = checks()
let dsh
let node

// DSH keeps writing to its home while it shuts down: wait for it to exit before removing the directory.
async function cleanup() {
  if (dsh && dsh.exitCode === null) {
    const exited = new Promise((r) => dsh.once('exit', r))
    dsh.kill('SIGTERM')
    await Promise.race([exited, new Promise((r) => setTimeout(r, 10_000))])
  }
  node?.stop()
  if (!keep) rmSync(root, { recursive: true, force: true })
}
process.on('SIGINT', () => cleanup().finally(() => process.exit(130)))

// ---- the model: DSH's mock server behind a switch, so each phase scripts its own turn ----

const { startMockLlmServer } = await import(pathToFileURL(join(dshCheckout, 'packages/test-support/llm-mock-server/lib/index.js')).href)
let main // the mock for the agent's own requests (they carry tools)
const side = await startMockLlmServer({ sequence: ['success'], repeatLast: true, successText: 'E2E title' }) // titles and other side calls
async function script(opts) {
  await main?.close?.()
  main = await startMockLlmServer({ repeatLast: true, ...opts })
}
const proxy = createServer((req, res) => {
  const chunks = []
  req.on('data', (c) => chunks.push(c))
  req.on('end', () => {
    const body = Buffer.concat(chunks)
    let target = side
    try {
      if (Array.isArray(JSON.parse(body.toString()).tools) && main) target = main
    } catch {}
    const url = new URL(req.url, target.baseURL.endsWith('/') ? target.baseURL : `${target.baseURL}/`)
    const out = httpRequest(url, { method: req.method, headers: { ...req.headers, host: url.host } }, (r) => {
      res.writeHead(r.statusCode, r.headers)
      r.pipe(res)
    })
    out.on('error', (e) => { res.writeHead(502); res.end(String(e)) })
    out.end(body)
  })
})
await new Promise((r) => proxy.listen(0, '127.0.0.1', r))
const modelURL = `http://127.0.0.1:${proxy.address().port}/v1`

// ---- the node (the harness plays the hub) and DSH with the plugin ----

node = await startNode({ jarvisd, keep })
check('the node starts without herdr', typeof node.info.version === 'string')

const patch = join(root, 'patch.yml')
const askPlugin = join(root, 'e2e-ask.js')
writeFileSync(askPlugin, `export const name = 'e2e-ask'
export function apply(ctx) {
  // Test fixture: a command containing JARVIS_E2E_ASK needs approval, so the run exercises the approval chain.
  ctx.on('tools/pre-execute', (exec, next) => {
    const cmd = exec?.arguments?.command
    if (exec?.name === 'bash' && typeof cmd === 'string' && cmd.includes('JARVIS_E2E_ASK')) return { kind: 'ask', reason: 'e2e approval' }
    return next()
  })
}
`)
writeFileSync(patch, `- insert:
    - id: jarvis-dsh-runtime
      name: ${join(here, '..', 'index.js')}
      config:
        socket: ${node.runtimeSocket}
        interactions: phone
    - id: e2e-ask
      name: ${askPlugin}
# Commands run unconfined but still ask: a sandbox cannot start inside another sandbox (an agent's shell running this).
- id: permission
  config:
    presets:
      e2e:
        sandbox: danger-full-access
        approval: ask
    defaultPreset: e2e
`)
const dshLog = []
dsh = spawn(process.execPath, [join(dshCheckout, 'apps/cli/lib/bin.js'), '--profile', 'web', '--patch', patch, '--no-open', '--port', '0'], {
  cwd: work,
  env: { ...process.env, DSH_HOME: dshHome, DEEPSEEK_BASE_URL: modelURL, DEEPSEEK_API_KEY: 'mock-key' },
  stdio: ['ignore', 'pipe', 'pipe'],
})
dsh.stdout.on('data', (d) => dshLog.push(String(d)))
dsh.stderr.on('data', (d) => dshLog.push(String(d)))

const userText = (sid, text) => node.items(sid).some((i) => i.role === 'user' && i.kind === 'text' && i.text === text)
const assistantText = (sid, text) => node.items(sid).some((i) => i.role === 'assistant' && i.kind === 'text' && i.text === text)

try {
  const launchers = await until('the dsh launcher (the plugin connected)', async () => {
    const { launchers } = await node.call('launchers.list')
    return launchers.find((l) => l.name === 'dsh') ? launchers : undefined
  }, 120_000)
  check('the runtime connects and offers its launcher', launchers.some((l) => l.name === 'dsh' && l.source === 'dsh'))

  const { session } = await node.call('session.start', { launcher: 'dsh', cwd: work, async: true })
  const sid = session.session_id
  check('a session starts from the phone', typeof sid === 'string' && sid !== '', sid)
  await until('the new session listed', async () => ((await node.call('sessions.list')).sessions ?? []).some((s) => s.session_id === sid))
  const row = ((await node.call('sessions.list')).sessions ?? []).find((s) => s.session_id === sid)
  check('the session carries the runtime\'s agent and label', row.agent === 'dsh' && row.agent_label === 'DSH' && row.space_kind === 'dsh')
  check('it is a runtime source that can send, interrupt and answer, without a terminal', row.source_kind === 'runtime' && row.capabilities?.send === true && row.capabilities?.interrupt === true && row.capabilities?.approve === true && !row.capabilities?.terminal, JSON.stringify(row.capabilities))
  await node.call('session.watch', { session_id: sid, since_seq: 0 })

  // Text turn: thinking, text, working → done.
  await script({ sequence: ['reasoning_success'], reasoningText: 'thinking it over', successText: 'Hello from DSH', chunkSize: 3 })
  await node.call('session.send', { session_id: sid, text: 'hello from the phone', client_msg_id: randomUUID() })
  await until('the reply', () => assistantText(sid, 'Hello from DSH'), 60_000)
  check('the prompt comes back as a user item', userText(sid, 'hello from the phone'))
  check('thinking comes through', node.items(sid).some((i) => i.kind === 'thinking' && i.text.includes('thinking it over')))
  await until('done', () => node.status(sid) === 'done', 20_000).catch(() => {})
  check('the session ends the turn as done', node.status(sid) === 'done', String(node.status(sid)))
  const page = await node.call('session.items', { session_id: sid, turns: 2, fold_work: true, lazy_images: true })
  check('history pages from the end', page.items.some((i) => i.text === 'Hello from DSH') && page.history === `dsh:${sid}` && page.to_seq > 0)

  // Approval: the phone allows the command; its output comes back.
  node.resolve = (ix) => (ix.kind === 'permission' ? { allow: true } : { dismiss: true })
  await script({ sequence: ['tool_call_success', 'success'], toolName: 'bash', toolArguments: JSON.stringify({ command: 'echo JARVIS_E2E_ASK ran', description: 'e2e' }), successText: 'The command ran' })
  await node.call('session.send', { session_id: sid, text: 'run the command', client_msg_id: randomUUID() })
  const ix = await until('the approval', () => node.asked.find((i) => i.kind === 'permission'), 60_000)
  check('the approval reaches the phone with the command', ix.tool_name === 'Bash' && ix.tool_input?.command === 'echo JARVIS_E2E_ASK ran', JSON.stringify({ tool: ix.tool_name, input: ix.tool_input }))
  await until('the tool result', () => node.items(sid).some((i) => i.kind === 'tool_result' && /JARVIS_E2E_ASK ran/.test(i.text ?? '')), 60_000)
  check('the allowed command runs and its output comes back', true)
  await until('the follow-up reply', () => assistantText(sid, 'The command ran'), 60_000)
  check('the turn goes on after the approval', true)
  check('the card is DSH\'s, with DSH\'s reason beside the command', ix.agent === 'dsh' && ix.agent_label === 'DSH' && ix.reason === 'e2e approval' && ix.summary === 'echo JARVIS_E2E_ASK ran', JSON.stringify({ agent: ix.agent, label: ix.agent_label, reason: ix.reason, summary: ix.summary }))

  // Refusal with words: rejected, and the words reach the agent in the turn.
  node.resolve = (x) => (x.kind === 'permission' ? { allow: false, message: 'not this one, JARVIS_E2E_WHY' } : { dismiss: true })
  const asked = node.asked.length
  await script({ sequence: ['tool_call_success', 'success'], toolName: 'bash', toolArguments: JSON.stringify({ command: 'echo JARVIS_E2E_ASK again', description: 'e2e' }), successText: 'Understood' })
  await node.call('session.send', { session_id: sid, text: 'run it again', client_msg_id: randomUUID() })
  await until('the second approval', () => node.asked.length > asked, 60_000)
  await until('the reason as a user message', () => node.items(sid).some((i) => i.role === 'user' && /JARVIS_E2E_WHY/.test(i.text ?? '')), 60_000)
  check('a refusal with words reaches the agent', !node.items(sid).some((i) => i.kind === 'tool_result' && /JARVIS_E2E_ASK again/.test(i.text ?? '') && !i.is_error))
  await until('the turn after the refusal ends', () => node.status(sid) === 'done', 60_000).catch(() => {})

  // Question: the phone picks an option; the agent gets it.
  node.resolve = (q) => (q.kind === 'question' ? { answers: { 'Which database?': 'sqlite' } } : { dismiss: true })
  await script({ sequence: ['tool_call_success', 'success'], toolName: 'ask_user_question', toolArguments: JSON.stringify({ questions: [{ id: 'db', question: 'Which database?', options: [{ label: 'postgres' }, { label: 'sqlite' }] }] }), successText: 'Using sqlite' })
  await node.call('session.send', { session_id: sid, text: 'ask me something', client_msg_id: randomUUID() })
  const q = await until('the question', () => node.asked.find((x) => x.kind === 'question'), 60_000)
  check('the question reaches the phone with its options', q.questions?.[0]?.question === 'Which database?' && q.questions[0].options.length === 2)
  await until('the answer as a tool result', () => node.items(sid).some((i) => i.kind === 'tool_result' && /sqlite/.test(i.text ?? '')), 60_000)
  check('the answer goes back to the agent', true)
  await until('the reply after the answer', () => assistantText(sid, 'Using sqlite'), 60_000)

  // Interrupt: a slow reply stopped from the phone; its text streams meanwhile.
  await script({ sequence: ['slow_success'], successText: 'a long slow reply '.repeat(30), chunkSize: 2, chunkDelayMs: 100 })
  await node.call('session.send', { session_id: sid, text: 'say something long', client_msg_id: randomUUID() })
  await until('working', () => node.status(sid) === 'working', 20_000)
  await until('live text as deltas', () => node.deltas(sid).some((t) => t.includes('a long slow')), 20_000)
  check('live text arrives as deltas while the reply streams', true)
  await node.call('session.interrupt', { session_id: sid })
  await until('the interrupt notice', () => node.items(sid).some((i) => i.kind === 'notice' && i.text === '已中断'), 30_000)
  check('an interrupt from the phone stops the turn', true)

  check('pushed batches are continuous', node.gaps(sid).length === 0, `${node.batches(sid).length} batches`)
} catch (err) {
  check('run', false, err.message)
  console.log('--- node log ---\n' + node.log.join('').slice(-3000))
  console.log('--- dsh log ---\n' + dshLog.join('').slice(-3000).replace(/token=[\w-]+/g, 'token=<redacted>'))
} finally {
  await main?.close?.()
  await side.close?.()
  proxy.close()
}
const failed = summary()
await cleanup()
process.exit(failed ? 1 : 0)
