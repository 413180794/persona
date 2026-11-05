package node

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/transcript"
)

// The streamed background-task scan finds what parsing every short marker line finds, across a line far larger than
// its read buffer and a last line that is only half written when one scan runs.
func TestBackgroundTasksStreamed(t *testing.T) {
	for _, name := range []string{"claude-notifications.jsonl", "claude-session.jsonl", "claude-subagent.jsonl"} {
		src, err := os.ReadFile(filepath.Join("..", "transcript", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		lines := bytes.Split(bytes.TrimRight(src, "\n"), []byte("\n"))
		var want transcript.Meta
		for _, l := range lines {
			if len(l) <= 64<<10 && containsAny(l, bgMarkers) {
				transcript.ParseLine(0, l, &want)
			}
		}
		huge := append(append([]byte(`{"type":"user","note":"`), bytes.Repeat([]byte("run_in_background "), 40000)...), `"}`...)
		half := len(lines) / 2
		var head bytes.Buffer
		for _, l := range lines[:half] {
			head.Write(l)
			head.WriteByte('\n')
		}
		head.Write(huge)
		head.WriteByte('\n')
		rest := append(bytes.Join(lines[half:], []byte("\n")), '\n')

		path := filepath.Join(t.TempDir(), "s.jsonl")
		n := &Node{bgScans: map[string]*bgScan{}}
		for _, upto := range []int{len(rest) / 2, len(rest)} {
			if err := os.WriteFile(path, append(bytes.Clone(head.Bytes()), rest[:upto]...), 0o600); err != nil {
				t.Fatal(err)
			}
			n.backgroundTasks(path)
		}
		if got := n.bgScans[path].meta.Background; !reflect.DeepEqual(got, want.Background) {
			t.Errorf("%s: background tasks\n got %+v\nwant %+v", name, got, want.Background)
		}
		if st, _ := os.Stat(path); n.bgScans[path].off != st.Size() {
			t.Errorf("%s: scanned up to %d of %d bytes", name, n.bgScans[path].off, st.Size())
		}
	}
}

// A big transcript's first read runs off the caller's path: the first call answers "not known yet" at once, a later
// one has the tasks, and appended lines are read in line again.
func TestBackgroundTasksBigFirstReadIsAsync(t *testing.T) {
	defer func(v int64) { bgSyncLimit = v }(bgSyncLimit)
	bgSyncLimit = 1 << 10
	src, err := os.ReadFile(filepath.Join("..", "transcript", "testdata", "claude-notifications.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var want transcript.Meta
	for _, l := range bytes.Split(bytes.TrimRight(src, "\n"), []byte("\n")) {
		if len(l) <= 64<<10 && containsAny(l, bgMarkers) {
			transcript.ParseLine(0, l, &want)
		}
	}
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, src, 0o600); err != nil {
		t.Fatal(err)
	}
	n := &Node{bgScans: map[string]*bgScan{}}
	if _, _, _, pending := n.backgroundTasks(path); !pending {
		t.Fatal("a big first read should not run on the caller's path")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, _, _, pending := n.backgroundTasks(path); !pending {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the background read never finished")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := n.bgScans[path].meta.Background; !reflect.DeepEqual(got, want.Background) {
		t.Errorf("background tasks\n got %+v\nwant %+v", got, want.Background)
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"type":"user","message":{"role":"user","content":"hi"}}` + "\n")
	f.Close()
	if _, _, _, pending := n.backgroundTasks(path); pending {
		t.Fatal("a small append should be read in line")
	}
}

// A subagent transcript that has not changed is not read again for the session list.
func TestSubagentEdgesCached(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-x.jsonl")
	write := func(text string) os.FileInfo {
		line := `{"type":"assistant","timestamp":"2026-10-29T01:00:00Z","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"` + text + `"}]}}` + "\n"
		if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
		st, _ := os.Stat(path)
		return st
	}
	st := write("one")
	_, first := subagentEdgesCached(path, st)
	_, again := subagentEdgesCached(path, st)
	if first != again {
		t.Fatalf("cached read differs: %q vs %q", first, again)
	}
	edgeCache.Lock()
	e := edgeCache.m[path]
	e.readAt = time.Time{} // as if read long ago
	edgeCache.m[path] = e
	edgeCache.Unlock()
	st = write("two, longer")
	if _, last := subagentEdgesCached(path, st); last == first && first != "" {
		t.Fatalf("a changed file should be read again, still %q", last)
	}
}

// A goal set at the start of a long transcript is still known once the start is out of the quick tail
// (docs/agent-goal.md), and its later check is read from the appended lines.
func TestBackgroundTasksKeepsGoal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	set := `{"type":"attachment","uuid":"g1","timestamp":"2026-11-05T08:00:00Z","attachment":{"type":"goal_status","met":false,"sentinel":true,"condition":"ship it"}}` + "\n"
	filler := bytes.Repeat([]byte(`{"type":"assistant","uuid":"x","message":{"role":"assistant","content":[{"type":"text","text":"working"}]}}`+"\n"), quickTail/64)
	if err := os.WriteFile(path, append([]byte(set), filler...), 0o600); err != nil {
		t.Fatal(err)
	}
	n := &Node{bgScans: map[string]*bgScan{}}
	if _, _, g, _ := n.backgroundTasks(path); g == nil || g.Objective != "ship it" || g.Status != "active" {
		t.Fatalf("goal after the first read: %+v", g)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"type":"attachment","uuid":"g2","timestamp":"2026-11-05T09:00:00Z","attachment":{"type":"goal_status","met":true,"condition":"ship it","reason":"shipped"}}` + "\n")
	f.Close()
	if _, _, g, _ := n.backgroundTasks(path); g == nil || g.Status != "complete" || g.Reason != "shipped" {
		t.Fatalf("goal after the appended check: %+v", g)
	}
}
