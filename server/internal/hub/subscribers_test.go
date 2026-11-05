package hub

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// The typed live pushes encode to the same bytes as the map[string]any they replaced.
func TestLivePushesEncodeAsTheirMaps(t *testing.T) {
	ts := time.Date(2026, 11, 5, 14, 30, 0, 123456789, time.UTC)
	text := "a <b> & \"c\"\n\u2028 中文 ↓"
	live := &proto.SessionLive{Verb: "Frosting", ElapsedS: 12, TokensDown: 22800, Thinking: true, At: "2026-11-05T14:30:00Z"}
	cases := []struct {
		name  string
		typed any
		old   map[string]any
	}{
		{"delta", deltaPush{Type: "delta", MachineID: "m1", SessionID: "s1", Text: text},
			map[string]any{"type": "delta", "machine_id": "m1", "session_id": "s1", "text": text}},
		{"delta empty", deltaPush{Type: "delta"},
			map[string]any{"type": "delta", "machine_id": "", "session_id": "", "text": ""}},
		{"screen", screenPush{Type: "screen", MachineID: "m1", SessionID: "s1", Text: text, Status: "working", TS: ts},
			map[string]any{"type": "screen", "machine_id": "m1", "session_id": "s1", "text": text, "status": "working", "ts": ts}},
		{"live", livePush{Type: "live", MachineID: "m1", SessionID: "s1", Live: live},
			map[string]any{"type": "live", "machine_id": "m1", "session_id": "s1", "live": live}},
		{"live cleared", livePush{Type: "live", MachineID: "m1", SessionID: "s1"},
			map[string]any{"type": "live", "machine_id": "m1", "session_id": "s1", "live": (*proto.SessionLive)(nil)}},
	}
	for _, tc := range cases {
		got, err := json.Marshal(tc.typed)
		if err != nil {
			t.Fatal(err)
		}
		want, err := json.Marshal(tc.old)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s:\n got  %s\n want %s", tc.name, got, want)
		}
	}
}

// unencodable fails the test if anything tries to encode it.
type unencodable struct{ t *testing.T }

func (u unencodable) MarshalJSON() ([]byte, error) {
	u.t.Error("encoded a push nobody follows")
	return []byte("null"), nil
}

// A delta for a session no app follows queues nothing and is never encoded; one that an app follows reaches only it.
func TestDeltaToUnsubscribedSessionQueuesNothing(t *testing.T) {
	follower := &client{out: make(chan []byte, 4), subs: map[[2]string]bool{{"m1", "s1"}: true}}
	other := &client{out: make(chan []byte, 4), subs: map[[2]string]bool{{"m1", "s2"}: true}}
	idle := &client{out: make(chan []byte, 4)}
	h := &Hub{clients: map[*client]struct{}{follower: {}, other: {}, idle: {}}}

	h.toSubscribers("m1", "s3", unencodable{t})
	h.toSubscribers("m2", "s1", unencodable{t})
	h.toSubscribersWith("m1", "s1", proto.FeatureSessionLive, unencodable{t}) // nobody asked for live
	for _, c := range []*client{follower, other, idle} {
		if n := len(c.out); n != 0 {
			t.Fatalf("queued %d messages for an unfollowed session", n)
		}
	}

	h.toSubscribers("m1", "s1", deltaPush{Type: "delta", MachineID: "m1", SessionID: "s1", Text: "hi"})
	if got, want := string(<-follower.out), `{"machine_id":"m1","session_id":"s1","text":"hi","type":"delta"}`; got != want {
		t.Errorf("follower got %s, want %s", got, want)
	}
	if len(other.out)+len(idle.out) != 0 {
		t.Error("a delta reached an app that does not follow the session")
	}
}

// benchSubscribersHub is a hub with five connected apps, the first subscribed of them following m1/s1.
func benchSubscribersHub(b *testing.B, subscribed int) *Hub {
	b.Helper()
	h := &Hub{clients: map[*client]struct{}{}}
	for i := 0; i < 5; i++ {
		c := &client{out: make(chan []byte, 1), subs: map[[2]string]bool{}}
		if i < subscribed {
			c.subs[[2]string{"m1", "s1"}] = true
		}
		h.clients[c] = struct{}{}
	}
	return h
}

func BenchmarkToSubscribersDelta(b *testing.B) {
	for _, subscribed := range []int{0, 1} {
		b.Run(fmt.Sprintf("subscribed=%d", subscribed), func(b *testing.B) {
			h := benchSubscribersHub(b, subscribed)
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				h.toSubscribers("m1", "s1", deltaPush{Type: "delta", MachineID: "m1", SessionID: "s1", Text: "chunk of streamed text"})
			}
		})
	}
}
