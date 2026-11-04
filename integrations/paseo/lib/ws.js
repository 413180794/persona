// A minimal RFC 6455 WebSocket client: what the bridge needs to talk to the Paseo daemon (its endpoint /ws) with no
// dependencies, over TCP (host:port) or a unix socket (a daemon listening on unix://…). Text messages only: the
// bridge advertises no binary capability, so the daemon sends none (one that comes anyway is dropped).

import { createHash, randomBytes } from 'node:crypto'
import { EventEmitter } from 'node:events'
import { connect } from 'node:net'

const GUID = '258EAFA5-E914-47DA-95CA-C5AB0DC85B11'
/** The largest message taken (Paseo's own limit is 100 MiB). */
export const MAX_MESSAGE = 128 * 1024 * 1024
const MAX_HEAD = 64 * 1024
const OP = { cont: 0x0, text: 0x1, binary: 0x2, close: 0x8, ping: 0x9, pong: 0xa }

/**
 * Open a WebSocket. Resolves once the handshake is done; rejects when the socket or the handshake fails, or after
 * timeoutMs.
 * @param {{ host?: string, port?: number, socketPath?: string, path?: string, headers?: Record<string, string>,
 *   timeoutMs?: number }} target - host and port, or a unix socket path; path is the request path (default /ws).
 * @returns {Promise<WsClient>}
 */
export function dialWebSocket(target) {
  const path = target.path ?? '/ws'
  const host = target.socketPath ? 'localhost' : target.port ? `${hostForHeader(target.host ?? '127.0.0.1')}:${target.port}` : target.host
  const key = randomBytes(16).toString('base64')
  return new Promise((resolve, reject) => {
    const socket = target.socketPath ? connect(target.socketPath) : connect({ host: target.host ?? '127.0.0.1', port: target.port })
    let head = Buffer.alloc(0)
    let done = false
    const fail = (err) => {
      if (done) return
      done = true
      clearTimeout(timer)
      socket.destroy()
      reject(err)
    }
    const timer = setTimeout(() => fail(new Error('websocket handshake timed out')), target.timeoutMs ?? 10_000)
    timer.unref?.()
    socket.once('error', fail)
    socket.once('close', () => fail(new Error('connection closed during the websocket handshake')))
    socket.once('connect', () => {
      const lines = [`GET ${path} HTTP/1.1`, `Host: ${host}`, 'Upgrade: websocket', 'Connection: Upgrade',
        `Sec-WebSocket-Key: ${key}`, 'Sec-WebSocket-Version: 13']
      for (const [k, v] of Object.entries(target.headers ?? {})) lines.push(`${k}: ${v}`)
      socket.write(`${lines.join('\r\n')}\r\n\r\n`)
    })
    const onData = (chunk) => {
      head = Buffer.concat([head, chunk])
      const end = head.indexOf('\r\n\r\n')
      if (end < 0) {
        if (head.length > MAX_HEAD) fail(new Error('websocket handshake answer too long'))
        return
      }
      socket.off('data', onData)
      const [status, ...fields] = head.subarray(0, end).toString('latin1').split('\r\n')
      const headers = {}
      for (const f of fields) {
        const i = f.indexOf(':')
        if (i > 0) headers[f.slice(0, i).trim().toLowerCase()] = f.slice(i + 1).trim()
      }
      const code = Number(/^HTTP\/1\.1 (\d{3})/.exec(status)?.[1])
      if (code !== 101) return fail(Object.assign(new Error(`websocket handshake refused: ${status}`), { status: code }))
      const accept = createHash('sha1').update(key + GUID).digest('base64')
      if (headers['sec-websocket-accept'] !== accept || headers.upgrade?.toLowerCase() !== 'websocket') {
        return fail(new Error('websocket handshake: not a websocket answer'))
      }
      done = true
      clearTimeout(timer)
      socket.removeAllListeners('error')
      socket.removeAllListeners('close')
      resolve(new WsClient(socket, head.subarray(end + 4)))
    }
    socket.on('data', onData)
  })
}

// An IPv6 address goes in brackets in a Host header.
function hostForHeader(host) {
  return host.includes(':') && !host.startsWith('[') ? `[${host}]` : host
}

/**
 * The client end of a WebSocket: 'message' (text), 'close' (code, reason) once, 'error'. Pings are answered; a
 * message over MAX_MESSAGE, a masked server frame or a bad opcode closes it.
 */
