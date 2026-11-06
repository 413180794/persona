// End-to-end: a runtime on a resident node (`personad node --attach`, what launchd keeps running on a Mac) connects
// before any hub does and stays connected while the hub reconnects; after the resident node restarts it is back within
// a couple of seconds (#224). A real resident node in a fresh HOME; hub connections reach it through
// `personad node --stdio`, as the hub's ssh does; the echo runtime plays the runtime.
//
//   PERSONAD=<personad binary> node integrations/runtime-sdk/e2e/resident.mjs

import { randomUUID } from 'node:crypto'
import { createEchoRuntime } from '../examples/echo-runtime.mjs'
import { checks, startNode, startResident, until } from '../testing/node-harness.mjs'

const personad = process.env.PERSONAD
if (!personad) {
  console.error('set PERSONAD (a personad binary)')
  process.exit(2)
}
const { check, summary } = checks()
let resident = await startResident({ personad })
const removeHome = resident.removeHome // the restarted node below reuses this HOME; remove it once, at the end
let connects = 0
const { runtime, sessions } = createEchoRuntime({
  socketPath: resident.runtimeSocket,
  retryMs: 1000,
  answerFirst: 'phone',
  log: (m) => { if (m.includes('connected to the node')) connects++ },
})
const listed = async (hub, sid) => ((await hub.call('sessions.list')).sessions ?? []).some((s) => s.session_id === sid)
let hub
try {
  runtime.start()
  await until('the runtime connected', () => runtime.connected, 10_000)
  check('the runtime connects to the resident node before any hub', runtime.connected && !runtime.hubConnected)
  const sid = 'echo-before-hub'
  sessions.set(sid, { id: sid, cwd: '/tmp', title: 'before the hub', items: [], busy: false, updatedAt: Date.now() })
  runtime.sessionsChanged(0)
  await new Promise((r) => setTimeout(r, 300))

  hub = await startNode({ personad, home: resident.home })
  check('the first hub connection lists it in its first list', await listed(hub, sid))
  await until('told a hub is here', () => runtime.hubConnected, 5000).catch(() => {})
  check('the runtime hears the hub is there', runtime.hubConnected)

  hub.stop() // the hub's connection drops (a deploy, a network blip)
  await until('told the hub left', () => !runtime.hubConnected, 10_000).catch(() => {})
  check('the runtime hears the hub left, and stays connected', !runtime.hubConnected && runtime.connected && connects === 1, `connects ${connects}`)

  hub = await startNode({ personad, home: resident.home }) // the hub reconnects
  check('the next connection lists it at once, with no runtime reconnect', (await listed(hub, sid)) && connects === 1, `connects ${connects}`)
  await hub.call('session.watch', { session_id: sid, since_seq: 0 })
  await hub.call('session.send', { session_id: sid, text: 'after the reconnect', client_msg_id: randomUUID() })
  await until('the echo', () => hub.items(sid).some((i) => i.text === 'echo: after the reconnect'), 10_000).catch(() => {})
  check('a send through the next connection is answered', hub.items(sid).some((i) => i.text === 'echo: after the reconnect'))
  hub.stop()
  hub = undefined

  // The resident node restarts (a new binary): the runtime is back within a couple of seconds, hub or not.
  await resident.stop()
  await until('noticed', () => !runtime.connected, 10_000)
  const home = resident.home
  const down = Date.now()
  resident = await startResident({ personad, home })
  await until('reconnected', () => runtime.connected, 15_000).catch(() => {})
  const took = Date.now() - down
  check('after a resident node restart the runtime is back within 3 s', runtime.connected && took < 3000, `${took} ms (resident start included)`)
} catch (err) {
  check('run', false, err.message)
  console.log('--- resident log ---\n' + resident.log.join('').slice(-2000))
} finally {
  runtime.stop()
  hub?.stop()
  await resident.stop()
  removeHome()
}
process.exit(summary() ? 1 : 0)
