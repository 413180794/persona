// The smallest complete Jarvis runtime (docs/runtimes.md「最小例子」): sessions in memory, every message answered with
// an echo after a short "thinking" stream, and a permission card before each echo when the text says so. Start it
// next to a jarvisd node to see a runtime in the apps without any agent, or copy it as the skeleton of a new runtime:
//
//   node integrations/runtime-sdk/examples/echo-runtime.mjs [--socket <path>]
//
// What a real runtime replaces: `sessions` with its own session store, `reply` with its agent, and the item mapping
// with its own (see integrations/dsh/lib/map.js).

import { randomUUID } from 'node:crypto'
import { basename } from 'node:path'
import { pathToFileURL } from 'node:url'
import { createRuntime, interactionId, light, RpcError } from '../index.js'

/** An echo runtime; options are createRuntime's (socketPath, answerFirst…). */
export function createEchoRuntime(options = {}) {
  const sessions = new Map() // id -> { id, cwd, title, items: [], busy, updatedAt }
  let rt

  const now = () => new Date().toISOString()
  const toSeq = (s) => s.items.length // seq = position: item n has seq n (1-based)

  function add(s, item) {
    const seq = s.items.length + 1
    const it = { id: `${seq}`, seq, ts: now(), ...item }
    s.items.push(it)
    s.updatedAt = Date.now()
    rt.append(s.id, { items: [light(it)], fromSeq: seq - 1, toSeq: seq, history: `echo:${s.id}` })
    return it
  }

  async function reply(s, text, clientMsgId) {
    s.busy = true
    rt.sessionsChanged(0)
    add(s, { role: 'user', kind: 'text', text })
    const answer = `echo: ${text}`
    if (/\bask\b/i.test(text)) {
      const res = await rt.ask({ id: interactionId('echo', s.id, 'permission', clientMsgId ?? randomUUID()), kind: 'permission',
        session_id: s.id, tool_name: 'Echo', tool_input: { text }, summary: `echo ${text}` })
      if (res.allow === false) {
        add(s, { role: 'system', kind: 'notice', text: '已拒绝' })
        s.busy = false
        rt.sessionsChanged(0)
        return
      }
    }
    for (let i = 1; i <= answer.length; i += 4) {
      rt.stream(s.id, answer.slice(0, i))
      await new Promise((r) => setTimeout(r, 20))
    }
    rt.streamEnd(s.id)
    add(s, { role: 'assistant', kind: 'text', text: answer })
    s.busy = false
    rt.sessionsChanged(0)
  }

  function page(s, params) {
    const all = s.items
    const history = `echo:${s.id}`
    if (typeof params.through_seq === 'number' && params.through_seq > 0) {
      return { items: all.filter((i) => i.seq >= (params.from_seq ?? 1) && i.seq <= params.through_seq).map(light), more: false }
    }
    if (params.after_seq !== undefined && params.after_seq !== null) {
      const after = all.filter((i) => i.seq > params.after_seq)
      return { items: after.map(light), has_more: false, more_after: false, history, from_seq: params.after_seq, to_seq: Math.max(toSeq(s), params.after_seq) }
    }
    const before = params.before_seq > 0 ? all.filter((i) => i.seq < params.before_seq) : all
    const limit = params.limit > 0 ? params.limit : 80
    const out = before.slice(-limit)
    return { items: out.map(light), has_more: out.length < before.length, history, to_seq: params.before_seq > 0 ? out.at(-1)?.seq ?? 0 : toSeq(s) }
  }

  const get = (id) => {
    const s = sessions.get(id)
    if (!s) throw new RpcError(`no session ${id}`, 'not_found')
    return s
  }

  rt = createRuntime({
    async listSessions() {
      return [...sessions.values()].map((s) => ({
        session_id: s.id, title: s.title, cwd: s.cwd, status: s.busy ? 'working' : 'done',
        updated_at: new Date(s.updatedAt).toISOString(), rev: toSeq(s), space_id: s.cwd, space_name: basename(s.cwd),
        last_text: s.items.findLast((i) => i.role === 'assistant')?.text ?? '',
      }))
    },
    async items(params) {
      return page(get(params.session_id), params)
    },
    async cursor(id) {
      return toSeq(get(id))
    },
    async item(params) {
      const it = get(params.session_id).items.find((i) => i.id === params.item_id)
      if (!it) throw new RpcError(`no item ${params.item_id}`, 'not_found')
      return it
    },
    async send(params) {
      const s = get(params.session_id)
      void reply(s, params.text ?? '', params.client_msg_id)
    },
    async interrupt(params) {
      get(params.session_id).busy = false
      rt.sessionsChanged(0)
    },
    async start(params) {
      const id = `echo-${randomUUID().slice(0, 8)}`
      const s = { id, cwd: params.cwd, title: basename(params.cwd) || 'echo', items: [], busy: false, updatedAt: Date.now() }
      sessions.set(id, s)
      if (params.prompt) void reply(s, params.prompt)
      return { session: { session_id: id, title: s.title, cwd: s.cwd, status: 'idle', updated_at: now() } }
    },
    async rename(params) {
      get(params.session_id).title = params.title
    },
  }, { runtime: 'echo', name: 'Echo', version: '1', launcher: 'echo', launcherModel: 'echo', ...options })

  return { runtime: rt, sessions }
}

// Run it: node examples/echo-runtime.mjs [--socket <path>]
if (import.meta.url === pathToFileURL(process.argv[1] ?? '').href) {
  const i = process.argv.indexOf('--socket')
  const { runtime } = createEchoRuntime({ answerFirst: 'phone', log: (m) => console.error(m), ...(i > 0 ? { socketPath: process.argv[i + 1] } : {}) })
  runtime.start()
  console.error('echo runtime: waiting for the jarvisd node; Ctrl-C to stop')
}
