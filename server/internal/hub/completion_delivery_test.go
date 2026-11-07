package hub

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// Drive the real assistant manager and its WebSocket bus through the Hub to
// both phone transports. The APNs relay and devices are local test endpoints.
func TestChildCompletionDelivery(t *testing.T) {
	h, relay, privateKey := relayHub(t)
	h.cfg.Notify.DoneDelaySeconds = 1
	if err := h.push.setPrefs("phone", NotifyPrefs{Input: true, Done: DoneAlways}); err != nil {
		t.Fatal(err)
	}
	m := newMachine(h, MachineConfig{ID: "box", Name: "Box", Kind: "local"})
	h.machines, h.byID["box"] = append(h.machines, m), m
	connectAssistant(t, h)
	app := &client{device: "phone", out: make(chan []byte, 1024)}
	h.onClientMessage(context.Background(), app, []byte(`{"type":"client_hello","features":["ws_notify"]}`))
	<-app.out // client_hello_ack
	h.mu.Lock()
	h.clients[app] = struct{}{}
	h.mu.Unlock()
	root := proto.Session{SessionID: "root", Agent: "codex", Status: proto.StatusWorking, UpdatedAt: time.Now(), Rev: 10}
	child := proto.Session{SessionID: "child", Agent: "codex", ParentSessionID: "root", RootSessionID: "root", IsSubagent: true,
		Status: proto.StatusWorking, UpdatedAt: time.Now(), Rev: 10}
	publish := func() { m.setSessions([]proto.Session{root, child}) }
	var notices []map[string]any
	check := func(want int) {
		t.Helper()
		// Longer than the configured completion delay, including the asynchronous bus delivery.
		time.Sleep(1200 * time.Millisecond)
		for len(app.out) > 0 {
			var msg map[string]any
			if err := json.Unmarshal(<-app.out, &msg); err != nil {
				t.Fatal(err)
			}
			if msg["type"] == "notify" && msg["kind"] == "done" {
				notices = append(notices, msg)
			}
		}
		relay.mu.Lock()
		calls := append(relay.got[:0:0], relay.got...)
		relay.mu.Unlock()
		if len(notices) != want || len(calls) != want {
			t.Fatalf("completion delivery: websocket=%d relay=%d, want %d", len(notices), len(calls), want)
		}
		for _, notice := range notices {
			if notice["session_id"] != "root" || notice["body"] != "All work verified" {
				t.Fatalf("unexpected websocket completion: %+v", notice)
			}
		}
		for _, call := range calls {
			if payload := opened(t, privateKey, call); payload["session_id"] != "root" {
				t.Fatalf("unexpected APNs completion: %+v", payload)
			}
		}
		t.Logf("completion delivery: websocket=%d relay=%d", len(notices), len(calls))
	}
	publish()
	root.Status, root.LastText = proto.StatusDone, "Waiting for child report"
	publish()
	check(0)
	child.Status, child.LastText = proto.StatusDone, "Child report retained"
	root.Status, root.WokeByTask = proto.StatusWorking, true
	publish()
	check(0)
	root.Status, root.LastText, root.Rev = proto.StatusDone, "All work verified", 20
	publish()
	check(1)
	root.Status, root.WokeByTask = proto.StatusWorking, false
	publish()
	root.Status = proto.StatusDone
	publish()
	check(1)
	for _, sess := range m.sessionList() {
		if sess.SessionID == "child" && sess.Status == proto.StatusDone && sess.LastText == child.LastText && sess.ParentSessionID == "root" {
			return
		}
	}
	t.Fatal("child completion state/report was lost")
}
