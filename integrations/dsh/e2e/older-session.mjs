// End-to-end: a DSH session that dropped out of the runtime's list (DSH lists its latest sessions only, here 1)
// can still be sent to from the phone, and its reply comes back (docs/integrations/dsh.md "Verification"). It used to go to herdr as a
// Claude session to resume. Same setup as run.mjs: a real `dsh web` (isolated DSH_HOME, DSH's llm-mock-server as the
// model) and a real `personad node --stdio` played as the hub by the runtime SDK's harness.
//
//   DSH_CHECKOUT=<dsh source checkout, built> PERSONAD=<personad binary> node integrations/dsh/e2e/older-session.mjs

import { spawn } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { checks, startNode, until } from '../../runtime-sdk/testing/node-harness.mjs'

const here = dirname(fileURLToPath(import.meta.url))
const dshCheckout = process.env.DSH_CHECKOUT
const personad = process.env.PERSONAD
if (!dshCheckout || !personad) {
  console.error('set DSH_CHECKOUT (a built dsh source checkout) and PERSONAD (a personad binary)')
  process.exit(2)
}
const root = mkdtempSync(join('/tmp', 'jdsh-older-'))
const dshHome = join(root, 'dsh')
const workA = join(root, 'a')
const workB = join(root, 'b')
for (const d of [dshHome, workA, workB]) mkdirSync(d, { recursive: true })
const { check, summary } = checks()

const { startMockLlmServer } = await import(pathToFileURL(join(dshCheckout, 'packages/test-support/llm-mock-server/lib/index.js')).href)
const llm = await startMockLlmServer({ sequence: ['success'], repeatLast: true, successText: 'Reply from DSH' })
const node = await startNode({ personad })
const patch = join(root, 'patch.yml')
writeFileSync(patch, `- insert:
    - id: persona-dsh-runtime
      name: ${join(here, '..', 'index.js')}
      config:
        socket: ${node.runtimeSocket}
        interactions: browser
        maxSessions: 1
`)
const startDsh = () => spawn(process.execPath, [join(dshCheckout, 'apps/cli/lib/bin.js'), '--profile', 'web', '--patch', patch, '--no-open', '--port', '0'], {
  cwd: workA,
  env: { ...process.env, DSH_HOME: dshHome, DEEPSEEK_BASE_URL: new URL('v1', llm.baseURL.endsWith('/') ? llm.baseURL : `${llm.baseURL}/`).href, DEEPSEEK_API_KEY: 'mock-key' },
  stdio: 'ignore',
})
async function stopDsh() {
  if (dsh.exitCode !== null) return
  const exited = new Promise((r) => dsh.once('exit', r))
  dsh.kill('SIGTERM')
  await Promise.race([exited, new Promise((r) => setTimeout(r, 10_000))])
}
let dsh = startDsh()
const listed = async (sid) => ((await node.call('sessions.list')).sessions ?? []).some((s) => s.session_id === sid)
const replies = (sid) => node.items(sid).filter((i) => i.role === 'assistant' && i.kind === 'text').length

try {
  await until('the dsh launcher', async () => (await node.call('launchers.list')).launchers.some((l) => l.name === 'dsh'), 120_000)
  const a = (await node.call('session.start', { launcher: 'dsh', cwd: workA, async: true })).session.session_id
  await node.call('session.watch', { session_id: a, since_seq: 0 })
  await node.call('session.send', { session_id: a, text: 'first', client_msg_id: randomUUID() })
  await until('A answers', () => replies(a) >= 1, 60_000)
  await until('A listed', () => listed(a), 30_000)

  // B is newer and the plugin lists one session, but A stays: the phone sent to it.
  const b = (await node.call('session.start', { launcher: 'dsh', cwd: workB, async: true })).session.session_id
  await node.call('session.send', { session_id: b, text: 'second', client_msg_id: randomUUID() })
  await until('B listed', () => listed(b), 30_000)
  check('a session the phone sent to stays listed past the limit', await listed(a))

  // DSH restarts (the plugin forgets what the phone touched): its new list has only B. A is still DSH's.
  await stopDsh()
  dsh = startDsh()
  await until('the plugin back, listing only B', async () => (await listed(b)) && !(await listed(a)), 120_000)
  check('after a DSH restart the older session is out of the list', !(await listed(a)))

  const before = replies(a)
  const sent = await node.call('session.send', { session_id: a, text: 'to the older one', client_msg_id: randomUUID() }).then(() => 'sent', (e) => e.message)
  check('a send to the older, unlisted session goes to DSH', sent === 'sent', sent)
  await until('its reply comes back', () => replies(a) > before, 60_000).catch(() => {})
  check('the reply comes back to the phone', replies(a) > before)
  check('nothing went to herdr', !node.log.join('').includes('herdr workspace list'))
} catch (err) {
  check('run', false, err.message)
  console.log('--- node log ---\n' + node.log.join('').slice(-2000))
} finally {
  await stopDsh()
  node.stop()
  await llm.close?.()
  rmSync(root, { recursive: true, force: true })
}
process.exit(summary() ? 1 : 0)
