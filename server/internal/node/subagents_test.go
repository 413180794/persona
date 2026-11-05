package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Nested subagents name their launcher (parentAgentId) and forks say so (isFork); one whose parent is past the
// cutoff is still listed, naming the parent the list leaves out.
func TestSubagentsLaunchRelation(t *testing.T) {
	const parent = "aaaaaaaa-0000-0000-0000-000000000001"
	home := t.TempDir()
	tp := filepath.Join(home, ".claude", "projects", "-src-app", parent+".jsonl")
	dir := filepath.Join(strings.TrimSuffix(tp, ".jsonl"), "subagents")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tp, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	write := func(id, meta string, age time.Duration) {
		t.Helper()
		at := now.Add(-age)
		line := `{"type":"user","timestamp":"` + at.UTC().Format(time.RFC3339) + `","isSidechain":true}` + "\n"
		jp := filepath.Join(dir, "agent-"+id+".jsonl")
		if os.WriteFile(jp, []byte(line), 0o600) != nil || os.WriteFile(filepath.Join(dir, "agent-"+id+".meta.json"), []byte(meta), 0o600) != nil {
			t.Fatal("write subagent files")
		}
		if err := os.Chtimes(jp, at, at); err != nil {
			t.Fatal(err)
		}
	}
	write("a1", `{"agentType":"general-purpose","description":"示例：主任务","toolUseId":"toolu_a1","spawnDepth":1}`, time.Minute)
	write("b2", `{"agentType":"Explore","description":"示例：已结束","finished":true,"spawnDepth":1}`, 2*time.Minute)
	write("c3", `{"agentType":"general-purpose","finished":true,"parentAgentId":"a1","spawnDepth":2}`, 3*time.Minute)
	write("e5", `{"agentType":"fork","finished":true,"parentAgentId":"c3","spawnDepth":3,"isFork":true}`, 4*time.Minute)
	write("old0", `{"agentType":"general-purpose","finished":true,"spawnDepth":1}`, 25*time.Hour)
	write("kid9", `{"agentType":"Explore","finished":true,"parentAgentId":"old0","spawnDepth":2}`, 5*time.Minute)

	n := &Node{home: home, paths: map[string]string{parent: tp}}
	subs := n.subagents(parent)
	got := map[string]string{}
	for _, a := range subs {
		got[a.ID] = a.ParentID
		if a.Fork != (a.ID == "e5") {
			t.Fatalf("%s fork = %v", a.ID, a.Fork)
		}
	}
	want := map[string]string{"a1": "", "b2": "", "c3": "a1", "e5": "c3", "kid9": "old0"}
	if len(got) != len(want) {
		t.Fatalf("listed %v, want %v", got, want)
	}
	for id, p := range want {
		if q, ok := got[id]; !ok || q != p {
			t.Fatalf("%s: parent %q (listed %v), want %q", id, q, ok, p)
		}
	}
	if subs[0].ID != "a1" || !subs[0].Running {
		t.Fatalf("the running one first: %+v", subs[0])
	}
	// On the wire the relation is left out for the session's own subagents.
	for _, a := range subs {
		b, _ := json.Marshal(a)
		s := string(b)
		switch a.ID {
		case "a1":
			if strings.Contains(s, "parent_id") || strings.Contains(s, `"fork"`) {
				t.Fatalf("a top-level subagent carries the relation: %s", s)
			}
		case "e5":
			if !strings.Contains(s, `"parent_id":"c3"`) || !strings.Contains(s, `"fork":true`) {
				t.Fatalf("a nested fork without its relation: %s", s)
			}
		}
	}
}

// subagentTree makes a session transcript with an empty subagents directory and returns the node, the session id
// and the directory.
func subagentTree(tb testing.TB) (*Node, string, string) {
	tb.Helper()
	const parent = "aaaaaaaa-0000-0000-0000-000000000002"
	home := tb.TempDir()
	tp := filepath.Join(home, ".claude", "projects", "-src-app", parent+".jsonl")
	dir := filepath.Join(strings.TrimSuffix(tp, ".jsonl"), "subagents")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		tb.Fatal(err)
	}
	if err := os.WriteFile(tp, []byte("{}\n"), 0o600); err != nil {
		tb.Fatal(err)
	}
	return &Node{home: home, paths: map[string]string{parent: tp}}, parent, dir
}

