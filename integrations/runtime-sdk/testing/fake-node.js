// A fake jarvisd node for a runtime's unit tests: listens on a temp runtime socket, records what the runtime says and
// lets the test call it like the node does. Real end-to-end checks use node-harness.mjs instead.

import { createServer } from 'node:net'
import { mkdtempSync, rmSync } from 'node:fs'
import { join } from 'node:path'
import { Peer, RpcError } from '../lib/wire.js'

/**
 * @param {{ after?: (fn: () => void) => void }} [t] - a node:test context (cleans up after the test), or nothing.
 * @param {{ path?: string, hub?: boolean, features?: string[], tools?: object[] }} [opts] - the socket path (default:
 *   a fresh temp one); whether hello says a hub is connected (default true; setHub changes it and tells the runtime);
 *   the node's features; the hub's tools mcp.tools answers (n.tools changes them).
 * @returns {Promise<{ path: string, ready: Promise<void>, hello: any, peer: Peer, events: {event: string, params: any}[],
 *   last(event: string): any, all(event: string): any[], asked: any[], resolve: (ix: any) => any, close(): void }>}
 *   Also: tools / toolCalls / callTool (how mcp.call answers), refuseHello (an RpcError hello answers with, or none).
 */
export async function startFakeNode(t, opts = {}) {
  const dir = opts.path ? undefined : mkdtempSync('/tmp/jrt-') // unix socket paths are short on macOS: not under $TMPDIR
  const path = opts.path ?? join(dir, 'runtime.sock')
  const n = { path, events: [], hello: undefined, peer: undefined, asked: [], resolve: () => ({ dismiss: true }), hub: opts.hub ?? true, hellos: 0,
    tools: opts.tools ?? [], toolCalls: [], callTool: (params) => ({ content: [{ type: 'text', text: `${params.name} ran` }], is_error: false }),
    refuseHello: undefined }
  let connected
  n.ready = new Promise((r) => { connected = r })
  const server = createServer((socket) => {
    const p = new Peer(socket, {
      prefix: 'b',
      onRequest: async (method, params) => {
        if (method === 'runtime.hello') {
          n.hellos++
          if (n.refuseHello) throw n.refuseHello
          n.hello = params
          n.peer = p
          setImmediate(connected)
          return { protocol: 1, node_version: 'fake', hub: n.hub, features: opts.features ?? ['prefer_running', 'resume_argv', 'handover'] }
        }
        if (method === 'interaction.create') {
          n.asked.push(params.interaction)
          return n.resolve(params.interaction)
        }
        if (method === 'mcp.tools') return { tools: n.tools }
        if (method === 'mcp.call') {
          n.toolCalls.push(params)
          return n.callTool(params)
        }
        throw new Error(`unexpected ${method}`)
      },
      onEvent: (event, params) => n.events.push({ event, params }),
    })
  })
  await new Promise((r) => server.listen(path, r))
  n.last = (event) => n.events.filter((e) => e.event === event).at(-1)?.params
  n.all = (event) => n.events.filter((e) => e.event === event).map((e) => e.params)
  /** The hub comes or goes: the runtime is told, as a resident node tells it. */
  n.setHub = (connected) => {
    n.hub = connected
    n.peer?.notify('runtime.hub', { connected })
  }
  /** Answer the next hellos as a node does when another process holds the runtime id (undefined: accept again). */
  n.conflict = (on = true) => {
    n.refuseHello = on ? new RpcError('another process is connected to this node as runtime', 'runtime_conflict') : undefined
  }
  n.close = () => {
    server.close()
    if (dir) rmSync(dir, { recursive: true, force: true })
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
