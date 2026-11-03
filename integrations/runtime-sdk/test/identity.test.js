import { test } from 'node:test'
import assert from 'node:assert/strict'
import { mkdtempSync, mkdirSync, readFileSync, rmSync, statSync, symlinkSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { envValue, stateDir, prepareStateDir, prepareSocketDirectory, isAssistantDir } from '../lib/identity.js'

test('Persona environment wins, including an explicitly empty value', () => {
  assert.equal(envValue('JARVIS_HOME', { PERSONA_HOME: 'current', JARVIS_HOME: 'legacy' }), 'current')
  assert.equal(envValue('PERSONA_HOME', { JARVIS_HOME: 'legacy' }), 'legacy')
  assert.equal(envValue('JARVIS_HOME', { PERSONA_HOME: '', JARVIS_HOME: 'private' }), '')
})

for (const kind of ['fresh', 'legacy', 'current', 'both', 'linked', 'external', 'dangling']) {
  test(`state directory compatibility: ${kind}`, (t) => {
    const home = mkdtempSync(join(tmpdir(), 'persona-compat-'))
    t.after(() => rmSync(home, { recursive: true, force: true }))
    const current = join(home, '.persona')
    const legacy = join(home, '.jarvis')
    let expected = current
    if (['legacy', 'both', 'linked'].includes(kind)) mkdirSync(legacy)
    if (['current', 'both'].includes(kind)) mkdirSync(current)
    if (kind === 'linked') symlinkSync('.jarvis', current)
    if (['fresh', 'legacy', 'both', 'external', 'dangling'].includes(kind)) expected = legacy
    if (kind === 'external') { mkdirSync(join(home, 'external')); symlinkSync('external', legacy) }
    if (kind === 'dangling') symlinkSync('missing', legacy)
    assert.equal(stateDir(home, {}), expected)
    assert.equal(stateDir(home, { JARVIS_HOME: '~/profile' }), join(home, 'profile'))
    assert.equal(stateDir(home, { PERSONA_HOME: '~/current', JARVIS_HOME: '~/legacy' }), join(home, 'current'))
    assert.equal(isAssistantDir(join(home, '.jarvis', 'assistant'), home, {}), true)
    assert.equal(isAssistantDir(join(home, '.persona', 'assistant'), home, {}), true)
    assert.equal(isAssistantDir(join(home, 'profile', 'assistant'), home, { PERSONA_HOME: '~/profile' }), true)
    assert.equal(isAssistantDir(join(home, 'project', 'assistant'), home, {}), false)
  })
}
