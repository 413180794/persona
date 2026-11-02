// Runtime packages (docs/runtimes.md「自动更新」): the code of a runtime's plugin as files with one hash, the same as
// jarvisd's (server/internal/runtimepkg). The loader (loader.js) hashes the copy it ships with, keeps the versions the
// node sends in a store and checks every one before it runs it.

import { createHash, randomBytes } from 'node:crypto'
import { existsSync, mkdirSync, readdirSync, readFileSync, renameSync, rmSync, statSync, writeFileSync } from 'node:fs'
import { join, relative, sep } from 'node:path'

/**
 * The packages and the files each is made of, relative to the integrations directory (`*` only in the last segment).
 * Must stay the same as jarvisd's runtimepkg.Packages: a file one side counts and the other does not makes every
 * hello look like another version.
 */
export const PACKAGES = {
  'jarvis-dsh-runtime': ['dsh/package.json', 'dsh/lib/*.js', 'runtime-sdk/package.json', 'runtime-sdk/index.js', 'runtime-sdk/lib/*.js'],
}

/** A package name, as jarvisd takes it. */
export const NAME_RE = /^[a-z0-9][a-z0-9._-]{0,63}$/

/** Bounds a package (all its files), as jarvisd does. */
export const MAX_SIZE = 8 << 20

/** A file's path in a package: relative, slash-separated, no empty / . / .. segments. */
export function validPath(p) {
  if (typeof p !== 'string' || p === '' || p.startsWith('/') || p.includes('\\') || p.length > 256) return false
  return p.split('/').every((seg) => seg !== '' && seg !== '.' && seg !== '..')
}

/**
 * A package's hash: sha256 over its files sorted by path, each as "<path>\0<hex sha256 of the content>\n", in hex.
 * @param {{path: string, data: Buffer}[]} files
 */
export function packageHash(files) {
  const h = createHash('sha256')
  for (const f of [...files].sort((a, b) => (a.path < b.path ? -1 : a.path > b.path ? 1 : 0))) {
    h.update(`${f.path}\0${createHash('sha256').update(f.data).digest('hex')}\n`)
  }
  return h.digest('hex')
}

/**
 * The files of a package in an integrations directory, by its patterns (what the loader ships with).
 * @param {string} root - the integrations directory (where dsh/ and runtime-sdk/ are).
 * @param {string[]} patterns
 */
export function readShipped(root, patterns) {
  const files = []
  for (const pattern of patterns) {
    const at = pattern.lastIndexOf('/')
    const dir = at < 0 ? '' : pattern.slice(0, at)
    const base = pattern.slice(at + 1)
    if (!base.includes('*')) {
      const path = join(root, pattern)
      if (isFile(path)) files.push({ path: pattern, data: readFileSync(path) })
      continue
    }
    const [prefix, suffix] = base.split('*')
    let names = []
    try {
      names = readdirSync(join(root, dir))
    } catch {
      continue
    }
    for (const n of names.sort()) {
      if (!n.startsWith(prefix) || !n.endsWith(suffix) || n.length < prefix.length + suffix.length) continue
      const path = join(root, dir, n)
      if (isFile(path)) files.push({ path: dir ? `${dir}/${n}` : n, data: readFileSync(path) })
    }
  }
  return files
}

/** Every regular file under dir (a stored version), with slash-separated relative paths. */
export function readTree(dir) {
  const files = []
  const walk = (d) => {
    for (const e of readdirSync(d, { withFileTypes: true })) {
      const path = join(d, e.name)
      if (e.isDirectory()) walk(path)
      else if (e.isFile()) files.push({ path: relative(dir, path).split(sep).join('/'), data: readFileSync(path) })
    }
  }
  walk(dir)
  return files
}

function isFile(path) {
  try {
    return statSync(path).isFile()
  } catch {
    return false
  }
}

/**
 * The files of a package the node sent (runtime.package's answer), checked: name, paths, size, and that they hash to
 * what it says. Throws on anything off.
 * @param {{name: string, hash: string, files: {path: string, data: string}[]}} pkg
 * @param {string} name - the package asked for.
 */
export function verifyPackage(pkg, name) {
  if (!pkg || pkg.name !== name) throw new Error(`the node sent package ${pkg?.name}, not ${name}`)
  if (!Array.isArray(pkg.files) || pkg.files.length === 0) throw new Error(`package ${name} has no files`)
  const files = []
  const seen = new Set()
  let size = 0
  for (const f of pkg.files) {
    if (!validPath(f?.path) || seen.has(f.path) || typeof f.data !== 'string') throw new Error(`package ${name}: bad file ${JSON.stringify(f?.path)}`)
    seen.add(f.path)
    const data = Buffer.from(f.data, 'base64')
    if ((size += data.length) > MAX_SIZE) throw new Error(`package ${name} is over ${MAX_SIZE} bytes`)
    files.push({ path: f.path, data })
  }
  const hash = packageHash(files)
  if (hash !== pkg.hash) throw new Error(`package ${name}: its files hash to ${hash.slice(0, 12)}, not ${String(pkg.hash).slice(0, 12)}`)
  return { hash, files }
}

/**
 * Put a checked package in the store as <store>/<hash>/ (written aside, then renamed into place: never half a version).
 * @returns {string} its directory.
 */
export function storeVersion(store, hash, files) {
  const dir = join(store, hash)
  if (existsSync(dir)) {
    try {
      if (packageHash(readTree(dir)) === hash) return dir
    } catch {
      // unreadable: written again below
    }
    rmSync(dir, { recursive: true, force: true })
  }
  mkdirSync(store, { recursive: true, mode: 0o700 })
  const tmp = join(store, `.${hash}.${randomBytes(4).toString('hex')}.tmp`)
  try {
    for (const f of files) {
      const path = join(tmp, ...f.path.split('/'))
      mkdirSync(join(path, '..'), { recursive: true })
      writeFileSync(path, f.data, { mode: 0o644 })
    }
    renameSync(tmp, dir)
  } finally {
    rmSync(tmp, { recursive: true, force: true })
  }
  return dir
}

/** Write a small JSON file atomically. */
export function writeJSON(path, value) {
  const tmp = `${path}.${randomBytes(4).toString('hex')}.tmp`
  writeFileSync(tmp, `${JSON.stringify(value, null, 2)}\n`, { mode: 0o600 })
  renameSync(tmp, path)
}
