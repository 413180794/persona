import { appendFileSync, readFileSync } from 'node:fs'
import { createInterface } from 'node:readline'

// A fake `agy` whose account is read once per process, like the real CLI: `agy acp` keeps the models it started with,
// while `agy models` always reads the current sign-in (catalog.json). Catalog keys: models, hold (a prompt waits until it
// is cleared), delayNewMs (session/new answers late), discoveryError (`agy models` fails).
const directory = process.argv[2]
const settings = () => JSON.parse(readFileSync(`${directory}/catalog.json`, 'utf8'))
if (process.argv.includes('models')) {
  const catalog = settings()
  if (catalog.discoveryError) { console.error(catalog.discoveryError); process.exit(1) }
  for (const m of catalog.models) console.log(`${m.modelId}\t${m.name}`)
  process.exit(0)
}
const id = '22222222-3333-4444-8555-666666666666'
const advertised = settings().models
let model = advertised[0].modelId
const send = (value) => process.stdout.write(`${JSON.stringify(value)}\n`)
const configuration = () => [{ id: 'model', category: 'model', currentValue: model }, { id: 'mode', currentValue: 'default' }]
const sessionResult = () => ({ sessionId: id, models: { availableModels: advertised, currentModelId: model }, modes: { currentModeId: 'default' }, configOptions: configuration() })
createInterface({ input: process.stdin }).on('line', (line) => {
  const request = JSON.parse(line)
  appendFileSync(`${directory}/calls.jsonl`, `${line}\n`)
  const { method, params } = request
  if (!method) return
  const reply = (result, error) => {
    if (request.id !== undefined) send({ jsonrpc: '2.0', id: request.id, ...(error ? { error } : { result }) })
  }
  if (method === 'initialize') return reply({ protocolVersion: 1, agentInfo: { version: 'test' } })
  if (method === 'session/new') return setTimeout(() => reply(sessionResult()), settings().delayNewMs || 0)
  if (method === 'session/resume') return reply(sessionResult())
  if (method === 'session/set_model') {
    if (!advertised.some((entry) => entry.modelId === params.modelId)) {
      return reply(undefined, { code: -32602, message: `Unknown model ID: ${params.modelId}` })
    }
    model = params.modelId
    return reply({})
  }
  if (method === 'session/prompt') {
    send({ jsonrpc: '2.0', method: 'session/update', params: { sessionId: id, update: { sessionUpdate: 'agent_message_chunk', content: { type: 'text', text: 'OK' } } } })
    const finish = () => {
      if (settings().hold) return false
      reply({ stopReason: 'end_turn' })
      return true
    }
    if (finish()) return
    const timer = setInterval(() => { if (finish()) clearInterval(timer) }, 20)
    return
  }
  if (method === 'session/set_config_option') return reply({ configOptions: configuration() })
  if (method === 'session/list') return reply({ sessions: [] })
  reply({})
})
