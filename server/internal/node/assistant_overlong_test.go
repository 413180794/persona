package node

import (
	"context"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/osproc"
)

// A stream-json line longer than the scanner takes (a big image or tool result) ends the read: the process is
// stopped, instead of blocking on its next write with the conversation left answering and the node busy for good.
func TestAssistantOverlongLineStopsTheProcess(t *testing.T) {
	fakeClaudeHome(t, "read line\necho '{\"type\":\"system\",\"subtype\":\"init\"}'\nhead -c 17000000 /dev/zero | tr '\\0' a\necho\nsleep 30\n")
	n := codexTestNode(t)
	withFakeHub(t, n)
	id, err := n.assistantSend(context.Background(), "", t.TempDir(), "claude", "hello", nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer n.stopAssistant(id)
	n.mu.Lock()
	p := n.procs[id]
	n.mu.Unlock()
	if p == nil {
		t.Fatal("no process")
	}
	select {
	case <-p.exited:
	case <-time.After(10 * time.Second):
		t.Fatal("the process that printed an overlong line is still running")
	}
	waitFor(t, "the node no longer busy", func() bool { return !AssistantsBusy() })
}

// The same for the Codex app-server: an overlong line stops its process, which the exit goroutine then cleans up.
func TestCodexOverlongLineStopsTheProcess(t *testing.T) {
	old := codexLineMax
	codexLineMax = 128 * 1024 // above the scanner's 64 KiB first buffer, which also caps a line
	t.Cleanup(func() { codexLineMax = old })
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("no sleep")
	}
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = osproc.GroupAttr() // stop ends its process group, as spawnCodex starts it
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	inR, inW := io.Pipe()
	defer inW.Close()
	go func() { _, _ = io.Copy(io.Discard, inR) }()
	p := &assistantProc{agent: "codex", launcher: "cx_test", cmd: cmd, stdin: inW, exited: exited}
	c := &codexConn{n: codexTestNode(t), p: p, key: "proc-test"}
	done := make(chan struct{})
	go func() {
		c.read(strings.NewReader("{" + strings.Repeat("a", 256*1024) + "}\n"))
		close(done)
	}()
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("the app-server that printed an overlong line is still running")
	}
	<-done
}
