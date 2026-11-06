// Bundles src/bridge.mjs and the Agent SDK into dist/bridge.mjs, the one file the node runs (README.md "Build").
// The SDK's Claude Code binary (its optional per-platform packages) stays out: the node passes the machine's claude
// as pathToClaudeCodeExecutable.

import { readFileSync, statSync } from 'node:fs'
import { build } from 'esbuild'

const sdk = JSON.parse(readFileSync(new URL('./node_modules/@anthropic-ai/claude-agent-sdk/package.json', import.meta.url)))

await build({
  entryPoints: ['src/bridge.mjs'],
  outfile: 'dist/bridge.mjs',
  bundle: true,
  platform: 'node',
  format: 'esm',
  target: 'node22',
  minify: true,
  legalComments: 'none',
  external: ['@anthropic-ai/claude-agent-sdk-*'],
  define: { __SDK_VERSION__: JSON.stringify(sdk.version) },
  banner: {
    js: `// Built by build.mjs from src/ and @anthropic-ai/claude-agent-sdk ${sdk.version} (© Anthropic PBC, see its LICENSE.md); do not edit.\nimport { createRequire as __cr } from 'node:module'; const require = __cr(import.meta.url);`,
  },
  logLevel: 'warning',
})
console.log(`dist/bridge.mjs: ${(statSync('dist/bridge.mjs').size / 1024).toFixed(0)} KiB, SDK ${sdk.version}`)