// writeSubagent writes agent-<id>.jsonl, last touched age ago, and its meta.
func writeSubagent(tb testing.TB, dir, id, meta string, age time.Duration) {
	tb.Helper()
	at := time.Now().Add(-age)
	line := `{"type":"user","timestamp":"` + at.UTC().Format(time.RFC3339) + `","isSidechain":true}` + "\n"
	jp := filepath.Join(dir, "agent-"+id+".jsonl")
	if os.WriteFile(jp, []byte(line), 0o600) != nil || os.WriteFile(filepath.Join(dir, "agent-"+id+".meta.json"), []byte(meta), 0o600) != nil {
		tb.Fatal("write subagent files")
	}
	if err := os.Chtimes(jp, at, at); err != nil {
		tb.Fatal(err)
	}
}

// The session list asks for every live session's subagents each second: 30 finished ones, half of them past the
// one-day cutoff.
func BenchmarkSubagents(b *testing.B) {
	n, parent, dir := subagentTree(b)
	desc := strings.Repeat("x", 300)
	for i := range 30 {
		meta := `{"agentType":"general-purpose","description":"` + desc + `","toolUseId":"toolu_` + strconv.Itoa(i) + `","finished":true}`
		writeSubagent(b, dir, "s"+strconv.Itoa(i), meta, time.Duration(i)*2*time.Hour)
	}
	if got := len(n.subagents(parent)); got != 12 {
		b.Fatalf("listed %d subagents, want 12", got)
	}
	b.ReportAllocs()
	for b.Loop() {
		n.subagents(parent)
	}
}

// The listing caches parsed metas: one rewritten in place (the subagent finished) is read again, one past the
// cutoff is never read, and a deleted subagent drops out of the list.
func TestSubagentsMetaCache(t *testing.T) {
	n, parent, dir := subagentTree(t)
	writeSubagent(t, dir, "live1", `{"agentType":"general-purpose","description":"示例：在跑"}`, time.Minute)
	writeSubagent(t, dir, "gone2", `{"agentType":"Explore","finished":true}`, 2*time.Minute)
	writeSubagent(t, dir, "old3", `{"agentType":"Explore","finished":true}`, 30*time.Hour)

	byID := func() map[string]bool {
		t.Helper()
		got := map[string]bool{}
		for _, a := range n.subagents(parent) {
			got[a.ID] = a.Running
		}
		return got
	}
	if got := byID(); len(got) != 2 || !got["live1"] || got["gone2"] {
		t.Fatalf("first listing %v, want live1 running and gone2 finished", got)
	}
	old := filepath.Join(dir, "agent-old3.meta.json")
	metaCache.Lock()
	_, cached := metaCache.m[old]
	metaCache.Unlock()
	if cached {
		t.Fatal("the meta of a subagent past the cutoff was read")
	}

	// Claude Code rewrites the meta in place when the subagent finishes.
	mp := filepath.Join(dir, "agent-live1.meta.json")
	if err := os.WriteFile(mp, []byte(`{"agentType":"general-purpose","description":"示例：在跑","finished":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := byID(); len(got) != 2 || got["live1"] {
		t.Fatalf("after finishing %v, want live1 not running", got)
	}

	for _, p := range []string{"agent-gone2.jsonl", "agent-gone2.meta.json"} {
		if err := os.Remove(filepath.Join(dir, p)); err != nil {
			t.Fatal(err)
		}
	}
	if got := byID(); len(got) != 1 {
		t.Fatalf("after deleting gone2 %v, want only live1", got)
	}
}

// A meta that does not parse (caught mid-write) is cached as a failure and left out until the file changes: once
// rewritten whole, the subagent is listed.
func TestSubagentsMetaCacheParseFailure(t *testing.T) {
	n, parent, dir := subagentTree(t)
	writeSubagent(t, dir, "half4", `{"agentType":"gene`, time.Minute)
	mp := filepath.Join(dir, "agent-half4.meta.json")
	for range 2 { // the second listing is served from the cached failure
		if got := n.subagents(parent); len(got) != 0 {
			t.Fatalf("listed %+v from a meta that does not parse", got)
		}
	}
	metaCache.Lock()
	e, cached := metaCache.m[mp]
	metaCache.Unlock()
	if !cached || e.ok {
		t.Fatalf("parse failure cached = %v, ok = %v; want a cached failure", cached, e.ok)
	}
	if err := os.WriteFile(mp, []byte(`{"agentType":"general-purpose","description":"示例：写完"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := n.subagents(parent)
	if len(got) != 1 || got[0].ID != "half4" || got[0].Description != "示例：写完" || !got[0].Running {
		t.Fatalf("after the meta was written whole: %+v", got)
	}
}
