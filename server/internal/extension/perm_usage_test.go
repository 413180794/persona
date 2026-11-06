package extension

import (
	"slices"
	"testing"
)

// usage_gateways (#4576) is a permission of the closed vocabulary: shown on install, asked again on an upgrade that
// adds it, not covered by launcher_keys.
func TestUsageGatewaysPermission(t *testing.T) {
	keys := with(t, func(m map[string]any) { m["permissions"] = map[string]any{"launcher_keys": true} })
	both := with(t, func(m map[string]any) {
		m["permissions"] = map[string]any{"launcher_keys": true, "usage_gateways": true}
	})
	old, err := Parse(keys)
	if err != nil {
		t.Fatal(err)
	}
	m, err := Parse(both)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Permissions.UsageGateways {
		t.Fatal("not read")
	}
	has := func(lines []PermissionLine) bool {
		return slices.ContainsFunc(lines, func(l PermissionLine) bool { return l.Key == "usage_gateways" && l.Title != "" })
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
}
