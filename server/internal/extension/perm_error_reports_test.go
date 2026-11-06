package extension

import (
	"slices"
	"strings"
	"testing"
)

// error_reports: the permission is shown and asked again on an upgrade that adds it; the contribution needs it, a
// program that speaks the protocol and on_startup, and only knows the sources of the schema.
func TestErrorReportsPermissionAndPoint(t *testing.T) {
	contrib := func(m map[string]any, items ...map[string]any) {
		m["activation_events"] = []string{"on_startup"}
		m["contributes"] = map[string]any{"error_reports": items}
	}
	old, err := Parse(with(t, func(m map[string]any) { m["permissions"] = map[string]any{} }))
	if err != nil {
		t.Fatal(err)
	}
	m, err := Parse(with(t, func(m map[string]any) {
		contrib(m, map[string]any{"id": "all"}, map[string]any{"id": "apps", "sources": []string{"web", "android"}})
		m["permissions"] = map[string]any{"error_reports": true}
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got := m.ErrorReports(); len(got) != 2 || !slices.Equal(got[1].Sources, []string{"web", "android"}) {
		t.Fatalf("%+v", got)
	}
	has := func(lines []PermissionLine) bool {
		return slices.ContainsFunc(lines, func(l PermissionLine) bool { return l.Key == "error_reports" && l.Title != "" && l.Detail != "" })
	}
	if !has(Explain(m)) || has(Explain(old)) || !has(Added(old, m)) || has(Added(m, m)) {
		t.Errorf("explain %+v added %+v", Explain(m), Added(old, m))
	}
	if old.Permissions.Covers(m.Permissions) || !m.Permissions.Covers(old.Permissions) || Digest(old) == Digest(m) {
		t.Error("covers / digest")
	}
	for want, edit := range map[string]func(map[string]any){
		"needs permissions.error_reports": func(m map[string]any) { contrib(m, map[string]any{"id": "all"}) },
		"on_startup": func(m map[string]any) {
			contrib(m, map[string]any{"id": "all"})
			delete(m, "activation_events")
			m["permissions"] = map[string]any{"error_reports": true}
		},
		`no source "phone"`: func(m map[string]any) {
			contrib(m, map[string]any{"id": "all", "sources": []string{"phone"}})
			m["permissions"] = map[string]any{"error_reports": true}
		},
	} {
		if _, err := Parse(with(t, edit)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", want, err)
		}
	}
}
