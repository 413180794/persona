package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestWSNotifyDone(t *testing.T) {
	if !slices.Contains(proto.Features, "ws_notify") {
		t.Fatal("hub must advertise ws_notify")
	}
	for _, tc := range []struct {
		name    string
		hello   string
		done    string
		took    time.Duration
		receive bool
	}{
		{"capable long task", `{"type":"client_hello","features":["ws_notify"]}`, DoneLong, time.Minute, true},
		{"legacy", "", DoneAlways, time.Minute, false},
		{"other capability", `{"type":"client_hello","features":["app_ping"]}`, DoneAlways, time.Minute, false},
		{"short task", `{"type":"client_hello","features":["ws_notify"]}`, DoneLong, time.Second, false},
		{"always", `{"type":"client_hello","features":["ws_notify"]}`, DoneAlways, time.Second, true},
		{"off uses authenticated device", `{"type":"client_hello","device_id":"other-device","features":["ws_notify"]}`, DoneOff, time.Hour, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &client{device: "test-device", out: make(chan []byte, 4)}
			h := &Hub{clients: map[*client]struct{}{c: {}}, push: newPusher(t.TempDir(), nil, t.Logf)}
			if err := h.push.setPrefs(c.device, NotifyPrefs{Done: tc.done}); err != nil {
				t.Fatal(err)
			}
			if tc.hello != "" {
				h.onClientMessage(context.Background(), c, []byte(tc.hello))
				<-c.out // client_hello_ack
			}
			h.notifyDone("done", "test-machine", "测试设备", "test-session", "任务完成", "", pushWants{Kind: "done", Took: tc.took, Long: time.Minute})
			select {
			case raw := <-c.out:
				if !tc.receive {
					t.Fatalf("unexpected notification: %s", raw)
				}
				var got map[string]string
				if err := json.Unmarshal(raw, &got); err != nil {
					t.Fatal(err)
				}
				want := map[string]string{
					"type": "notify", "kind": "done", "machine_id": "test-machine", "session_id": "test-session",
					"title": "任务完成", "subtitle": "测试设备", "body": "已完成",
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("notification = %v, want %v", got, want)
				}
			default:
				if tc.receive {
					t.Fatal("capable client did not receive notification without APNs configured")
				}
			}
		})
	}
}

func TestWSNotifyInteractionAndAssistant(t *testing.T) {
	c := &client{device: "test-device", out: make(chan []byte, 4)}
	h := newTestHub(t)
	h.clients, h.push = map[*client]struct{}{c: {}}, newPusher(t.TempDir(), nil, t.Logf)
	connectAssistant(t, h) // notify_user is its tool; the push is the hub's (notify.request)
	h.onClientMessage(context.Background(), c, []byte(`{"type":"client_hello","features":["ws_notify"]}`))
	<-c.out
	ix := proto.Interaction{
		ID: "test-interaction", MachineID: "test-machine", SessionID: "test-session",
		Kind: proto.IxQuestion, SessionTitle: "测试会话", Summary: "请选择",
	}
	for _, input := range []bool{false, true} {
		if err := h.push.setPrefs(c.device, NotifyPrefs{Input: input, Done: DoneOff}); err != nil {
			t.Fatal(err)
		}
		h.notifyInteraction(ix, "测试设备")
		select {
		case raw := <-c.out:
			var got map[string]string
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			if !input || got["kind"] != "interaction" || got["interaction_id"] != ix.ID || got["body"] != ix.Summary {
				t.Fatalf("interaction notification: %s", raw)
			}
		default:
			if input {
				t.Fatal("missing interaction notification")
			}
		}
	}
	// Explicit assistant notifications reach every opted-in device, as they do via APNs.
	if err := h.push.setPrefs(c.device, NotifyPrefs{Input: false, Done: DoneOff}); err != nil {
		t.Fatal(err)
	}
	m := &Machine{cfg: MachineConfig{ID: "test-machine"}}
	if r := h.callAssistantTool(context.Background(), m, "test-session", "notify_user", json.RawMessage(`{"title":"提醒","body":"任务有新进展"}`)); r.IsError {
		t.Fatal(r.Content[0].Text)
	}
	select {
	case raw := <-c.out:
		var got map[string]string
		if err := json.Unmarshal(raw, &got); err != nil || got["kind"] != "assistant" || got["title"] != "提醒" || got["subtitle"] != "Persona" || got["body"] != "任务有新进展" {
			t.Fatalf("assistant notification: %s (%v)", raw, err)
		}
	default:
		t.Fatal("missing assistant notification")
	}
	// That it holds back the conversation's own finished push is the manager's (assistant TestFinishedPush…).
}

// A phone that stopped reading is dropped without waiting for its socket to close: notifyWS sends under h.mu, and a
// close that waited on the stuck connection (5 s for the close frame, 5 s more for the reply) held every machine's
// events and every app's pushes with it.
func TestWSNotifyDropsASlowClientWithoutHoldingTheHub(t *testing.T) {
	accepted := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		go func() { _, _, _ = conn.Read(context.Background()) }() // the hub reads its side, as serveWS does
		accepted <- conn                                          // and returns: the hijacked connection stays open
	}))
	defer srv.Close()
	phone, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http"), nil) // never reads
	if err != nil {
		t.Fatal(err)
	}
	defer phone.CloseNow()
	conn := <-accepted

	c := &client{device: "test-device", conn: conn, out: make(chan []byte, 1)}
	h := &Hub{clients: map[*client]struct{}{c: {}}, push: newPusher(t.TempDir(), nil, t.Logf)}
	h.onClientMessage(context.Background(), c, []byte(`{"type":"client_hello","features":["ws_notify"]}`)) // its ack fills the queue
	notified := make(chan struct{})
	go func() {
		defer close(notified)
		h.notifyDone("done", "test-machine", "测试设备", "test-session", "任务完成", "", pushWants{Kind: "done", Took: time.Hour, Long: time.Minute})
	}()
	select {
	case <-notified:
	case <-time.After(2 * time.Second):
		t.Fatal("notifying held the hub while a slow client's socket closed")
	}

	// It is still dropped, not just left without the message: the phone gets the close (1008) and resyncs on reconnect.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, _, err := phone.Read(ctx); websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("the slow client was not dropped: %v", err)
	}
}

// A client being dropped is closed once, however many sends find its queue full meanwhile: the close waits on the
// stuck connection for up to about 10 s, and every broadcast, push and notification to it used to start another.
func TestASlowClientIsClosedOnce(t *testing.T) {
	accepted := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		go func() { _, _, _ = conn.Read(context.Background()) }() // the hub reads its side, as serveWS does
		accepted <- conn
	}))
	defer srv.Close()
	phone, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http"), nil) // never reads
	if err != nil {
		t.Fatal(err)
	}
	defer phone.CloseNow()
	c := &client{conn: <-accepted, out: make(chan []byte, 1)}
	c.out <- []byte(`{}`) // full

	before := runtime.NumGoroutine()
	for range 200 {
		c.sendJSON(map[string]any{"type": "notify"})
	}
	time.Sleep(200 * time.Millisecond) // closes started meanwhile are parked by now
	if after := runtime.NumGoroutine(); after > before+5 {
		t.Fatalf("200 sends to one stuck client left %d goroutines, up from %d: one close each", after, before)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, _, err := phone.Read(ctx); websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("the slow client was not dropped: %v", err)
	}
}
