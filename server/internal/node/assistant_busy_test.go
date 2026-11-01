package node

import (
	"strings"
	"testing"

	"github.com/chyroc/jarvis/server/internal/host"
	"github.com/chyroc/jarvis/server/internal/transcript"
)

// A background task's notification starts a turn of its own; its result must not leave the conversation marked
// finished while claude goes on with the message queued behind it.
func TestAssistantBusyAcrossQueuedTurns(t *testing.T) {
	n := &Node{home: t.TempDir(), paths: map[string]string{}, files: map[string]*transcript.File{}, procs: map[string]*assistantProc{},
		agents: map[string]host.Agent{}, state: &stateFile{Sessions: map[string]sessionState{}}}
	p := &assistantProc{id: "11111111-2222-3333-4444-555555555555", busy: true}
	busy := func() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.busy }
	stream := func(lines ...string) { n.readAssistant(p, strings.NewReader(strings.Join(lines, "\n")+"\n")) }

	stream(`{"type":"stream_event","event":{"type":"message_start"}}`, `{"type":"result","subtype":"success"}`)
	if busy() {
		t.Fatal("a finished turn should clear busy")
	}
	stream(`{"type":"stream_event","event":{"type":"message_start"}}`)
	if !busy() {
		t.Fatal("a turn claude starts on its own (the queued message) should mark the conversation busy again")
	}
	stream(`{"type":"result","subtype":"success"}`)
	if busy() {
		t.Fatal("its result should clear busy")
	}
}
