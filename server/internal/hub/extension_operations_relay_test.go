package hub

import (
	"encoding/json"
	"testing"
)

// TestExtensionOperationRelay: a manager's extension_operation reaches only the apps that declared
// extension_operations, and stays out of the event log (#3277); other broadcasts reach every app as before.
func TestExtensionOperationRelay(t *testing.T) {
	h := newTestHub(t)
	knows := &client{out: make(chan []byte, 4), urlFeatures: map[string]bool{"extension_operations": true}}
	old := &client{out: make(chan []byte, 4)}
	h.clients[knows], h.clients[old] = struct{}{}, struct{}{}
	seq := func() int64 {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.events == nil {
			return 0
		}
		return h.events.seq
	}
	before := seq()
	if err := h.relayBroadcast(json.RawMessage(`{"type":"extension_operation","operation":{"id":"op1"}}`)); err != nil {
		t.Fatal(err)
	}
	if len(knows.out) != 1 || len(old.out) != 0 {
		t.Fatalf("to the one that knows it only: %d %d", len(knows.out), len(old.out))
	}
	if seq() != before {
		t.Fatal("it went into the event log")
	}
	if err := h.relayBroadcast(json.RawMessage(`{"type":"extension_upgrade","extension":"x"}`)); err != nil {
		t.Fatal(err)
	}
	if len(old.out) != 1 {
		t.Fatal("another broadcast does not reach an older app")
	}
}