export class WsClient extends EventEmitter {
  constructor(socket, rest) {
    super()
    this.socket = socket
    this.buf = rest?.length ? Buffer.from(rest) : Buffer.alloc(0)
    this.parts = []
    this.size = 0
    this.binary = false
    this.closed = false
    this.code = 1006
    this.reason = ''
    socket.setNoDelay?.(true)
    socket.on('data', (chunk) => {
      this.buf = this.buf.length ? Buffer.concat([this.buf, chunk]) : chunk
      this.parse()
    })
    socket.on('close', () => this.finish())
    // A reset or a broken pipe ends in 'close' anyway: 'error' only for whoever listens for it.
    socket.on('error', (err) => this.listenerCount('error') > 0 && this.emit('error', err))
    // What came with the handshake's answer is read once the caller has its listeners on (after dialWebSocket's
    // promise settles), not in a microtask before: a first message sent at once by the server would be lost.
    if (this.buf.length) setImmediate(() => this.parse())
  }

  parse() {
    while (!this.closed) {
      const b = this.buf
      if (b.length < 2) return
      const fin = (b[0] & 0x80) !== 0
      const op = b[0] & 0x0f
      if ((b[1] & 0x80) !== 0) return this.fail(1002, 'server frames must not be masked')
      let len = b[1] & 0x7f
      let at = 2
      if (len === 126) {
        if (b.length < 4) return
        len = b.readUInt16BE(2)
        at = 4
      } else if (len === 127) {
        if (b.length < 10) return
        const big = b.readBigUInt64BE(2)
        if (big > BigInt(MAX_MESSAGE)) return this.fail(1009, 'message too big')
        len = Number(big)
        at = 10
      }
      if (op < 0x8 && this.size + len > MAX_MESSAGE) return this.fail(1009, 'message too big')
      if (b.length < at + len) return
      const payload = b.subarray(at, at + len)
      this.buf = b.subarray(at + len)
      if (op >= 0x8) {
        if (!fin || len > 125) return this.fail(1002, 'bad control frame')
        if (op === OP.ping) this.frame(OP.pong, payload)
        else if (op === OP.close) {
          this.code = payload.length >= 2 ? payload.readUInt16BE(0) : 1005
          this.reason = payload.subarray(2).toString('utf8')
          this.frame(OP.close, payload.subarray(0, 2))
          return this.end()
        } else if (op !== OP.pong) return this.fail(1002, 'bad opcode')
        continue
      }
      if (op === OP.cont ? this.parts.length === 0 : (op !== OP.text && op !== OP.binary) || this.parts.length > 0) {
        return this.fail(1002, 'bad frame order')
      }
      if (op !== OP.cont) this.binary = op === OP.binary
      this.size += payload.length
      this.parts.push(Buffer.from(payload))
      if (fin) {
        const msg = Buffer.concat(this.parts)
        this.parts = []
        this.size = 0
        if (!this.binary) this.emit('message', msg.toString('utf8'))
      }
    }
  }

  /** Send a frame (masked, as a client must). */
  frame(op, payload) {
    if (this.closed || this.socket.destroyed) return false
    const n = payload.length
    let head
    if (n < 126) {
      head = Buffer.alloc(2)
      head[1] = 0x80 | n
    } else if (n < 65536) {
      head = Buffer.alloc(4)
      head[1] = 0x80 | 126
      head.writeUInt16BE(n, 2)
    } else {
      head = Buffer.alloc(10)
      head[1] = 0x80 | 127
      head.writeBigUInt64BE(BigInt(n), 2)
    }
    head[0] = 0x80 | op
    const mask = randomBytes(4)
    const body = Buffer.from(payload)
    for (let i = 0; i < body.length; i++) body[i] ^= mask[i & 3]
    this.socket.write(Buffer.concat([head, mask, body]))
    return true
  }

  /** Send one text message; false once closed. */
  send(text) {
    return this.frame(OP.text, Buffer.from(String(text), 'utf8'))
  }

  /** Close (a normal closure unless a code says otherwise). */
  close(code = 1000, reason = '') {
    if (this.closed) return
    const p = Buffer.alloc(2 + Buffer.byteLength(reason))
    p.writeUInt16BE(code, 0)
    p.write(reason, 2)
    this.code = code
    this.reason = reason
    this.frame(OP.close, p)
    this.end()
  }

  fail(code, reason) {
    if (this.listenerCount('error') > 0) this.emit('error', new Error(reason))
    this.close(code, reason)
  }

  end() {
    if (this.closed) return
    this.closed = true
    this.socket.end()
    setTimeout(() => this.socket.destroy(), 1000).unref?.()
    this.finish()
  }

  finish() {
    if (this.finished) return
    this.finished = true
    this.closed = true
    this.emit('close', this.code, this.reason)
  }
}
