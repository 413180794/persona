// DSH's sessions on disk, for the node while no `dsh web` with the Persona plugin runs them (docs/integrations/dsh.md "While DSH is not
// running"): the companion process (companion.mjs) lists the recent ones as offline and pages them, reading DSH's own
// session store without writing anything. A send wakes DSH (a command the operator configures) and is handed over: the
// node delivers it to the plugin once that runs the session (the node's handover feature). The plugin's rows win over
// these while it runs (prefer_running). Everything DSH-specific comes in through `deps`; the tests drive it with fakes.

import { basename, join } from 'node:path'
import { createRuntime, page, RpcError } from '../../runtime-sdk/index.js'
import { eventItems, itemsOf, personaSeq } from './map.js'
import { RUNTIME_VERSION } from './dsh.js'

const DISK_EVERY = 30_000 // ms: list the store at most this often (a list reads every session's header)
const WAKE_AGAIN = 60_000 // ms: a wake started within this long is not started again (DSH is starting)
const LOGS_KEPT = 4 // sessions whose decoded log is kept (a chat open on the phone pages one again and again)

/**
 * @typedef {object} DshFilesDeps
 * @property {{ list(): Promise<readonly object[]>, open(id: string, access: 'read'): Promise<{ read(): Promise<{events: readonly object[]}>, close(): Promise<void> }> }} persistence
 *   DSH's sessionPersistence (dsh-session-persistence-jsonl over $DSH_HOME/sessions), used only to read.
 * @property {(path: string) => any} readJson - a JSON file, undefined when missing or not JSON.
 * @property {(path: string) => number | undefined} mtime - a file's modification time (ms).
 * @property {() => Promise<void>} [wake] - start DSH (the configured command); absent: sends are only handed over.
 * @property {(msg: string) => void} [log]
 * @property {(path: string) => import('node:net').Socket} [connect] - tests.
 */

/**
 * @param {DshFilesDeps} deps
 * @param {{ home: string, runtime?: string, name?: string, socketPath?: string, listDays?: number,
 *   maxSessions?: number, pushDelayMs?: number, retryMs?: number, diskEveryMs?: number }} options - home: DSH_HOME.
 */
