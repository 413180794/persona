package extmgr

import (
	"fmt"
	"testing"
)

func TestExtensionDependencyChangesReconcileAllAncestors(t *testing.T) {
	h, _, _ := marketHub(t)
	reg := h.extensions.reg
	install := func(id string, deps string) {
		t.Helper()
		dir := declarativePackage(t, fmt.Sprintf(`{"manifest_version":1,"id":%q,"name":%q,"version":"1.0.0","depends":%s,"contributes":{"providers":[{"id":%q,"name":"Example","base_url":"https://provider.example.com"}]}}`, id, id, deps, id+"-provider"))
		pv, err := reg.Preview(t.Context(), dir, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = reg.Install(t.Context(), pv.ID, pv.Digest, "test"); err != nil {
			t.Fatal(err)
		}
	}
	status := func(id, want string) {
		t.Helper()
		p := h.extensions.get(id)
		if p == nil || p.status != want {
			t.Errorf("%s: got %+v, want %s", id, p, want)
		}
	}
	install("ancestor", `[{"id":"middle"}]`)
	install("sibling", `[{"id":"middle"}]`)
	install("middle", `[{"id":"base","version":">=1.0.0 <2.0.0"}]`)
	for _, id := range []string{"ancestor", "sibling", "middle"} {
		status(id, extensionNeedsSetup)
	}
	install("base", `[]`)
	for _, id := range []string{"ancestor", "sibling", "middle", "base"} {
		status(id, "running")
	}
	if err := reg.SetEnabled(t.Context(), "base", false, "test"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ancestor", "sibling", "middle"} {
		status(id, extensionNeedsSetup)
	}
	if err := reg.SetEnabled(t.Context(), "base", true, "test"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ancestor", "sibling", "middle"} {
		status(id, "running")
	}
	dir := declarativePackage(t, `{"manifest_version":1,"id":"base","name":"Base","version":"2.0.0","contributes":{"providers":[{"id":"base-provider","name":"Example","base_url":"https://provider.example.com"}]}}`)
	pv, err := reg.Preview(t.Context(), dir, "base")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reg.Upgrade(t.Context(), "base", pv.ID, pv.Digest, "test"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ancestor", "sibling", "middle"} {
		status(id, extensionNeedsSetup)
	}
	if _, err = reg.Rollback(t.Context(), "base", "test"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ancestor", "sibling", "middle"} {
		status(id, "running")
	}
	if err = reg.Uninstall(t.Context(), "base", true, "test"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ancestor", "sibling", "middle"} {
		status(id, extensionNeedsSetup)
	}
	for _, id := range []string{"ancestor", "sibling", "middle"} {
		if !reg.Get(id).Record.Enabled {
			t.Errorf("implicit switch change: %s", id)
		}
	}
}
