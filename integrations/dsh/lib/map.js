import { SNAPSHOT_RE } from '../../runtime-sdk/lib/compat.js'
// Pure mapping from DSH session events to Persona items (docs/integrations/dsh.md "Mapping"). No I/O here: the runtime (dsh.js) pages
// DSH and hands the events to these functions; the tests feed them recorded event shapes. The generic item helpers
// (page limits, clipping, one-line summaries) are the runtime SDK's.

import { appToolName, clipInput, light, snippet } from '../../runtime-sdk/index.js'

// The live snapshot the Persona assistant starts each message with (node/assistant_context.go; a bare <persona-context>
// tag, <persona-context> from older nodes, unlike the phone's quoted context, which carries attributes): context for the
// model, not shown in the chat.

/** Persona seq of a DSH event: DSH counts from 0, Persona treats 0 as "nothing yet". */
export const personaSeq = (dshSeq) => dshSeq + 1
/** DSH seq of a Persona seq. */
export const dshSeq = (seq) => seq - 1

// DSH's tools under the names the apps draw (the SDK's common ones, plus DSH's own); DSH's arguments are already the
// apps' (command, file_path, old_string, pattern…), so the chat draws a command, a diff or a file read as for Claude.
// A subagent call ({description, prompt}) is Claude's Agent: the chat draws it as a subtask and opens the subagent it
// started (the session's subagents carry the call's id).
const OWN_TOOLS = { read_image: 'Read', ask_user_question: 'AskUserQuestion', subagent: 'Agent', subagent_fork: 'Agent' }

/** Tool name the apps render. */
export function toolName(name) {
  return appToolName(name, OWN_TOOLS)
}

/** Parse a tool call's arguments (a JSON string in DSH), clipping very long strings as transcripts do. */
export function toolInput(name, raw) {
  let input = raw
  if (typeof raw === 'string') {
    try {
      input = raw === '' ? {} : JSON.parse(raw)
    } catch {
      input = { arguments: raw }
    }
  }
  if (input === null || typeof input !== 'object' || Array.isArray(input)) input = { value: input }
  if (name === 'web_search' && Array.isArray(input.queries) && input.query === undefined) {
    input = { ...input, query: input.queries.join(' / ') }
  }
  return clipInput(input)
}

function ts(ms) {
  return new Date(typeof ms === 'number' ? ms : Date.now()).toISOString()
}

function textOf(blocks) {
  if (!Array.isArray(blocks)) return ''
  return blocks.filter((b) => b && b.type === 'text' && typeof b.text === 'string').map((b) => b.text).join('')
}

/** An image block's placeholder: its bytes come from session.image. */
function imageOf(block) {
  const a = block.attachment ?? {}
  const img = { mime: a.mediaType ?? block.mediaType ?? 'image/png', data: '', lazy: true }
  if (typeof a.bytes === 'number') img.size = a.bytes
  if (typeof a.width === 'number') img.width = a.width
  if (typeof a.height === 'number') img.height = a.height
  return img
}

/** Where an item's image lives in DSH: the attachment id of block `index` of the event. */
export function attachmentOf(event, index) {
  const blocks = contentBlocks(event)
  const b = blocks?.[index]
  return b?.type === 'image' ? b.attachment?.attachmentId : undefined
}

function contentBlocks(event) {
  switch (event?.type) {
    case 'user/message':
      return event.data?.content
    case 'tool/result':
      return event.data?.message?.content
    case 'assistant/message':
      return event.data?.message?.content
    default:
      return undefined
  }
}

const TURN_NOTES = {
  aborted: '已中断',
  interrupted: '已中断',
  'max-tokens': '输出达到上限，这一轮停了',
  blocked: '这一轮被拦下了',
}

/**
 * The items of one DSH session event, in block order; ids are "<persona seq>#<block>". Only what a person reads in a
 * chat: the human's prompts (source kind user), the assistant's thinking / text / tool calls, tool results, and
 * notices for interrupted or failed turns and compactions. Context DSH injects (time, skills, instructions) is left
 * out; queued inbox splices are left out too, their message is logged again as a user/message when it enters.
 */
