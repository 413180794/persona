package hub

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/chyroc/jarvis/server/internal/connector"
	"github.com/chyroc/jarvis/server/internal/kb"
)

// Connectors' conversations in the knowledge base (connector.Archive): each one's cursor is a file in the data dir,
// its batches are ingested like mail's, as chats "<id>-<chat id>"; compressing, tagging and projects follow as for
// any chat.

const (
	kbConnectorPoll  = 10 * time.Minute // how often a connector is asked for changes, unless it says (connector.Poller)
	kbConnectorPages = 3                // calls per connector and run
	kbConnectorLimit = 20               // conversations per call
)

// kbConnectorSource names a connector's chats for Ingest; it has nothing to download.
type kbConnectorSource struct{ id string }

func (s kbConnectorSource) Name() string                                      { return s.id }
func (kbConnectorSource) Chats(context.Context, int) ([]kb.SourceChat, error) { return nil, nil }
func (kbConnectorSource) Download(context.Context, string, string) error {
	return errors.New("no attachments")
}
func (kbConnectorSource) Messages(context.Context, kb.SourceChat, string, int) ([]kb.SourceMessage, string, bool, error) {
	return nil, "", false, nil
}

// kbConnectors archives what moved in every signed-in connector with an archive that is due (its NextPoll, else
// kbConnectorPoll, since it was last asked).
func (h *Hub) kbConnectors(ctx context.Context) {
	e := h.kb.eng
	for _, c := range h.connectors.All() {
		a, ok := c.(connector.Archive)
		if !ok || connector.StatusOf(ctx, c).State != connector.StateOK || ctx.Err() != nil {
			continue
		}
		every := kbConnectorPoll
		if p, ok := c.(connector.Poller); ok {
			every = p.NextPoll()
		}
		h.kb.mu.Lock()
		if h.kb.archivedAt == nil {
			h.kb.archivedAt = map[string]time.Time{}
		}
		due := time.Since(h.kb.archivedAt[c.ID()]) >= every
		if due {
			h.kb.archivedAt[c.ID()] = time.Now()
		}
		h.kb.mu.Unlock()
		if !due {
			continue
		}
		if _, err := archiveConnector(ctx, e, c.ID(), a); err != nil {
			h.logf("kb: %s: %v", c.ID(), err)
		}
	}
}

// legacyCursors are cursors kept before connectors had cursors/<id>.json: read once when the new file is not
// there yet (mail's per-account UIDs, in the same format), left in place.
var legacyCursors = map[string]string{"mail": "mail-cursors.json"}

type connectorCursor struct {
	Cursor string `json:"cursor"`
}

// archiveConnector reads a connector's changes from its cursor, ingests them and moves the cursor on (only past
// what was ingested: a failure reads the same again next time).
func archiveConnector(ctx context.Context, e *kb.Engine, id string, a connector.Archive) (int, error) {
	file := filepath.Join(e.Store.Dir, "cursors", id+".json")
	var cur connectorCursor
	if raw, err := os.ReadFile(file); err == nil {
		_ = json.Unmarshal(raw, &cur)
	} else if old, ok := legacyCursors[id]; ok {
		if raw, err := os.ReadFile(filepath.Join(e.Store.Dir, old)); err == nil {
			cur.Cursor = string(raw)
		}
	}
	total := 0
	for page := 0; page < kbConnectorPages; page++ {
		batches, next, more, err := a.Changes(ctx, cur.Cursor, kbConnectorLimit)
		if err != nil {
			return total, err
		}
		for _, b := range batches {
			n, err := e.Ingest(ctx, kbConnectorSource{id}, b.Chat, b.Messages)
			if err != nil {
				return total, err
			}
			total += n
		}
		cur.Cursor = next
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			return total, err
		}
		raw, _ := json.Marshal(cur)
		if err := os.WriteFile(file, raw, 0o600); err != nil {
			return total, err
		}
		if !more {
			break
		}
	}
	return total, nil
}
