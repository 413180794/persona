package hub

import (
	"encoding/json"
	"net"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// GET /api/v1/interactions stamps each card with the hub's mute setting (#4360): the answer-only watch hides a
// muted agent's or space's cards and never buzzes for them. An agent turned back on inside a muted space stays loud.
func TestInteractionsCarryMutedFlag(t *testing.T) {
	h := newTestHub(t)
	box := newMachine(h, MachineConfig{ID: "box", Name: "box"})
	box.mu.Lock()
	box.sessions = []proto.Session{
		{SessionID: "loud", SpaceName: "desk"},
		{SessionID: "agentmuted", SpaceName: "desk"},
		{SessionID: "spacemuted", SpaceName: "team"},
		{SessionID: "allowed", SpaceName: "team"},
	}
	box.mu.Unlock()
	h.fleet.mu.Lock()
	h.byID["box"] = box
	h.fleet.mu.Unlock()
	h.mutesMu.Lock()
	h.mutes = &Pins{Sessions: []string{"box/agentmuted"}, Spaces: []string{"box/team"}, Allowed: []string{"box/allowed"}}
	h.mutesMu.Unlock()

	for _, sid := range []string{"loud", "agentmuted", "spacemuted", "allowed"} {
		reportInteraction(t, h, proto.Interaction{ID: "ix_" + sid, MachineID: "box", SessionID: sid, Kind: proto.IxQuestion})
	}

	r := httptest.NewRequest("GET", "https://example.com/api/v1/interactions", nil)
	r.RemoteAddr = net.JoinHostPort(netip.AddrFrom4([4]byte{127, 0, 0, 1}).String(), "1234")
	r.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w := httptest.NewRecorder()
	h.routes().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("GET interactions: %d %s", w.Code, w.Body.String())
	}
	var raw []map[string]any
	var body struct {
		Interactions []proto.Interaction `json:"interactions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &struct {
		Interactions *[]map[string]any `json:"interactions"`
	}{Interactions: &raw}); err != nil {
		t.Fatal(err)
	}

	muted := map[string]bool{}
	for _, ix := range body.Interactions {
		muted[ix.SessionID] = ix.Muted
	}
	want := map[string]bool{"loud": false, "agentmuted": true, "spacemuted": true, "allowed": false}
	if len(muted) != len(want) {
		t.Fatalf("got %d cards, want %d: %v", len(muted), len(want), muted)
	}
	for sid, wantMuted := range want {
		if muted[sid] != wantMuted {
			t.Fatalf("%s: muted=%v, want %v", sid, muted[sid], wantMuted)
		}
	}
	// The flag is omitempty: a card that is not muted serializes exactly like before, so older clients stay untouched.
	for _, ix := range raw {
		if ix["session_id"] == "loud" {
			if _, ok := ix["muted"]; ok {
				t.Fatalf("the loud card carries muted: %v", ix)
			}
			return
		}
	}
	t.Fatal("the loud card is missing")
}
