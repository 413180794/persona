package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// Marking many items done (or back) in one request: 全部标为已处理 over what the list shows, 全部看过 / 全部清除 on a
// section, and their 撤销 (docs/inbox.md「全部标为已处理」). Done stays in Persona, as for one item: Feishu, the mail
// servers and the connectors are not told.

// inboxBulkMax caps one request (the list holds a few hundred items at most).
const inboxBulkMax = 1000

// Why an item of a bulk request was left as it was.
const (
	bulkSkipMissing = "missing" // no such item (forgotten, or never there)
	bulkSkipChanged = "changed" // new messages came into it after the list the user acted on
)

type inboxBulkTarget struct {
	ID string `json:"id"`
	// Version is the item's `version` as listed; empty skips the check (the item as it is now).
	Version string `json:"version,omitempty"`
}

type inboxBulkSkip struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

type inboxBulkResult struct {
	// Changed are the items this request moved (done or back), as listed now: 撤销 sends these back.
	Changed []InboxItem `json:"changed"`
	// Unchanged were already in the state asked for (a retried request, another device): nothing written.
	Unchanged []string        `json:"unchanged"`
	Skipped   []inboxBulkSkip `json:"skipped"`
}

// inboxVersion names what an item holds now (its newest message / mail / notice): it changes when something new
// comes into the item, not when it is triaged, read elsewhere or filed.
func inboxVersion(it *InboxItem) string {
	sum := sha256.Sum256([]byte(it.ID + "\x00" + it.Last))
	return hex.EncodeToString(sum[:8])
}

// setInboxDoneBulk files (done) or brings back (!done) the targets in one go, written and announced once.
// Filing is idempotent (an item already done keeps when and why it was done) and checks the version: an item that
// took new messages since the user's list stays open, so what came in meanwhile is not filed unseen. Bringing back
// does not check it (an item done meanwhile by a reply elsewhere comes back too, as with 移回 on one item).
func (h *Hub) setInboxDoneBulk(ctx context.Context, targets []inboxBulkTarget, done bool, now time.Time) inboxBulkResult {
	res := inboxBulkResult{Changed: []InboxItem{}, Unchanged: []string{}, Skipped: []inboxBulkSkip{}}
	var changed []*InboxItem
	seen := map[string]bool{}
	h.inbox.mu.Lock()
	for _, t := range targets {
		if seen[t.ID] {
			continue
		}
		seen[t.ID] = true
		it, ok := h.inbox.items[t.ID]
		switch {
		case !ok:
			res.Skipped = append(res.Skipped, inboxBulkSkip{t.ID, bulkSkipMissing})
		case done && !it.open(), !done && it.open():
			res.Unchanged = append(res.Unchanged, t.ID)
		case done && t.Version != "" && t.Version != inboxVersion(it):
			res.Skipped = append(res.Skipped, inboxBulkSkip{t.ID, bulkSkipChanged})
		default:
			if done {
				it.done(now, "user")
			} else {
				it.DoneAt, it.DoneBy = nil, ""
			}
			changed = append(changed, it)
			res.Changed = append(res.Changed, it.listed())
		}
	}
	h.inbox.mu.Unlock()
	h.saveInbox(ctx, changed)
	return res
}

var errBulkTooMany = errors.New("too many items in one request")

func (h *Hub) inboxBulkRoutes(api *http.ServeMux) {
	// Many items at once: {"done": true|false, "items": [{"id", "version"}]}.
	api.HandleFunc("POST /api/v1/inbox/done", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Done  *bool             `json:"done"`
			Items []inboxBulkTarget `json:"items"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Done == nil {
			writeErr(w, http.StatusBadRequest, errors.New("done and items are required"))
			return
		}
		if len(in.Items) > inboxBulkMax {
			writeErr(w, http.StatusRequestEntityTooLarge, errBulkTooMany)
			return
		}
		writeJSON(w, h.setInboxDoneBulk(r.Context(), in.Items, *in.Done, time.Now()))
	})
}
