// End-to-end check of the Persona assistant on DSH (docs/integrations/dsh.md "Assistant", docs/integrations/runtimes.md "Assistant"): a real `dsh web` with
// the plugin (isolated DSH_HOME, DSH's own mock model), a real `personad node --stdio` (the harness plays the hub). The
// "hub" sends assistant messages with the DSH launcher, as the Persona tab, AI twin, Feishu /agent and scheduled tasks
// do (all go through assistant.send): a new conversation in the assistant's directory, the live snapshot and the
// memory with the first message, the user's skills in the assistant's directory, ask_user and the other assistant
// tools, the permission mode, an interrupt, and the conversation going on after the node restarts.
//
//   DSH_CHECKOUT=<dsh source checkout, built> PERSONAD=<personad binary> node integrations/dsh/e2e/assistant.mjs
//
// Nothing outside fresh temp directories is touched.

import { spawn } from 'node:child_process'
import { chmodSync, existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { dirname, join } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { scriptedModel } from '../../runtime-sdk/testing/model.mjs'
import { checks, startNode, until } from '../../runtime-sdk/testing/node-harness.mjs'

const here = dirname(fileURLToPath(import.meta.url))
const dshCheckout = process.env.DSH_CHECKOUT
const personad = process.env.PERSONAD
if (!dshCheckout || !personad) {
  console.error('set DSH_CHECKOUT (a built dsh source checkout) and PERSONAD (a personad binary)')
  process.exit(2)
}
const keep = process.env.KEEP === '1'
const root = mkdtempSync(join('/tmp', 'jdsh-asst-'))
const dshHome = join(root, 'dsh')
const home = join(root, 'home') // the node's HOME: the assistant's directory and the user's skills live here
for (const d of [dshHome, home]) mkdirSync(d, { recursive: true })
const { check, summary } = checks()
let dsh
let node

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

// A skill in the user's skill space, to see it linked into the assistant's directory.
const skillDir = join(home, '.persona', 'skills', 'e2e-skill')
mkdirSync(join(skillDir, 'scripts'), { recursive: true })
writeFileSync(join(skillDir, 'SKILL.md'), '---\nname: e2e-skill\ndescription: e2e stand-in\n---\n')
writeFileSync(join(skillDir, 'scripts', 'hello.sh'), '#!/bin/bash\necho hello\n')
chmodSync(join(skillDir, 'scripts', 'hello.sh'), 0o755)

const model = await scriptedModel(dshCheckout)
const assistantDir = join(home, '.persona', 'assistant')

async function startTheNode() {
  node = await startNode({ personad, keep: true, home })
  node.assistantTools = [{ name: 'ask_user', description: 'Ask the user on the phone', input_schema: { type: 'object', properties: { question: { type: 'string' } }, required: ['question'] } }]
  node.assistantContext = () => '- 现在：e2e 测试时间 PERSONA_E2E_SNAPSHOT\n- 在线电脑：测试机'
  node.callTool = (p) => p.name === 'ask_user' ? { content: [{ type: 'text', text: `the user answered postgres (assistant=${p.assistant === true})` }], is_error: false }
    : { content: [{ type: 'text', text: `${p.name} ran` }], is_error: false }
}
await startTheNode()

const patch = join(root, 'patch.yml')
writeFileSync(patch, `- insert:
    - id: persona-dsh-runtime
      name: ${join(here, '..', 'index.js')}
      config:
        socket: ${node.runtimeSocket}
        presets:
          default: e2e
          plan: e2e-plan
          bypassPermissions: e2e
# Commands run unconfined: a sandbox cannot start inside another sandbox (an agent's shell running this).
- id: permission
  config:
    presets:
      e2e:
        sandbox: danger-full-access
        approval: ask
      e2e-plan:
        sandbox: danger-full-access
        approval: ask
    defaultPreset: e2e
`)
const dshLog = []
dsh = spawn(process.execPath, [join(dshCheckout, 'apps/cli/lib/bin.js'), '--profile', 'web', '--patch', patch, '--no-open', '--port', '0'], {
  cwd: root,
  env: { ...process.env, DSH_HOME: dshHome, DEEPSEEK_BASE_URL: `${model.url}/v1`, DEEPSEEK_API_KEY: 'mock-key' },
  stdio: ['ignore', 'pipe', 'pipe'],
})
dsh.stdout.on('data', (d) => dshLog.push(String(d)))
dsh.stderr.on('data', (d) => dshLog.push(String(d)))

/** DSH's own log of a session, read-only (what the model was given, which the chat does not show). */
async function dshEvents(sid) {
  const req = createRequire(join(dshCheckout, 'packages/bundle/base/package.json'))
  const imp = (m) => import(pathToFileURL(req.resolve(m)).href)
  const { Context } = await imp('@deepseek-ai/cordis')
  const Persistence = await imp('@deepseek-ai/dsh-session-persistence-jsonl')
  const ctx = new Context()
  ctx.plugin(Persistence, { root: join(dshHome, 'sessions') })
  const p = await until('the store', () => ctx.get('sessionPersistence'), 10_000)
  const h = await p.open(sid, 'read')
  try {
    return (await h.read()).events
  } finally {
    await h.close()
  }
}
const userTexts = (events) => events.filter((e) => e.type === 'user/message').flatMap((e) => (e.data?.content ?? []).filter((b) => b.type === 'text').map((b) => b.text))
const items = async (sid) => (await node.call('session.items', { session_id: sid, turns: 10 })).items
const assistantSays = async (sid, text) => (await items(sid)).some((i) => i.role === 'assistant' && i.kind === 'text' && i.text === text)
const toolResult = async (sid, re) => (await items(sid)).find((i) => i.kind === 'tool_result' && re.test(i.text ?? ''))
const rowOf = async (sid) => ((await node.call('sessions.list')).sessions ?? []).find((s) => s.session_id === sid)
const idle = (sid) => until('the turn ends', async () => (await rowOf(sid))?.status === 'done', 60_000).catch(() => {})
const send = (sid, text, extra = {}) => node.call('assistant.send', { session_id: sid, launcher: 'dsh', text, ...extra }, 60_000)

try {
  const launchers = await until('the dsh launcher, for the assistant', async () => {
    const { launchers } = await node.call('launchers.list')
    return launchers?.find((l) => l.name === 'dsh' && l.assistant) ? launchers : undefined
  }, 120_000)
  check('DSH offers its launcher for the assistant', launchers.some((l) => l.name === 'dsh' && l.assistant === true && l.agent === 'dsh'))
  await node.call('assistant.prepare', { dir: '.persona/assistant', claude_md: '# 助手（e2e）\n' })

  // A new conversation, as the Persona tab starts one.
  await model.script({ sequence: ['success'], successText: 'Hello from the DSH assistant' })
  const { session_id: sid } = await node.call('assistant.send', { launcher: 'dsh', cwd: assistantDir, text: 'hello assistant' }, 60_000)
  check('a new assistant conversation starts on DSH', typeof sid === 'string' && sid.startsWith('session-'), sid)
  await node.call('session.watch', { session_id: sid, since_seq: 0 })
  await until('the reply', () => assistantSays(sid, 'Hello from the DSH assistant'), 60_000)
  const row = await rowOf(sid)
  check('it is listed as DSH\'s, in the assistant\'s directory', row?.agent === 'dsh' && row?.cwd === assistantDir, JSON.stringify({ agent: row?.agent, cwd: row?.cwd }))
  const shown = (await items(sid)).filter((i) => i.role === 'user' && i.kind === 'text').map((i) => i.text)
  check('the chat shows what was typed, not the snapshot', shown.length === 1 && shown[0] === 'hello assistant', JSON.stringify(shown))
  const first = userTexts(await dshEvents(sid)).join('\n')
  check('the model got the live snapshot with the message', first.includes('PERSONA_E2E_SNAPSHOT') && first.includes('<persona-context>'))
  check('…and the memory directory with the first one', first.includes('MEMORY.md'))
  check('the skills are in the assistant\'s directory for DSH', existsSync(join(assistantDir, '.agents', 'skills', 'e2e-skill', 'scripts', 'hello.sh')))
  await idle(sid)

  // ask_user, the assistant's own tool: through the node to the hub as the assistant's.
  await model.script({ sequence: ['tool_call_success', 'success'], toolName: 'mcp__persona__ask_user', toolArguments: JSON.stringify({ question: 'Which database?' }), successText: 'Using postgres' })
  await send(sid, 'ask me which database')
  await until('the ask_user result', () => toolResult(sid, /the user answered postgres/), 60_000)
  const ask = node.toolCalls.find((c) => c.name === 'ask_user')
  check('ask_user reaches the hub as the assistant\'s, with the conversation', ask?.assistant === true && ask?.session_id === sid, JSON.stringify(ask))
  check('…and its answer reaches the agent', Boolean(await toolResult(sid, /assistant=true/)))
  await until('the reply after ask_user', () => assistantSays(sid, 'Using postgres'), 60_000)
  await idle(sid)

  // The permission mode: plan mode goes with the next message as DSH's preset for it.
  await node.call('session.modes', { session_id: sid, permission_mode: 'plan' })
  await model.script({ sequence: ['success'], successText: 'Planning' })
  await send(sid, 'make a plan')
  await until('the plan-mode reply', () => assistantSays(sid, 'Planning'), 60_000)
  const presets = (await dshEvents(sid)).filter((e) => e.type === 'permission/preset').map((e) => e.data?.preset)
  check('plan mode sets DSH\'s preset for it', presets.at(-1) === 'e2e-plan', presets.join(','))
  check('the conversation shows the mode', (await rowOf(sid))?.permission_mode === 'plan')
  await node.call('session.modes', { session_id: sid, permission_mode: 'default' })
  await idle(sid)

  // An interrupt from the phone stops the turn.
  await model.script({ sequence: ['slow_success'], successText: 'a long slow reply '.repeat(30), chunkSize: 2, chunkDelayMs: 100 })
  await send(sid, 'say something long')
  await until('working', async () => (await rowOf(sid))?.status === 'working', 30_000)
  await node.call('session.interrupt', { session_id: sid })
  await until('the interrupt notice', async () => (await items(sid)).some((i) => i.kind === 'notice' && i.text === '已中断'), 30_000)
  check('an interrupt stops the assistant\'s turn', true)
  await idle(sid)

  // The node restarts (a deploy): the conversation goes on in DSH, the next message reaches it.
  node.stop()
  await startTheNode()
  await until('DSH back on the new node', async () => (await rowOf(sid)) !== undefined, 60_000)
  await model.script({ sequence: ['success'], successText: 'Still here' })
  await send(sid, 'are you still there')
  await node.call('session.watch', { session_id: sid, since_seq: 0 })
  await until('the reply after the restart', () => assistantSays(sid, 'Still here'), 60_000)
  check('after a node restart the conversation goes on in DSH', userTexts(await dshEvents(sid)).filter((t) => t === 'are you still there').length === 1)
} catch (err) {
  check('run', false, err.message)
  console.log('--- node log ---\n' + node.log.join('').slice(-4000))
  console.log('--- dsh log ---\n' + dshLog.join('').slice(-3000).replace(/token=[\w-]+/g, 'token=<redacted>'))
} finally {
  await model.close()
}
const failed = summary()
await cleanup()
process.exit(failed ? 1 : 0)