export function createDshFiles(deps, options) {
  if (!options?.home) throw new Error('createDshFiles: options.home (DSH_HOME) is required')
  const home = options.home
  const label = options.name ?? 'DSH'
  const log = deps.log ?? (() => {})
  const diskEvery = options.diskEveryMs ?? DISK_EVERY
  let disk = { at: 0, rows: [] } // the store's last list
  const logs = new Map() // session id -> { key, events } (newest last)
  let waking // the running wake
  let wokeAt = 0

  /** The sessions in the store, as list rows before rt.recent; from the last read when it is recent. */
  async function diskRows() {
    if (Date.now() - disk.at < diskEvery) return disk.rows
    const archived = new Set(deps.readJson(join(home, 'storages', 'workspace.json'))?.global?.archivedSessionIds ?? [])
    const rows = []
    for (const snap of await deps.persistence.list()) {
      const h = snap?.header
      if (!h?.id || h.origin === 'subagent' || h.parentSession || archived.has(h.id)) continue
      const path = snap.location?.path
      rows.push({ snap, id: String(h.id), updatedAt: (path && deps.mtime(path)) || h.createdAt || 0 })
    }
    disk = { at: Date.now(), rows }
    return rows
  }

  /** What DSH's projection cache knows of a session (title, last reply, last seq), if it is this log's. */
  function cached(header) {
    const doc = deps.readJson(join(home, 'storages', 'session_projcache', 'sessions', `${header.id}.json`))
    const rec = doc?.record
    if (!rec || rec.identity?.createdAt !== header.createdAt || rec.identity?.cwd !== header.cwd) return {}
    const rows = rec.rows ?? {}
    const title = typeof rows.title?.val === 'string' ? rows.title.val : ''
    const turns = rows.turnOutline?.val?.turns
    const lastText = Array.isArray(turns) && typeof turns.at(-1)?.response === 'string' ? turns.at(-1).response : ''
    const seq = typeof rows.turnBoundary?.seq === 'number' ? rows.turnBoundary.seq : undefined
    return { title, lastText, seq }
  }

  async function listSessions() {
    const rows = await diskRows()
    const chosen = rt.recent(rows, { days: options.listDays ?? 7, max: options.maxSessions ?? 80, id: (r) => r.id, updatedAt: (r) => r.updatedAt })
    return chosen.map(({ snap, id, updatedAt }) => {
      const h = snap.header
      const c = cached(h)
      const cwd = typeof h.cwd === 'string' ? h.cwd : ''
      const row = {
        session_id: id,
        title: c.title || (cwd ? basename(cwd) : ''),
        cwd,
        status: 'offline', // on disk: DSH runs it once woken, and its plugin's row takes over
        agent: 'dsh',
        agent_label: label,
        updated_at: new Date(updatedAt || Date.now()).toISOString(),
        rev: c.seq !== undefined ? personaSeq(c.seq) : Math.round(updatedAt),
      }
      if (cwd) {
        row.space_id = cwd
        row.space_name = basename(cwd)
      }
      if (c.lastText) row.last_text = c.lastText
      return row
    })
  }

  /** A session's events from its log, read once per change of the file. */
  async function eventsOf(id) {
    const row = disk.rows.find((r) => r.id === id)
    const path = row?.snap.location?.path
    const key = `${path ?? ''}:${path ? deps.mtime(path) ?? '' : ''}`
    const hit = logs.get(id)
    if (hit && row && hit.key === key) {
      logs.delete(id)
      logs.set(id, hit)
      return hit.events
    }
    let handle
    try {
      handle = await deps.persistence.open(id, 'read')
    } catch {
      throw new RpcError(`DSH 里没有会话 ${id}`, 'not_found') // not in the store (or not readable): not one of these
    }
    try {
      const { events } = await handle.read()
      logs.delete(id)
      logs.set(id, { key: row ? key : '', events })
      while (logs.size > LOGS_KEPT) logs.delete(logs.keys().next().value)
      return events
    } finally {
      await handle.close()
    }
  }

  async function items(params) {
    const id = params.session_id
    const events = await eventsOf(id)
    const last = events.at(-1)
    return page(itemsOf(events), params, { history: `dsh:${id}`, toSeq: last ? personaSeq(last.seq) : 0 })
  }

  async function item(params) {
    const seq = Number(String(params.item_id).split('#')[0]) - 1
    const event = (await eventsOf(params.session_id)).find((e) => e.seq === seq)
    const it = event ? eventItems(event, { full: true }).find((x) => x.id === params.item_id) : undefined
    if (!it) throw new RpcError(`no item ${params.item_id}`, 'not_found')
    return it
  }

  /** Start DSH once (a wake within the last minute is still starting it). */
  function wake() {
    if (typeof deps.wake !== 'function') return Promise.resolve()
    if (waking) return waking
    if (Date.now() - wokeAt < WAKE_AGAIN) return Promise.resolve()
    wokeAt = Date.now()
    waking = deps.wake().catch((err) => log(`persona: waking DSH: ${err.message}`)).finally(() => { waking = undefined })
    return waking
  }

  // A send: DSH takes the session, not this process (it only reads). Wake DSH and hand the message over: the node
  // gives it to the plugin as soon as that lists the session as running, or answers for it.
  async function send(params) {
    await wake()
    disk.at = 0 // the next list reads the store again
    throw new RpcError(`${label} 正在接手这个会话`, 'handover', { delivered: 'none' })
  }

  const rt = createRuntime({ listSessions, items, item, send }, {
    runtime: options.runtime ?? 'dsh-files',
    name: label,
    agent: 'dsh',
    version: RUNTIME_VERSION,
    socketPath: options.socketPath,
    requires: ['prefer_running', 'handover'],
    log,
    connect: deps.connect,
    pushDelayMs: options.pushDelayMs,
    retryMs: options.retryMs,
    refreshMs: diskEvery,
  })
  return rt
}
