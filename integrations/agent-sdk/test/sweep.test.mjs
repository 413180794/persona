import { test } from 'node:test'
import assert from 'node:assert/strict'
import { existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { tmpdir } from 'node:os'
import { ReplayGuard, redactor, settingsFile, sweepSettings } from '../src/lib.mjs'

test('a killed bridge leaves no key behind: the next one sweeps settings of pids that are gone', () => {
  const root = mkdtempSync(join(tmpdir(), 'sweep-test-'))
  try {
    const mine = settingsFile({ env: { ANTHROPIC_AUTH_TOKEN: 'sk-test-x' } }, root)
    const gone = join(root, 'persona-agent-sdk-999999999-abc')
    mkdirSync(gone)
    const other = join(root, 'not-ours-123-abc')
    mkdirSync(other)
    assert.equal(sweepSettings(root), 1)
    assert.ok(!existsSync(gone), 'a dead pid’s settings stay')
    assert.ok(existsSync(mine.dir), 'swept a live bridge’s settings')
    assert.ok(existsSync(other), 'swept something not the bridge’s')
  } finally {
    rmSync(root, { recursive: true, force: true })
  }
})

test('the sweep never throws: what it cannot remove is skipped', () => {
  const root = mkdtempSync(join(tmpdir(), 'sweep-test-'))
  try {
    // A file, not a directory, under a bridge's name: left alone.
    writeFileSync(join(root, 'persona-agent-sdk-999999998-abc'), 'x')
    assert.equal(sweepSettings(root), 0)
    assert.ok(existsSync(join(root, 'persona-agent-sdk-999999998-abc')))
  } finally {
    rmSync(root, { recursive: true, force: true })
  }
})

test('one header value alone is redacted, not only the whole header block', () => {
  const r = redactor({ ANTHROPIC_CUSTOM_HEADERS: 'X-Team: hdr-secret-one\nX-Other: hdr-secret-two' })
  const out = r('400: bad header X-Other: hdr-secret-two (value hdr-secret-one)')
  assert.ok(!out.includes('hdr-secret-one') && !out.includes('hdr-secret-two'), out)
})

test('on a resumed session a result answering a pending user line is kept even with no stream_event before it', () => {
  const g = new ReplayGuard(new Set(), true)
  assert.equal(g.keep({ type: 'result', subtype: 'success' }), false, 'a replayed result before any input was kept')
  assert.equal(g.keep({ type: 'result', subtype: 'error_during_execution', is_error: true }, true), true, 'the answer was dropped')
})
