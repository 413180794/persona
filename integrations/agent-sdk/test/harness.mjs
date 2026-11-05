// spawnBridge runs dist/bridge.mjs for the protocol tests and the real-gateway smoke (scripts/test/agent_sdk_smoke.mjs):
// send writes stdin lines, until waits for a stdout message (from index `from` on), end closes stdin and waits for
// the exit. Lines that are not JSON are skipped.

import { spawn } from 'node:child_process'
import { fileURLToPath } from 'node:url'

export const BRIDGE = fileURLToPath(new URL('../dist/bridge.mjs', import.meta.url))

export function spawnBridge({ env, cwd, timeoutMs = 5000 }) {
  const child = spawn(process.execPath, [BRIDGE], { cwd, env, stdio: ['pipe', 'pipe', 'pipe'] })
  const msgs = []
  let raw = ''
  let stderr = ''
  let waiters = []
  child.stderr.on('data', (d) => { stderr += d })
  child.stdout.on('data', (d) => {
    raw += d
    let i
    while ((i = raw.indexOf('\n')) >= 0) {
      const line = raw.slice(0, i)
      raw = raw.slice(i + 1)
      try { msgs.push(JSON.parse(line)) } catch { continue }
      waiters = waiters.filter((w) => !w())
    }
  })
  const exited = new Promise((resolve) => child.on('exit', (code) => resolve(code)))
  const b = {
    msgs,
    pid: child.pid,
    exited,
    get stderr() { return stderr },
    get stdoutRaw() { return JSON.stringify(msgs) + raw },
    send: (...lines) => { for (const l of lines) child.stdin.write(`${typeof l === 'string' ? l : JSON.stringify(l)}\n`) },
    user: (text) => b.send({ type: 'user', message: { role: 'user', content: text } }),
    control: (id, request) => b.send({ type: 'control_request', request_id: id, request }),
    until: (pred, from = 0) => new Promise((resolve, reject) => {
      const t = setTimeout(() => reject(new Error(`timed out; got ${msgs.slice(-5).map((m) => m.type).join(', ')}`)), timeoutMs)
      const check = () => {
        const m = msgs.slice(from).find(pred)
        if (m) { clearTimeout(t); resolve(m); return true }
        return false
      }
      if (!check()) waiters.push(check)
    }),
    end: () => { child.stdin.end(); return exited },
    kill: (sig = 'SIGTERM') => child.kill(sig),
  }
  return b
}
