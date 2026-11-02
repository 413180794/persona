import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createHash, randomBytes } from 'node:crypto'
import { createServer } from 'node:http'
import { connect } from 'node:net'
import { createRuntime, Peer, SUBPROTOCOL } from '../index.js'
import { until } from '../testing/fake-node.js'

// The node's end of a daemon connection: a WebSocket client wrapped as the socket wire.js's Peer reads, so the test
// answers hello and calls the runtime like jarvisd does.
function dialNode(url, { token, protocols = [SUBPROTOCOL] } = {}) {
  const ws = new WebSocket(url, { protocols, headers: token ? { authorization: `Bearer ${token}` } : {} })
  const handlers = { data: [], close: [], error: [] }
  const socket = {
    setEncoding() {},
    on(ev, fn) { (handlers[ev] ??= []).push(fn) },
    write(s) { ws.send(s.endsWith('\n') ? s.slice(0, -1) : s) },
    destroy() { ws.close() },
  }
  const n = { ws, hello: undefined, events: [], opened: false, closed: false }
  ws.onmessage = (e) => handlers.data.forEach((fn) => fn(String(e.data) + '\n'))
  ws.onclose = () => { n.closed = true; handlers.close.forEach((fn) => fn()) }
  ws.onerror = () => {}
  ws.onopen = () => { n.opened = true }
  n.peer = new Peer(socket, {
    prefix: 'n',
    onRequest: async (method, params) => {
      if (method === 'runtime.hello') {
        n.hello = params
        return { protocol: 1, node_version: 'test', hub: true, features: ['prefer_running'] }
      }
      throw new Error(`unexpected ${method}`)
    },
    onEvent: (event, params) => n.events.push({ event, params }),
  })
  n.last = (event) => n.events.filter((e) => e.event === event).at(-1)?.params
  return n
}

function toy(extra = {}) {
  const sent = []
  const driver = {
    listSessions: async () => [{ session_id: 's1', status: 'idle' }],
    items: async () => ({ items: [], to_seq: 0 }),
    send: async (p) => { sent.push(p) },
    ...extra,
  }
  return { driver, sent }
}

test('a listening runtime serves its endpoint: the node dials in with the token and the protocol runs as on the socket', async (t) => {
  const { driver, sent } = toy()
  const rt = createRuntime(driver, { runtime: 'toy', name: 'Toy', listen: { port: 0, token: 'tok-1' }, refreshMs: 0 })
  t.after(() => rt.stop())
  const addr = await rt.start()
  assert.equal(addr.address, '127.0.0.1', 'loopback by default')
  const n = dialNode(`ws://127.0.0.1:${addr.port}/jarvis/runtime`, { token: 'tok-1' })
  t.after(() => n.ws.close())
  await until('hello', () => n.hello, 3000)
  assert.equal(n.hello.runtime, 'toy')
  assert.deepEqual(n.hello.caps, { start: false, send: true, interrupt: false, rename: false, close: false, assistant: false })
  await until('the list', () => n.last('runtime.sessions'), 3000)
  await n.peer.call('session.send', { session_id: 's1', text: 'over the websocket', client_msg_id: 'm-1' })
  assert.equal(sent[0].text, 'over the websocket')
  // A big message both ways (beyond a 16-bit frame length).
  const big = 'x'.repeat(200_000)
  await n.peer.call('session.send', { session_id: 's1', text: big, client_msg_id: 'm-2' })
  assert.equal(sent[1].text.length, big.length)
  assert.ok(rt.connected)
})

test('a wrong token, a missing subprotocol or another path is refused before the protocol starts', async (t) => {
  const { driver } = toy()
  const logs = []
  const rt = createRuntime(driver, { runtime: 'toy', listen: { port: 0, token: 'tok-R4ND0M' }, refreshMs: 0, log: (m) => logs.push(m) })
  t.after(() => rt.stop())
  const { port } = await rt.start()
  for (const [url, opts] of [
    [`ws://127.0.0.1:${port}/jarvis/runtime`, { token: 'wrong-fixture-token' }],
    [`ws://127.0.0.1:${port}/jarvis/runtime`, {}],
    [`ws://127.0.0.1:${port}/jarvis/runtime`, { token: 'tok-R4ND0M', protocols: ['something-else'] }],
    [`ws://127.0.0.1:${port}/elsewhere`, { token: 'tok-R4ND0M' }],
  ]) {
    const n = dialNode(url, opts)
    await until('refused', () => n.closed, 3000)
    assert.equal(n.opened, false, `${url} ${JSON.stringify(opts)}`)
    assert.equal(n.hello, undefined)
  }
  assert.ok(logs.some((l) => /refused a connection/.test(l)))
  assert.ok(!logs.some((l) => l.includes('tok-')), 'no token in the log')
  assert.equal(rt.connected, false)
})

test('a newer connection from the node replaces the last one', async (t) => {
  const { driver } = toy()
  const rt = createRuntime(driver, { runtime: 'toy', listen: { port: 0, token: 'tok' }, refreshMs: 0 })
  t.after(() => rt.stop())
  const { port } = await rt.start()
  const a = dialNode(`ws://127.0.0.1:${port}/jarvis/runtime`, { token: 'tok' })
  await until('first hello', () => a.hello, 3000)
  const b = dialNode(`ws://127.0.0.1:${port}/jarvis/runtime`, { token: 'tok' })
  t.after(() => b.ws.close())
  await until('second hello', () => b.hello, 3000)
  await until('the first closed', () => a.closed, 3000)
  await b.peer.call('session.send', { session_id: 's1', text: 'x' })
  assert.ok(rt.connected)
})

