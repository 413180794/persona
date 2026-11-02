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

// A runtime id, as the node takes it ([A-Za-z0-9][A-Za-z0-9_.-]*, up to 128); launcher names follow the same rule.
const ID = /^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$/

/** A configured id, or the default; a bad one fails the plugin's load (a typo must not join as `dsh` silently). */
function idOf(config, key, fallback) {
  const v = config[key]
  if (v === undefined || v === null || v === '') return fallback
  if (typeof v !== 'string' || !ID.test(v)) throw new Error(`jarvis-dsh-runtime: config.${key} must match ${ID}, got ${JSON.stringify(v)}`)
  return v
}

/**
 * @param {import('@deepseek-ai/cordis').Context} ctx
 * @param {{ socket?: string, interactions?: 'auto'|'phone'|'browser', listDays?: number, maxSessions?: number,
 *   runtime?: string, name?: string, launcher?: string, phoneTools?: boolean }} [config] - runtime / name / launcher:
 *   how this DSH appears in Jarvis (default dsh / DSH / dsh); a second DSH on the same machine needs its own.
 */
export function apply(ctx, config = {}) {
  const runtimeID = idOf(config, 'runtime', 'dsh')
  const launcher = idOf(config, 'launcher', runtimeID === 'dsh' ? 'dsh' : runtimeID)
  const label = typeof config.name === 'string' && config.name.trim() !== '' ? config.name.trim() : 'DSH'
  ctx.inject(['sessionController'], (scope) => {
    const runtime = createDshRuntime({
      controller: scope.sessionController,
      on: (event, listener, options) => scope.on(event, listener, options),
      userQuestions: optional(scope, 'userQuestions'),
      workspace: optional(scope, 'workspaceRegistry'),
      registerTool: config.phoneTools === false ? undefined : (definition) => {
        const tools = optional(scope, 'tools')
        return typeof tools?.register === 'function' ? tools.register(definition) : undefined
      },
      hasLiveClient: () => {
        const gateway = optional(scope, 'typertGateway')
        return typeof gateway?.hasLiveClient === 'function' ? gateway.hasLiveClient() : true
      },
      log: (message) => scope.logger?.info?.(message),
    }, {
      runtime: runtimeID,
      name: label,
      launcher,
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
