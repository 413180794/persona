package node

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestAssistantSendRefusesUnreachableCodexWriterBeforeLaunch(t *testing.T) {
	n := codexTestNode(t)
	withFakeHub(t, n)
	cwd := filepath.Join(n.home, ".persona", "assistant")
	home := t.TempDir() // A launcher can run in a different home from the node's default.
	rollouts := filepath.Join(home, "sessions", "2026", "11", "18")
	locks := filepath.Join(home, "thread-writer-locks")
	for _, dir := range []string{cwd, rollouts, locks} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(rollouts, "rollout-2026-11-18T00-00-00-"+testThread+".jsonl")
	data := fmt.Appendf(nil, `{"type":"session_meta","payload":{"id":%q}}`+"\n", testThread)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	n.setPath(testThread, path)
	n.state.put(testThread, sessionState{Launcher: "codex", Cwd: cwd})
	fixtureHome := t.TempDir()
	t.Setenv("HOME", fixtureHome)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(fixtureHome, ".claude"))
	t.Setenv("CODEX_HOME", n.codexHome())
	// Any regression that starts the launcher still has a scratch home and no model provider.
	if err := os.WriteFile(filepath.Join(fixtureHome, ".bashrc"), []byte("codex(){ return 1; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtureHome, ".zshrc"), []byte("codex(){ return 1; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lock, err := os.Create(filepath.Join(locks, testThread+".lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if codexWriterHeld(n.codexHome(), testThread) || !codexWriterHeld(home, testThread) {
		t.Fatal("writer probe did not distinguish the two homes")
	}
	for _, id := range []string{"first", "second"} {
		_, err := nodeCall(t, n, "session.send", map[string]any{"session_id": testThread, "text": "scratch follow-up", "client_msg_id": id})
		if code := rpcCode(err); code != codexWriterConflictCode {
			t.Fatalf("attempt %s: code %q, error %v", id, code, err)
		}
	}
	if _, err := os.Stat(n.memoryDir(cwd)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the Codex launch path prepared a memory directory: %v", err)
	}
	if !codexWriterHeld(home, testThread) {
		t.Fatal("the original writer lost its lock")
	}
	// When the node knows the owner, the same lock must allow delivery through the owner's transport.
	p, c, app := newFakeCodex(t, n, cwd)
	p.id, p.launcher, c.threadID = testThread, "codex", testThread
	p.fingerprint = n.executionFingerprint(p.launcher, p.model)
	app.mu.Lock()
	app.results["turn/start"] = map[string]any{"turn": map[string]any{"id": "scratch-turn"}}
	app.mu.Unlock()
	n.mu.Lock()
	n.procs[testThread] = p
	n.mu.Unlock()
	prompt := map[string]any{"session_id": testThread, "text": "scratch follow-up", "client_msg_id": "owner-route"}
	for range 2 {
		if _, err := nodeCall(t, n, "session.send", prompt); err != nil {
			t.Fatal(err)
		}
	}
	if len(app.sent("turn/start")) != 1 || len(app.sent("turn/steer")) != 0 || len(app.sent("thread/resume")) != 0 {
		t.Fatal("the same follow-up must be delivered once on the existing owner")
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if codexWriterHeld(home, testThread) {
		t.Fatal("an unlocked file still exists but must not count as a writer")
	}
}
