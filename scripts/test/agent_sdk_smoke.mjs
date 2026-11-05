#!/usr/bin/env node
// A smoke run of the Agent SDK bridge (integrations/agent-sdk/dist/bridge.mjs) against a real gateway: the
// provider's env comes from one claude alias in ~/.zshrc ($ZDOTDIR first) and reaches the bridge only through its
// environment; nothing of it is printed. Pick a cheap model's alias (a flash / haiku one):
//   node scripts/test/agent_sdk_smoke.mjs <alias>
// Steps: one short turn (text deltas, result), set_model, apply_flag_settings{effortLevel}, interrupt, a resume of
// the same session without replay, and a stdio MCP server's tool. One line per step; exits 1 on the first failure.
// Scratch files go to /tmp/bridge/ and are removed at the end.

import { execFileSync } from 'node:child_process'
import { mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { homedir } from 'node:os'
import { join } from 'node:path'
import { redactor, secretValues } from '../../integrations/agent-sdk/src/lib.mjs'
import { spawnBridge } from '../../integrations/agent-sdk/test/harness.mjs'

const TMP = '/tmp/bridge'
const STEP_MS = 180_000

const name = process.argv[2]
if (!name) {
  console.error('usage: agent_sdk_smoke.mjs <alias in ~/.zshrc>')
  process.exit(2)
}

// The alias's K=V words before `claude` (the contract's launchers.zshrc shape: quoted or not).
function aliasEnv(alias) {
  const rc = join(process.env.ZDOTDIR || homedir(), '.zshrc')
  const line = readFileSync(rc, 'utf8').split('\n').find((l) => l.trim().startsWith(`alias ${alias}=`))
  if (!line) throw new Error(`no alias ${alias} in ${rc}`)
  let body = line.trim().slice(`alias ${alias}=`.length)
  if (/^["']/.test(body)) body = body.slice(1, body.lastIndexOf(body[0]))
  const env = {}
  for (const word of body.split(/\s+/)) {
    const m = /^([A-Z_][A-Z0-9_]*)=(.*)$/.exec(word)
    if (!m) break
    env[m[1]] = m[2].replace(/^(["'])(.*)\1$/, '$2')
  }
  if (!env.ANTHROPIC_BASE_URL || !(env.ANTHROPIC_AUTH_TOKEN || env.ANTHROPIC_API_KEY)) throw new Error(`alias ${alias} sets no gateway`)
  return env
}

const provider = aliasEnv(name)
// The values that must not show, by the bridge's own rule: the key, the gateway's address, request headers.
const secrets = secretValues(provider)
const scrub = redactor(provider)
const model = provider.ANTHROPIC_MODEL
const otherModel = provider.ANTHROPIC_DEFAULT_HAIKU_MODEL || model

// The env the node would give it (CONTRACT §2, roughly): inherited provider vars out, the alias's in, a scratch
// config dir so the user's settings and hooks stay out of it.
const env = { ...process.env }
for (const k of Object.keys(env)) {
  if (/^(ANTHROPIC_|CLAUDE_CODE_|API_TIMEOUT_MS$|HTTPS?_PROXY$|NO_PROXY$)/.test(k)) delete env[k]
}
Object.assign(env, provider, { CLAUDE_CONFIG_DIR: join(TMP, 'config'), CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: '1' })
delete env.CLAUDE_CODE_EFFORT_LEVEL // apply_flag_settings sets it below
const claude = execFileSync('which', ['claude'], { encoding: 'utf8' }).trim()
const cwd = join(TMP, 'work')

// The MCP test server: stdio JSON-RPC with one tool, echo, that also notes each call in a file.
const MCP_LOG = join(TMP, 'echo-calls.txt')
const MCP_SERVER = `
import { appendFileSync } from 'node:fs'
import { createInterface } from 'node:readline'
const send = (m) => process.stdout.write(JSON.stringify({ jsonrpc: '2.0', ...m }) + '\\n')
for await (const l of createInterface({ input: process.stdin })) {
  if (!l.trim()) continue
  const { id, method, params } = JSON.parse(l)
  if (method === 'initialize') send({ id, result: { protocolVersion: params.protocolVersion, capabilities: { tools: {} }, serverInfo: { name: 'echo', version: '1.0.0' } } })
  else if (method === 'tools/list') send({ id, result: { tools: [{ name: 'echo', description: 'Echo the text back.', inputSchema: { type: 'object', properties: { text: { type: 'string' } }, required: ['text'] } }] } })
  else if (method === 'tools/call') { appendFileSync(${JSON.stringify(MCP_LOG)}, params.arguments.text + '\\n'); send({ id, result: { content: [{ type: 'text', text: 'echo: ' + params.arguments.text }] } }) }
  else if (id !== undefined) send({ id, result: {} })
}
`

function bridge(options) {
  const b = spawnBridge({ env, cwd, timeoutMs: STEP_MS })
  b.send({ type: 'persona_start', options: { cwd, pathToClaudeCodeExecutable: claude, permissionMode: 'default', settings: { env: provider }, ...options } })
  return b
}

// leakedArgs: how many of pid's descendants carry a provider secret in their command line (ps -o args), and how
// many descendants there are. Only counts: the args are never printed.
function leakedArgs(pid) {
  const rows = execFileSync('ps', ['-A', '-o', 'pid=,ppid=,args='], { encoding: 'utf8', maxBuffer: 64 << 20 }).split('\n')
  const procs = rows.map((r) => /^\s*(\d+)\s+(\d+)\s(.*)$/.exec(r)).filter(Boolean).map(([, p, pp, args]) => ({ p: +p, pp: +pp, args }))
  const tree = new Set([pid])
  for (let grew = true; grew;) {
    grew = false
    for (const x of procs) if (tree.has(x.pp) && !tree.has(x.p)) { tree.add(x.p); grew = true }
  }
  const kids = procs.filter((x) => x.p !== pid && tree.has(x.p))
  return { kids: kids.length, leaks: kids.filter((x) => secrets.some((v) => x.args.includes(v))).length }
}

const text = (m) => (m.message?.content ?? []).filter((c) => c.type === 'text').map((c) => c.text).join('')
const isDelta = (m) => m.type === 'stream_event' && m.event?.type === 'content_block_delta' && m.event.delta?.type === 'text_delta'
const ok = (step, detail) => console.log(`ok   ${step}: ${detail}`)
let current

async function main() {
  rmSync(TMP, { recursive: true, force: true })
  mkdirSync(cwd, { recursive: true })
  mkdirSync(join(TMP, 'config'), { recursive: true })
  writeFileSync(join(TMP, 'echo-mcp.mjs'), MCP_SERVER)

  // 1. A short turn.
  let step = 'turn'
  let b = current = bridge({ model })
  const ready = await b.until((m) => m.type === 'persona_bridge' && m.event === 'ready')
  b.user('Remember the word "kumquat". Reply with just: noted')
  const r1 = await b.until((m) => m.type === 'result')
  const deltas = b.msgs.filter(isDelta).length
  if (!deltas || r1.is_error) throw new Error(`${step}: ${deltas} text deltas, result ${r1.subtype}`)
  const session = r1.session_id
  ok(step, `sdk ${ready.sdk_version}, claude ${ready.claude_version || '?'}, ${deltas} text deltas, result ${r1.subtype}`)

  // 1b. The provider's env went to claude in a settings file, not on its command line.
  step = 'ps args'
  const { kids, leaks } = leakedArgs(b.pid)
  if (!kids || leaks) throw new Error(`${step}: ${leaks} of ${kids} descendant processes carry a secret in their args`)
  ok(step, `no secret in the args of the bridge's ${kids} descendant process(es)`)

  // 2. set_model.
  step = 'set_model'
  b.control('m1', { subtype: 'set_model', model: otherModel })
  let resp = await b.until((m) => m.type === 'control_response' && m.response.request_id === 'm1')
  if (resp.response.subtype !== 'success') throw new Error(`${step}: ${resp.response.error}`)
  let from = b.msgs.length
  b.user('Reply with just: ok')
  let r = await b.until((m) => m.type === 'result', from)
  const used = b.msgs.slice(from).find((m) => m.type === 'assistant')?.message?.model
  ok(step, `success; the next turn ran on ${used ?? '?'} (${r.subtype})`)

  // 3. apply_flag_settings{effortLevel}.
  step = 'apply_flag_settings'
  b.control('e1', { subtype: 'apply_flag_settings', settings: { effortLevel: 'low' } })
  resp = await b.until((m) => m.type === 'control_response' && m.response.request_id === 'e1')
  if (resp.response.subtype !== 'success') throw new Error(`${step}: ${resp.response.error}`)
  ok(step, 'effortLevel low: success')

  // 4. interrupt, mid-answer.
  step = 'interrupt'
  from = b.msgs.length
  b.user('Count from 1 to 400, one number per line, nothing else.')
  await b.until(isDelta, from)
  b.control('i1', { subtype: 'interrupt' })
  resp = await b.until((m) => m.type === 'control_response' && m.response.request_id === 'i1')
  if (resp.response.subtype !== 'success') throw new Error(`${step}: ${resp.response.error}`)
  r = await b.until((m) => m.type === 'result', from)
  ok(step, `success; the turn ended ${r.subtype}${r.is_error ? ' (error)' : ''}`)

  // 5. stdin closed: the bridge exits; resumed, the same session goes on without the old messages again.
  step = 'resume'
  const code = await b.end()
  if (code !== 0 || b.msgs.at(-1)?.event !== 'exit') throw new Error(`${step}: the first bridge exited ${code}`)
  const before = new Set(b.msgs.map((m) => m.uuid).filter(Boolean))
  b = current = bridge({ model, resume: session })
  await b.until((m) => m.type === 'persona_bridge' && m.event === 'ready')
  await new Promise((res) => setTimeout(res, 3000)) // what a resumed CLI replays comes right away
  const early = b.msgs.filter((m) => m.type !== 'persona_bridge' && m.type !== 'system').map((m) => m.type)
  from = b.msgs.length
  b.user('What word did I ask you to remember? Reply with just the word.')
  r = await b.until((m) => m.type === 'result', from)
  const replayed = b.msgs.filter((m) => m.uuid && before.has(m.uuid)).length
  const results = b.msgs.filter((m) => m.type === 'result').length
  if (r.session_id !== session) throw new Error(`${step}: another session ${r.session_id}`)
  if (replayed || results !== 1 || early.length) throw new Error(`${step}: ${replayed} old uuids, ${results} results, before the turn: ${early}`)
  if (!/kumquat/i.test(r.result ?? '')) throw new Error(`${step}: the answer does not remember (${scrub(r.result).slice(0, 80)})`)
  const dropped = b.stderr.split('\n').filter((l) => l.includes('dropped a replayed')).map((l) => l.split('replayed ')[1])
  ok(step, `same session, remembered the word, 0 old messages again, 1 result; the guard dropped ${dropped.length}${dropped.length ? ` (${dropped.join(', ')})` : ''}`)
  await b.end()

  // 6. A stdio MCP server's tool, allowed by allowedTools.
  step = 'mcp'
  b = current = bridge({
    model,
    mcpServers: { echo: { type: 'stdio', command: process.execPath, args: [join(TMP, 'echo-mcp.mjs')] } },
    allowedTools: ['mcp__echo__echo'],
  })
  b.user('Call the echo tool (mcp__echo__echo) with text "persona-ping", then reply with what it returned.')
  const init = await b.until((m) => m.type === 'system' && m.subtype === 'init') // a streaming CLI sends it with the first turn
  const status = init.mcp_servers?.find((s) => s.name === 'echo')?.status
  r = await b.until((m) => m.type === 'result')
  const called = b.msgs.some((m) => m.type === 'assistant' && (m.message?.content ?? []).some((c) => c.type === 'tool_use' && c.name === 'mcp__echo__echo'))
  let log = ''
  try { log = readFileSync(MCP_LOG, 'utf8') } catch {}
  if (!called || !log.includes('persona-ping')) throw new Error(`${step}: server ${status}, tool_use ${called}, calls ${JSON.stringify(log)}`)
  ok(step, `server ${status}, the tool was called (${log.trim().split('\n').length}x) and answered: ${scrub(text(b.msgs.filter((m) => m.type === 'assistant').at(-1))).slice(0, 60)}`)
  await b.end()
}

try {
  await main()
  console.log('agent_sdk_smoke: all steps passed')
} catch (err) {
  console.log(`FAIL ${scrub(err.message)}`)
  if (current) console.log(`bridge stderr (last lines):\n${scrub(current.stderr).split('\n').slice(-15).join('\n')}`)
  current?.kill()
  process.exitCode = 1
} finally {
  rmSync(TMP, { recursive: true, force: true })
}
