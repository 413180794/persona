// Pure mapping from opencode's session messages to Persona items (docs/integrations/opencode.md "Mapping"). No I/O: the host
// (opencode.js) hands in the messages (rows of opencode.db's session_message, or what the plugin API's
// session.context returns: the same fields) and the tests feed recorded shapes.
//
// seq: a message's `seq` in opencode's event log. It is stable, unique and growing within a session (it has gaps:
// the log holds other events too), so it is the item's seq as is. A message is rewritten in place while it runs (its
// tool calls finish one by one), so the host pushes it again until the turn is over; ids therefore come from the
// position of a part in the message and never move when a result arrives: part i is `<seq>#<2i>`, its result
// `<seq>#<2i+1>`.

import { appToolName, clipInput, snippet } from '../../runtime-sdk/index.js'

// opencode's tools under the names the apps draw (the SDK maps read / write / edit / grep / glob itself).
const OWN_TOOLS = { shell: 'Bash', subagent: 'Agent', websearch: 'WebSearch', webfetch: 'WebFetch', skill: 'Skill', question: 'AskUserQuestion', todowrite: 'TodoWrite' }

/** Tool name the apps render. */
export const toolName = (name) => appToolName(name, OWN_TOOLS)

const dropUndefined = (o) => Object.fromEntries(Object.entries(o).filter(([, v]) => v !== undefined && v !== null))

/** An opencode tool call's input in the shape the apps read for that tool (Claude's argument names). */
export function toolInput(name, input) {
  const a = input && typeof input === 'object' ? input : {}
  switch (name) {
    case 'shell':
      return clipInput(dropUndefined({ command: a.command, description: a.description, timeout: a.timeout }))
    case 'read':
      return clipInput(dropUndefined({ file_path: a.path, offset: a.offset, limit: a.limit }))
    case 'write':
      return clipInput(dropUndefined({ file_path: a.path, content: a.content }))
    case 'edit':
      return clipInput(dropUndefined({ file_path: a.path, old_string: a.oldString, new_string: a.newString, replace_all: a.replaceAll }))
    case 'grep':
    case 'glob':
      return clipInput(dropUndefined({ pattern: a.pattern, path: a.path, glob: a.include ?? a.glob }))
    case 'webfetch':
      return clipInput(dropUndefined({ url: a.url, format: a.format }))
    case 'websearch':
      return clipInput(dropUndefined({ query: a.query }))
    case 'subagent':
      return clipInput(dropUndefined({ description: a.description, prompt: a.prompt, subagent_type: a.agent }))
    default:
      return clipInput(a)
  }
}

// The finish reasons that stop a turn short without an error, in the words DSH uses for the same (dsh/lib/map.js).
const FINISH_NOTES = { length: '输出达到上限，这一轮停了', 'content-filter': '这一轮被拦下了' }

const ts = (ms) => new Date(typeof ms === 'number' && ms > 0 ? ms : Date.now()).toISOString()

/** The text of a tool's result: its text parts, files named, or the error. */
export function resultText(state) {
  if (state?.status === 'error' || (state?.error && state.status !== 'completed')) {
    const e = state.error
    const text = typeof e === 'string' ? e : e?.message ?? (e ? JSON.stringify(e) : '')
    const out = textParts(state.content)
    return [out, text].filter(Boolean).join('\n') || '出错了'
  }
  return textParts(state?.content)
}

function textParts(content) {
  const out = []
  for (const c of Array.isArray(content) ? content : []) {
    if (c?.type === 'text' && typeof c.text === 'string') out.push(c.text)
    else if (c?.type === 'file') out.push(`［文件 ${c.name ?? c.uri ?? ''}］`)
  }
  return out.join('\n')
}

const errorText = (e) => (typeof e === 'string' ? e : e?.message ?? '')

/**
 * The items of one message at seq. Images are placeholders (lazy): their data comes from `image`. Messages that are
 * not for people (the model's instructions, injected reminders, model / agent switches) give none.
 * @param {object} msg - { id, type, time, ...fields }
 * @param {number} seq
 */
