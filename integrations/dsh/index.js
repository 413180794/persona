// Jarvis runtime for DeepSeek Harness (docs/dsh.md): a Host plugin for the `dsh web` profile that hangs DSH's sessions
// into the jarvisd node on this machine (~/.jarvis/runtime.sock, docs/runtimes.md), so the Jarvis apps list them, show
// their work, send to them and answer their approvals and questions. It does nothing until a jarvisd node listens.

import { createDshRuntime } from './lib/dsh.js'

export const name = 'jarvis-dsh-runtime'

/** Read an optional service without declaring it (absent in profiles without it). */
function optional(ctx, key) {
  try {
    return typeof ctx.get === 'function' ? ctx.get(key) : undefined
  } catch {
    return undefined
  }
}

/**
 * @param {import('@deepseek-ai/cordis').Context} ctx
 * @param {{ socket?: string, interactions?: 'auto'|'phone'|'browser', listDays?: number, maxSessions?: number }} [config]
 */
export function apply(ctx, config = {}) {
  ctx.inject(['sessionController'], (scope) => {
    const runtime = createDshRuntime({
      controller: scope.sessionController,
      on: (event, listener, options) => scope.on(event, listener, options),
      userQuestions: optional(scope, 'userQuestions'),
      hasLiveClient: () => {
        const gateway = optional(scope, 'typertGateway')
        return typeof gateway?.hasLiveClient === 'function' ? gateway.hasLiveClient() : true
      },
      log: (message) => scope.logger?.info?.(message),
    }, {
      socketPath: typeof config.socket === 'string' && config.socket !== '' ? config.socket : undefined,
      interactions: ['auto', 'phone', 'browser'].includes(config.interactions) ? config.interactions : 'auto',
      listDays: Number.isFinite(config.listDays) && config.listDays > 0 ? config.listDays : 7,
      maxSessions: Number.isFinite(config.maxSessions) && config.maxSessions > 0 ? config.maxSessions : 80,
    })
    scope.effect(() => {
      runtime.start()
      return () => runtime.stop()
    }, 'jarvis-dsh-runtime: DSH sessions in Jarvis')
  })
}
