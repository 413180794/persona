// Jarvis runtime for pi (docs/pi.md): a pi extension that puts the sessions of this pi process — a terminal, an RPC
// host, or pi-web's server — into the Jarvis apps through the jarvisd node on this machine
// (~/.jarvis/runtime.sock, docs/runtimes.md). It does nothing visible until a node listens.
//
// Configuration (optional): <agent dir>/jarvis.json — { "approve": ["bash"], "askUser": true, "answerFirst": "auto" }.

import { readFileSync } from 'node:fs'
import { homedir } from 'node:os'
import { join } from 'node:path'
import { SessionManager } from '@earendil-works/pi-coding-agent'
import { Type } from 'typebox'
import { createPiHost, piWebOf } from './lib/pi.js'

function config() {
  const dir = process.env.PI_CODING_AGENT_DIR || join(homedir(), '.pi', 'agent')
  try {
    return JSON.parse(readFileSync(join(dir, 'jarvis.json'), 'utf8'))
  } catch {
    return {}
  }
}

// One host per process: pi loads this module once and calls the factory once per session.
function host() {
  if (!globalThis.__jarvisPiHost) {
    const c = config()
    const web = piWebOf()
    globalThis.__jarvisPiHost = createPiHost({
      SessionManager,
      web,
      // A pi in a herdr pane: the node shows it as herdr's session (pi-web's server may run in a pane too: not it).
      paneId: web ? undefined : process.env.HERDR_PANE_ID || undefined,
      log: (m) => { if (process.env.JARVIS_PI_DEBUG) console.error(m) },
    }, {
      socketPath: typeof c.socket === 'string' && c.socket ? c.socket : undefined,
      answerFirst: ['auto', 'phone', 'local'].includes(c.answerFirst) ? c.answerFirst : 'auto',
      approve: Array.isArray(c.approve) ? c.approve.filter((t) => typeof t === 'string') : [],
    })
    globalThis.__jarvisPiAskUser = c.askUser !== false
  }
  return globalThis.__jarvisPiHost
}

export default function jarvis(pi) {
  // Not for one-shot runs (pi -p, --mode json): nothing to drive from the phone, outside pi-web's server.
  if (!piWebOf() && process.argv.some((a) => a === '-p' || a === '--print' || a === 'json')) return
  host().register(pi, globalThis.__jarvisPiAskUser ? { Type } : undefined)
}
