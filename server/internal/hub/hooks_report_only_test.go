package hub

import (
	"encoding/json"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// A report-only hook event (node/globalhooks.go) is not Persona's hooks running in the session: the hub must not wait
// for them to ask for a card (hasHooks), but a hook of Persona's own still counts.
func TestReportOnlyHookEventDoesNotMeanPersonaHooks(t *testing.T) {
	h := newTestHub(t)
	m := newMachine(h, MachineConfig{ID: "box", Name: "box"})
	m.onEvent("hook.event", json.RawMessage(`{"session_id":"s-report","event":"Stop","report_only":true}`))
	if m.hasHooks(proto.Session{SessionID: "s-report"}) {
		t.Error("a report-only event made the hub wait for a card that never comes")
	}
	m.onEvent("hook.event", json.RawMessage(`{"session_id":"s-own","event":"Stop"}`))
	if !m.hasHooks(proto.Session{SessionID: "s-own"}) {
		t.Error("a hook event of Persona's own no longer counts")
	}
}
