// Where the Paseo daemon is: its home (PASEO_HOME, ~/.paseo), the address it listens on now (paseo.pid's listen,
// written once it listens), and its local credential (local-credential, rewritten each time it starts listening; only
// needed when the daemon has a password). The same files Paseo's own CLI reads.

import { readFileSync } from 'node:fs'
import { homedir } from 'node:os'
import { join } from 'node:path'

/** The Paseo home: PASEO_HOME, else ~/.paseo. */
export function paseoHome(env = process.env) {
  const h = env.PASEO_HOME?.trim()
  if (h) return h.startsWith('~/') ? join(homedir(), h.slice(2)) : h
  return join(homedir(), '.paseo')
}

/**
 * Parse a Paseo listen string: "host:port", "[v6]:port", "unix:///path" (or unix:/path). Null for anything else (a
 * Windows pipe: not supported).
 * @returns {{ host: string, port: number } | { socketPath: string } | null}
 */
export function parseListen(listen) {
  const s = String(listen ?? '').trim()
  if (s.startsWith('unix:')) {
    const path = s.replace(/^unix:(\/\/)?/, '')
    return path.startsWith('/') ? { socketPath: path } : null
  }
  const m = /^\[([^\]]+)\]:(\d+)$/.exec(s) ?? /^([^:\s/]+):(\d+)$/.exec(s)
  if (!m) return null
  const port = Number(m[2])
  if (!(port > 0 && port < 65536)) return null
  return { host: m[1], port }
}

/**
 * Where to reach the daemon of a home: the address its paseo.pid says it listens on — Paseo's own CLI reaches a
 * local daemon the same way, never a default port, which may be another home's daemon. No target while there is no
 * paseo.pid (not running) or it has no address yet (starting). pid is the daemon's (supervisor's) process.
 * @returns {{ target?: object, listen?: string, pid?: number, serverId?: string, running: boolean }}
 */
export function daemonTarget(home = paseoHome()) {
  let pidFile
  try {
    pidFile = JSON.parse(readFileSync(join(home, 'paseo.pid'), 'utf8'))
  } catch {}
  const listen = typeof pidFile?.listen === 'string' && pidFile.listen ? pidFile.listen : undefined
  const target = listen ? parseListen(listen) : undefined
  // A wildcard listen is reached on loopback.
  if (target?.host === '0.0.0.0') target.host = '127.0.0.1'
  if (target?.host === '::') target.host = '::1'
  return { target: target ?? undefined, listen, pid: Number(pidFile?.pid) || undefined, serverId: pidFile?.serverId, running: pidFile !== undefined }
}

/** The daemon's local credential (43 base64url characters), or undefined. */
export function localCredential(home = paseoHome()) {
  try {
    const t = readFileSync(join(home, 'local-credential'), 'utf8').trim()
    return /^[A-Za-z0-9_-]{20,200}$/.test(t) ? t : undefined
  } catch {
    return undefined
  }
}
