// A scripted Anthropic model for the bridge's end-to-end run: /v1/messages, streamed as Claude Code reads it. What it
// answers depends on the conversation's last user message — a marker in the prompt picks a turn, a tool result gets a
// reply quoting it — so one server serves every step of the run. Requests without tools (titles and other side calls)
// get a short fixed answer.

import { createServer } from 'node:http'

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

/** What a turn says: text in chunks (with a pause between), or a tool call. */
const TURNS = {
  E2E_HELLO: { text: ['Hello ', 'from ', 'the ', 'mock ', 'model, ', 'step ', 'by ', 'step.'], pause: 150 },
  // Not one of Claude Code's read-only commands (those run without asking): this one writes a file.
  E2E_BASH: { tool: { name: 'Bash', input: { command: 'touch e2e-marker.txt && echo e2e-ran', description: 'Write a marker file' } } },
  E2E_ASK: { tool: { name: 'AskUserQuestion', input: { questions: [{ question: 'Which color should it be?', header: 'Color', multiSelect: false,
    options: [{ label: 'Red', description: 'Warm' }, { label: 'Blue', description: 'Cool' }] }] } } },
  E2E_PLAN: { tool: { name: 'ExitPlanMode', input: { plan: '## Plan\n\n1. Write the test\n2. Make it pass' } } },
  E2E_SLOW: { text: Array.from({ length: 60 }, (_, i) => `word${i} `), pause: 250 },
}

function lastUser(messages) {
  for (let i = (messages ?? []).length - 1; i >= 0; i--) {
    const m = messages[i]
    if (m.role !== 'user') continue
    const blocks = typeof m.content === 'string' ? [{ type: 'text', text: m.content }] : m.content ?? []
    return blocks
  }
  return []
}

/** The turn for a request: a tool's result is answered, else the marker in the user's words picks one. */
export function turnFor(req) {
  if (!Array.isArray(req.tools) || req.tools.length === 0) return { text: ['E2E title'] }
  const blocks = lastUser(req.messages)
  const result = blocks.find((b) => b.type === 'tool_result')
  if (result) {
    const content = typeof result.content === 'string' ? result.content : (result.content ?? []).map((c) => c.text ?? '').join('')
    return { text: [`Tool said: ${content.slice(0, 200)}`] }
  }
  const said = blocks.filter((b) => b.type === 'text').map((b) => b.text).join('\n')
  for (const [marker, turn] of Object.entries(TURNS)) if (said.includes(marker)) return turn
  return { text: ['OK'] }
}

export async function startModel() {
  const requests = []
  let n = 0
  const server = createServer((req, res) => {
    const chunks = []
    req.on('data', (c) => chunks.push(c))
    req.on('end', async () => {
      let body = {}
      try {
        body = JSON.parse(Buffer.concat(chunks).toString() || '{}')
      } catch {}
      const path = new URL(req.url, 'http://x').pathname
      if (path.endsWith('/count_tokens')) {
        res.writeHead(200, { 'content-type': 'application/json' })
        res.end(JSON.stringify({ input_tokens: 42 }))
        return
      }
      if (!path.endsWith('/v1/messages')) {
        res.writeHead(404, { 'content-type': 'application/json' })
        res.end(JSON.stringify({ type: 'error', error: { type: 'not_found_error', message: path } }))
        return
      }
      requests.push(body)
      const turn = turnFor(body)
      const id = `msg_e2e_${++n}`
      const usage = { input_tokens: 100, output_tokens: 10, cache_creation_input_tokens: 0, cache_read_input_tokens: 0 }
      if (!body.stream) {
        const content = turn.tool ? [{ type: 'tool_use', id: `toolu_e2e_${n}`, name: turn.tool.name, input: turn.tool.input }] : [{ type: 'text', text: turn.text.join('') }]
        res.writeHead(200, { 'content-type': 'application/json' })
        res.end(JSON.stringify({ id, type: 'message', role: 'assistant', model: body.model ?? 'claude-mock', content,
          stop_reason: turn.tool ? 'tool_use' : 'end_turn', stop_sequence: null, usage }))
        return
      }
      res.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache', connection: 'keep-alive' })
      let gone = false
      res.on('close', () => { gone = true })
      const send = (event, data) => !gone && res.write(`event: ${event}\ndata: ${JSON.stringify({ type: event, ...data })}\n\n`)
      send('message_start', { message: { id, type: 'message', role: 'assistant', model: body.model ?? 'claude-mock', content: [], stop_reason: null, stop_sequence: null, usage } })
      if (turn.tool) {
        send('content_block_start', { index: 0, content_block: { type: 'tool_use', id: `toolu_e2e_${n}`, name: turn.tool.name, input: {} } })
        send('content_block_delta', { index: 0, delta: { type: 'input_json_delta', partial_json: JSON.stringify(turn.tool.input) } })
        send('content_block_stop', { index: 0 })
        send('message_delta', { delta: { stop_reason: 'tool_use', stop_sequence: null }, usage: { output_tokens: 20 } })
      } else {
        send('content_block_start', { index: 0, content_block: { type: 'text', text: '' } })
        for (const piece of turn.text) {
          if (gone) return
          send('content_block_delta', { index: 0, delta: { type: 'text_delta', text: piece } })
          if (turn.pause) await sleep(turn.pause)
        }
        send('content_block_stop', { index: 0 })
        send('message_delta', { delta: { stop_reason: 'end_turn', stop_sequence: null }, usage: { output_tokens: 20 } })
      }
      send('message_stop', {})
      res.end()
    })
  })
  await new Promise((r) => server.listen(0, '127.0.0.1', r))
  return {
    url: `http://127.0.0.1:${server.address().port}`,
    requests,
    close: () => new Promise((r) => server.close(r)),
  }
}
