import { lstatSync, mkdirSync, readlinkSync, realpathSync, statSync, symlinkSync } from 'node:fs'
import { homedir } from 'node:os'
import { basename, dirname, isAbsolute, join, normalize, resolve } from 'node:path'

export function envValue(name, env = process.env) {
  const suffix = name.replace(/^(PERSONA|JARVIS)_/, '')
  const current = `PERSONA_${suffix}`
  const legacy = `JARVIS_${suffix}`
  return Object.hasOwn(env, current) ? env[current] : env[legacy]
}

export function stateDir(home = homedir(), env = process.env) {
  const override = envValue('PERSONA_HOME', env)
  if (override) return normalize(override === '~' ? home : override.startsWith('~/') ? join(home, override.slice(2)) : isAbsolute(override) ? override : join(home, override))
  const current = join(home, '.persona')
  const legacy = join(home, '.jarvis')
  try {
    lstatSync(legacy)
    try {
      const a = statSync(current)
      const b = statSync(legacy)
      if (a.dev === b.dev && a.ino === b.ino) return current
    } catch {}
    return legacy
  } catch (err) {
    if (err.code !== 'ENOENT') throw err
    return info(current) ? current : legacy
  }
}

export function socketDir(home = homedir(), env = process.env) {
  const root = stateDir(home, env)
  const legacy = join(home, '.jarvis')
  return root === join(home, '.persona') && sameDirectory(root, legacy) ? legacy : root
}

function info(path) {
  try { return lstatSync(path) } catch (err) { if (err.code !== 'ENOENT') throw err }
}

function isDirectory(path) {
  try { return statSync(path).isDirectory() } catch { return false }
}

function sameDirectory(a, b) {
  try {
    a = statSync(a)
    b = statSync(b)
    return a.dev === b.dev && a.ino === b.ino
  } catch { return false }
}

function alias(home, source, target, env) {
  try { symlinkSync(basename(source), target) } catch (err) {
    if (err.code === 'EEXIST') {
      if (!sameDirectory(source, target)) throw new Error('state directories changed during initialization; retry without moving either directory')
      return
    }
    if (isDirectory(stateDir(home, env))) return
    throw err
  }
  if (!sameDirectory(source, target)) throw new Error('state directories changed during initialization; retry without moving either directory')
}

function ensureDirectory(root, current, legacy) {
  if (isDirectory(root)) return
  const entry = info(root)
  if (entry?.isSymbolicLink()) {
    if (isDirectory(root)) return
    if (root !== current && root !== legacy) throw new Error(`state directory ${root} has an unavailable symlink target`)
    let raw = readlinkSync(root)
    if (!isAbsolute(raw)) raw = `${dirname(root)}/${raw}`
    while (raw.endsWith('/') || raw.endsWith('/.')) raw = raw.endsWith('/.') ? raw.slice(0, -2) : raw.slice(0, -1)
    const target = normalize(raw)
    if (![current, legacy].includes(target)) throw new Error(`state directory ${root} has an unavailable symlink target`)
    const parent = raw.slice(0, raw.lastIndexOf('/'))
    if (!parent || realpathSync.native(parent) !== realpathSync.native(dirname(target))) throw new Error(`state directory ${root} has an unavailable symlink target`)
    if (info(target)) {
      if (isDirectory(root)) return
      throw new Error(`state directory ${root} has an unavailable symlink target`)
    }
    mkdirSync(target, { recursive: true, mode: 0o700 })
  }
  mkdirSync(root, { recursive: true, mode: 0o700 })
}

export function prepareStateDir(home = homedir(), env = process.env) {
  const current = join(home, '.persona')
  const legacy = join(home, '.jarvis')
  const root = stateDir(home, env)
  if (root !== current && root !== legacy) {
    ensureDirectory(root, current, legacy)
    return root
  }
  const a = info(current)
  const b = info(legacy)
  if (!a && (!b || !b.isSymbolicLink() || isDirectory(legacy))) {
    mkdirSync(legacy, { recursive: true, mode: 0o700 })
    alias(home, legacy, current, env)
    return stateDir(home, env)
  }
  if (!b && isDirectory(current)) {
    alias(home, current, legacy, env)
    return stateDir(home, env)
  }
  ensureDirectory(root, current, legacy)
  const other = root === legacy ? current : legacy
  if (!info(other)) alias(home, root, other, env)
  return stateDir(home, env)
}

export function prepareSocketDirectory(socket, home = homedir(), env = process.env) {
  const dir = dirname(socket)
  if ([join(home, '.persona'), join(home, '.jarvis')].includes(dir)) prepareStateDir(home, {})
  else if (dir === stateDir(home, env)) prepareStateDir(home, env)
  else mkdirSync(dir, { recursive: true, mode: 0o700 })
  return dir
}

export function isAssistantDir(cwd, home = homedir(), env = process.env) {
  if (typeof cwd !== 'string') return false
  cwd = normalize(cwd)
  if (basename(cwd) !== 'assistant') return false
  const expected = join(stateDir(home, env), 'assistant')
  if (['.jarvis', '.persona'].includes(basename(dirname(cwd))) || cwd === expected) return true
  return sameDirectory(cwd, expected)
}
