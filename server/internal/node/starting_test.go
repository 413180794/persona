package node

import (
	"context"
	"encoding/json"
	"github.com/chyroc/jarvis/server/internal/host"
	"github.com/chyroc/jarvis/server/internal/transcript"
	"strings"
	"testing"
)

// Right after a message starts a session, claude has not written its transcript yet: the phone's backlog request
// must get an empty chat, not "transcript not found" (which flashed as an error on the chat page).
func TestItemsOfStartingSession(t *testing.T) {
	home := t.TempDir()
	n := &Node{home: home, paths: map[string]string{}, files: map[string]*transcript.File{}, procs: map[string]*assistantProc{},
		agents: map[string]host.Agent{}, state: &stateFile{Sessions: map[string]sessionState{}}}
	const started = "11111111-2222-3333-4444-555555555555"
	n.state.put(started, sessionState{Cwd: home + "/.jarvis/assistant"})
	res, err := n.handle(context.Background(), "session.items", json.RawMessage(`{"session_id":"`+started+`","turns":2}`))
	if err != nil {
		t.Fatalf("a starting session should not fail: %v", err)
	}
	if b, _ := json.Marshal(res); !strings.Contains(string(b), `"items":[]`) {
		t.Fatalf("want an empty page, got %s", b)
	}
	_, err = n.handle(context.Background(), "session.items", json.RawMessage(`{"session_id":"99999999-2222-3333-4444-555555555555"}`))
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("an unknown session should still fail, got %v", err)
	}
}
