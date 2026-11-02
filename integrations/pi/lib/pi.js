// The pi runtime (docs/pi.md): the sessions of one pi process — a pi in a terminal, `pi --mode rpc`, or pi-web's
// server, which hosts many — in Jarvis through the runtime SDK (docs/runtimes.md). pi loads the extension once per
// session, but there is one runtime connection per process: index.js registers each session's API with the host made
// here, once per process.
//
// The same host, made with listDisk and no sessions of its own, is the pi-web companion (companion.mjs): pi-web loads
// extensions only when a session opens, so a small resident process lists the recent sessions on disk and starts or
// wakes them through pi-web's HTTP API, while the extension inside pi-web serves the ones running there. When both list
// a session the node takes the one running it. Everything pi-specific comes in through `deps`, so the tests drive it
// with fakes.

import { basename } from 'node:path'
import { readFileSync, statSync } from 'node:fs'
import { createRuntime, interactionId, page, RpcError, snippet, summaryOf } from '../../runtime-sdk/index.js'
import { branchItems, branchKey, entryItems, firstText, HOST_ENTRY, imageOf, lastHost, lastText, partialText, toolInput, toolName } from './map.js'

export const RUNTIME_VERSION = '0.2.0'
const DISK_EVERY = 30_000 // ms: list the sessions on disk at most this often (pi-web)
const TERMINAL_HOSTS = new Set(['herdr', 'terminal']) // resumed in the machine's terminal engine, not in pi-web

/**
 * @typedef {object} PiDeps
 * @property {{ listAll(): Promise<object[]>, open(path: string): object }} SessionManager - pi's (sessions on disk).
 * @property {{ port: number, host: string, password?: string } | undefined} web - pi-web's HTTP API (inside its
 *   server, or the companion's).
 * @property {(url: string, init: object) => Promise<Response>} [fetch]
 * @property {(msg: string) => void} [log]
 * @property {(path: string) => import('node:net').Socket} [connect] - tests.
 * @property {number} [pid]
 * @property {string} [paneId] - the terminal pane this pi runs in (HERDR_PANE_ID of a pi in herdr; not pi-web's).
 */

/**
 * @param {PiDeps} deps
 * @param {{ socketPath?: string, answerFirst?: 'auto'|'phone'|'local', approve?: string[], listDays?: number,
 *   maxSessions?: number, retryMs?: number, pushDelayMs?: number, listDisk?: boolean, runtime?: string }} [options]
 *   listDisk: list the recent sessions on disk too, and start new ones (the companion; needs web).
 */
