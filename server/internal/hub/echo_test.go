package hub

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/rpc"
)

// echoMem keeps sent_messages.echo like the table does (db.RecordEcho / Echoes).
type echoMem struct {
	*sentStore
	emu    sync.Mutex
	echoes map[string]json.RawMessage // client_msg_id -> record, for machine "box"
	order  []string
}

func (s *echoMem) RecordEcho(_ context.Context, id, machine string, echo json.RawMessage) error {
	s.emu.Lock()
	defer s.emu.Unlock()
	if machine != "box" {
		return nil
	}
	var old, now struct {
		Line string `json:"line"`
	}
	_ = json.Unmarshal(s.echoes[id], &old)
	_ = json.Unmarshal(echo, &now)
	if old.Line != "" && now.Line == "" {
		return nil
	}
	if _, ok := s.echoes[id]; !ok {
		s.order = append(s.order, id)
	}
	s.echoes[id] = echo
	return nil
}

func (s *echoMem) Echoes(_ context.Context, machine string) ([]json.RawMessage, error) {
	s.emu.Lock()
	defer s.emu.Unlock()
	var out []json.RawMessage
	for _, id := range s.order {
		out = append(out, s.echoes[id])
	}
	return out, nil
}

func (s *echoMem) count() int {
	s.emu.Lock()
	defer s.emu.Unlock()
	return len(s.order)
}

func (s *echoMem) line(id string) string {
	s.emu.Lock()
	defer s.emu.Unlock()
	var r struct {
		Line string `json:"line"`
	}
	_ = json.Unmarshal(s.echoes[id], &r)
	return r.Line
}

// The node's session.echo is kept with the send (a late "expected" record does not undo a found line), and a node
// that connects is handed what was kept (session.echoes) before anything else asks it for items.
func TestEchoesAreKeptAndHandedBack(t *testing.T) {
	h := newTestHub(t)
	store := &echoMem{sentStore: newSentStore(), echoes: map[string]json.RawMessage{}}
	h.store = store
	m := newMachine(h, MachineConfig{ID: "box", Name: "box"})
	m.onEvent("session.echo", json.RawMessage(`{"session_id":"s1","client_msg_id":"m1","text":"hi","after_seq":3,"line":"u1"}`))
	time.Sleep(50 * time.Millisecond)
	m.onEvent("session.echo", json.RawMessage(`{"session_id":"s1","client_msg_id":"m1","text":"hi","after_seq":3}`))
	m.onEvent("session.echo", json.RawMessage(`{"session_id":"s1","client_msg_id":"m2","text":"再来","after_seq":5}`))
	deadline := time.Now().Add(2 * time.Second)
	for store.count() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if store.line("m1") != "u1" {
		t.Fatal("the found line was lost")
	}

	hubIn, nodeOut := io.Pipe()
	nodeIn, hubOut := io.Pipe()
	hubPeer, nodePeer := rpc.New("h", hubOut), rpc.New("n", nodeOut)
	var mu sync.Mutex
	var got []string
	var handed []struct {
		ClientMsgID string `json:"client_msg_id"`
		Line        string `json:"line"`
	}
	nodePeer.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, method)
		if method == "session.echoes" {
			var p struct {
				Echoes json.RawMessage `json:"echoes"`
			}
			_ = json.Unmarshal(params, &p)
			_ = json.Unmarshal(p.Echoes, &handed)
		}
		return map[string]any{"ok": true}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = hubPeer.Serve(ctx, hubIn) }()
	go func() { _ = nodePeer.Serve(ctx, nodeIn) }()
	m.configureNode(ctx, hubPeer)
	mu.Lock()
	defer mu.Unlock()
	if len(handed) != 2 || handed[0].ClientMsgID != "m1" || handed[0].Line != "u1" || handed[1].ClientMsgID != "m2" {
		t.Fatalf("handed back %+v (calls %v)", handed, got)
	}
}
