package extmgr

import (
	"context"

	"github.com/nwverse/persona/server/internal/db"
)

// What a row of the ledger tells a screen about taking its message back (docs/protocol.md "Connector API"): whether it
// can be now. It is read from the same check RecallSent acts on (recallCheck), so a button never offers what the
// recall would refuse for a reason known without asking the other side. A message that ages out between the read and
// the tap is refused by the route (409 too_old) and the screen reads the ledger again.

// sentItem is a row of the ledger as the routes give it: the row, and whether a recall of it is offered now. The field
// is extra to the row's own, so a client that does not know it reads the row as before.
type sentItem struct {
	db.SentAsUser
	// CanRecall: the message can be taken back now (recallCheck finds nothing against it).
	CanRecall bool `json:"can_recall"`
}

// sentItems are rows as the routes give them, judged at the one moment now.
func (h *Manager) sentItems(ctx context.Context, rows []db.SentAsUser) []sentItem {
	now := sentLedgerNow()
	out := make([]sentItem, len(rows))
	for i, row := range rows {
		out[i].SentAsUser = row
		_, err := h.recallCheck(ctx, row, now)
		out[i].CanRecall = err == nil
	}
	return out
}
