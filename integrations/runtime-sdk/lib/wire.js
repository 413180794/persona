// The JSON-lines RPC of server/internal/rpc, for the runtime end of runtime.sock: one JSON object per line, either a
// request {"id","method","params"}, a response {"id","result"} / {"id","error":{"message","code"}} or an event
// {"event","params"}. Both ends may call each other.

/** A failed call's error, with the code the other end set (rpc.Error.Code). */
export class RpcError extends Error {
  constructor(message, code, data) {
    super(message)
    this.name = 'RpcError'
    if (code !== undefined) this.code = code
    if (data !== undefined) this.data = data
  }
}

export class Peer {
  /**
   * @param {import('node:net').Socket} socket - connected stream.
   * @param {{prefix?: string, onRequest?: (method: string, params: any) => any, onEvent?: (event: string, params: any) => void, maxLine?: number}} [opts]
   */
  constructor(socket, opts = {}) {
    this.socket = socket
    this.prefix = opts.prefix ?? 'd'
    this.onRequest = opts.onRequest
    this.onEvent = opts.onEvent
    this.maxLine = opts.maxLine ?? 64 * 1024 * 1024
    this.next = 0
    this.pending = new Map()
    this.closed = false
    this.buffer = ''
    this.donePromise = new Promise((resolve) => { this.resolveDone = resolve })
    socket.setEncoding('utf8')
    socket.on('data', (chunk) => this.read(chunk))
    socket.on('close', () => this.close(new Error('connection closed')))
    socket.on('error', (err) => this.close(err))
  }

  /** Resolves once the connection is gone. */
  get done() {
    return this.donePromise
  }

  read(chunk) {
    this.buffer += chunk
    let nl
    while ((nl = this.buffer.indexOf('\n')) >= 0) {
      const line = this.buffer.slice(0, nl)
      this.buffer = this.buffer.slice(nl + 1)
      if (line.trim() !== '') this.dispatch(line)
    }
    if (this.buffer.length > this.maxLine) this.close(new Error('line too long'))
  }

  dispatch(line) {
    let m
    try {
      m = JSON.parse(line)
    } catch {
      return
    }
    if (typeof m.method === 'string' && m.id !== undefined) {
      void this.handle(m)
    } else if (typeof m.event === 'string') {
      try {
        this.onEvent?.(m.event, m.params)
      } catch {
        // a bad event handler must not take the connection down
      }
    } else if (m.id !== undefined) {
      const p = this.pending.get(m.id)
      if (!p) return
      this.pending.delete(m.id)
      if (m.error) p.reject(new RpcError(m.error.message ?? 'error', m.error.code, m.error.data))
      else p.resolve(m.result)
    }
  }

  async handle(m) {
    let out
    try {
      if (!this.onRequest) throw new Error(`no handler for ${m.method}`)
      const result = await this.onRequest(m.method, m.params ?? {})
      out = { id: m.id, result: result === undefined ? null : result }
    } catch (err) {
      const error = { message: err instanceof Error ? err.message : String(err) }
      if (err && typeof err.code === 'string') error.code = err.code
      out = { id: m.id, error }
    }
    this.write(out)
  }

  write(m) {
    if (this.closed) return false
    this.socket.write(JSON.stringify(m) + '\n')
    return true
  }

  /** Call a method on the other end. */
  call(method, params = {}, { timeoutMs = 30_000, signal } = {}) {
    if (this.closed) return Promise.reject(new RpcError('connection closed', 'closed'))
    const id = `${this.prefix}${++this.next}`
    return new Promise((resolve, reject) => {
      let timer
      const cleanup = () => {
        clearTimeout(timer)
        signal?.removeEventListener('abort', onAbort)
      }
      const onAbort = () => {
        this.pending.delete(id)
        cleanup()
        reject(signal.reason ?? new Error('aborted'))
      }
      this.pending.set(id, {
        resolve: (v) => { cleanup(); resolve(v) },
        reject: (e) => { cleanup(); reject(e) },
      })
      if (timeoutMs > 0) {
        timer = setTimeout(() => {
          this.pending.delete(id)
          cleanup()
          reject(new RpcError(`${method}: timed out`, 'timeout'))
        }, timeoutMs)
        timer.unref?.()
      }
      if (signal) {
        if (signal.aborted) return onAbort()
        signal.addEventListener('abort', onAbort, { once: true })
      }
      this.write({ id, method, params })
    })
  }

  /** Send a one-way event. */
  notify(event, params = {}) {
    return this.write({ event, params })
  }

  close(err) {
    if (this.closed) return
    this.closed = true
    for (const p of this.pending.values()) p.reject(new RpcError(err?.message ?? 'connection closed', 'closed'))
    this.pending.clear()
    this.socket.destroy()
    this.resolveDone(err)
  }
}
