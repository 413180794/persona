//go:build linux || darwin

package devices

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nwverse/persona/server/internal/admission"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/nodeproto"
	"github.com/nwverse/persona/server/internal/rpc"
)

type receiptStateStore struct {
	nodeStates
	current  string
	writeErr error
}

func (s *receiptStateStore) NodeSessionReceipt(context.Context, string, string) (string, error) {
	return s.current, nil
}

func (s *receiptStateStore) PutNodeSessionReceipt(_ context.Context, _, _, previous, revision string, _ db.NodeSession) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	if previous != s.current {
		return db.ErrNodeSessionRevision
	}
	s.current = revision
	return nil
}

func TestNodeStateReceiptPausedWhileLegacyProtocolStillWrites(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "admission")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	g, err := admission.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Control("init", ""); err != nil {
		t.Fatal(err)
	}
	st := &receiptStateStore{}
	d := New(Options{Store: st, Admission: g})
	m := &Machine{d: d, cfg: MachineConfig{ID: "test-node"}}
	raw := json.RawMessage(`{"session_id":"s1","state":{"launcher":"claude"},"revision":"00000000000000000000000000000001"}`)
	st.writeErr = errors.New("rollback transaction")
	if _, err := m.onRequest(t.Context(), nodeproto.MethodStatePutReceipt, raw); err == nil || st.current != "" {
		t.Fatal("server acknowledged a failed database write")
	}
	st.writeErr = nil
	out, err := m.onRequest(t.Context(), nodeproto.MethodStatePutReceipt, raw)
	if err != nil || out.(nodeproto.StateRevision).Revision != st.current || st.current == "" {
		t.Fatalf("successful write lacked its receipt: %v %v", out, err)
	}
	if _, err := g.Control("close", "node-fence"); err != nil {
		t.Fatal(err)
	}
	var paused *rpc.Error
	if _, err := m.onRequest(t.Context(), nodeproto.MethodStatePutReceipt, raw); !errors.As(err, &paused) || paused.Code != nodeproto.CodeStatePaused {
		t.Fatalf("paused node state call returned %v", err)
	}
	// This old transport cannot tell that a notification has been persisted.
	// The production release must account for it independently.
	m.onEvent("state.put", raw)
	if st.puts != 1 {
		t.Fatalf("expected the compatibility notification to still reach the store, got %d", st.puts)
	}
}
