package extension

import (
	"slices"
	"strings"
	"testing"
)

// agent_runtimes (#5413) is a permission of the closed vocabulary: shown on install, asked again on an upgrade that
// adds it, and only for an extension with a program on the hub.
func TestAgentRuntimesPermission(t *testing.T) {
	old, err := Parse(with(t, func(m map[string]any) { m["permissions"] = map[string]any{"network": []string{"api.example.com"}} }))
	if err != nil {
		t.Fatal(err)
	}
	m, err := Parse(with(t, func(m map[string]any) {
		m["permissions"] = map[string]any{"network": []string{"api.example.com"}, "agent_runtimes": true}
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !m.Permissions.AgentRuntimes {
		t.Fatal("not read")
	}
	has := func(lines []PermissionLine) bool {
		return slices.ContainsFunc(lines, func(l PermissionLine) bool { return l.Key == "agent_runtimes" && l.Title != "" })
	}
	if !has(Explain(m)) || has(Explain(old)) {
		t.Errorf("explain: %+v", Explain(m))
	}
	if !has(Added(old, m)) || has(Added(m, m)) {
		t.Errorf("added: %+v", Added(old, m))
	}
	if old.Permissions.Covers(m.Permissions) || !m.Permissions.Covers(old.Permissions) {
		t.Error("covers")
	}
	if Digest(old) == Digest(m) {
		t.Error("the digest does not see it")
	}
	_, err = Parse(with(t, func(m map[string]any) {
		delete(m, "runtime")
		m["permissions"] = map[string]any{"agent_runtimes": true}
	}))
	if err == nil || !strings.Contains(err.Error(), "agent_runtimes without a runtime") {
		t.Errorf("without a runtime: %v", err)
	}
}
