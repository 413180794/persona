package node

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestOwnerFollowupToDesktopCodexInAssistantDirectory(t *testing.T) {
	n := remoteTestNode(t)
	withFakeHub(t, n)
	cwd := filepath.Join(n.dir, "assistant")
	home := n.codexHome()
	f, _ := connectRemoteFixture(t, n, home, "desktop", remoteRaw)
	dir := filepath.Join(home, "sessions", "2026", "11", "18")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-2026-11-18T00-00-00-"+remoteRaw+".jsonl")
	meta := fmt.Appendf(nil, `{"type":"session_meta","payload":{"id":%q,"originator":"Codex Desktop","source":"vscode"}}`+"\n", remoteRaw)
	if err := os.WriteFile(path, meta, 0o600); err != nil {
		t.Fatal(err)
	}
	if id := n.registerCodex(home, remoteRaw, path); id != remoteRaw {
		t.Fatalf("expected the desktop thread under its raw id, got %s", id)
	}
	n.state.put(remoteRaw, sessionState{Launcher: "codex", Cwd: cwd})
	if !n.isAssistantSession(remoteRaw) {
		t.Fatal("the fixture must take the headless-session path because of its cwd")
	}
	params := map[string]any{"session_id": remoteRaw, "text": "scratch follow-up", "client_msg_id": "desktop-follow-up", "owner_input": true}
	for range 2 {
		if _, err := nodeCall(t, n, "session.send", params); err != nil {
			t.Fatal(err)
		}
	}
	if f.count("thread/resume") != 1 || f.count("turn/start") != 1 || f.count("turn/steer") != 0 {
		t.Fatal("the already-loaded owner must receive the follow-up exactly once")
	}
}

func TestCodexResumeWriterConflictOnlyMatchesTheRequestedThread(t *testing.T) {
	message := "thread " + testThread + " already has an active writer"
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{&codexError{Code: -32600, Message: message}, true},
		{&codexError{Code: -32600, Message: "thread " + remoteRaw + " already has an active writer"}, false},
		{&codexError{Code: -32600, Message: "invalid request"}, false},
		{&codexError{Code: -32602, Message: message}, false},
		{errors.New(message), false},
	} {
		if got := codexResumeWriterConflict(tc.err, testThread); got != tc.want {
			t.Errorf("%v: got %v, want %v", tc.err, got, tc.want)
		}
	}
}