export function messageItems(msg, seq) {
  const out = []
  const at = ts(msg?.time?.created)
  const add = (n, it) => out.push({ id: `${seq}#${n}`, seq, ts: at, ...it })
  switch (msg?.type) {
    case 'user': {
      if (msg.text) add(0, { role: 'user', kind: 'text', text: msg.text })
      ;(msg.files ?? []).forEach((f, i) => {
        if (typeof f?.mime === 'string' && f.mime.startsWith('image/')) add(1 + i, { role: 'user', kind: 'image', image: { mime: f.mime, data: '', lazy: true } }) // bytes: session.image
        else add(1 + i, { role: 'user', kind: 'text', text: `［附件 ${f?.name ?? f?.mime ?? ''}］` })
      })
      break
    }
    case 'assistant': {
      const parts = msg.content ?? []
      parts.forEach((p, i) => {
        if (p?.type === 'reasoning' && p.text) add(2 * i, { role: 'assistant', kind: 'thinking', text: p.text })
        else if (p?.type === 'text' && p.text) add(2 * i, { role: 'assistant', kind: 'text', text: p.text })
        else if (p?.type === 'tool') {
          const state = p.state ?? {}
          add(2 * i, { role: 'assistant', kind: 'tool_use', tool_name: toolName(p.name), tool_use_id: p.id, tool_input: toolInput(p.name, state.input) })
          if (state.status === 'completed' || state.status === 'error') {
            add(2 * i + 1, { role: 'user', kind: 'tool_result', tool_use_id: p.id, is_error: state.status === 'error', text: resultText(state) })
          }
        }
      })
      if (msg.finish === 'error' || msg.error) {
        if (msg.error?.type === 'aborted') add(2 * parts.length, { role: 'system', kind: 'notice', text: '已中断' })
        else add(2 * parts.length, { role: 'system', kind: 'notice', text: errorText(msg.error) ? `出错了：${snippet(errorText(msg.error))}` : '出错了' })
      } else if (FINISH_NOTES[msg.finish]) {
        // A turn the model did not end itself: say why, or the phone shows half a sentence (or nothing) as the answer.
        add(2 * parts.length, { role: 'system', kind: 'notice', text: FINISH_NOTES[msg.finish] })
      }
      break
    }
    case 'compaction':
      if (msg.status !== 'pending') add(0, { role: 'system', kind: 'notice', text: '已压缩上下文' })
      break
    case 'shell': {
      // A command the user ran with ! in opencode: a Bash call and its output.
      const id = `shell-${msg.id ?? seq}`
      add(0, { role: 'assistant', kind: 'tool_use', tool_name: 'Bash', tool_use_id: id, tool_input: { command: msg.command } })
      if (msg.status && msg.status !== 'running') {
        add(1, { role: 'user', kind: 'tool_result', tool_use_id: id, is_error: typeof msg.exit === 'number' && msg.exit !== 0, text: msg.output?.output ?? '' })
      }
      break
    }
    case 'idle':
      // The end of a turn; only a turn that did not finish says so (an assistant message with an error says it too).
      if (msg.outcome === 'interrupted') add(0, { role: 'system', kind: 'notice', text: '已中断' })
      break
    // system, synthetic, model-switched, agent-switched, location-switched: not in the chat
  }
  return out
}

/** The image the item at index of a user message stands for ({ mime, data }), undefined for others. */
export function imageOf(msg, index) {
  if (msg?.type !== 'user') return undefined
  const f = (msg.files ?? [])[index - 1]
  if (!f || typeof f.mime !== 'string' || !f.mime.startsWith('image/') || typeof f.data !== 'string') return undefined
  return { mime: f.mime, data: f.data }
}

/** Parse a session_message row (data is JSON) into the message shape messageItems reads. */
export function fromRow(row) {
  let data
  try {
    data = JSON.parse(row.data)
  } catch {
    return { id: row.id, type: row.type }
  }
  return { ...data, id: row.id, type: row.type }
}

/** The last thing the assistant said in the messages (oldest first), one line (the list's last_text). */
export function lastText(messages) {
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i]
    if (m?.type !== 'assistant') continue
    for (let j = (m.content ?? []).length - 1; j >= 0; j--) {
      const p = m.content[j]
      if (p?.type === 'text' && p.text) return snippet(p.text)
    }
  }
  return ''
}

/** What the model saw in an assistant message: input and cached tokens (opencode counts reasoning apart). */
export function contextTokens(msg) {
  const t = msg?.tokens
  if (!t) return 0
  return (t.input ?? 0) + (t.cache?.read ?? 0) + (t.cache?.write ?? 0)
}

/** The live text of an assistant message being generated: its text parts so far, in order. */
export function liveText(texts) {
  return [...texts.entries()].sort((a, b) => a[0] - b[0]).map(([, t]) => t).join('')
}

/** A permission request as the apps draw it: the Claude-style tool name, input and one line. */
export function permissionCard(req) {
  const action = String(req?.action ?? '')
  const resources = Array.isArray(req?.resources) ? req.resources.map(String) : []
  const first = resources[0] ?? ''
  let input
  switch (action) {
    case 'shell':
      input = { command: first }
      break
    case 'read':
    case 'write':
    case 'edit':
      input = { file_path: first }
      break
    case 'webfetch':
      input = { url: first }
      break
    default:
      input = resources.length ? { resources } : {}
  }
  return { tool_name: toolName(action), tool_input: input, summary: snippet(first || action) }
}