test('mounted on the runtime\'s own HTTP server through accept(), with a token read per connection', async (t) => {
  const { driver } = toy()
  let current = 'one'
  const rt = createRuntime(driver, { runtime: 'toy', listen: true, token: () => current, refreshMs: 0 })
  t.after(() => rt.stop())
  assert.equal(await rt.start(), undefined)
  const server = createServer((req, res) => res.end('the daemon\'s own API'))
  server.on('upgrade', (req, socket, head) => {
    if (req.url === '/api/jarvis') rt.accept(req, socket, head)
    else socket.destroy()
  })
  await new Promise((r) => server.listen(0, '127.0.0.1', r))
  t.after(() => server.close())
  const url = `ws://127.0.0.1:${server.address().port}/api/jarvis`
  current = 'two' // rotated
  const old = dialNode(url, { token: 'one' })
  await until('refused', () => old.closed, 3000)
  assert.equal(old.hello, undefined)
  const n = dialNode(url, { token: 'two' })
  t.after(() => n.ws.close())
  await until('hello', () => n.hello, 3000)
  assert.throws(() => createRuntime(driver, { runtime: 'toy', listen: { port: 0 } }), /token/)
  assert.throws(() => createRuntime(driver, { runtime: 'toy' }).accept({}, {}, Buffer.alloc(0)), /dials runtime.sock/)
})

// Raw frames: what a WebSocket client may send that Node's own client does not (fragments, pings between them,
// unmasked frames).
function frame(op, payload, { fin = true, mask = true } = {}) {
  const n = payload.length
  const head = n < 126 ? Buffer.from([(fin ? 0x80 : 0) | op, (mask ? 0x80 : 0) | n]) : Buffer.from([(fin ? 0x80 : 0) | op, (mask ? 0x80 : 0) | 126, n >> 8, n & 0xff])
  if (!mask) return Buffer.concat([head, payload])
  const key = randomBytes(4)
  const body = Buffer.from(payload)
  for (let i = 0; i < body.length; i++) body[i] ^= key[i & 3]
  return Buffer.concat([head, key, body])
}

async function rawConnect(port, token) {
  const key = randomBytes(16).toString('base64')
  const sock = connect(port, '127.0.0.1')
  await new Promise((r) => sock.once('connect', r))
  sock.write(`GET /jarvis/runtime HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: ${key}\r\n`
    + `Sec-WebSocket-Version: 13\r\nSec-WebSocket-Protocol: ${SUBPROTOCOL}\r\nAuthorization: Bearer ${token}\r\n\r\n`)
  let buf = Buffer.alloc(0)
  sock.on('data', (d) => { buf = Buffer.concat([buf, d]) })
  await until('101', () => buf.includes('\r\n\r\n'), 3000)
  const end = buf.indexOf('\r\n\r\n') + 4
  const headers = buf.subarray(0, end).toString()
  const accept = createHash('sha1').update(key + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11').digest('base64')
  assert.match(headers, /^HTTP\/1\.1 101/)
  assert.ok(headers.includes(`Sec-WebSocket-Accept: ${accept}`))
  assert.ok(headers.includes(`Sec-WebSocket-Protocol: ${SUBPROTOCOL}`))
  buf = buf.subarray(end)
  return { sock, take: () => { const b = buf; buf = Buffer.alloc(0); return b }, peek: () => buf }
}

test('fragmented messages, pings between them, and an unmasked frame', async (t) => {
  const { driver, sent } = toy()
  const rt = createRuntime(driver, { runtime: 'toy', listen: { port: 0, token: 'tok' }, refreshMs: 0 })
  t.after(() => rt.stop())
  const { port } = await rt.start()
  const c = await rawConnect(port, 'tok')
  t.after(() => c.sock.destroy())
  await until('the hello request', () => c.peek().includes('runtime.hello'), 3000)
  const f = c.take() // one unmasked text frame from the server
  const len = f[1] & 0x7f
  const hello = JSON.parse(f.subarray(len === 126 ? 4 : 2).toString())
  const answer = Buffer.from(JSON.stringify({ id: hello.id, result: { protocol: 1, hub: true, features: [] } }))
  c.sock.write(frame(0x1, answer.subarray(0, 10), { fin: false }))
  c.sock.write(frame(0x9, Buffer.from('hi'))) // a ping in the middle of a message
  c.sock.write(frame(0x0, answer.subarray(10)))
  await until('the pong', () => c.peek().includes(Buffer.from([0x8a, 0x02])), 3000)
  await until('connected', () => rt.connected, 3000)
  const call = Buffer.from(JSON.stringify({ id: 'n1', method: 'session.send', params: { session_id: 's1', text: 'pieces' } }))
  c.sock.write(Buffer.concat([frame(0x1, call.subarray(0, 5), { fin: false }), frame(0x0, call.subarray(5, 9), { fin: false }), frame(0x0, call.subarray(9))]))
  await until('the send', () => sent.length === 1, 3000)
  assert.equal(sent[0].text, 'pieces')
  c.take()
  c.sock.write(frame(0x1, Buffer.from('{}'), { mask: false }))
  await until('closed with 1002', () => c.peek().length >= 4 && c.peek()[0] === 0x88, 3000)
  assert.equal(c.peek().readUInt16BE(2), 1002)
  await until('disconnected', () => !rt.connected, 3000)
})
