// Persona runtime for DeepSeek Harness (docs/integrations/dsh.md): a Host plugin for the `dsh web` profile that hangs DSH's sessions
// into the personad node on this machine (~/.persona/runtime.sock, docs/integrations/runtimes.md), so the Persona apps list them, show
// their work, send to them and answer their approvals and questions. It does nothing until a personad node listens.
//
// This file is only the loader, and stays as it is: DSH keeps it for as long as the plugin is enabled. The runtime
// itself (lib/plugin.js and what it imports) comes from a versioned directory — the copy shipped next to this file, or
// the one the node sent when it carries another (docs/integrations/runtimes.md "Auto-update") — and is swapped without DSH's plugin
// manager. config.autoUpdate: take the node's versions (default: yes, except as a daemon with config.listen).

import { runtimeStorePath } from '../runtime-sdk/lib/compat.js'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { prepareSocketDirectory } from '../runtime-sdk/lib/identity.js'
import { defaultSocketPath } from '../runtime-sdk/index.js'
import { createLoader } from '../runtime-sdk/lib/loader.js'

export const name = 'persona-dsh-runtime'

/** The integrations directory this loader ships in (dsh/ and runtime-sdk/ side by side). */
const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..')

/**
 * @param {import('@deepseek-ai/cordis').Context} ctx
 * @param {object} [config] - the runtime's (lib/plugin.js start), plus autoUpdate.
 */
export function apply(ctx, config = {}) {
  ctx.inject(['sessionController'], (scope) => {
    const log = (message) => scope.logger?.info?.(message)
    const socket = typeof config.socket === 'string' && config.socket !== ''
      ? config.socket : defaultSocketPath()
    const daemon = config.listen !== undefined && config.listen !== null && config.listen !== false
    const autoUpdate = typeof config.autoUpdate === 'boolean' ? config.autoUpdate : !daemon
    let store
    if (autoUpdate) {
      try {
        store = runtimeStorePath(prepareSocketDirectory(socket), name)
      } catch (err) {
        log(`Persona runtime cache unavailable: ${err.message}`)
      }
    }
    const loader = createLoader({
      name,
      root: ROOT,
      entry: 'dsh/lib/plugin.js',
      // The versions (the shipped one too: a reload in the same process must not get cached code) live beside the
      // node's socket, in the user's own ~/.persona. Without updates (a daemon) there is none: the shipped files run in
      // place, and a new copy takes a DSH restart.
      store,
      autoUpdate,
      context: { ctx: scope, config },
      log,
    })
    scope.effect(() => {
      loader.start().catch((err) => (scope.logger?.warn ?? log)(`persona-dsh-runtime: ${err.message}`))
      return () => void loader.stop()
    }, 'persona-dsh-runtime: DSH sessions in Persona')
  })
}
