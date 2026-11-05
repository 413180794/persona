// A scripted OpenAI-compatible chat server for driving a throwaway opencode without a real model:
//   node mock-llm.mjs <port>
// The last user message picks the reply: "run <command>" calls the shell tool with that command, "slow ..." streams
// its words slowly (for interrupt / live text checks), "ask ..." has no special meaning (plain text), anything else
// answers "pong: <text>". After a tool result it says "done". Requests are logged to stderr as one line each.

import { createServer } from 'node:http'

const port = Number(process.argv[2]) || 0
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
let n = 0

const chunk = (id, delta, finish) => `data: ${JSON.stringify({ id, object: 'chat.completion.chunk', created: 0, model: 'mock-1',
  choices: [{ index: 0, delta, finish_reason: finish ?? null }] })}\n\n`

function textOf(content) {
  return typeof content === 'string' ? content : (content ?? []).map((p) => p?.text ?? '').join('')
}

const server = createServer(async (req, res) => {
  if (req.method === 'GET') {
    res.setHeader('content-type', 'application/json')
    res.end(JSON.stringify({ object: 'list', data: [{ id: 'mock-1', object: 'model' }] }))
    return
  }
  let body = ''
  for await (const c of req) body += c
  let json
  try { json = JSON.parse(body) } catch { json = {} }
  const messages = json.messages ?? []
  const last = messages.at(-1)
  const user = [...messages].reverse().find((m) => m.role === 'user')
  const text = textOf(user?.content).trim()
  const id = `chatcmpl-${++n}`
  console.error(`mock-llm: ${messages.length} messages, last=${last?.role}, tools=${(json.tools ?? []).length}`)
  res.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache' })
  res.write(chunk(id, { role: 'assistant', content: '' }))
  const shell = (json.tools ?? []).map((t) => t.function?.name).find((x) => x === 'shell' || x === 'bash')
  const words = (s) => s.split(/(\s+)/).filter(Boolean)
  if (last?.role === 'tool') {
    for (const w of words('The command finished: done.')) res.write(chunk(id, { content: w }))
    res.write(chunk(id, {}, 'stop'))
  } else if (/^run /i.test(text) && shell) {
    const command = text.slice(4)
    res.write(chunk(id, { content: 'Running it. ' }))
    res.write(chunk(id, { tool_calls: [{ index: 0, id: `call_${n}`, type: 'function', function: { name: shell, arguments: JSON.stringify({ command, description: 'mock command' }) } }] }))
    res.write(chunk(id, {}, 'tool_calls'))
  } else if (/^slow/i.test(text)) {
    for (const w of words('This is a slow answer that keeps streaming word by word so that an interrupt can land in the middle of it and the live text can be seen on the phone.')) {
      if (res.destroyed) return
      res.write(chunk(id, { content: w }))
      await sleep(250)
    }
    res.write(chunk(id, {}, 'stop'))
  } else {
    for (const w of words(`pong: ${text}`)) res.write(chunk(id, { content: w }))
    res.write(chunk(id, {}, 'stop'))
  }
  res.write('data: [DONE]\n\n')
  res.end()
})
server.listen(port, '127.0.0.1', () => console.error(`mock-llm: listening on ${server.address().port}`))
