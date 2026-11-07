import { assistantRootName, defaultStateRoot, legacyEnvName } from './compat.js'
import { lstatSync, mkdirSync, statSync } from 'node:fs'
import { homedir } from 'node:os'
import { basename, dirname, isAbsolute, join, normalize } from 'node:path'

export function envValue(name, env = process.env) {
  name = name.startsWith('PERSONA_') ? name : `PERSONA_${name}`
  return Object.hasOwn(env, name) ? env[name] : env[legacyEnvName(name)]
}

export function stateDir(home = homedir(), env = process.env) {
  const override = envValue('PERSONA_HOME', env)
  if (!override) return defaultStateRoot(home)
  return normalize(override === '~' ? home : override.startsWith('~/') ? join(home, override.slice(2)) : isAbsolute(override) ? override : join(home, override))
}

export function socketDir(home = homedir(), env = process.env) {
  return stateDir(home, env)
}

function sameDirectory(a, b) {
  try {
    a = statSync(a)
    b = statSync(b)
    return a.dev === b.dev && a.ino === b.ino
  } catch { return false }
}

function ensureDirectory(root) {
  let entry
  try { entry = lstatSync(root) } catch (err) { if (err.code !== 'ENOENT') throw err }
  if (entry?.isSymbolicLink()) {
    try { if (statSync(root).isDirectory()) return } catch {}
    throw new Error('unavailable symlink target; restore it before upgrading')
  }
  mkdirSync(root, { recursive: true, mode: 0o700 })
}

export function prepareStateDir(home = homedir(), env = process.env) {
  const root = stateDir(home, env)
  ensureDirectory(root)
  return root
}

export function prepareSocketDirectory(socket, home = homedir(), env = process.env) {
  const dir = dirname(socket)
  ensureDirectory(dir)
  return dir
}

export function isAssistantDir(cwd, home = homedir(), env = process.env) {
  if (typeof cwd !== 'string') return false
  cwd = normalize(cwd)
  if (basename(cwd) !== 'assistant') return false
  const expected = join(stateDir(home, env), 'assistant')
  return assistantRootName(basename(dirname(cwd))) || cwd === expected || sameDirectory(cwd, expected)
}
