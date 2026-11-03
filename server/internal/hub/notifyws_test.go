package hub

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"testing"
	"time"

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
	h := &Hub{
		clients: map[*client]struct{}{c: {}}, push: newPusher(t.TempDir(), nil, t.Logf),
		notified: map[string]time.Time{},
	}
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
	if _, err := h.runAssistantTool(context.Background(), m, "test-session", "notify_user", json.RawMessage(`{"title":"提醒","body":"任务有新进展"}`)); err != nil {
		t.Fatal(err)
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
	if h.notified["test-session"].IsZero() {
		t.Fatal("assistant notification did not suppress the later done notification")
	}
}
