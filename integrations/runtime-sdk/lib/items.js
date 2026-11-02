// Helpers every runtime needs to turn its own content into Jarvis items the way the apps expect (docs/runtimes.md
// 「内容」): the page limits and the notes of server/internal/transcript, interaction ids that survive a node
// restart, one-line summaries.

import { createHash } from 'node:crypto'

// Pages and pushes carry at most this much text per item; the whole of a cut item comes from session.item.
export const PAGE_TEXT = 24 * 1024
/** Tool output is cut shorter: the app has a 看全文 button for it. */
export const PAGE_OUTPUT = 4 * 1024
/** Tool inputs are not fetched whole: long strings in them are clipped for good. */
export const MAX_INPUT = 24 * 1024

/** Cut s to at most n bytes on a UTF-8 boundary, noting the original size. */
export function clipTo(s, n, note = '\n…（已截断，共 %d 字节）') {
  const buf = Buffer.from(s, 'utf8')
  if (buf.length <= n) return s
  let end = n
  while (end > 0 && (buf[end] & 0xc0) === 0x80) end--
  return buf.subarray(0, end).toString('utf8') + note.replace('%d', String(buf.length))
}

/**
 * The page / push copy of an item: long text cut with the note the apps read and full_size set (only a tool's output
 * says to tap for the whole). Items already short enough come back unchanged.
 */
export function light(item) {
  if (typeof item?.text !== 'string') return item
  const limit = item.kind === 'tool_result' ? PAGE_OUTPUT : PAGE_TEXT
  const note = item.kind === 'tool_result' ? '\n…（已截断，点开看全文，共 %d 字节）' : '\n…（已截断，共 %d 字节）'
  const buf = Buffer.from(item.text, 'utf8')
  if (buf.length <= limit) return item
  let end = limit
  while (end > 0 && (buf[end] & 0xc0) === 0x80) end--
  let text = buf.subarray(0, end).toString('utf8')
  const nl = text.lastIndexOf('\n')
  if (nl > 0 && Buffer.byteLength(text.slice(nl), 'utf8') < 512) text = text.slice(0, nl) // end on a whole line
  return { ...item, text: text + note.replace('%d', String(buf.length)), full_size: buf.length }
}

/** A tool input object with very long string fields clipped (a Write's content…). */
export function clipInput(input) {
  if (input === null || typeof input !== 'object' || Array.isArray(input)) return input
  if (Buffer.byteLength(JSON.stringify(input), 'utf8') <= MAX_INPUT) return input
  const out = {}
  for (const [k, v] of Object.entries(input)) {
    out[k] = typeof v === 'string' && Buffer.byteLength(v, 'utf8') > MAX_INPUT / 2 ? clipTo(v, MAX_INPUT / 2) : v
  }
  return out
}

/** One line, at most 200 characters (a list row's last_text, a card's summary). */
export function snippet(s) {
  const one = String(s).split(/\s+/).filter(Boolean).join(' ')
  const chars = [...one]
  return chars.length > 200 ? chars.slice(0, 200).join('') + '…' : one
}

/** One line saying what a tool is about to do, for a permission card. */
export function summaryOf(name, input) {
  if (!input || typeof input !== 'object') return name ?? ''
  for (const k of ['command', 'file_path', 'path', 'url', 'pattern', 'query']) {
    if (typeof input[k] === 'string' && input[k]) return snippet(input[k])
  }
  return name ?? ''
}

/**
 * A stable interaction id: the same tool call asks with the same id, so after a node restart the hub hangs the
 * repeated ask on the card the phone already shows.
 */
export function interactionId(runtime, sessionId, kind, key) {
  return `${runtime}-` + createHash('sha256').update(`${sessionId}\0${kind}\0${key}`).digest('hex').slice(0, 16)
}

// The tools the apps draw as tools (a command, a diff, a file read, a search, a to-do list), under Claude Code's names;
// their arguments are Claude's too (command, file_path, old_string / new_string, pattern, url, query, todos).
const APP_TOOLS = {
  bash: 'Bash', read: 'Read', write: 'Write', edit: 'Edit', multi_edit: 'MultiEdit', grep: 'Grep', glob: 'Glob', ls: 'LS',
  web_fetch: 'WebFetch', web_search: 'WebSearch', todo_write: 'TodoWrite',
}

/**
 * The name the apps know a runtime's tool by: `own` (the runtime's names for the rest: DSH's ask_user_question, pi's
 * find), then the common ones (bash → Bash, read → Read…), else as it is.
 * @param {string} name
 * @param {Record<string, string>} [own]
 */
export function appToolName(name, own = {}) {
  return own[name] ?? APP_TOOLS[name] ?? name
}

/**
 * One session.items page over items a runtime has in memory (sorted by seq; several may share a seq), with the
 * paging every runtime needs (docs/protocol.md「session.items」): from the end by `turns` (a turn starts at a user's
 * message) or `limit`, back from `before_seq`, forward from `after_seq` (at most 500 items, `more_after` when cut),
 * or the span `from_seq` … `through_seq`. Texts are cut with light(). toSeq is the session's current to_seq.
 * @param {object[]} all
 * @param {object} params
 * @param {{ history: string, toSeq: number }} at
 */
export function page(all, params = {}, { history, toSeq }) {
  if (typeof params.through_seq === 'number' && params.through_seq > 0) {
    const from = typeof params.from_seq === 'number' ? params.from_seq : 1
    return { items: all.filter((i) => i.seq >= from && i.seq <= params.through_seq).map(light), more: false }
  }
  if (params.after_seq !== undefined && params.after_seq !== null) {
    const after = all.filter((i) => i.seq > params.after_seq)
    let out = after
    let to = Math.max(toSeq, params.after_seq)
    let cut = false
    if (after.length > 500) {
      let n = 500
      while (n < after.length && after[n].seq === after[n - 1].seq) n++ // a record's items stay together
      out = after.slice(0, n)
      cut = n < after.length
      if (cut) to = out.at(-1).seq
    }
    return { items: out.map(light), has_more: false, more_after: cut, history, from_seq: params.after_seq, to_seq: to }
  }
  const before = params.before_seq > 0 ? all.filter((i) => i.seq < params.before_seq) : all
  let start
  if (params.turns > 0) {
    let turns = 0
    start = 0
    for (let i = before.length - 1; i >= 0; i--) {
      const it = before[i]
      const opens = it.role === 'user' && (it.kind === 'text' || it.kind === 'image') && !(before[i - 1]?.role === 'user' && before[i - 1]?.seq === it.seq)
      if (opens && ++turns === Math.min(params.turns, 10)) {
        start = i
        break
      }
    }
  } else {
    const limit = params.limit > 0 ? Math.min(params.limit, 500) : 80
    start = Math.max(0, before.length - limit)
    while (start > 0 && before[start].seq === before[start - 1].seq) start-- // not half a record
  }
  const out = before.slice(start)
  return {
    items: out.map(light),
    has_more: start > 0,
    history,
    to_seq: params.before_seq > 0 ? (out.at(-1)?.seq ?? 0) : toSeq,
  }
}
