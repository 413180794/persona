// End-to-end check of pi in herdr (docs/integrations/pi.md "pi in herdr"): a real herdr (a named session of its own, its config in
// a temp dir, with herdr's pi integration installed there), a real pi TUI in a herdr pane with this package, a real
// `personad node --stdio` on that herdr (the runtime SDK's harness plays the hub), the companion for sessions on disk,
// and the SDK's scripted model.
//
// The pi in the pane is one herdr session: its pane, space and screen are herdr's, its conversation and control the
// extension's (the SDK's conformance checks). After pi quits, a send from the phone resumes it in a new herdr tab
// (`pi --session`), not in pi-web.
//
//   DSH_CHECKOUT=<dsh source checkout, built> PERSONAD=<personad binary> node integrations/pi/e2e/herdr.mjs
//
// Needs herdr and pi on PATH. Nothing outside a fresh temp directory is touched; the herdr session is stopped by name.

import { execFileSync, spawn } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { conformance } from '../../runtime-sdk/testing/conformance.mjs'
import { scriptedModel } from '../../runtime-sdk/testing/model.mjs'
import { checks, startNode, until } from '../../runtime-sdk/testing/node-harness.mjs'

const here = dirname(fileURLToPath(import.meta.url))
const pkg = resolve(here, '..')
const personad = process.env.PERSONAD
if (!process.env.DSH_CHECKOUT || !personad) {
  console.error('set DSH_CHECKOUT (a built dsh source checkout, for its mock model server) and PERSONAD (a personad binary)')
  process.exit(2)
}
const root = mkdtempSync(join('/tmp', 'jpih-'))
const home = join(root, 'home')
const agentDir = join(home, '.pi', 'agent')
const work = join(root, 'work')
const config = join(root, 'cfg') // herdr's config (its named session's socket lives under it): short, for the socket path
for (const d of [agentDir, work, config]) mkdirSync(d, { recursive: true })
const session = `jpi-${process.pid}`
const sock = join(config, 'herdr', 'sessions', session, 'herdr.sock')
const { check, summary } = checks()
const PATH = `${dirname(execFileSync('/usr/bin/which', ['pi'], { encoding: 'utf8' }).trim())}:${dirname(execFileSync('/usr/bin/which', ['herdr'], { encoding: 'utf8' }).trim())}:/usr/bin:/bin`

const model = await scriptedModel(process.env.DSH_CHECKOUT)
writeFileSync(join(agentDir, 'models.json'), JSON.stringify({ providers: { mock: {
  baseUrl: model.url, api: 'anthropic-messages', apiKey: 'mock-key',
  models: [{ id: 'mock-model', name: 'Mock', reasoning: true, input: ['text'], contextWindow: 200000, maxTokens: 8192,
    cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 } }],
} } }))
writeFileSync(join(agentDir, 'settings.json'), JSON.stringify({ defaultProvider: 'mock', defaultModel: 'mock-model', quietStartup: true, packages: [pkg] }))
writeFileSync(join(agentDir, 'persona.json'), JSON.stringify({ approve: ['bash'], askUser: true, answerFirst: 'phone' }))

const env = { PATH, HOME: home, XDG_CONFIG_HOME: config, PI_CODING_AGENT_DIR: agentDir, PI_OFFLINE: '1', TERM: 'xterm-256color' }
// herdr's own pi integration, installed the way a user installs it (into this run's HOME).
execFileSync('herdr', ['integration', 'install', 'pi'], { env, stdio: 'ignore' })

const node = await startNode({ personad, env: { HERDR_SOCKET_PATH: sock } })
const paneEnv = { ...env, PERSONA_RUNTIME_SOCKET: node.runtimeSocket }
const logs = { herdr: [], companion: [] }
const server = spawn('herdr', ['--session', session, 'server'], { env: paneEnv, stdio: ['ignore', 'pipe', 'pipe'] })
server.stdout.on('data', (d) => logs.herdr.push(String(d)))
server.stderr.on('data', (d) => logs.herdr.push(String(d)))
const herdr = (...args) => execFileSync('herdr', args, { env: { ...env, HERDR_SOCKET_PATH: sock }, encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] })
const H = (...args) => JSON.parse(herdr(...args) || '{}')
const companion = spawn(process.execPath, [join(pkg, 'companion.mjs'), '--socket', node.runtimeSocket, '--port', '1'], { env: paneEnv, stdio: ['ignore', 'ignore', 'pipe'] })
companion.stderr.on('data', (d) => logs.companion.push(String(d)))

async function stop(child) {
  if (child.exitCode !== null) return
  const exited = new Promise((r) => child.once('exit', r))
  child.kill('SIGTERM')
  await Promise.race([exited, new Promise((r) => setTimeout(r, 10_000))])
}
async function cleanup() {
  await stop(companion)
  try {
    execFileSync('herdr', ['--session', session, 'server', 'stop'], { env, stdio: 'ignore' }) // by name: never another herdr
  } catch {}
  await stop(server)
  node.stop()
  await model.close()
  if (process.env.KEEP !== '1') rmSync(root, { recursive: true, force: true })
}

