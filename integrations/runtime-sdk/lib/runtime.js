// The runtime side of Jarvis's runtime protocol (docs/runtimes.md, wire in docs/protocol.md「Runtime ↔ node」), shared
// by every runtime: connecting to the node's runtime.sock and reconnecting, runtime.hello, pushing the session list,
// watching sessions with continuous seq batches, throttled live text, asking the phone (surviving a node restart) and
// the "who answers first" policy. A runtime supplies a driver with what only it knows: its sessions, their pages, and
// how to send to them; it calls back append / stream / ask / sessionsChanged as things happen.

import { existsSync, statSync } from 'node:fs'
import { createConnection } from 'node:net'
import { homedir } from 'node:os'
import { join } from 'node:path'
import { Peer, RpcError } from './wire.js'

/** Version of the protocol this SDK speaks (runtime.hello answers the node's). */
export const PROTOCOL = 1

const PHONE_DRIVEN_FOR = 6 * 60 * 60 * 1000 // a session the phone sent to or started recently: it answers first
const RESUME_WAIT = 2 * 60 * 1000 // how long a pending ask waits for the next node (jarvisd node.ResumeWait)
const APPEND_WINDOW = 80 // ms: committed content within this window goes out as one batch
const STREAM_EVERY = 120 // ms: live text at most this often (the whole text so far each time)
const RETRY_PRESENT = 1000 // ms: retry this often while the socket file is there (a node is, or is restarting)
const TOUCHED = 50 // the sessions the phone sent to or started last, listed whatever a runtime's limits say
const RETRY_MAX = 30_000 // ms: the longest wait while there is no socket file (no node on this machine yet)
const NEW_NODE_EVERY = 5000 // ms: after an older node, look this often for a new one (the socket file replaced)
const NEW_NODE_MAX = 10 * 60_000 // ms: …and connect again after this long anyway

/** The node's runtime socket for the user this process runs as. */
export function defaultSocketPath() {
  return process.env.JARVIS_RUNTIME_SOCKET || join(homedir(), '.jarvis', 'runtime.sock')
}

/**
 * @typedef {object} Driver What a runtime implements. Only listSessions and items are required; each optional
 *   method that is present turns on the matching capability (caps in runtime.hello).
 * @property {() => Promise<object[]>} listSessions - its sessions as Session rows (session_id, title, cwd, status,
 *   updated_at, rev, last_text, space_*…; docs/protocol.md「数据类型」).
 * @property {(params: object) => Promise<object>} items - one session.items page (before_seq / turns / limit /
 *   after_seq / from_seq + through_seq → items, has_more, history, to_seq, from_seq, more_after, more).
 * @property {(sessionId: string) => Promise<number>} [cursor] - the session's current to_seq (where a watch from now
 *   starts); default: the to_seq of a one-turn page.
 * @property {(params: object) => Promise<object>} [image] - session.image → {mime, data}.
 * @property {(params: object) => Promise<object>} [item] - session.item → the whole item.
 * @property {(params: {session_id: string, text: string, images?: object[], client_msg_id?: string}) => Promise<void>} [send]
 * @property {(params: {session_id: string}) => Promise<void>} [interrupt]
 * @property {(params: {cwd: string, prompt?: string}) => Promise<{session: object}>} [start]
 * @property {(params: {session_id: string, title: string}) => Promise<void>} [rename]
 * @property {(params: {session_id: string}) => Promise<void>} [close]
 * @property {() => boolean} [hasLocalClient] - the runtime's own UI is open (the policy gives it first turn).
 */

/**
 * @param {Driver} driver
 * @param {{ runtime: string, name?: string, agent?: string, version?: string, launcher?: string, launcherModel?: string,
 *   socketPath?: string, answerFirst?: 'auto'|'phone'|'local', log?: (msg: string) => void,
 *   connect?: (path: string) => import('node:net').Socket, pushDelayMs?: number, retryMs?: number,
 *   refreshMs?: number, requires?: string[] }} options - requires: node features (runtime.hello's features) this
 *   runtime cannot do without; on a node lacking one it does not stay connected, and tries again once the node
 *   restarts (its socket file is replaced) or after 10 minutes.
 */
