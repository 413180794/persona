package hub

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// marshalledDelta is how sessionsDelta used to build its message and the map it returns, kept as the reference the
// hand-built bytes are checked against.
func marshalledDelta(machine string, list []sentSession, have map[string][]byte) ([]byte, map[string][]byte) {
	shown := forDelta(list, have)
	next := make(map[string][]byte, len(shown))
	upsert, remove := []json.RawMessage{}, []string{}
	for _, s := range shown {
		next[s.id] = s.raw
		if old, ok := have[s.id]; !ok || !bytes.Equal(old, s.raw) {
			upsert = append(upsert, s.raw)
		}
	}
	for id := range have {
		if _, ok := next[id]; !ok {
			remove = append(remove, id)
		}
	}
	if len(upsert) == 0 && len(remove) == 0 {
		return nil, next
	}
	slices.Sort(remove)
	b, _ := json.Marshal(map[string]any{"type": "sessions_delta", "machine_id": machine, "upsert": upsert, "remove": remove})
	return b, next
}

func haveOf(list []sentSession) map[string][]byte {
	have := map[string][]byte{}
	for _, s := range forDelta(list, nil) {
		have[s.id] = s.raw
	}
	return have
}

func TestSessionsDeltaBytes(t *testing.T) {
	live := proto.Session{SessionID: "s1", Status: proto.StatusWorking, LastText: "reading <a> & \"b\""}
	idle := proto.Session{SessionID: "s2", Status: proto.StatusIdle}
	hist := proto.Session{SessionID: "h1", Agent: "codex", SourceKind: "codex_history", Status: proto.StatusOffline}
	odd := proto.Session{SessionID: "q\"<&> ", Status: proto.StatusIdle}
	base := encodeSessions([]proto.Session{live, idle, hist, odd})
	changed := live
	changed.LastText = "writing"
	ended := proto.Session{SessionID: "s2", Agent: "codex", SourceKind: "codex_history", Status: proto.StatusOffline}

	cases := []struct {
		name    string
		machine string
		list    []sentSession
		have    map[string][]byte
		same    bool // nothing changed: have comes back as is
	}{
		{"first push, all new", "box", base, nil, false},
		{"add", "box", encodeSessions([]proto.Session{live, idle, hist, odd, {SessionID: "s3"}}), haveOf(base), false},
		{"change", "box", encodeSessions([]proto.Session{changed, idle, hist, odd}), haveOf(base), false},
		{"remove, ids escaped", "m\"<1>", encodeSessions([]proto.Session{live, hist}), haveOf(base), false},
		{"add and remove", "box", encodeSessions([]proto.Session{{SessionID: "s0"}, live}), haveOf(base), false},
		{"history skipped", "box", encodeSessions([]proto.Session{live, idle, hist, odd, {SessionID: "h2", SourceKind: "codex_history"}}), haveOf(base), true},
		{"live session ended to history", "box", encodeSessions([]proto.Session{live, ended, hist, odd}), haveOf(base), false},
		{"no-op", "box", base, haveOf(base), true},
		{"empty list, empty have", "box", nil, map[string][]byte{}, true},
		{"everything gone", "box", nil, haveOf(base), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wantMsg, wantNext := marshalledDelta(c.machine, c.list, maps.Clone(c.have))
			gotMsg, gotNext := sessionsDelta(c.machine, c.list, c.have)
			if !bytes.Equal(gotMsg, wantMsg) {
				t.Fatalf("message\n got %s\nwant %s", gotMsg, wantMsg)
			}
			if !maps.EqualFunc(gotNext, wantNext, bytes.Equal) {
				t.Fatalf("next %v, want %v", gotNext, wantNext)
			}
			if c.same && (gotMsg != nil || fmt.Sprintf("%p", gotNext) != fmt.Sprintf("%p", c.have)) {
				t.Fatalf("unchanged list: want (nil, have), got %s and a new map", gotMsg)
			}
		})
	}
}

// benchDelta is a 150-session list as a delta app has it, and the same list with one session changed.
func benchDelta() (have map[string][]byte, same, one []sentSession) {
	var list []proto.Session
	for i := range 150 {
		list = append(list, proto.Session{SessionID: fmt.Sprintf("s%03d", i), Status: proto.StatusIdle, Title: "title",
			LastText: strings.Repeat("x", 600), Cwd: "/Users/me/projects/app", UpdatedAt: time.Unix(int64(i), 0)})
	}
	same = encodeSessions(list)
	list[7].LastText = strings.Repeat("y", 600)
	return haveOf(same), same, encodeSessions(list)
}

func BenchmarkSessionsDelta(b *testing.B) {
	have, same, one := benchDelta()
	b.Run("unchanged", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sessionsDelta("box", same, have)
		}
	})
	b.Run("one-changed", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sessionsDelta("box", one, have)
		}
	})
}

// BenchmarkSessionsDeltaMarshalled is the same with the old builder, for comparison.
func BenchmarkSessionsDeltaMarshalled(b *testing.B) {
	have, same, one := benchDelta()
	b.Run("unchanged", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			marshalledDelta("box", same, have)
		}
	})
	b.Run("one-changed", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			marshalledDelta("box", one, have)
		}
	})
}
