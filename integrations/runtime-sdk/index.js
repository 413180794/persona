// Jarvis runtime SDK: what a runtime without a terminal (DSH, a pi extension, a platform built on DSH…) needs to plug
// into Jarvis the standard way (docs/runtimes.md). Testing helpers are under ./testing.

export { createRuntime, defaultSocketPath, PROTOCOL } from './lib/runtime.js'
export { Peer, RpcError } from './lib/wire.js'
export { PAGE_TEXT, PAGE_OUTPUT, MAX_INPUT, appToolName, clipTo, clipInput, light, page, snippet, summaryOf, interactionId } from './lib/items.js'
