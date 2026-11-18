import assert from 'node:assert/strict'
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { test } from 'node:test'
import { startFakeNode, until } from '../../runtime-sdk/testing/fake-node.js'
import { createAgyRuntime } from '../lib/runtime.js'

// Two accounts with different model lists; the CLI's sign-in is switched by rewriting catalog.json and the token file.
const ACCOUNT_A = [{ modelId: 'gemini-a-high', name: 'Model A High' }, { modelId: 'gemini-flash', name: 'Flash' }]
const ACCOUNT_B = [{ modelId: 'gemini-b-high', name: 'Model B High' }, { modelId: 'gemini-flash', name: 'Flash' }]
const FAKE = fileURLToPath(new URL('./fake-acp-signin.mjs', import.meta.url))

async function fixture(t, options = {}) {
  const home = mkdtempSync(join(tmpdir(), 'agy-signin-'))
  t.after(() => rmSync(home, { recursive: true, force: true }))
  const agyDir = join(home, 'settings')
  mkdirSync(agyDir)
  const catalogFile = join(home, 'catalog.json')
  const tokenFile = join(agyDir, 'antigravity-oauth-token')
  const state = () => JSON.parse(readFileSync(catalogFile, 'utf8'))
  const patch = (value) => writeFileSync(catalogFile, JSON.stringify({ ...state(), ...value }))
  let tokenWrites = 0
  // The CLI rewrites the token on sign-in and on each refresh. Its content is never read by Persona, so any content does.
  const writeToken = () => writeFileSync(tokenFile, 'x'.repeat(++tokenWrites))
  const signIn = (models) => { patch({ models }); writeToken() }
  writeFileSync(catalogFile, JSON.stringify({ models: ACCOUNT_A, ...options.catalog }))
  writeToken()
  const bin = join(home, 'agy')
  writeFileSync(bin, `#!/bin/sh\nexec '${process.execPath}' '${FAKE}' '${home}' "$@"\n`)
  chmodSync(bin, 0o755)
  const n = await startFakeNode(t)
  const logs = []
  const runtime = createAgyRuntime({
    home,
    agyDir,
    agyBin: bin,
    personadBin: '/missing',
    socketPath: n.path,
    signInCheckMs: options.signInCheckMs ?? 20,
    log: (m) => logs.push(m),
  })
  t.after(() => runtime.stop())
  runtime.start()
  await n.ready
  const calls = () => {
    try {
      return readFileSync(join(home, 'calls.jsonl'), 'utf8').split('\n').filter(Boolean).map(JSON.parse)
    } catch {
      return [] // no agy has run yet
    }
  }
  const initializes = () => calls().filter((call) => call.method === 'initialize').length
  const setModels = () => calls().filter((call) => call.method === 'session/set_model').map((call) => call.params.modelId)
  const published = () => n.last('runtime.launchers')?.launchers[0].models.map((model) => model.id) || []
  const recycled = () => logs.some((line) => /agy acp recycled/.test(line))
  return { n, home, state, patch, signIn, writeToken, calls, initializes, setModels, published, recycled, logs }
}

test('a sign-in change recycles an idle agy acp, and the next call runs on the new account', async (t) => {
  const f = await fixture(t)
  const { session } = await f.n.peer.call('session.start', { cwd: f.home })
  assert.equal(f.initializes(), 1)
  f.signIn(ACCOUNT_B)
  await until('idle agy acp recycled', () => f.recycled())
  await until('catalog of the new account published', () => f.published().includes('gemini-b-high'))
  // The next call starts a fresh agy, which advertises account B: its model is accepted without a retry.
  await f.n.peer.call('session.modes', { session_id: session.session_id, model: 'gemini-b-high' })
  assert.equal(f.initializes(), 2)
  assert.deepEqual(f.setModels(), ['gemini-b-high'])
})

test('a token refresh that keeps the same models does not recycle the agy acp', async (t) => {
  const f = await fixture(t)
  await f.n.peer.call('session.start', { cwd: f.home })
  f.writeToken()
  f.writeToken()
  await new Promise((resolve) => setTimeout(resolve, 150))
  assert.equal(f.recycled(), false)
  assert.equal(f.initializes(), 1)
})

