import { test } from 'node:test'
import assert from 'node:assert/strict'
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { install, MARK, shimText } from '../install.mjs'

test('installs a one-line shim, is idempotent, refuses a file that is not its own, uninstalls', (t) => {
  const dir = mkdtempSync('/tmp/ocinst-')
  t.after(() => rmSync(dir, { recursive: true, force: true }))
  const entry = '/opt/persona/integrations/opencode/index.js'
  assert.equal(install({ configDir: dir, entry }), 'installed')
  const file = join(dir, 'plugins', 'persona.js')
  assert.equal(readFileSync(file, 'utf8'), shimText(entry))
  assert.match(readFileSync(file, 'utf8'), /export \{ default \} from "file:\/\/\/opt\/persona\/integrations\/opencode\/index\.js"/)
  assert.equal(install({ configDir: dir, entry }), 'unchanged')
  assert.equal(install({ configDir: dir, entry: '/elsewhere/index.js' }), 'updated')
  assert.equal(install({ configDir: dir, entry, uninstall: true }), 'removed')
  assert.equal(install({ configDir: dir, entry, uninstall: true }), 'absent')
  mkdirSync(join(dir, 'plugins'), { recursive: true })
  writeFileSync(file, 'export default {}\n')
  assert.equal(install({ configDir: dir, entry }), 'foreign')
  assert.equal(install({ configDir: dir, entry, uninstall: true }), 'foreign')
  assert.equal(readFileSync(file, 'utf8'), 'export default {}\n', 'left alone')
  assert.equal(install({ configDir: dir, entry, force: true }), 'updated')
  assert.ok(readFileSync(file, 'utf8').startsWith(MARK))
})