export function createRuntime(driver, options) {
  if (!options?.runtime) throw new Error('createRuntime: options.runtime (the runtime id) is required')
  const socketPath = options.socketPath ?? defaultSocketPath()
  const answerFirst = options.answerFirst ?? 'auto'
  const pushDelay = options.pushDelayMs ?? 300
  const baseRetry = options.retryMs ?? 1000
  const log = options.log ?? (() => {})
  const connect = options.connect ?? ((path) => createConnection(path))

  let peer
  let stopped = false
  let retryTimer
  let retryMs = baseRetry
  let refreshTimer
  let pushTimer
  let pushing = false
  let pushAgain = false
  const listed = new Set() // ids in the last pushed list
  const watched = new Map() // session id -> { last, catchingUp, held: [], batch, timer }
  const streams = new Map() // session id -> { text, timer, sentAt }
  const pending = new Map() // session id -> interactions waiting on the phone
  const phoneDriven = new Map() // session id -> when the phone last sent to it or started it (oldest first)
  const sends = new Map() // client_msg_id -> the send's promise: a retry gets the first one's answer (oldest first)
  const drivenBy = (id) => {
    phoneDriven.delete(id)
    phoneDriven.set(id, Date.now())
    while (phoneDriven.size > 200) phoneDriven.delete(phoneDriven.keys().next().value)
  }
  // Whether the node has a hub connected (runtime.hello's hub, then runtime.hub). Older nodes do not say: assume so.
  let hubConnected = true
  // What the node does beyond the first protocol (runtime.hello's features; empty from older nodes).
  let nodeFeatures = []
  // Features this runtime cannot do without (options.requires): it does not stay on a node lacking one.
  const requires = Array.isArray(options.requires) ? options.requires : []
  let warnedRequires = false

  // ---- connection ----

  // After an older node: try again when the socket file is replaced (a node restarting re-creates it: an update), or
  // after NEW_NODE_MAX anyway — not every few seconds, which would fill the node's log with connects and leaves.
  function awaitNewNode() {
    const id = () => {
      try {
        const st = statSync(socketPath)
        return `${st.ino}:${st.mtimeMs}`
      } catch {
        return ''
      }
    }
    const was = id()
    const since = Date.now()
    const look = () => {
      if (stopped) return
      if (id() !== was || Date.now() - since >= NEW_NODE_MAX) return open()
      retryTimer = setTimeout(look, NEW_NODE_EVERY)
      retryTimer.unref?.()
    }
    clearTimeout(retryTimer)
    retryTimer = setTimeout(look, NEW_NODE_EVERY)
    retryTimer.unref?.()
  }

  function scheduleConnect(delay) {
    if (stopped) return
    clearTimeout(retryTimer)
    retryTimer = setTimeout(open, delay)
    retryTimer.unref?.()
  }

  // The wait before the next try. A socket file means a node is there or coming back (a resident node keeps it while
  // it restarts): try again soon. No file: no node on this machine yet, back off.
  function backoff() {
    if (socketPresent()) {
      retryMs = baseRetry
      return Math.min(baseRetry, RETRY_PRESENT)
    }
    const d = retryMs
    retryMs = Math.min(retryMs * 2, RETRY_MAX)
    return d
  }

  function socketPresent() {
    try {
      return existsSync(socketPath)
    } catch {
      return false
    }
  }

  function hello() {
    const caps = {
      start: typeof driver.start === 'function',
      send: typeof driver.send === 'function',
      interrupt: typeof driver.interrupt === 'function',
      rename: typeof driver.rename === 'function',
      close: typeof driver.close === 'function',
    }
    const h = { runtime: options.runtime, name: options.name ?? options.runtime, version: options.version ?? '', caps }
    if (options.agent) h.agent = options.agent
    if (options.launcher && caps.start) {
      h.launcher = options.launcher
      if (options.launcherModel) h.launcher_model = options.launcherModel
    }
    return h
  }

  function open() {
    if (stopped) return
    let socket
    try {
      socket = connect(socketPath)
    } catch {
      scheduleConnect(backoff())
      return
    }
    let heldOff = false // left an older node on purpose: awaitNewNode decides when to come back
    socket.once('error', () => {}) // handled by close
    socket.once('connect', async () => {
      const p = new Peer(socket, { prefix: 'd', onRequest: (m, params) => request(m, params), onEvent: (event, params) => {
        if (event === 'runtime.hub' && peer === p) setHub(params?.connected !== false)
      } })
      for (const w of watched.values()) clearTimeout(w.timer)
      watched.clear() // a new node asks again for what the phone watches
      listed.clear()
      peer = p // bound before hello: the node may call back the moment it has answered
      try {
        const res = await p.call('runtime.hello', hello(), { timeoutMs: 10_000 })
        setHub(res?.hub !== false)
        nodeFeatures = Array.isArray(res?.features) ? res.features.filter((f) => typeof f === 'string') : []
        const missing = requires.filter((f) => !nodeFeatures.includes(f))
        if (missing.length > 0) {
          // An older node: stay off it, and look again now and then (an updated node restarts and is found).
          if (!warnedRequires) log(`jarvis: this jarvisd node lacks ${missing.join(', ')}; update jarvisd (connecting again once it restarts)`)
          warnedRequires = true
          if (peer === p) peer = undefined
          heldOff = true
          p.close(new Error('node too old'))
          awaitNewNode()
          return
        }
        warnedRequires = false
      } catch (err) {
        log(`jarvis: hello failed: ${err.message}`)
        if (peer === p) peer = undefined
        p.close(err)
        scheduleConnect(backoff())
        return
      }
      retryMs = baseRetry
      log('jarvis: connected to the node')
      schedulePush(0)
      const why = await p.done
      if (peer === p) peer = undefined
      log(`jarvis: node connection closed${why?.message ? `: ${why.message}` : ''}`)
      scheduleConnect(baseRetry)
    })
    socket.once('close', () => {
      if (heldOff) return
      if (!peer || peer.socket !== socket) scheduleConnect(backoff())
    })
  }

  // ---- session list ----

  function schedulePush(delay = pushDelay) {
    if (!peer) return
    clearTimeout(pushTimer)
    pushTimer = setTimeout(pushSessions, delay)
    pushTimer.unref?.()
  }

  async function pushSessions() {
    if (!peer) return
    if (pushing) {
      pushAgain = true
      return
    }
    pushing = true
    try {
      const rows = await driver.listSessions()
      const list = []
      for (const row of rows ?? []) {
        if (!row?.session_id) continue
        // A session with a card on the phone needs you, whatever the runtime says.
        list.push((pending.get(row.session_id) ?? 0) > 0 ? { ...row, status: 'blocked' } : row)
      }
      listed.clear()
      for (const s of list) listed.add(s.session_id)
      peer?.notify('runtime.sessions', { sessions: list })
    } catch (err) {
      log(`jarvis: list sessions: ${err.message}`)
    } finally {
      pushing = false
      if (pushAgain) {
        pushAgain = false
        schedulePush()
      }
    }
  }

  // ---- watching ----

  async function cursorOf(sessionId) {
    if (typeof driver.cursor === 'function') return driver.cursor(sessionId)
    const page = await driver.items({ session_id: sessionId, turns: 1 })
    return page?.to_seq ?? 0
  }

  async function watch(params) {
    const id = params.session_id
    const since = typeof params.since_seq === 'number' ? params.since_seq : 0
    const prev = watched.get(id)
    if (prev) clearTimeout(prev.timer)
    const w = { last: since, catchingUp: true, held: [], batch: undefined, timer: undefined }
    watched.set(id, w)
    try {
      if (since > 0) {
        // Catch up after since, then the live batches go on from where it ends.
        let after = since
        for (let i = 0; i < 8; i++) {
          const page = await driver.items({ session_id: id, after_seq: after })
          if (watched.get(id) !== w) return { ok: true }
          if ((page.items?.length ?? 0) > 0 || page.to_seq > w.last) {
            peer?.notify('session.items', { session_id: id, items: page.items ?? [], history: page.history, from_seq: page.from_seq ?? after, to_seq: page.to_seq })
            w.last = page.to_seq
          }
          if (!page.more_after) break
          after = page.to_seq
        }
      } else {
        w.last = await cursorOf(id)
      }
    } catch (err) {
      log(`jarvis: catch up ${id}: ${err.message}`)
    } finally {
      w.catchingUp = false
      const held = w.held.filter((a) => a.toSeq > w.last)
      w.held = []
      for (const a of held) queue(id, w, a)
    }
    return { ok: true }
  }

  function unwatch(params) {
    const w = watched.get(params.session_id)
    if (w) clearTimeout(w.timer)
    watched.delete(params.session_id)
    return { ok: true }
  }

  /**
   * New committed content of a session: the items of what was just written (maybe none — content the chat does not
   * show still moves the seq), from fromSeq (exclusive) to toSeq. Only watched sessions are pushed; batches that touch
   * are merged; a batch that does not start where the last one ended says so (from_seq), and the phone fills the gap.
   */
  function append(sessionId, { items = [], fromSeq, toSeq, history } = {}) {
    if (typeof toSeq !== 'number') throw new Error('append needs toSeq')
    const w = watched.get(sessionId)
    if (!w) {
      if (listed.has(sessionId)) schedulePush(2000) // rev moved: the phone pulls sessions it does not watch
      return
    }
    const a = { items, fromSeq: typeof fromSeq === 'number' ? fromSeq : undefined, toSeq, history }
    if (w.catchingUp) {
      w.held.push(a)
      return
    }
    queue(sessionId, w, a)
  }

  function queue(id, w, a) {
    if (a.toSeq <= w.last && w.batch === undefined) return // already pushed (caught up past it)
    const b = w.batch
    if (b && (a.fromSeq === undefined || a.fromSeq === b.toSeq) && a.history === b.history) {
      b.items.push(...a.items)
      b.toSeq = Math.max(b.toSeq, a.toSeq)
    } else {
      if (b) flush(id, w)
      w.batch = { items: [...a.items], fromSeq: a.fromSeq ?? w.last, toSeq: a.toSeq, history: a.history }
    }
    if (!w.timer) {
      w.timer = setTimeout(() => flush(id, w), APPEND_WINDOW)
      w.timer.unref?.()
    }
  }

  function flush(id, w) {
    clearTimeout(w.timer)
    w.timer = undefined
    const b = w.batch
    w.batch = undefined
    if (!b || watched.get(id) !== w || !peer) return
    const msg = { session_id: id, items: b.items, from_seq: b.fromSeq, to_seq: b.toSeq }
    if (b.history !== undefined) msg.history = b.history
    peer.notify('session.items', msg)
    w.last = Math.max(w.last, b.toSeq)
  }

  // ---- live text ----

  /** The text being generated in a session so far (the whole of it each call); sent at most every 120 ms. */
  function stream(sessionId, text) {
    if (!watched.has(sessionId) || !peer) return
    let s = streams.get(sessionId)
    if (!s) {
      s = { text: '', timer: undefined, sentAt: 0 }
      streams.set(sessionId, s)
    }
    s.text = text
    if (s.timer) return
    const wait = Math.max(0, s.sentAt + STREAM_EVERY - Date.now()) // the first text goes out at once
    s.timer = setTimeout(() => {
      s.timer = undefined
      s.sentAt = Date.now()
      if (streams.get(sessionId) === s) peer?.notify('session.delta', { session_id: sessionId, text: s.text })
    }, wait)
    s.timer.unref?.()
  }

  /** The generated text is done (committed as items, or abandoned). */
  function streamEnd(sessionId) {
    const s = streams.get(sessionId)
    if (s) clearTimeout(s.timer)
    streams.delete(sessionId)
    if (watched.has(sessionId)) peer?.notify('session.delta', { session_id: sessionId, text: '' })
  }

  // ---- asking the phone ----

  /**
   * Whether the phone answers this session's approvals and questions first ('auto': the phone drove the session in
   * the last 6 hours, or the runtime's own UI is not open).
   */
  function setHub(connected) {
    if (connected === hubConnected) return
    hubConnected = connected
    log(`jarvis: the node's hub ${connected ? 'is back' : 'is gone'}`)
  }

  function phoneFirst(sessionId) {
    // No hub: nobody can answer on the phone now. The local UI asks (a card already on the phone waits for the hub).
    if (!peer || !hubConnected || !listed.has(sessionId)) return false
    if (answerFirst === 'phone') return true
    if (answerFirst === 'local') return false
    const at = phoneDriven.get(sessionId)
    if (at !== undefined && Date.now() - at < PHONE_DRIVEN_FOR) return true
    return typeof driver.hasLocalClient === 'function' && driver.hasLocalClient() === false
  }

  /**
   * Put a question / plan / permission card on the phone and wait for the answer: the Resolution, or {dismiss: true}
   * when nobody answers there (handed back to the computer, expired, no hub). The session shows as needing you
   * meanwhile. Rejects only when signal aborts (the runtime withdrew it: the card is taken off).
   */
  async function ask(interaction, { signal, timeoutSeconds } = {}) {
    const sessionId = interaction.session_id
    pending.set(sessionId, (pending.get(sessionId) ?? 0) + 1)
    schedulePush(50)
    try {
      const deadline = Date.now() + RESUME_WAIT
      for (;;) {
        if (signal?.aborted) throw signal.reason ?? new Error('aborted')
        const p = peer
        if (!p) {
          if (Date.now() > deadline) return { dismiss: true }
          await sleep(1000, signal)
          continue
        }
        try {
          const params = { interaction }
          if (timeoutSeconds) params.timeout_seconds = timeoutSeconds
          return await p.call('interaction.create', params, { timeoutMs: 0, signal })
        } catch (err) {
          if (signal?.aborted) throw err
          if (err instanceof RpcError && (err.code === 'node_gone' || err.code === 'closed') && Date.now() < deadline) {
            await sleep(1000, signal) // the node restarts: ask the next one with the same id
            continue
          }
          log(`jarvis: ask the phone: ${err.message}`)
          return { dismiss: true }
        }
      }
    } catch (err) {
      if (signal?.aborted && interaction.id) peer?.notify('interaction.cancel', { id: interaction.id })
      throw err
    } finally {
      pending.set(sessionId, Math.max(0, (pending.get(sessionId) ?? 1) - 1))
      schedulePush(50)
    }
  }

  // ---- node → runtime calls ----

  async function request(method, params) {
    const need = (fn, what) => {
      if (typeof fn !== 'function') throw new RpcError(`unsupported: ${what}`, 'unsupported')
      return fn
    }
    switch (method) {
      case 'session.items':
        return driver.items(params)
      case 'session.image':
        return need(driver.image, 'image')(params)
      case 'session.item':
        return need(driver.item, 'item')(params)
      case 'session.watch':
        return watch(params)
      case 'session.unwatch':
        return unwatch(params)
      case 'session.send': {
        need(driver.send, 'send')
        drivenBy(params.session_id)
        // Once per client_msg_id: a retry (the app's, the node's after a reconnect) answers as the first did, or waits for
        // it; a failed one may be sent again.
        const key = typeof params.client_msg_id === 'string' && params.client_msg_id ? params.client_msg_id : undefined
        if (key && sends.has(key)) return sends.get(key)
        const p = (async () => {
          await driver.send(params)
          schedulePush() // what the phone sends to is active now: the list may change (a runtime keeps it listed)
          return { ok: true }
        })()
        if (key) {
          sends.set(key, p)
          p.catch(() => sends.delete(key))
          while (sends.size > 500) sends.delete(sends.keys().next().value)
        }
        return p
      }
      case 'session.interrupt':
        await need(driver.interrupt, 'interrupt')(params)
        return { ok: true }
      case 'session.rename':
        await need(driver.rename, 'rename')(params)
        schedulePush()
        return { ok: true }
      case 'session.close':
        await need(driver.close, 'close')(params)
        schedulePush()
        return { ok: true }
      case 'session.start': {
        const res = await need(driver.start, 'start')(params)
        const id = res?.session?.session_id
        if (id) drivenBy(id)
        schedulePush()
        return res
      }
    }
    throw new RpcError(`unknown method ${method}`, 'unsupported')
  }

  return {
    /** Connect (and keep reconnecting) to the node. */
    start() {
      stopped = false
      if (options.refreshMs !== 0) {
        refreshTimer = setInterval(() => schedulePush(), options.refreshMs ?? 30_000)
        refreshTimer.unref?.()
      }
      open()
    },
    /** Disconnect for good. */
    stop() {
      stopped = true
      clearTimeout(retryTimer)
      clearTimeout(pushTimer)
      clearInterval(refreshTimer)
      for (const w of watched.values()) clearTimeout(w.timer)
      for (const s of streams.values()) clearTimeout(s.timer)
      peer?.close(new Error('runtime stopped'))
      peer = undefined
    },
    /** The session list changed (a session appeared, went away, started or ended a turn, was renamed…). */
    sessionsChanged: (delayMs) => schedulePush(delayMs),
    append,
    stream,
    streamEnd,
    ask,
    phoneFirst,
    /** Whether the phone is watching a session (no need to compute what nobody gets). */
    watching: (sessionId) => watched.has(sessionId),
    /**
     * Whether the phone sent to or started session id lately (the latest 50): such a session stays listed past a
     * runtime's age and count limits — a chat open on the phone must stay in the list, or the node no longer knows
     * whose it is.
     */
    touched(id) {
      return [...phoneDriven.keys()].slice(-TOUCHED).includes(id)
    },
    /**
     * Which of a runtime's sessions to list (docs/runtimes.md「列表」): those pinned (running, waiting for an answer) and
     * those the phone touched always; the rest updated within `days`, newest first, until `max` in all. Rows are
     * anything; `id(row)` and `updatedAt(row)` (ms) read them (by default a runtime.sessions row). Newest first.
     * @template T
     * @param {T[]} rows
     * @param {{ days?: number, max?: number, pinned?: (row: T) => boolean, id?: (row: T) => string, updatedAt?: (row: T) => number }} [o]
     * @returns {T[]}
     */
    recent(rows, o = {}) {
      const days = o.days ?? 7
      const max = o.max ?? 80
      const id = o.id ?? ((r) => r.session_id)
      const at = o.updatedAt ?? ((r) => Date.parse(r.updated_at) || 0)
      const pinned = o.pinned ?? (() => false)
      const touched = new Set([...phoneDriven.keys()].slice(-TOUCHED))
      const since = Date.now() - days * 24 * 60 * 60 * 1000
      const sorted = [...rows].sort((a, b) => at(b) - at(a))
      const always = sorted.filter((r) => pinned(r) || touched.has(id(r)))
      const rest = sorted.filter((r) => !pinned(r) && !touched.has(id(r)) && at(r) >= since).slice(0, Math.max(0, max - always.length))
      return [...always, ...rest].sort((a, b) => at(b) - at(a))
    },
    /** Whether a node is connected. */
    get connected() {
      return peer !== undefined
    },
    /**
     * Whether the connected node does this (runtime.hello's features): "prefer_running" — when two runtimes list one
     * session, the one running it answers, so listing sessions only on disk (status offline) is safe.
     */
    nodeHas: (feature) => peer !== undefined && nodeFeatures.includes(feature),
    /** Whether that node has a hub connected (the phone can be asked). */
    get hubConnected() {
      return peer !== undefined && hubConnected
    },
    /** Push the list now (tests). */
    pushNow: pushSessions,
  }
}

function sleep(ms, signal) {
  return new Promise((resolve, reject) => {
    const t = setTimeout(resolve, ms)
    signal?.addEventListener('abort', () => {
      clearTimeout(t)
      reject(signal.reason ?? new Error('aborted'))
    }, { once: true })
  })
}
