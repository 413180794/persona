package connector

import (
	"context"

	"github.com/nwverse/persona/server/internal/kb"
)

// ChatBatch is one conversation's new messages, oldest first.
type ChatBatch struct {
	Chat     kb.SourceChat      `json:"chat,omitempty"`
	Messages []kb.SourceMessage `json:"messages,omitempty"`
}

// Archive is a connector whose conversations go into the knowledge base by one cursor for the whole account
// (GitHub's updated_at, mail's UID, iMessage's ROWID) rather than one per conversation: Changes gives what moved
// since cursor ("" the first time) and the cursor to go on from; more says another call has more now. The hub
// keeps the cursor (cursors/<id>.json in the data dir) and ingests every batch as a chat "<id>-<chat id>".
type Archive interface {
	Changes(ctx context.Context, cursor string, limit int) (batches []ChatBatch, next string, more bool, err error)
}
