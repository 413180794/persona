package node

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/osproc"
)

// keptProc starts cat as a conversation's process: what is written to it comes back as its output.
func keptProc(t *testing.T, n *Node, id, agent string) *assistantProc {
	t.Helper()
	cmd := exec.Command("cat")
	cmd.SysProcAttr = osproc.GroupAttr()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &assistantProc{id: id, agent: agent, cmd: cmd, stdin: stdin, exited: make(chan struct{}), born: time.Now()}
	liveAssistants.Store(p, struct{}{}) // as spawnClaude does
	go n.readAssistant(p, stdout)
	go func() { _ = cmd.Wait(); liveAssistants.Delete(p); close(p.exited) }()
	t.Cleanup(func() { p.stop() })
	n.procs[id] = p
	return p
}

// A resident node's connection ends (the hub restarted): its conversations go on, and the Node serving the
// next connection takes them over — what they say from then on is its own.
func TestResidentNodeKeepsConversations(t *testing.T) {
	const id, codexID = "11111111-2222-3333-4444-000000000011", "11111111-2222-3333-4444-000000000012"
	k := newAssistantKeeper()
	a, b := assistantNode(t), assistantNode(t)
	a.keeper, b.keeper = k, k
	p := keptProc(t, a, id, "claude")
	cx := keptProc(t, a, codexID, "codex")
	cx.cx = &codexConn{n: a, p: cx, key: "codex-key"}
	a.akeys = map[string]*assistantProc{"codex-key": cx}
	b.akeys = map[string]*assistantProc{}

	a.handOverAssistants()
	if len(a.procs) != 0 {
		t.Fatal("the ended connection's Node still has conversations")
	}
	if !cx.alive() {
		t.Fatal("the Codex conversation was stopped with the connection")
	}

	if !p.alive() {
		t.Fatal("the claude conversation was stopped with the connection")
	}

	b.state.put(id, sessionState{Launcher: "claude", Cwd: "/home/u/.persona/assistant"})
	adopted := k.adopt(b)
	if len(adopted) != 2 || b.procs[id] != p || b.procs[codexID] != cx {
		t.Fatalf("not taken over: %v", adopted)
	}
	if b.akeys["codex-key"] != cx || len(a.akeys) != 0 || cx.ownerOr(a) != b {
		t.Fatal("Codex MCP identity did not move to the new connection")
	}
	if _, err := p.stdin.Write([]byte(`{"type":"system","subtype":"background_tasks_changed","tasks":[{"description":"watch the deploy"}]}` + "\n")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if st, _ := b.state.get(id); len(st.Tasks) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the conversation's output did not reach the Node that took it over")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !AssistantsBusy() {
		t.Fatal("a conversation waiting on a background task is busy: a restart of the node waits for it")
	}

	b.handOverAssistants()
	k.stopAll() // the resident node itself ends
	select {
	case <-p.exited:
	case <-time.After(5 * time.Second):
		t.Fatal("stopAll left the process running")
	}
	if AssistantsBusy() {
		t.Fatal("an exited conversation is still counted")
	}
}

// A relay finds a resident node that is restarting (its socket refuses) once it is back, instead of serving the
// connection itself, also while the new node has removed the stale socket and is not listening yet; with no
// resident node at all it does not wait.
func TestRelayWaitsForARestartingResidentNode(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "attach") // unix socket paths are short
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "node-attach.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	ln.Close() // exec'ing: the socket stays, nobody answers
	go func() {
		time.Sleep(600 * time.Millisecond)
		_ = os.Remove(path)
		// As Attach does between removing the stale socket and listening, but long enough for a dial to land in it.
		time.Sleep(500 * time.Millisecond)
		ln, err := net.Listen("unix", path)
		if err != nil {
			return
		}
		defer ln.Close()
		if c, err := ln.Accept(); err == nil {
			c.Close()
		}
	}()
	c, err := dialResident(context.Background(), path, 5*time.Second)
	if err != nil {
		t.Fatalf("did not wait for the restarting node: %v", err)
	}
	c.Close()
	start := time.Now()
	if _, err := dialResident(context.Background(), filepath.Join(dir, "none.sock"), 5*time.Second); err == nil || time.Since(start) > time.Second {
		t.Fatalf("no resident node: %v after %s", err, time.Since(start))
	}
}
