import { existsSync } from 'node:fs'
import { join } from 'node:path'
import { MCP_SERVER_NAMES, defaultStateRoot, legacyEnvName } from '../../runtime-sdk/lib/compat.js'
import { stateDir } from '../../runtime-sdk/lib/identity.js'

export function daemonCandidates(home, options) {
  const roots = [stateDir(home), join(home, '.persona'), join(home, '.jarvis')]
  return [options.personadBin, options.jarvisdBin, ...roots.flatMap((root) => [join(root, 'bin', 'personad'), join(root, 'bin', 'jarvisd')])]
}
export function daemonBinary(home, options) {
  return daemonCandidates(home, options).find((p) => p && existsSync(p)) || join(defaultStateRoot(home), 'bin', 'personad')
}
export function mcpServers(bin, pid, assistant) {
  const env = []
  for (const [name, value] of [['PERSONA_SESSION_ID', pid], ...(assistant ? [['PERSONA_ASSISTANT', '1']] : [])]) {
    env.push({ name, value }, { name: legacyEnvName(name), value })
  }
  return MCP_SERVER_NAMES.map((name) => ({ name, command: bin, args: ['mcp'], env }))
}
export function assistantSearchPaths(home) {
  return [...new Set([join(stateDir(home), 'assistant', '.bin'), join(home, '.jarvis', 'assistant', '.bin'), join(home, '.local', 'bin')])]
}
