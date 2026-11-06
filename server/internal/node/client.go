package node

import "github.com/nwverse/persona/server/internal/nodeclient"

// sockRequest is a line on the node socket: the clients' type (internal/nodeclient), the node serving it.
type sockRequest = nodeclient.Request
