package hub

import (
	"context"
	"encoding/json"
	"net"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
)

func TestElicitationCapabilityAndResolution(t *testing.T) {
	h := newTestHub(t)
	legacy := &client{out: make(chan []byte, 32)}
	h.clients[legacy] = struct{}{}
	ix := proto.Interaction{ID: "ix_example", MachineID: "test-machine", SessionID: "test-session", Kind: proto.IxElicitation,
		Summary: "请选择", ExpiresAt: time.Now().Add(time.Minute), Elicitation: &proto.Elicitation{Mode: "form", Message: "请选择"}}
	if r, err := h.waitInteraction(context.Background(), ix); err != nil || !r.Dismiss || len(h.pendingInteractions()) != 0 {
		t.Fatalf("legacy-only must return to terminal: %+v %v", r, err)
	}
	capable := &client{out: make(chan []byte, 32)}
	h.clients[capable] = struct{}{}
	h.onClientHello(capable, []byte(`{"platform":"android","version":"test","features":["elicitation","ws_notify"]}`))
	<-capable.out
	for _, action := range []string{"accept", "decline", "cancel", "dismiss"} {
		done := make(chan proto.Resolution, 1)
		go func() { r, _ := h.waitInteraction(context.Background(), ix); done <- r }()
		select {
		case raw := <-capable.out:
			var event struct {
				Type        string            `json:"type"`
				Interaction proto.Interaction `json:"interaction"`
			}
			if json.Unmarshal(raw, &event) != nil || event.Type != "interaction" || event.Interaction.Kind != proto.IxElicitation {
				t.Fatalf("event: %s", raw)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("capable client missed interaction")
		}
		for len(legacy.out) > 0 {
			var event map[string]any
			_ = json.Unmarshal(<-legacy.out, &event)
			if event["type"] == "interaction" || event["type"] == "notify" {
				t.Fatalf("legacy received elicitation: %v", event)
			}
		}
		joining := &client{out: make(chan []byte, 8)}
		h.sendPendingInteractions(joining, false)
		if len(joining.out) != 0 {
			t.Fatal("pre-hello snapshot leaked elicitation")
		}
		h.onClientHello(joining, []byte(`{"features":["elicitation"]}`))
		<-joining.out // ack
		if len(joining.out) != 1 {
			t.Fatal("hello must replay pending elicitation")
		}
		if err := h.resolve(ix.ID, proto.Resolution{Allow: new(bool)}); err == nil {
			t.Fatal("invalid action consumed request")
		}
		want := proto.Resolution{Action: action, Dismiss: action == "dismiss", Content: map[string]any{"value": true}}
		if err := h.resolve(ix.ID, want); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-done:
			if got.Action != want.Action || got.Dismiss != want.Dismiss {
				t.Fatalf("resolution: %+v", got)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("hook did not get resolution")
		}
		for len(capable.out) > 0 {
			<-capable.out
		}
	}
	h.interactions[ix.ID] = &pending{ix: ix}
	for _, tc := range []struct {
		header string
		count  int
	}{{"", 0}, {"ios/test", 0}, {"android/test", 1}} {
		r := httptest.NewRequest("GET", "https://example.com/api/v1/interactions", nil)
		r.RemoteAddr = net.JoinHostPort(netip.AddrFrom4([4]byte{127, 0, 0, 1}).String(), "1234")
		r.Header.Set("Authorization", "Bearer "+h.cfg.Token)
		r.Header.Set("X-Jarvis-Client", tc.header)
		w := httptest.NewRecorder()
		h.routes().ServeHTTP(w, r)
		var body struct {
			Interactions []proto.Interaction `json:"interactions"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil || len(body.Interactions) != tc.count {
			t.Fatalf("REST %q: %d %s", tc.header, w.Code, w.Body.String())
		}
	}
}
