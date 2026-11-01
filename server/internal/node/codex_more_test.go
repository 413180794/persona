package node

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
)

func TestCodexRemotePendingReplayWhileJoining(t *testing.T) {
	n := remoteTestNode(t)
	hub := withFakeHub(t, n)
	home := t.TempDir()
	f, _ := connectRemoteFixture(t, n, home, "one", remoteRaw)
	f.mu.Lock()
	f.resumeRequest = true
	f.mu.Unlock()
	id := n.registerCodex(home, remoteRaw, "")
	if err := n.attachCodex(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "replayed approval answered", func() bool { return f.replyCount() == 1 })
	f.push(map[string]any{"id": 71, "method": "item/commandExecution/requestApproval", "params": map[string]any{"threadId": remoteRaw, "itemId": "cmd-1", "command": "printf synthetic"}})
	time.Sleep(80 * time.Millisecond)
	if f.replyCount() != 1 || len(hub.interactions()) != 1 {
		t.Fatal("replayed request was answered twice")
	}
}

func TestCodexRemoteThreadDeltasAndWrappedItem(t *testing.T) {
	n := remoteTestNode(t)
	home := t.TempDir()
	f, c := connectRemoteFixture(t, n, home, "one", remoteRaw, remoteOther)
	for raw, text := range map[string]string{remoteRaw: "第一线程", remoteOther: "第二线程"} {
		f.push(map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"threadId": raw, "delta": text}})
	}
	waitFor(t, "separate thread deltas", func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.threads[remoteRaw].partial == "第一线程" && c.threads[remoteOther].partial == "第二线程"
	})
	f.push(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": remoteOther, "item": map[string]any{"payload": map[string]any{"type": "agentMessage", "id": "message2", "text": "第二线程完成"}}}})
	waitFor(t, "wrapped item decoded", func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.threads[remoteOther].partial == "第二线程完成"
	})
	c.mu.Lock()
	first := c.threads[remoteRaw].partial
	c.mu.Unlock()
	if first != "第一线程" {
		t.Fatal("second thread overwrote first")
	}
}

func TestCodexStdioOpenRolloutIsVisibleButNeverWritable(t *testing.T) {
	n := remoteTestNode(t)
	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "2026", "11", "01")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-synthetic-"+remoteRaw+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	n.codices.scan = func() []codexProcess { return []codexProcess{{pid: os.Getpid(), home: home, endpoint: "stdio://"}} }
	n.discoverCodex()
	id := n.registerCodex(home, remoteRaw, path)
	list := n.codexSessions(map[string]bool{})
	for _, s := range list {
		if s.SessionID == id {
			if s.Status != proto.StatusUnknown || s.Capabilities.Send {
				t.Fatalf("stdio runtime capability: %+v", s)
			}
			return
		}
	}
	t.Fatal("open stdio rollout omitted")
}
