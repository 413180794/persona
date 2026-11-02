// End-to-end harness for a runtime (docs/runtimes.md「验收」): starts a real `jarvisd node --stdio` in an isolated HOME
// and plays the hub on its stdio, so a check can drive the runtime the way the phone does and watch what reaches the
// hub. The runtime under test connects to `runtimeSocket`. Nothing outside the temp directory is touched.

import { spawn } from 'node:child_process'
import { mkdirSync, mkdtempSync, rmSync } from 'node:fs'
import { join } from 'node:path'
import { Peer } from '../lib/wire.js'

/**
 * @param {{ jarvisd: string, keep?: boolean }} opts
 */
export async function startNode({ jarvisd, keep = false }) {
  const root = mkdtempSync(join('/tmp', 'jrt-e2e-'))
  const home = join(root, 'home')
  mkdirSync(home, { recursive: true })
  const child = spawn(jarvisd, ['node', '--stdio'], { env: { PATH: '/usr/bin:/bin', HOME: home }, stdio: ['pipe', 'pipe', 'pipe'] })
  const log = []
  child.stderr.on('data', (d) => log.push(String(d)))
  const stdio = {
    setEncoding: (e) => child.stdout.setEncoding(e),
    on: (ev, fn) => (ev === 'data' ? child.stdout.on('data', fn) : ev === 'close' ? child.on('exit', () => fn()) : child.on(ev, fn)),
    write: (s) => child.stdin.write(s),
    destroy: () => child.kill(),
  }
  const h = { root, home, runtimeSocket: join(home, '.jarvis', 'runtime.sock'), events: [], asked: [], log }
  h.resolve = () => ({ dismiss: true }) // how the "phone" answers interaction cards
  h.hub = new Peer(stdio, {
    prefix: 'h',
    onEvent: (event, params) => h.events.push({ event, params, at: Date.now() }),
    onRequest: async (method, params) => {
      switch (method) {
        case 'state.load':
          return { sessions: {} }
        case 'interaction.create':
          h.asked.push(params.interaction)
          return h.resolve(params.interaction)
        case 'mcp.tools':
          return { tools: [] }
        default:
          return {}
      }
    },
  })
  h.info = await h.hub.call('node.info', {}, { timeoutMs: 40_000 })
  /** Call the node as the hub does. */
  h.call = (method, params = {}, timeoutMs = 30_000) => h.hub.call(method, params, { timeoutMs })
  /** Items pushed for a session so far, in order. */
  h.items = (sid) => h.events.filter((e) => e.event === 'session.items' && e.params.session_id === sid).flatMap((e) => e.params.items)
  /** The pushed batches of a session. */
  h.batches = (sid) => h.events.filter((e) => e.event === 'session.items' && e.params.session_id === sid).map((e) => e.params)
  /** Batches that do not start where the one before ended (should be none). */
  h.gaps = (sid) => {
    const b = h.batches(sid)
    return b.slice(1).filter((x, i) => x.from_seq !== b[i].to_seq)
  }
  /** A session's status in the last pushed list. */
  h.status = (sid) => h.events.filter((e) => e.event === 'sessions.changed').at(-1)?.params.sessions.find((s) => s.session_id === sid)?.status
  /** The last pushed list. */
  h.sessions = () => h.events.filter((e) => e.event === 'sessions.changed').at(-1)?.params.sessions ?? []
  h.deltas = (sid) => h.events.filter((e) => e.event === 'session.delta' && e.params.session_id === sid).map((e) => e.params.text)
  h.stop = () => {
    try {
      child.kill('SIGTERM')
    } catch {}
    if (!keep) rmSync(root, { recursive: true, force: true })
  }
  return h
}

/** A minimal check reporter: PASS / FAIL lines and a summary; exit code 1 when any failed. */
export function checks() {
  const results = []
  return {
    check(name, ok, detail = '') {
      results.push({ name, ok: !!ok })
      console.log(`${ok ? 'PASS' : 'FAIL'} ${name}${detail ? ` — ${detail}` : ''}`)
      return !!ok
    },
    summary() {
      const failed = results.filter((r) => !r.ok).length
      console.log(`${results.length - failed}/${results.length} checks passed`)
      return failed
    },
  }
}

export { until } from './fake-node.js'
