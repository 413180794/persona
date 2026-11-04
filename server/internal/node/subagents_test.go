package node

import (
	"encoding/json"
	"os"
	"path/filepath"
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
