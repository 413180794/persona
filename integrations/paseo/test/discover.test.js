import { test } from 'node:test'
import assert from 'node:assert/strict'
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { daemonTarget, localCredential, parseListen, paseoHome } from '../lib/discover.js'

test("the daemon is where its home's paseo.pid says, never a default port", (t) => {
  const home = mkdtempSync('/tmp/ppd-')
  t.after(() => rmSync(home, { recursive: true, force: true }))
  assert.deepEqual(daemonTarget(home), { target: undefined, listen: undefined, pid: undefined, serverId: undefined, running: false }, 'not running')
  writeFileSync(join(home, 'paseo.pid'), JSON.stringify({ pid: 42, listen: null }))
  const starting = daemonTarget(home)
  assert.deepEqual([starting.target, starting.running, starting.pid], [undefined, true, 42], 'starting: no address yet')
  writeFileSync(join(home, 'paseo.pid'), JSON.stringify({ pid: 42, listen: '0.0.0.0:7001', serverId: 'srv' }))
  assert.deepEqual(daemonTarget(home).target, { host: '127.0.0.1', port: 7001 })
  writeFileSync(join(home, 'paseo.pid'), JSON.stringify({ pid: 42, listen: 'unix:///tmp/p.sock' }))
  assert.deepEqual(daemonTarget(home).target, { socketPath: '/tmp/p.sock' })
  assert.deepEqual(parseListen('[::1]:6767'), { host: '::1', port: 6767 })
  assert.equal(parseListen('pipe:\\\\.\\x'), null)
  assert.equal(localCredential(home), undefined)
  writeFileSync(join(home, 'local-credential'), 'abcdefghijklmnopqrstuvwxyz0123456789_-ABCDE\n')
  assert.equal(localCredential(home), 'abcdefghijklmnopqrstuvwxyz0123456789_-ABCDE')
  assert.equal(paseoHome({ PASEO_HOME: '/x/y' }), '/x/y')
})
