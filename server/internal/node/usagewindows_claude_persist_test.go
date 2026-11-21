package node

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestClaudeUsagePersistence(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	five := now.Add(time.Hour).Unix()
	weekly := now.Add(48 * time.Hour).Unix()
	usage := filepath.Join(dir, "usage")
	if err := os.Mkdir(usage, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(usage, "claude.json")
	initial := fmt.Sprintf(`{"rate_limits":{"five_hour":{"used_percentage":0},"seven_day":{"used_percentage":74,"resets_at":%d}}}`, weekly)
	if err := os.WriteFile(path, []byte(initial), 0600); err != nil {
		t.Fatal(err)
	}
	n := &Node{home: t.TempDir(), dir: dir}
	event := fmt.Sprintf(`{"unifiedWindows":{"five_hour":{"utilization":0.25,"resetsAt":%d},"seven_day":{"utilization":0.64,"resetsAt":%d}}}`, five, weekly)
	n.recordClaudeUsage("claude:claude.json", json.RawMessage(event), now)
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		a, ok := parseClaudeStatusLimits(data, now, now)
		if ok && len(a.Windows) == 2 && a.Windows[0].ResetsAt != nil && a.Windows[0].UsedPercent == 25 && a.Windows[1].UsedPercent == 74 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("event not persisted: %s", data)
		}
		time.Sleep(10 * time.Millisecond)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0600 {
		t.Fatalf("mode %o", st.Mode().Perm())
	}
	restart := &Node{home: n.home, dir: dir}
	snap := restart.usageSnapshot()
	if len(snap.Accounts) != 1 || len(snap.Accounts[0].Windows) != 2 || snap.Accounts[0].Windows[0].UsedPercent != 25 {
		t.Fatalf("restart snapshot: %+v", snap)
	}
	reset := time.Unix(five, 0)
	older := map[string]proto.UsageWindow{"five_hour": {UsedPercent: 10, ResetsAt: &reset}}
	if err := persistClaudeWindows(dir, older); err != nil {
		t.Fatal(err)
	}
	st2, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(st, st2) || !st.ModTime().Equal(st2.ModTime()) {
		t.Fatal("unchanged file rewritten")
	}
}

func TestClaudeUsageConcurrentMerge(t *testing.T) {
	dir := t.TempDir()
	reset := time.Now().Add(time.Hour).Truncate(time.Second)
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := reset.Add(time.Duration(i/10) * time.Hour)
			if err := persistClaudeWindows(dir, map[string]proto.UsageWindow{"five_hour": {UsedPercent: float64(i), ResetsAt: &r}}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	data, err := os.ReadFile(filepath.Join(dir, "usage", "claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	a, ok := parseClaudeStatusLimits(data, time.Now(), time.Now())
	if !ok || len(a.Windows) != 1 || a.Windows[0].UsedPercent != 29 || a.Windows[0].ResetsAt.Unix() != reset.Add(2*time.Hour).Unix() {
		t.Fatalf("result: %s", data)
	}
}

func TestClaudeUsageOtherSourceDoesNotPersist(t *testing.T) {
	dir := t.TempDir()
	n := &Node{dir: dir}
	now := time.Now()
	n.recordClaudeUsage("claude-stream:other", json.RawMessage(fmt.Sprintf(`{"rateLimitType":"five_hour","utilization":0.3,"resetsAt":%d}`, now.Add(time.Hour).Unix())), now)
	if _, err := os.Stat(filepath.Join(dir, "usage")); !os.IsNotExist(err) {
		t.Fatalf("unexpected file: %v", err)
	}
}

func TestClaudeUsageLockDoesNotBlockAssistant(t *testing.T) {
	dir := t.TempDir()
	usage := filepath.Join(dir, "usage")
	if err := os.Mkdir(usage, 0700); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockClaudeUsage(filepath.Join(usage, "claude.lock"))
	if err != nil {
		t.Fatal(err)
	}
	held := true
	defer func() {
		if held {
			unlock()
		}
	}()
	now := time.Now()
	n := &Node{dir: dir}
	done := make(chan struct{})
	go func() {
		n.recordClaudeUsage("claude:claude.json", json.RawMessage(fmt.Sprintf(`{"rateLimitType":"five_hour","utilization":0.3,"resetsAt":%d}`, now.Add(time.Hour).Unix())), now)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stdout reader blocked on file lock")
	}
	path := filepath.Join(usage, "claude.json")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("wrote while locked: %v", err)
	}
	unlock()
	held = false
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("write did not complete after unlock")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