export function createPiHost(deps, options = {}) {
  const web = deps.web
  const listDisk = options.listDisk === true && web !== undefined
  const log = deps.log ?? (() => {})
  const doFetch = deps.fetch ?? globalThis.fetch
  const listDays = options.listDays ?? 7
  const maxSessions = options.maxSessions ?? 80
  const approve = new Set(options.approve ?? [])
  const live = new Map() // session id -> { id, pi, ctx, running, pushed, key }
  let disk = { at: 0, rows: [], paths: new Map() }
  const hosts = new Map() // session file -> { mtime, host }: where each session on disk last ran
  let started = false

  const rt = createRuntime({
    listSessions,
    items,
    cursor: async (id) => view(id).then((v) => v.toSeq),
    image,
    item,
    send,
    interrupt,
    ...(listDisk ? { start } : {}),
    rename,
    hasLocalClient: () => true, // a terminal, or pi-web's browser tabs (they cannot be counted from here)
  }, {
    runtime: options.runtime ?? (listDisk ? 'pi-files' : web ? 'pi-web' : `pi-${deps.pid ?? process.pid}`),
    name: web ? 'Pi Web' : 'pi',
    agent: 'pi',
    version: RUNTIME_VERSION,
    ...(listDisk ? { launcher: 'pi-web', launcherModel: 'pi' } : {}),
    socketPath: options.socketPath,
    answerFirst: options.answerFirst ?? 'auto',
    log,
    connect: deps.connect,
    retryMs: options.retryMs,
    pushDelayMs: options.pushDelayMs,
    // Listing sessions only on disk is safe only where the runtime running a session answers for it.
    ...(listDisk ? { requires: ['prefer_running'] } : {}),
  })

  // ---- the sessions ----

  /** A session's entries, branch and seq bookkeeping: the live one's, or from its file (pi-web). */
  async function view(id) {
    const s = live.get(id)
    let sm = s?.ctx?.sessionManager
    if (!sm) {
      const path = await diskPath(id)
      if (!path) throw new RpcError(`这个 pi 会话不在这里（${id}）`, 'not_found')
      sm = deps.SessionManager.open(path)
    }
    const entries = sm.getEntries()
    const branch = sm.getBranch()
    return { sm, entries, branch, toSeq: entries.length, history: `pi:${id}:${branchKey(entries, branch)}` }
  }

  async function diskPath(id) {
    if (!listDisk) return undefined
    if (!disk.paths.has(id)) await refreshDisk(true)
    return disk.paths.get(id)
  }

  async function refreshDisk(force = false) {
    if (!listDisk || (!force && Date.now() - disk.at < DISK_EVERY)) return
    try {
      const all = await deps.SessionManager.listAll()
      disk = { at: Date.now(), rows: all, paths: new Map(all.map((s) => [s.id, s.path])) }
    } catch (err) {
      log(`jarvis: list pi sessions: ${err.message}`)
    }
  }

  function liveRow(s) {
    const sm = s.ctx.sessionManager
    const branch = sm.getBranch()
    const cwd = sm.getCwd()
    const name = sm.getSessionName?.() ?? ''
    const row = {
      session_id: s.id,
      title: name || firstText(branch) || basename(cwd),
      cwd,
      status: s.running ? 'working' : branch.some((e) => e.message?.role === 'assistant') ? 'done' : 'idle',
      updated_at: new Date(s.updatedAt ?? Date.now()).toISOString(),
      rev: sm.getEntries().length,
      space_id: cwd,
      space_name: basename(cwd),
    }
    const lt = lastText(branch)
    if (lt) row.last_text = lt
    const model = s.ctx.model?.id
    if (model) row.model = model
    if (deps.paneId && s.ctx.mode === 'tui') row.pane_id = deps.paneId // the node joins it with herdr's pane
    return row
  }

  /** Where the session in file last ran (its last host entry), read once per change of the file. */
  function hostOfFile(path) {
    try {
      const mtime = statSync(path).mtimeMs
      const c = hosts.get(path)
      if (c?.mtime === mtime) return c.host
      let host
      for (const line of readFileSync(path, 'utf8').split('\n')) {
        if (!line.includes(`"${HOST_ENTRY}"`)) continue
        try {
          const e = JSON.parse(line)
          if (e.type === 'custom' && e.customType === HOST_ENTRY && typeof e.data?.host === 'string') host = e.data.host
        } catch {}
      }
      hosts.set(path, { mtime, host })
      if (hosts.size > 500) hosts.delete(hosts.keys().next().value)
      return host
    } catch {
      return undefined
    }
  }

  async function listSessions() {
    const rows = [...live.values()].filter((s) => s.ctx).map(liveRow)
    if (listDisk) {
      await refreshDisk()
      // The running ones always; on disk, those the phone touched and the recent ones (the SDK's rule).
      const off = rt.recent(disk.rows.filter((d) => !live.has(d.id)), { days: listDays, max: Math.max(0, maxSessions - rows.length),
        id: (d) => d.id, updatedAt: (d) => new Date(d.modified).getTime() })
      for (const d of off) {
        const host = hostOfFile(d.path)
        const resume = TERMINAL_HOSTS.has(host) && rt.nodeHas('resume_argv')
        rows.push({
          session_id: d.id,
          title: d.name || snippet(d.firstMessage ?? '') || basename(d.cwd || ''),
          cwd: d.cwd ?? '',
          status: 'offline', // on disk: pi-web opens it when the phone sends to it
          updated_at: new Date(d.modified).toISOString(),
          rev: new Date(d.modified).getTime(),
          space_id: d.cwd ?? '',
          space_name: basename(d.cwd || ''),
          // It ran in a terminal (herdr): the node resumes it there (`pi --session`), not here in pi-web.
          ...(resume ? { resume_argv: ['pi', '--session', d.path] } : {}),
        })
      }
    }
    return rows
  }

  async function items(params) {
    const v = await view(params.session_id)
    return page(branchItems(v.entries, v.branch), params, { history: v.history, toSeq: v.toSeq })
  }

  function splitItemID(itemID) {
    const [seqText, idxText] = String(itemID).split('#')
    const seq = Number(seqText)
    const idx = Number(idxText)
    if (!Number.isSafeInteger(seq) || !Number.isSafeInteger(idx) || seq < 1) throw new RpcError(`bad item id ${itemID}`, 'bad_request')
    return { seq, idx }
  }

  async function image(params) {
    const { seq, idx } = splitItemID(params.item_id)
    const v = await view(params.session_id)
    const b = imageOf(v.entries[seq - 1], idx)
    if (!b) throw new RpcError(`no image ${params.item_id}`, 'not_found')
    return { mime: b.mimeType ?? 'image/png', data: b.data }
  }

  async function item(params) {
    const { seq } = splitItemID(params.item_id)
    const v = await view(params.session_id)
    const it = entryItems(v.entries[seq - 1], seq).find((x) => x.id === params.item_id)
    if (!it) throw new RpcError(`no item ${params.item_id}`, 'not_found')
    return it
  }

  function content(params) {
    const images = (params.images ?? []).filter((i) => i?.data).map((i) => ({ type: 'image', data: i.data, mimeType: i.mime || 'image/jpeg' }))
    const text = typeof params.text === 'string' ? params.text : ''
    if (!text.trim() && images.length === 0) throw new RpcError('消息是空的', 'bad_request')
    return { text, images }
  }

  async function send(params) {
    const id = params.session_id
    const { text, images } = content(params) // once per client_msg_id: the SDK's
    const s = live.get(id)
    if (s) {
      const body = images.length ? [...(text ? [{ type: 'text', text }] : []), ...images] : text
      // Into the running turn while it works (pi's steer), a new turn when idle.
      await s.pi.sendUserMessage(body, s.running ? { deliverAs: 'steer' } : undefined)
      return
    }
    const path = listDisk ? await diskPath(id) : undefined
    if (!path) throw new RpcError('这个 pi 会话没有在这里运行，这次没有发送', 'not_running')
    if (TERMINAL_HOSTS.has(hostOfFile(path))) {
      // A session that ran in a terminal goes back to one (the node resumes it in herdr); this node cannot.
      throw new RpcError('这个 pi 会话是在终端（herdr）里跑的，要在 herdr 里接着它：更新这台电脑上的 jarvisd 后再发。这次没有发送', 'not_running')
    }
    // On disk: pi-web opens it and prompts (its extension load registers it here as it starts).
    await webCall(`/api/agent/${encodeURIComponent(id)}`, { type: 'prompt', message: text, ...(images.length ? { images } : {}) })
  }

  async function interrupt(params) {
    const s = live.get(params.session_id)
    if (s?.running) s.ctx.abort()
  }

  async function rename(params) {
    const s = live.get(params.session_id)
    if (!s) throw new RpcError('这个 pi 会话没有在运行，改不了名', 'not_running')
    s.pi.setSessionName(params.title)
  }

  async function start(params) {
    if (!params.cwd) throw new RpcError('要选一个目录', 'bad_request')
    const prompt = typeof params.prompt === 'string' ? params.prompt.trim() : ''
    const res = await webCall('/api/agent/new', prompt ? { cwd: params.cwd, type: 'prompt', message: prompt } : { cwd: params.cwd, type: 'ensure_session' })
    const id = res?.sessionId
    if (!id) throw new RpcError('pi-web 没有返回会话', 'start_failed')
    // offline: it runs in pi-web, not here (the extension there lists it as running; the node takes that row).
    return { session: { session_id: id, cwd: params.cwd, title: basename(params.cwd), status: 'offline',
      space_id: params.cwd, space_name: basename(params.cwd), updated_at: new Date().toISOString() } }
  }

  async function webCall(path, body) {
    const headers = { 'content-type': 'application/json' }
    if (web.password) headers.authorization = 'Basic ' + Buffer.from(`pi:${web.password}`).toString('base64')
    const res = await doFetch(`http://${web.host}:${web.port}${path}`, { method: 'POST', headers, body: JSON.stringify(body), signal: AbortSignal.timeout(30_000) })
    const data = await res.json().catch(() => ({}))
    if (!res.ok || data?.success === false || data?.error) {
      throw new RpcError(`pi-web：${data?.error ?? res.status}`, data?.code === 'prompt_rejected' ? 'rejected' : 'pi_web')
    }
    return data
  }

  // ---- what happens in a session ----

  /** Push what a session wrote since the last push (only the phone's watched sessions get the items). */
  function commit(id) {
    const s = live.get(id)
    const sm = s?.ctx?.sessionManager
    if (!sm) return
    const entries = sm.getEntries()
    if (entries.length === s.pushed) return
    const branch = sm.getBranch()
    const key = branchKey(entries, branch)
    const fresh = branchItems(entries, branch).filter((i) => i.seq > s.pushed)
    rt.append(id, { items: rt.watching(id) ? fresh : [], fromSeq: s.key === key ? s.pushed : undefined, toSeq: entries.length, history: `pi:${id}:${key}` })
    s.pushed = entries.length
    s.key = key
    s.updatedAt = Date.now()
  }

  /** pi's API for one session (the extension's factory argument): its events, tools and approvals. */
  function register(pi, toolDeps) {
    let current // this API's session id (a /new in the terminal moves it to another)
    const track = (ctx) => {
      const id = ctx.sessionManager.getSessionId()
      if (current && current !== id) drop(current)
      current = id
      let s = live.get(id)
      if (!s) {
        const entries = ctx.sessionManager.getEntries()
        s = { id, pi, ctx, running: false, pushed: entries.length, key: branchKey(entries, ctx.sessionManager.getBranch()), updatedAt: Date.now() }
        live.set(id, s)
        rt.sessionsChanged()
      }
      s.ctx = ctx
      s.pi = pi
      return s
    }
    const drop = (id) => {
      if (live.get(id)?.pi === pi) live.delete(id)
      rt.streamEnd(id)
      rt.sessionsChanged()
    }
    pi.on('session_start', (_e, ctx) => {
      if (!started) {
        started = true
        rt.start()
      }
      track(ctx)
      stampHost(pi, ctx)
    })
    pi.on('session_shutdown', (_e, ctx) => drop(ctx?.sessionManager?.getSessionId() ?? current))
    pi.on('agent_start', (_e, ctx) => {
      track(ctx).running = true
      rt.sessionsChanged(0)
    })
    pi.on('agent_settled', (_e, ctx) => {
      const s = track(ctx)
      s.running = false
      commit(s.id)
      rt.streamEnd(s.id)
      rt.sessionsChanged(0)
    })
    pi.on('message_update', (e, ctx) => {
      const s = track(ctx)
      if (e?.assistantMessageEvent?.type === 'text_delta') rt.stream(s.id, partialText(e.assistantMessageEvent.partial ?? e.message))
    })
    for (const event of ['message_end', 'tool_execution_end', 'session_compact', 'session_tree']) {
      pi.on(event, (_e, ctx) => {
        const s = track(ctx)
        if (event === 'message_end') rt.streamEnd(s.id)
        commit(s.id)
      })
    }
    pi.on('session_info_changed', (_e, ctx) => {
      track(ctx)
      rt.sessionsChanged()
    })
    if (approve.size > 0) pi.on('tool_call', (e, ctx) => approval(track(ctx), e, ctx))
    if (toolDeps?.Type) pi.registerTool(askUserTool(toolDeps.Type, track))
  }

  /** Records where the session runs now when that changed (HOST_ENTRY): pi-web, herdr, or a terminal outside it. */
  function stampHost(pi, ctx) {
    const host = web ? 'pi-web' : ctx.mode === 'tui' ? (deps.paneId ? 'herdr' : 'terminal') : undefined
    if (!host || typeof pi.appendEntry !== 'function') return // RPC and one-shot runs: some program's, not a place
    if (lastHost(ctx.sessionManager.getEntries()) !== host) pi.appendEntry(HOST_ENTRY, { host })
  }

  /** A tool the user asked to approve (options.approve): the phone or pi's own dialog decides; without either, no. */
  async function approval(s, e, ctx) {
    const name = e.toolName ?? e.name
    if (!approve.has(name)) return undefined
    const input = e.input ?? e.args ?? {}
    const summary = summaryOf(name, input)
    if (rt.phoneFirst(s.id)) {
      const res = await rt.ask({
        id: interactionId('pi', s.id, 'permission', e.toolCallId), kind: 'permission', session_id: s.id,
        tool_name: toolName(name), tool_use_id: e.toolCallId, tool_input: toolInput(name, input), summary,
      }, { signal: ctx.signal })
      if (res?.allow === true) return undefined
      if (res?.allow === false) return { block: true, reason: res.message?.trim() || '用户在手机上拒绝了' }
    }
    if (ctx.hasUI) {
      const ok = await ctx.ui.confirm(`允许 ${name}？`, summary)
      return ok ? undefined : { block: true, reason: '用户拒绝了' }
    }
    return { block: true, reason: '没有人批准（没有界面，手机也没回答）' }
  }

  /** ask_user: the model asks the user; the phone or pi's own dialog answers. */
  function askUserTool(Type, track) {
    return {
      name: 'ask_user',
      label: 'Ask user',
      description: 'Ask the user a question, optionally with options to pick from, and wait for the answer. The user may answer from their phone. Use it when you need a decision or information only the user has.',
      parameters: Type.Object({
        question: Type.String({ description: 'The question, in one or two sentences' }),
        options: Type.Optional(Type.Array(Type.String(), { description: 'Answers to pick from (the user may also write their own)' })),
      }),
      executionMode: 'sequential',
      async execute(toolCallId, params, signal, _onUpdate, ctx) {
        const s = track(ctx)
        const options = (params.options ?? []).filter((o) => typeof o === 'string' && o.trim())
        let answer
        const askPhone = async () => {
          const res = await rt.ask({
            id: interactionId('pi', s.id, 'question', toolCallId), kind: 'question', session_id: s.id,
            tool_name: 'AskUserQuestion', tool_use_id: toolCallId,
            questions: [{ question: params.question, header: '', multi_select: false, options: options.map((label) => ({ label, description: '' })) }],
          }, { signal })
          const a = res?.answers?.[params.question]
          return typeof a === 'string' && a.trim() ? a : undefined
        }
        const askHere = async () => {
          if (!ctx.hasUI) return undefined
          return options.length ? ctx.ui.select(params.question, options) : ctx.ui.input(params.question)
        }
        if (rt.phoneFirst(s.id)) answer = (await askPhone()) ?? (await askHere())
        else answer = (await askHere()) ?? (rt.hubConnected ? await askPhone() : undefined)
        const text = answer ? `The user answered: ${answer}` : 'The user did not answer.'
        return { content: [{ type: 'text', text }], details: { question: params.question, options, answer: answer ?? null } }
      },
    }
  }

  return {
    register,
    /** Connect without a session of its own (the companion). */
    start() {
      if (!started) {
        started = true
        rt.start()
      }
    },
    stop() {
      rt.stop()
      started = false
    },
    runtime: rt,
    live,
  }
}

/**
 * Where pi-web's server listens, read from its process (`next start -p <port> -H <host>`, PORT, PI_WEB_HOSTNAME);
 * undefined when this process is not pi-web's server.
 */
export function piWebOf(argv = process.argv, env = process.env) {
  const isWeb = env.JARVIS_PI_HOST === 'web' || (env.JARVIS_PI_HOST !== 'cli' && argv.some((a) => a.includes('pi-web')) && argv.includes('start'))
  if (!isWeb) return undefined
  const i = argv.indexOf('-p')
  const port = Number(i >= 0 ? argv[i + 1] : env.PORT) || 30141
  let host = env.PI_WEB_HOSTNAME || '127.0.0.1'
  if (host === '0.0.0.0' || host === '::') host = '127.0.0.1'
  return { port, host, password: env.PI_WEB_PASSWORD || undefined }
}
