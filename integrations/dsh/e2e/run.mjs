// End-to-end check of the DSH runtime (docs/dsh.md「验证」, the acceptance list of docs/runtimes.md): a real `dsh web`
// (isolated DSH_HOME, the model played by DSH's own llm-mock-server) with this plugin loaded through --patch, a real
// `jarvisd node --stdio` started by the runtime SDK's harness, which plays the hub. It starts a session from the
// "phone", sends to it, and answers an approval and a question.
//
//   DSH_CHECKOUT=<dsh source checkout, built> JARVISD=<jarvisd binary> node integrations/dsh/e2e/run.mjs
//
// Nothing outside fresh temp directories is touched: DSH_HOME, the node's HOME and the workspace all live there.

import { spawn } from 'node:child_process'
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { conformance } from '../../runtime-sdk/testing/conformance.mjs'
import { scriptedModel } from '../../runtime-sdk/testing/model.mjs'
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

// ---- the model (the SDK's scripted model: DSH's mock server behind a switch) ----

const model = await scriptedModel(dshCheckout)
const modelURL = `${model.url}/v1`

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

  // What every runtime does (the SDK's checks), then what is DSH's own: its cards' labels and reasons.
  const { approval } = await conformance({
    node, check, sid, model,
    bashArgs: (command) => ({ command, description: 'e2e' }),
    ask: { tool: 'ask_user_question', args: (question, options) => ({ questions: [{ id: 'db', question, options: options.map((label) => ({ label })) }] }) },
    history: (h) => h === `dsh:${sid}`,
  })
  check('the card is DSH\'s, with DSH\'s reason beside the command', approval.agent === 'dsh' && approval.agent_label === 'DSH' && approval.reason === 'e2e approval' && approval.summary === 'echo JARVIS_E2E_ASK ran', JSON.stringify({ agent: approval.agent, label: approval.agent_label, reason: approval.reason, summary: approval.summary }))
} catch (err) {
  check('run', false, err.message)
  console.log('--- node log ---\n' + node.log.join('').slice(-3000))
  console.log('--- dsh log ---\n' + dshLog.join('').slice(-3000).replace(/token=[\w-]+/g, 'token=<redacted>'))
} finally {
  await model.close()
}
const failed = summary()
await cleanup()
process.exit(failed ? 1 : 0)
