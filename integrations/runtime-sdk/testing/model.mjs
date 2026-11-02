// A scripted model for runtimes' end-to-end runs: DSH's llm-mock-server (Anthropic's /v1/messages; DSH's checkout,
// built, is the only thing it needs) behind a switch, so each phase of a run scripts its own turn. Requests that carry
// tools are the agent's own and get the current script; the rest (titles and other side calls) a fixed short reply.
//
//   const model = await scriptedModel(process.env.DSH_CHECKOUT)
//   await model.script({ sequence: ['success'], successText: 'Hello' })   // llm-mock-server's options
//   … the runtime's provider points at model.url (append /v1 where the client wants it) …
//   await model.close()

import { createServer, request as httpRequest } from 'node:http'
import { join } from 'node:path'
import { pathToFileURL } from 'node:url'

/**
 * @param {string} dshCheckout - a built DSH source checkout (packages/test-support/llm-mock-server).
 * @returns {Promise<{ url: string, script(opts: object): Promise<void>, close(): Promise<void> }>}
 */
export async function scriptedModel(dshCheckout) {
  if (!dshCheckout) throw new Error('scriptedModel: set DSH_CHECKOUT (a built dsh source checkout, for its mock model server)')
  const { startMockLlmServer } = await import(pathToFileURL(join(dshCheckout, 'packages/test-support/llm-mock-server/lib/index.js')).href)
  const side = await startMockLlmServer({ sequence: ['success'], repeatLast: true, successText: 'E2E title' })
  let main = await startMockLlmServer({ sequence: ['success'], repeatLast: true, successText: 'unscripted' })
  const proxy = createServer((req, res) => {
    const chunks = []
    req.on('data', (c) => chunks.push(c))
    req.on('end', () => {
      const body = Buffer.concat(chunks)
      let target = side
      try {
        if (Array.isArray(JSON.parse(body.toString()).tools)) target = main
      } catch {}
      const url = new URL(req.url, target.baseURL.endsWith('/') ? target.baseURL : `${target.baseURL}/`)
      const out = httpRequest(url, { method: req.method, headers: { ...req.headers, host: url.host } }, (r) => {
        res.writeHead(r.statusCode, r.headers)
        r.pipe(res)
      })
      out.on('error', (e) => { res.writeHead(502); res.end(String(e)) })
      out.end(body)
    })
  })
  await new Promise((r) => proxy.listen(0, '127.0.0.1', r))
  return {
    url: `http://127.0.0.1:${proxy.address().port}`,
    /** The next turns the agent gets (llm-mock-server's options; repeatLast by default). */
    async script(opts) {
      const next = await startMockLlmServer({ repeatLast: true, ...opts })
      const old = main
      main = next
      await old?.close?.()
    },
    async close() {
      await main?.close?.()
      await side.close?.()
      proxy.close()
    },
  }
}
