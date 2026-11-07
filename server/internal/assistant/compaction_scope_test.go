package assistant

import (
	"context"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestMainCompactionScope(t *testing.T) {
	ctx := context.Background()
	f := newFakeHub().withMachine()
	s := newTestService(t, f)
	for _, main := range []bool{false, true} {
		sess, err := s.start(ctx, StartParams{Main: main, Text: "hello"})
		if err != nil {
			t.Fatal(err)
		}
		if got := f.lastSend(t)["is_main"]; got != main {
			t.Fatalf("start main=%v: is_main=%v", main, got)
		}
		f.setSessions("server", proto.Session{SessionID: sess.SessionID, Assistant: true})
		params, err := s.SendParams(ctx, SendParams{MachineID: "server", SessionID: sess.SessionID, Params: map[string]any{"text": "next", "is_main": !main}})
		if err != nil {
			t.Fatal(err)
		}
		if params["is_main"] != main {
			t.Fatalf("send main=%v: %v", main, params)
		}
		if main {
			wrong, err := s.SendParams(ctx, SendParams{MachineID: "other", SessionID: sess.SessionID, Params: map[string]any{"is_main": true}})
			if err != nil || wrong["is_main"] != false {
				t.Fatalf("wrong machine: %v %v", wrong, err)
			}
			setSettings(t, s, func(v *Settings) { v.Main = "replacement" })
			former, err := s.SendParams(ctx, SendParams{MachineID: "server", SessionID: sess.SessionID, Params: map[string]any{}})
			if err != nil || former["is_main"] != false {
				t.Fatalf("former main: %v %v", former, err)
			}
		}
	}
}
