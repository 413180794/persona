// End-to-end acceptance of the echo example (docs/integrations/runtimes.md "Acceptance") against a real `personad node`: the template a new
// runtime copies for its own e2e (replace starting the echo runtime with starting yours).
//
//   PERSONAD=<personad binary> node integrations/runtime-sdk/e2e/echo.mjs

import { randomUUID } from 'node:crypto'
import { createEchoRuntime } from '../examples/echo-runtime.mjs'
import { checks, startNode, until } from '../testing/node-harness.mjs'

const personad = process.env.PERSONAD
if (!personad) {
  console.error('set PERSONAD (a personad binary)')
  process.exit(2)
}
const { check, summary } = checks()
const node = await startNode({ personad, keep: process.env.KEEP === '1' })
const { runtime } = createEchoRuntime({ socketPath: node.runtimeSocket, retryMs: 100, answerFirst: 'phone' })
try {
  runtime.start()
  const launchers = await until('the echo launcher', async () => {
    const { launchers } = await node.call('launchers.list')
    return launchers.find((l) => l.name === 'echo') ? launchers : undefined
  }, 20_000)
  check('the runtime connects and offers its launcher', launchers.some((l) => l.source === 'echo'))
  const { session } = await node.call('session.start', { launcher: 'echo', cwd: '/tmp' })
  const sid = session.session_id
  const row = await until('listed', async () => ((await node.call('sessions.list')).sessions ?? []).find((s) => s.session_id === sid))
  check('its session is listed with the runtime\'s agent and label', row.agent === 'echo' && row.agent_label === 'Echo' && row.space_kind === 'echo' && !row.pane_id)
  check('it is a runtime source with the declared capabilities', row.source_kind === 'runtime' && row.source_instance_id === 'echo' && row.capabilities?.send === true && row.capabilities?.interrupt === true && !row.capabilities?.terminal && !row.capabilities?.attach, JSON.stringify(row.capabilities))
  await node.call('session.watch', { session_id: sid, since_seq: 0 })
  node.resolve = (ix) => (ix.kind === 'permission' ? { allow: true } : { dismiss: true })
  const id = randomUUID()
  await node.call('session.send', { session_id: sid, text: 'ask then echo', client_msg_id: id })
  await node.call('session.send', { session_id: sid, text: 'ask then echo', client_msg_id: id }) // an app retry
  await until('the echo', () => node.items(sid).some((i) => i.text === 'echo: ask then echo'), 10_000)
  check('a retried send is delivered once', node.items(sid).filter((i) => i.role === 'user').length === 1)
  const card = node.asked.find((i) => i.kind === 'permission' && i.session_id === sid)
  check('the permission card reaches the phone as the runtime\'s own', card?.agent === 'echo' && card?.agent_label === 'Echo', JSON.stringify({ agent: card?.agent, label: card?.agent_label }))
  check('live text arrives as deltas', node.deltas(sid).some((t) => t.length > 0 && 'echo: ask then echo'.startsWith(t)))
  const page = await node.call('session.items', { session_id: sid, turns: 1 })
  check('history pages through the node', page.items.length === 2 && page.history === `echo:${sid}`)
  await until('done', () => node.status(sid) === 'done', 5000).catch(() => {})
  check('the turn ends as done', node.status(sid) === 'done', String(node.status(sid)))
  check('pushed batches are continuous', node.gaps(sid).length === 0, `${node.batches(sid).length} batches`)
  const keys = await node.call('session.keys', { session_id: sid, keys: ['enter'] }).then(() => 'ok', (e) => e.message)
  check('terminal calls are refused as unsupported', /^unsupported/.test(keys), keys)
  runtime.stop()
  await until('its sessions leave with it', async () => !((await node.call('sessions.list')).sessions ?? []).some((s) => s.session_id === sid), 5000)
  check('its sessions leave with it', true)
} catch (err) {
  check('run', false, err.message)
  console.log('--- node log ---\n' + node.log.join('').slice(-3000))
} finally {
  runtime.stop()
  node.stop()
}
process.exit(summary() ? 1 : 0)
