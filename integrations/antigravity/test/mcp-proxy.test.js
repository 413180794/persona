import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { once } from 'node:events'
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { createInterface } from 'node:readline'
import { describe, it } from 'node:test'
import { fileURLToPath } from 'node:url'
import { mcpServers } from '../lib/compat.js'
import { agyInputSchema } from '../lib/mcp-schema.js'

const PROXY = fileURLToPath(new URL('../mcp-proxy.mjs', import.meta.url))
const FAKE = fileURLToPath(new URL('./fake-mcp-server.mjs', import.meta.url))

/** The proxy in front of the fake server, with a call(method, params) that resolves to the response. */
function proxied(t, tools) {
  const args = []
  if (tools) {
    const dir = mkdtempSync(join(tmpdir(), 'persona-mcp-proxy-'))
    t.after(() => rmSync(dir, { recursive: true, force: true }))
    args.push(join(dir, 'tools.json'))
    writeFileSync(args[0], JSON.stringify(tools))
  }
  const proc = spawn(process.execPath, [PROXY, process.execPath, FAKE, ...args], { stdio: ['pipe', 'pipe', 'inherit'] })
  t.after(() => proc.kill())
  const waiting = new Map()
  createInterface({ input: proc.stdout }).on('line', (line) => {
    const msg = JSON.parse(line)
    const resolve = waiting.get(msg.id)
    waiting.delete(msg.id)
    resolve?.(msg)
  })
  let id = 0
  const call = (method, params, requestID = ++id) => new Promise((resolve) => {
    waiting.set(requestID, resolve)
    proc.stdin.write(JSON.stringify({ jsonrpc: '2.0', id: requestID, method, params }) + '\n')
  })
  return { proc, call }
}

describe('MCP proxy (#7333)', { timeout: 15_000 }, () => {
  it('adds persona_call to the server\'s tools', async (t) => {
    const { call } = proxied(t)
    const res = await call('tools/list', {})
    assert.deepEqual(res.result.tools.map((x) => x.name), ['todo_create', 'agents_list', 'persona_call'])
  })

  it('keeps persona_call eager with many smaller connector tools and dispatches omitted tools (#10756)', async (t) => {
    const size = (tool) => Buffer.byteLength(JSON.stringify(tool))
    // Model agy's largest-first deferral with an 8 KiB declaration budget. The old unbounded response loses
    // persona_call: every connector schema is smaller than its 590-byte declaration.
    const eager = (tools) => {
      const kept = [...tools].sort((a, b) => size(a) - size(b))
      let bytes = kept.reduce((sum, tool) => sum + size(tool), 0)
      while (bytes > 8 * 1024) bytes -= size(kept.pop())
      return kept
    }
    for (const count of [32, 128, 1024]) {
      const tools = Array.from({ length: count }, (_, i) => ({
        name: `connector_read_${i}`, description: 'Read only. '.repeat(35),
        inputSchema: { type: 'object', properties: {} },
      }))
      const { call } = proxied(t, tools)
      const { result: { tools: declarations } } = await call('tools/list', {})
      const dispatch = declarations.find((tool) => tool.name === 'persona_call')
      assert.ok(dispatch)
      assert.ok(tools.every((tool) => size(tool) < 590 && size(tool) < size(dispatch)))
      assert.ok(!eager([...tools, dispatch]).some((tool) => tool.name === 'persona_call'), 'fixture reproduces deferral')
      assert.ok(eager(declarations).some((tool) => tool.name === 'persona_call'))
      assert.ok(declarations.length <= 9)
      assert.ok(Buffer.byteLength(JSON.stringify(declarations)) <= 4 * 1024)

      const omitted = tools.at(-1).name
      assert.ok(!declarations.some((tool) => tool.name === omitted))
      const list = await call('tools/call', { name: 'persona_call', arguments: {} })
      assert.equal(list.result.content[0].text, `The Persona tools: ${tools.map((tool) => tool.name).join(', ')}.`)
      const via = await call('tools/call', { name: 'persona_call', arguments: { name: omitted, arguments: { filter: 'open' } } })
      assert.deepEqual(JSON.parse(via.result.content[0].text), { name: omitted, args: { filter: 'open' } })
    }
  })

  it('reserves the dispatcher before oversized schemas and keeps later small tools', async (t) => {
    const tools = [
      { name: 'large', description: 'x'.repeat(16 * 1024), inputSchema: { type: 'object' } },
      { name: 'small', description: 'Read only', inputSchema: { type: 'object' } },
    ]
    const { call } = proxied(t, tools)
    const listed = await call('tools/list', {})
    assert.deepEqual(listed.result.tools.map((tool) => tool.name), ['small', 'persona_call'])
    const via = await call('tools/call', { name: 'persona_call', arguments: { name: 'large' } })
    assert.deepEqual(JSON.parse(via.result.content[0].text), { name: 'large', args: {} })
  })

  it('sends persona_call on as a call of the named tool, and other calls unchanged', async (t) => {
    const { call } = proxied(t)
    await call('tools/list', {})
    const via = await call('tools/call', { name: 'persona_call', arguments: { name: 'todo_create', arguments: { title: 'x' } } })
    assert.deepEqual(JSON.parse(via.result.content[0].text), { name: 'todo_create', args: { title: 'x' } })
    const bare = await call('tools/call', { name: 'agents_list', arguments: {} })
    assert.deepEqual(JSON.parse(bare.result.content[0].text), { name: 'agents_list', args: {} })
    const noArgs = await call('tools/call', { name: 'persona_call', arguments: { name: 'agents_list' } })
    assert.deepEqual(JSON.parse(noArgs.result.content[0].text), { name: 'agents_list', args: {} })
  })

  it('lists the tools without a name and leaves an unknown one to the server', async (t) => {
    const { call } = proxied(t)
    await call('tools/list', {})
    const list = await call('tools/call', { name: 'persona_call', arguments: {} })
    assert.match(list.result.content[0].text, /todo_create, agents_list/)
    const unknown = await call('tools/call', { name: 'persona_call', arguments: { name: 'nope' } })
    assert.match(unknown.error.message, /unknown tool nope/)
  })

  it('ends with its server when agy closes stdin', async (t) => {
    const { proc } = proxied(t)
    proc.stdin.end()
    const [code] = await once(proc, 'exit')
    assert.equal(code, 0)
  })
})

