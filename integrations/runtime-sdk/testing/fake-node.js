// A fake jarvisd node for a runtime's unit tests: listens on a temp runtime socket, records what the runtime says and
// lets the test call it like the node does. Real end-to-end checks use node-harness.mjs instead.

import { createServer } from 'node:net'
import { mkdtempSync, rmSync } from 'node:fs'
import { join } from 'node:path'
import { Peer } from '../lib/wire.js'

/**
 * @param {{ after?: (fn: () => void) => void }} [t] - a node:test context (cleans up after the test), or nothing.
 * @returns {Promise<{ path: string, ready: Promise<void>, hello: any, peer: Peer, events: {event: string, params: any}[],
 *   last(event: string): any, all(event: string): any[], asked: any[], resolve: (ix: any) => any, close(): void }>}
 */
export async function startFakeNode(t) {
  const dir = mkdtempSync('/tmp/jrt-') // unix socket paths are short on macOS: not under $TMPDIR
  const path = join(dir, 'runtime.sock')
  const n = { path, events: [], hello: undefined, peer: undefined, asked: [], resolve: () => ({ dismiss: true }) }
  let connected
  n.ready = new Promise((r) => { connected = r })
  const server = createServer((socket) => {
    const p = new Peer(socket, {
      prefix: 'b',
      onRequest: async (method, params) => {
        if (method === 'runtime.hello') {
          n.hello = params
          n.peer = p
          setImmediate(connected)
          return { protocol: 1, node_version: 'fake' }
        }
        if (method === 'interaction.create') {
          n.asked.push(params.interaction)
          return n.resolve(params.interaction)
        }
        throw new Error(`unexpected ${method}`)
      },
      onEvent: (event, params) => n.events.push({ event, params }),
    })
  })
  await new Promise((r) => server.listen(path, r))
  n.last = (event) => n.events.filter((e) => e.event === event).at(-1)?.params
  n.all = (event) => n.events.filter((e) => e.event === event).map((e) => e.params)
  n.close = () => {
    server.close()
    rmSync(dir, { recursive: true, force: true })
  }
  t?.after?.(n.close)
  return n
}

/** Wait until ok() is truthy (it may be async), or fail after ms. */
export async function until(what, ok, ms = 3000) {
  const end = Date.now() + ms
  for (;;) {
    const v = await ok()
    if (v) return v
    if (Date.now() > end) throw new Error(`timed out waiting for ${what}`)
    await new Promise((r) => setTimeout(r, 10))
  }
}