const rows = async () => (await node.call('sessions.list')).sessions ?? []
const row = async (sid) => (await rows()).find((s) => s.session_id === sid)
const statusOf = async (sid) => (await row(sid))?.status

try {
  await until('herdr up', () => { try { H('workspace', 'list'); return true } catch { return false } }, 20_000)
  const pane = H('workspace', 'create', '--cwd', work, '--label', 'work', '--no-focus').result.root_pane.pane_id
  execFileSync('herdr', ['pane', 'run', pane, 'pi'], { env: { ...env, HERDR_SOCKET_PATH: sock } })

  // The pi in the pane: one herdr session, completed by its extension.
  const first = await until('the pi in herdr listed', async () => (await rows()).find((s) => s.agent === 'pi' && s.pane_id === pane), 60_000)
  const sid = first.session_id
  check('the pi in a herdr pane is listed once, as herdr\'s session', (await rows()).filter((s) => s.session_id === sid).length === 1 &&
    first.space_kind === 'herdr' && !first.source_kind && first.workspace_id !== '', JSON.stringify({ pane: first.pane_id, space: first.space_kind, source: first.source_kind }))
  await until('the extension serves it', async () => !(await row(sid))?.read_only_reason, 30_000).catch(() => {})
  check('its extension serves it (no read-only reason)', !(await row(sid))?.read_only_reason, (await row(sid))?.read_only_reason)
  const screen = await node.call('session.screen', { session_id: sid })
  check('its screen is herdr\'s pane', typeof screen.text === 'string' && screen.text.length > 0)
  await node.call('session.watch', { session_id: sid, since_seq: 0 })

  // What every runtime does, through the herdr row: content and control from the extension in the pane.
  const { approval } = await conformance({
    node, check, sid, model,
    bashArgs: (command) => ({ command }),
    ask: { tool: 'ask_user', args: (question, options) => ({ question, options }) },
    history: (h) => h?.startsWith(`pi:${sid}:`),
    status: statusOf,
    ended: (s) => s === 'done' || s === 'idle',
  })
  check('the approval card is pi\'s', approval.agent === 'pi', approval.agent)
  const typed = herdr('pane', 'read', pane, '--source', 'recent')
  check('nothing from the phone was typed into the pane', !typed.includes('hello from the phone'))

  // pi quits: the session is on disk, listed by the companion with how herdr resumes it.
  execFileSync('herdr', ['pane', 'send-text', pane, '/quit'], { env: { ...env, HERDR_SOCKET_PATH: sock } })
  execFileSync('herdr', ['pane', 'send-keys', pane, 'enter'], { env: { ...env, HERDR_SOCKET_PATH: sock } })
  const off = await until('offline, from disk', async () => { const r = await row(sid); return r && r.status === 'offline' ? r : undefined }, 60_000)
  check('after pi quits the session is listed from disk', off.source_instance_id === 'pi-files', off.source_instance_id)

  // A send from the phone resumes it in a new herdr tab, and the message goes there.
  await model.script({ sequence: ['success'], successText: 'Back in herdr' })
  const sent = await node.call('session.send', { session_id: sid, text: 'are you still there?', client_msg_id: randomUUID() }).then(() => 'sent', (e) => e.message)
  check('a send to it resumes it and is delivered', sent === 'sent', sent)
  await until('the reply', () => node.items(sid).some((i) => i.role === 'assistant' && i.text === 'Back in herdr'), 60_000).catch(() => {})
  const back = await row(sid)
  check('it is herdr\'s session again, in a new pane', back?.space_kind === 'herdr' && back.pane_id && back.pane_id !== pane && !back.source_kind, JSON.stringify({ pane: back?.pane_id, src: back?.source_kind }))
  const page = await node.call('session.items', { session_id: sid, turns: 1 })
  check('the message reached the resumed pi and it answered', page.items.some((i) => i.role === 'user' && i.text === 'are you still there?') && page.items.some((i) => i.text === 'Back in herdr'))
  const agents = H('agent', 'list').result?.agents ?? []
  check('the resumed pi runs in herdr, in the same workspace', agents.some((a) => a.agent === 'pi' && a.pane_id === back?.pane_id && a.workspace_id === first.workspace_id))
} catch (err) {
  check('run', false, err.message)
  console.log('--- node log ---\n' + node.log.join('').split('\n').filter((l) => !/herdr: .* failed/.test(l)).join('\n').slice(-2500))
  console.log('--- companion log ---\n' + logs.companion.join('').slice(-1500))
}
const failed = summary()
await cleanup()
process.exit(failed ? 1 : 0)