describe('MCP declaration enums (#9944)', { timeout: 15_000 }, () => {
  const prompt = readFileSync(new URL('../../../server/internal/prompts/mcp/agents/task_review.md', import.meta.url), 'utf8')
  const taskSchema = JSON.parse(prompt.match(/```json\s*\n([\s\S]*?)\n```/)[1])
  const taskTool = { name: 'task_review', description: 'Review a task', inputSchema: taskSchema }
  const catalogTool = { name: 'tool_catalog', inputSchema: { type: 'object' } }

  it('projects the real task_review schema and dispatches it even when its declaration is too large', async (t) => {
    assert.deepEqual(taskSchema.properties.evidence.properties.ready.enum, [true])
    const { call } = proxied(t, [taskTool])
    const listed = await call('tools/list', {})
    const expected = structuredClone(taskTool)
    delete expected.inputSchema.properties.evidence.properties.ready.enum
    assert.deepEqual(agyInputSchema(taskSchema), expected.inputSchema)
    assert.deepEqual(listed.result.tools.map((tool) => tool.name), ['persona_call'])
    for (const ready of [true, false]) {
      const args = { todo_id: 'todo_fixture', verdict: 'parked', evidence: { kind: 'merge_review', ready } }
      for (const params of [
        { name: 'task_review', arguments: args },
        { name: 'persona_call', arguments: { name: 'task_review', arguments: args } },
      ]) {
        const result = await call('tools/call', params)
        assert.deepEqual(JSON.parse(result.result.content[0].text), { name: 'task_review', args })
      }
    }
  })

  it('changes only enum keywords in schema positions, including properties named enum', async (t) => {
    const data = { enum: [true], properties: { enum: { enum: [1] } } }
    const schema = {
      type: 'object', required: ['enum'], default: data, examples: [data], const: data, 'x-metadata': data,
      properties: {
        enum: { type: 'boolean', enum: [true], default: true, examples: [true], const: true },
        numeric: { type: 'number', enum: [1, 2], minimum: 1 },
        mixed: { enum: ['one', true] }, empty: { enum: [] }, emptyString: { enum: ['valid', ''] },
        valid: { type: 'string', enum: ['accepted', 'false', ' '] },
        allowed: true, blocked: false, reference: { $ref: '#/$defs/enum' },
        tuple: { items: [{ enum: [0] }, { enum: ['ok'] }], additionalItems: { enum: [false] } },
        array: { items: { enum: [1] }, contains: { enum: [true] }, prefixItems: [{ enum: [false] }],
          unevaluatedItems: { enum: [0] } },
        union: { allOf: [{ enum: [true] }], anyOf: [{ enum: [1] }], oneOf: [{ enum: [false] }],
          not: { enum: [false] }, if: { enum: [true] }, then: { enum: [1] }, else: { enum: [0] } },
      },
      patternProperties: { '^enum$': { enum: [1] } }, additionalProperties: { enum: [true] },
      propertyNames: { enum: ['', 'name'] }, $defs: { enum: { enum: [1] } }, definitions: { enum: { enum: [true] } },
      dependencies: { enum: { enum: [true] }, requiredDependency: ['enum'] },
      dependentSchemas: { enum: { enum: [true] } }, unevaluatedProperties: { enum: [false] }, contentSchema: { enum: [1] },
    }
    const expected = structuredClone(schema)
    for (const name of ['enum', 'numeric', 'mixed', 'empty', 'emptyString']) delete expected.properties[name].enum
    delete expected.properties.tuple.items[0].enum
    delete expected.properties.tuple.additionalItems.enum
    for (const name of ['items', 'contains', 'unevaluatedItems']) delete expected.properties.array[name].enum
    delete expected.properties.array.prefixItems[0].enum
    for (const name of ['allOf', 'anyOf', 'oneOf']) delete expected.properties.union[name][0].enum
    for (const name of ['not', 'if', 'then', 'else']) delete expected.properties.union[name].enum
    delete expected.patternProperties['^enum$'].enum
    for (const name of ['additionalProperties', 'propertyNames', 'unevaluatedProperties', 'contentSchema']) delete expected[name].enum
    for (const name of ['$defs', 'definitions', 'dependencies', 'dependentSchemas']) delete expected[name].enum.enum
    const tool = { name: 'nested', inputSchema: schema, annotations: data, outputSchema: schema, _meta: data }
    const { call } = proxied(t, [tool])
    const result = await call('tools/list', {})
    assert.deepEqual(result.result.tools[0], { ...tool, inputSchema: expected })
  })

  it('correlates list responses by id and leaves tool results and ordinary messages unchanged', async (t) => {
    const tools = [taskTool, { ...catalogTool, inputSchema: {
      type: 'object', properties: { ready: { type: 'boolean', enum: [true] } },
    } }]
    const { call } = proxied(t, tools)
    const pending = call('tools/list', { hold: true }, 'list')
    const catalog = await call('tools/call', { name: 'tool_catalog', arguments: {} }, 0)
    assert.deepEqual(catalog.result, { tools })
    const listed = await pending
    assert.equal(listed.result.tools[0].name, 'tool_catalog')
    assert.equal(listed.result.tools[0].inputSchema.properties.ready.enum, undefined)
    assert.deepEqual((await call('test/echo', { tools }, 'list')).result, { tools })
    assert.ok((await call('tools/list', { fail: true }, 0)).error)
    assert.deepEqual((await call('test/echo', { tools }, 0)).result, { tools })
  })
})

describe('mcpServers', () => {
  it('passes one persona server; an assistant chat\'s through the proxy', () => {
    const agent = mcpServers('/bin/personad', 'agy-1', false)
    assert.deepEqual(agent.map((s) => [s.name, s.command, s.args]), [['persona', '/bin/personad', ['mcp']]])
    const chat = mcpServers('/bin/personad', 'agy-1', true)
    assert.deepEqual(chat.map((s) => [s.name, s.command, s.args]), [['persona', process.execPath, [PROXY, '/bin/personad', 'mcp']]])
    assert.ok(chat[0].env.some((e) => e.name === 'PERSONA_ASSISTANT' && e.value === '1'))
    assert.ok(chat[0].env.some((e) => e.name === 'PERSONA_SESSION_ID' && e.value === 'agy-1'))
  })
})