test('a running turn is not interrupted by the recycle; it happens once the turn ends', async (t) => {
  const f = await fixture(t, { catalog: { hold: true } })
  const { session } = await f.n.peer.call('session.start', { cwd: f.home })
  await f.n.peer.call('session.send', { session_id: session.session_id, text: 'hi', client_msg_id: 'turn-1' })
  await until('prompt sent', () => f.calls().some((call) => call.method === 'session/prompt'))
  f.signIn(ACCOUNT_B)
  await until('sign-in seen during the turn', () => f.published().includes('gemini-b-high'))
  await new Promise((resolve) => setTimeout(resolve, 150))
  assert.equal(f.recycled(), false)
  assert.equal(f.initializes(), 1)
  f.patch({ hold: false })
  await until('recycled after the turn ended', () => f.recycled())
})

test('an Unknown model ID for a model the CLI lists recycles once and retries on the new account', async (t) => {
  const f = await fixture(t, { signInCheckMs: 3_600_000 })
  const { session } = await f.n.peer.call('session.start', { cwd: f.home })
  f.signIn(ACCOUNT_B)
  // The running agy rejects gemini-b-high (Unknown model ID); discovery lists it, so it is recycled and the call retried.
  await f.n.peer.call('session.modes', { session_id: session.session_id, model: 'gemini-b-high' })
  assert.equal(f.recycled(), true)
  assert.equal(f.initializes(), 2)
  assert.equal(f.setModels().filter((model) => model === 'gemini-b-high').length, 2)
})

test('an Unknown model ID for a model the CLI does not list fails at once without recycling', async (t) => {
  const f = await fixture(t, { signInCheckMs: 3_600_000 })
  const { session } = await f.n.peer.call('session.start', { cwd: f.home })
  await assert.rejects(f.n.peer.call('session.modes', { session_id: session.session_id, model: 'gemini-gone' }), /Unknown model ID: gemini-gone/)
  assert.equal(f.setModels().filter((model) => model === 'gemini-gone').length, 1)
  assert.equal(f.initializes(), 1)
  assert.equal(f.recycled(), false)
})

test('a session/new answer that arrives after a fresh discovery does not overwrite its catalog', async (t) => {
  // Discovery fails at start, so the first session list is what fills the catalog (the old behavior, kept until then).
  const f = await fixture(t, { signInCheckMs: 3_600_000, catalog: { discoveryError: 'Please sign in', delayNewMs: 1000 } })
  const started = f.n.peer.call('session.start', { cwd: f.home })
  // The session/new of the process on account A is still on its way while the CLI's discovery reads account B.
  await until('session/new sent', () => f.calls().some((call) => call.method === 'session/new'))
  // The start's own discovery must have failed first: a refresh asked while it runs joins it and gets its error.
  await until('first discovery failed', () => f.logs.some((line) => /agy model discovery error/.test(line)))
  f.patch({ discoveryError: undefined, models: ACCOUNT_B })
  await f.n.peer.call('launchers.models', { refresh: true })
  assert.deepEqual(f.published(), ['gemini-b-high', 'gemini-flash'])
  await started
  await new Promise((resolve) => setTimeout(resolve, 50))
  assert.deepEqual(f.published(), ['gemini-b-high', 'gemini-flash'])
})

test('an availability check whose model the running agy lacks is probed once more on a fresh agy acp', async (t) => {
  const f = await fixture(t, { signInCheckMs: 3_600_000 })
  await f.n.peer.call('session.start', { cwd: f.home })
  f.signIn(ACCOUNT_B)
  writeFileSync(join(f.home, 'settings', 'settings.json'), JSON.stringify({ model: 'gemini-b-high' }))
  const result = await f.n.peer.call('launchers.check', { cwd: f.home, prompt: 'Reply with OK' })
  assert.equal(result.ok, true)
  assert.equal(result.answer, 'OK')
  assert.equal(f.recycled(), true)
  assert.equal(f.initializes(), 2)
  assert.equal(f.setModels().filter((model) => model === 'gemini-b-high').length, 2)
})
