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