export function eventItems(event, { full = false } = {}) {
  if (!event || typeof event.seq !== 'number') return []
  const seq = personaSeq(event.seq)
  const at = ts(event.time)
  const out = []
  const push = (idx, item) => out.push(full ? { id: `${seq}#${idx}`, seq, ts: at, ...item } : light({ id: `${seq}#${idx}`, seq, ts: at, ...item }))
  const d = event.data ?? {}
  switch (event.type) {
    case 'user/message': {
      const kind = d.source?.kind
      if (kind === 'subagent-settled') {
        const summary = typeof d.source?.summary === 'string' ? d.source.summary : textOf(d.content)
        push(0, { role: 'system', kind: 'notice', text: summary ? `子 Agent 完成：${summary}` : '子 Agent 完成' })
        break
      }
      if (kind !== 'user') break
      ;(d.content ?? []).forEach((b, i) => {
        const text = b?.type === 'text' && typeof b.text === 'string' ? b.text.replace(SNAPSHOT_RE, '') : ''
        if (text.trim() !== '') push(i, { role: 'user', kind: 'text', text })
        else if (b?.type === 'image') push(i, { role: 'user', kind: 'image', image: imageOf(b) })
      })
      break
    }
    case 'assistant/message': {
      ;(d.message?.content ?? []).forEach((b, i) => {
        if (b?.type === 'reasoning' && typeof b.text === 'string') push(i, { role: 'assistant', kind: 'thinking', text: b.text })
        else if (b?.type === 'text' && typeof b.text === 'string') push(i, { role: 'assistant', kind: 'text', text: b.text })
        else if (b?.type === 'tool-call') {
          push(i, { role: 'assistant', kind: 'tool_use', tool_use_id: b.id, tool_name: toolName(b.name), tool_input: toolInput(b.name, b.arguments) })
        }
      })
      break
    }
    case 'tool/result': {
      // Compaction re-surfaces older results; those are history already shown once.
      if (event.surfaceOp !== undefined && event.surfaceOp !== 'append') break
      const m = d.message ?? {}
      push(0, { role: 'user', kind: 'tool_result', tool_use_id: m.toolCallId, text: textOf(m.content), is_error: m.isError === true })
      ;(m.content ?? []).forEach((b, i) => {
        if (b?.type === 'image') push(i + 1, { role: 'user', kind: 'image', tool_use_id: m.toolCallId, image: imageOf(b) })
      })
      break
    }
    case 'turn/end': {
      const reason = d.reason?.kind
      if (reason === 'error') {
        const err = d.reason?.error
        const msg = typeof err?.message === 'string' ? err.message : ''
        push(0, { role: 'system', kind: 'notice', text: msg ? `出错了：${snippet(msg)}` : '出错了' })
      } else if (TURN_NOTES[reason]) {
        push(0, { role: 'system', kind: 'notice', text: TURN_NOTES[reason] })
      }
      break
    }
    case 'compaction/end':
      push(0, { role: 'system', kind: 'notice', text: '上下文已压缩' })
      break
    default:
      break
  }
  return out
}

/** Items of a run of events (a page's records or a live batch), oldest first. */
export function itemsOf(events, opts) {
  const out = []
  for (const e of events) out.push(...eventItems(e, opts))
  return out
}

/** The last thing the assistant said in a run of events, for the list's last_text. */
export function lastText(events) {
  for (let i = events.length - 1; i >= 0; i--) {
    const e = events[i]
    if (e?.type !== 'assistant/message') continue
    const blocks = e.data?.message?.content ?? []
    for (let j = blocks.length - 1; j >= 0; j--) {
      const b = blocks[j]
      if (b?.type === 'text' && typeof b.text === 'string' && b.text.trim() !== '') return snippet(b.text)
    }
  }
  return ''
}

/** The text an assistant-stream frame adds (text deltas only), or '' for anything else. */
export function streamText(frame) {
  if (frame?.type !== 'chunk') return ''
  const c = frame.chunk
  return c?.type === 'text-delta' && typeof c.text === 'string' ? c.text : ''
}
