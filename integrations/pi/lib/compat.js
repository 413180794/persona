import { existsSync } from 'node:fs'
import { join } from 'node:path'

const OLD_HOST = 'jarvis-host'
const OLD_ALLOW = 'jarvis-allow'
export const LEGACY_RELOAD = 'jarvis-reload'
export function customTypeMatches(type, canonical) {
  return type === canonical || (canonical === 'persona-host' && type === OLD_HOST) || (canonical === 'persona-allow' && type === OLD_ALLOW)
}
export function legacyCustomType(type) { return type === 'persona-host' ? OLD_HOST : type === 'persona-allow' ? OLD_ALLOW : type }
export function piConfigPath(dir) {
  const canonical = join(dir, 'persona.json')
  return existsSync(canonical) ? canonical : join(dir, 'jarvis.json')
}
export function piHostKind(env) {
  return Object.hasOwn(env, 'PERSONA_PI_HOST') ? env.PERSONA_PI_HOST : env.JARVIS_PI_HOST
}

// Old and new modules can coexist in pi's module cache. Shared accessors keep a
// later reload by either version on one process-owned slot, including false values.
export function sharedPiGlobals() {
  for (const suffix of ['Pi', 'PiHost', 'PiAskUser']) {
    const canonical = `__persona${suffix}`, legacy = `__jarvis${suffix}`
    let value = globalThis[canonical] ?? globalThis[legacy]
    if (suffix !== 'PiAskUser') {
      const other = globalThis[legacy]
      if (other && other !== value) {
        try { (suffix === 'Pi' ? other.host : other)?.stop() } catch {}
      }
    }
    for (const key of [canonical, legacy]) Object.defineProperty(globalThis, key, {
      configurable: true, enumerable: true, get: () => value, set: (next) => { value = next },
    })
  }
}
