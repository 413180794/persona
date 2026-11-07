package connector

import (
	"context"

	"github.com/nwverse/persona/server/internal/connectorwire"
)

// Defined in the contract package connectorwire (docs/server/architecture.md "Contract packages"); the old names stay.
type ChatBatch = connectorwire.ChatBatch

const ArchiveMediaMost = connectorwire.ArchiveMediaMost

// Archive is a connector whose conversations go into the knowledge base by one cursor for the whole account
// (GitHub's updated_at, mail's UID, iMessage's ROWID) rather than one per conversation: Changes gives what moved
// since cursor ("" the first time) and the cursor to go on from; more says another call has more now. The hub
// keeps the cursor (cursors/<id>.json in the data dir) and ingests every batch as a chat "<id>-<chat id>".
type Archive interface {
	Changes(ctx context.Context, cursor string, limit int) (batches []ChatBatch, next string, more bool, err error)
}

// ArchiveMedia is an Archive whose messages' Media (refs the connector made) can be fetched, so the knowledge base
// keeps a conversation's pictures and files with it as it does for the hub's own sources. Optional: without it the
// lines say [attachment] (in Chinese) and nothing is kept. At most ArchiveMediaMost bytes.
type ArchiveMedia interface {
	ArchiveMedia(ctx context.Context, ref string) (Media, error)
}
