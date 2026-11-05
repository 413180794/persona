package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/server/internal/proto"
)

// pushed is a session-list message as an app reads it.
type pushed struct {
	Type     string          `json:"type"`
	Machine  string          `json:"machine_id"`
	Sessions []proto.Session `json:"sessions"`
	Upsert   []proto.Session `json:"upsert"`
	Remove   []string        `json:"remove"`
}

func drainPushed(c *client) []pushed {
	var out []pushed
	for len(c.out) > 0 {
		var p pushed
		_ = json.Unmarshal(<-c.out, &p)
		out = append(out, p)
	}
	return out
}

// applyPushed is what an app does with the messages: a list replaces, a delta patches.
func applyPushed(have map[string]proto.Session, msgs []pushed) map[string]proto.Session {
	for _, p := range msgs {
		switch p.Type {
		case "sessions":
			have = map[string]proto.Session{}
			for _, s := range p.Sessions {
				have[s.SessionID] = s
			}
		case "sessions_delta":
			for _, s := range p.Upsert {
				have[s.SessionID] = s
			}
			for _, id := range p.Remove {
				delete(have, id)
			}
		}
	}
	return have
}

func sessionIDs(m map[string]proto.Session) []string {
	var out []string
	for id := range m {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

func TestSessionsDelta(t *testing.T) {
	h := newTestHub(t)
	m := newMachine(h, MachineConfig{ID: "box", Name: "Box", Kind: "local"})
	h.machines, h.byID["box"] = append(h.machines, m), m
	live := proto.Session{SessionID: "s1", Status: proto.StatusWorking, LastText: "reading"}
	idle := proto.Session{SessionID: "s2", Status: proto.StatusIdle}
	hist := proto.Session{SessionID: "h1", Agent: "codex", SourceKind: "codex_history", Status: proto.StatusOffline}
	m.sessions = []proto.Session{live, idle, hist}
	old := &client{out: make(chan []byte, 64), subs: map[[2]string]bool{}, role: roleFull}
	app := &client{out: make(chan []byte, 64), subs: map[[2]string]bool{}, role: roleFull, delta: true, have: map[string]map[string][]byte{}}
	h.clients[old], h.clients[app] = struct{}{}, struct{}{}
	broadcast := func() { h.broadcastSessions(m, h.decorated(m)) }

	// Connected: the whole list, without the Codex history.
	h.snapshotSessions(app, m)
	got := drainPushed(app)
	state := applyPushed(nil, got)
	if len(got) != 1 || got[0].Type != "sessions" || !slices.Equal(sessionIDs(state), []string{"s1", "s2"}) {
		t.Fatalf("snapshot %+v", got)
	}

	// One session changes: the old app gets every session again (history too), the delta app that one.
	live.LastText = "writing"
	m.sessions = []proto.Session{live, idle, hist}
	broadcast()
	o, d := drainPushed(old), drainPushed(app)
	if len(o) != 1 || len(o[0].Sessions) != 3 {
		t.Fatalf("old app got %+v", o)
	}
	if len(d) != 1 || d[0].Type != "sessions_delta" || len(d[0].Upsert) != 1 || d[0].Upsert[0].LastText != "writing" || len(d[0].Remove) != 0 {
		t.Fatalf("delta app got %+v", d)
	}
	state = applyPushed(state, d)

	// The same again: nobody gets anything.
	broadcast()
	if o, d := drainPushed(old), drainPushed(app); len(o)+len(d) != 0 {
		t.Fatalf("unchanged list sent: %+v %+v", o, d)
	}

	// A live Codex session starts, then ends (to history, same id) while another session goes: the delta app is told
	// both, and still never gets the history it did not have.
	codex := proto.Session{SessionID: "c1", Agent: "codex", SourceKind: "codex_app_server", Status: proto.StatusWorking}
	m.sessions = []proto.Session{live, idle, hist, codex}
	broadcast()
	state = applyPushed(state, drainPushed(app))
	codex.SourceKind, codex.Status = "codex_history", proto.StatusOffline
	m.sessions = []proto.Session{live, hist, codex}
	broadcast()
	d = drainPushed(app)
	if len(d) != 1 || len(d[0].Upsert) != 1 || d[0].Upsert[0].SessionID != "c1" || !slices.Equal(d[0].Remove, []string{"s2"}) {
		t.Fatalf("delta %+v", d)
	}
	state = applyPushed(state, d)

	// Replayed over the snapshot, the deltas give the hub's list, less the history the app never had.
	want := map[string]proto.Session{}
	for _, s := range h.decorated(m) {
		if s.SessionID != "h1" {
			want[s.SessionID] = s
		}
	}
	a, _ := json.Marshal(state)
	b, _ := json.Marshal(want)
	if string(a) != string(b) {
		t.Fatalf("app has\n%s\nhub has\n%s", a, b)
	}
}

// TestSessionsDeltaOverTheSocket: an app that asks (?features=sessions_delta) gets the snapshot without history and
// then deltas; one that does not gets whole lists, history included (an older app on a newer hub).
func TestSessionsDeltaOverTheSocket(t *testing.T) {
	h := newTestHub(t)
	m := newMachine(h, MachineConfig{ID: "box", Name: "Box", Kind: "local"})
	h.machines, h.byID["box"] = append(h.machines, m), m
	m.sessions = []proto.Session{{SessionID: "s1", Status: proto.StatusWorking, LastText: "a"},
		{SessionID: "h1", Agent: "codex", SourceKind: "codex_history", Status: proto.StatusOffline}}
	srv := httptest.NewServer(h.routes())
	defer srv.Close()
	dial := func(query string) *websocket.Conn {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/ws" + query
		c, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + h.cfg.Token}}})
		if err != nil {
			t.Fatal(err)
		}
		c.SetReadLimit(1 << 20)
		return c
	}
	next := func(c *websocket.Conn, typ string) pushed {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for {
			_, b, err := c.Read(ctx)
			if err != nil {
				t.Fatalf("waiting for %s: %v", typ, err)
			}
			var p pushed
			_ = json.Unmarshal(b, &p)
			if p.Type == typ {
				return p
			}
		}
	}
	newer, older := dial("?features=sessions_delta"), dial("")
	defer newer.CloseNow()
	defer older.CloseNow()
	if p := next(newer, "sessions"); len(p.Sessions) != 1 || p.Sessions[0].SessionID != "s1" {
		t.Fatalf("delta snapshot %+v", p)
	}
	if p := next(older, "sessions"); len(p.Sessions) != 2 {
		t.Fatalf("whole snapshot %+v", p)
	}
	time.Sleep(50 * time.Millisecond) // both registered
	m.sessions = []proto.Session{{SessionID: "s1", Status: proto.StatusWorking, LastText: "b"}, m.sessions[1]}
	h.broadcastSessions(m, h.decorated(m))
	if p := next(newer, "sessions_delta"); len(p.Upsert) != 1 || p.Upsert[0].LastText != "b" {
		t.Fatalf("delta %+v", p)
	}
	if p := next(older, "sessions"); len(p.Sessions) != 2 || p.Sessions[0].LastText != "b" {
		t.Fatalf("whole list %+v", p)
	}
}

