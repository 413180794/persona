package connector

import (
	"context"

	"github.com/nwverse/persona/server/internal/connectorwire"
)

// Snapshot is a connector that knows the user's live state worth a glance: the chats waiting on them and the rest of
// today's meetings. The assistant's snapshot (what it knows when a conversation starts) and the "Today" card read it;
// it answers quickly (the hub waits a few seconds, then goes on without it).
type Snapshot interface {
	Snapshot(ctx context.Context, tz string) (SnapshotData, error)
}

// Defined in the contract package connectorwire (docs/server/architecture.md "Contract packages"); the old names stay.
type (
	SnapshotChat    = connectorwire.SnapshotChat
	SnapshotChats   = connectorwire.SnapshotChats
	SnapshotData    = connectorwire.SnapshotData
	SnapshotMeeting = connectorwire.SnapshotMeeting
)
