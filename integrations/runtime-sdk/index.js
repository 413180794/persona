// Persona runtime SDK: what a runtime without a terminal (DSH, a pi extension, a platform built on DSH…) needs to plug
// into Persona the standard way (docs/integrations/runtimes.md), dialing the node's runtime.sock or, as a daemon the node dials,
// serving a WebSocket endpoint (docs/integrations/daemons.md). Testing helpers are under ./testing.

export { createRuntime, defaultSocketPath, DEFAULT_LISTEN_PATH, PROTOCOL } from './lib/runtime.js'
export { SUBPROTOCOL, tokenMatches, upgrade } from './lib/listen.js'
export { Peer, RpcError } from './lib/wire.js'
export { PAGE_TEXT, PAGE_OUTPUT, MAX_INPUT, appToolName, clipTo, clipInput, light, page, snippet, summaryOf, interactionId } from './lib/items.js'
export { createLoader, LOADER_API } from './lib/loader.js'
export { PACKAGES, packageHash, readShipped, readTree, storeVersion, validPath, verifyPackage } from './lib/package.js'
export { currentHash, installCurrent, selfPackage } from './lib/package.js'
