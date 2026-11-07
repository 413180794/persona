// Package connectorwire is the connectors' part of the contract (docs/server/architecture.md "Contract packages",
// docs/protocol/connector-protocol.md): inbox things and replies, conversation batches, posts and snapshots as a connector's
// program, the manager and the hub pass them on. It holds only types, constants and trivial pure helpers and imports
// only the standard library and other contract packages; internal/connector, where the connectors' interfaces and
// implementations live, aliases these types.
package connectorwire