// The whole-list message is the sessions' own bytes in a list: what marshalling them would give, and nothing
// spent on checking and copying again what was just encoded.
func TestSessionsMessage(t *testing.T) {
	list := encodeSessions([]proto.Session{
		{SessionID: "s1", Title: "<b>&</b> 标题  ", LastText: strings.Repeat("x", 4<<10)},
		{SessionID: "s2", Status: proto.StatusWorking, Queued: []string{"next"}},
	})
	for _, tc := range []struct {
		machine string
		list    []sentSession
	}{{"box", list}, {"<m&\"x\">", list}, {"box", nil}} {
		raws := make([]json.RawMessage, len(tc.list))
		for i, s := range tc.list {
			raws[i] = s.raw
		}
		want, _ := json.Marshal(map[string]any{"type": "sessions", "machine_id": tc.machine, "sessions": raws})
		if got := sessionsMessage(tc.machine, tc.list); string(got) != string(want) {
			t.Fatalf("sessionsMessage(%q):\n got %s\nwant %s", tc.machine, got, want)
		}
	}

	var many []proto.Session
	for i := range 150 { // a busy machine's list (docs/server.md「推送给 App 的量」)
		many = append(many, proto.Session{SessionID: fmt.Sprintf("s%03d", i), LastText: strings.Repeat("x", 1<<10)})
	}
	list = encodeSessions(many)
	size := len(sessionsMessage("box", list))
	// Pushes are seconds apart, collections in between: whatever the encoder keeps pooled is gone by the next one.
	const runs = 20
	var total uint64
	for range runs {
		runtime.GC()
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		sessionsMessage("box", list)
		runtime.ReadMemStats(&after)
		total += after.TotalAlloc - before.TotalAlloc
	}
	if per := int(total / runs); per > size+size/4 {
		t.Fatalf("a %d-byte list message allocates %d bytes", size, per)
	}
}
