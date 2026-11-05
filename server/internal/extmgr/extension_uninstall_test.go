package extmgr

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/nwverse/persona/server/internal/extension"
)

func installUninstallFixture(t *testing.T, h *Manager, id string, pack bool) *extension.Extension {
	t.Helper()
	contributes := fmt.Sprintf(`"contributes":{"providers":[{"id":%q,"name":"Example","base_url":"https://example.com"}]}`, id+"-p")
	if pack {
		contributes = `"extension_pack":[{"id":"a"},{"id":"b"}]`
	}
	dir := declarativePackage(t, fmt.Sprintf(`{"manifest_version":1,"id":%q,"name":%q,"version":"1.0.0",%s}`, id, id, contributes))
	pv, err := h.extensions.reg.Preview(t.Context(), "dir:"+dir, "")
	if err != nil {
		t.Fatal(err)
	}
	p, err := h.extensions.reg.Install(t.Context(), pv.ID, pv.Digest, "test")
	if err != nil {
		t.Fatal(err)
	}
	// Fixtures use market origins so a full-rights remote device may remove them.
	p.Record.Origin = "market:demo/" + id
	return p
}

func TestPackUninstallPartialFailure(t *testing.T) {
	for _, admin := range []bool{false, true} {
		for _, failID := range []string{"a", "both"} {
			t.Run(fmt.Sprintf("admin=%v/fail=%s", admin, failID), func(t *testing.T) {
				var h *Manager
				var store *extensionStore
				var request func() (int, map[string]any)
				if admin {
					h, store = extensionTestHub(t)
					mux := adminRoutes(h) // relayed with the admin header (the hub's admin session in front)
					request = func() (int, map[string]any) {
						return adminCall(t, mux, "DELETE", "/api/v1/admin/extensions/both", `{"members":true,"keep_data":true}`)
					}
				} else {
					var phone string
					h, phone, _ = marketDevices(t)
					store = h.store.(*extensionStore)
					mux := h.routes()
					request = func() (int, map[string]any) {
						return call(t, mux, "DELETE", "/api/v1/extensions/both/-/?members=1&keep_data=1", phone, "")
					}
				}
				for _, id := range []string{"a", "b", "both"} {
					installUninstallFixture(t, h, id, id == "both")
				}
				data := filepath.Join(h.extensions.reg.DataDir("a"), "kept.txt")
				if err := os.MkdirAll(filepath.Dir(data), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(data, []byte("example"), 0o600); err != nil {
					t.Fatal(err)
				}
				store.failDelete = failID
				code, out := request()
				if code != 409 || out["ok"] != false || out["code"] != "partial_uninstall" {
					t.Fatalf("%d %v", code, out)
				}
				if h.extensions.reg.Get("both") == nil {
					t.Fatal("pack must remain for retry")
				}
				failed := out["failed_members"].([]any)
				if len(failed) != 1 || failed[0].(map[string]any)["id"] != failID {
					t.Fatalf("failed: %v", failed)
				}
				remaining := out["remaining_members"].([]any)
				want := 0
				if failID == "a" {
					want = 1
				}
				if len(remaining) != want || h.extensions.reg.Get("b") != nil {
					t.Fatalf("remaining=%v", remaining)
				}
				store.failDelete = ""
				if code, out = request(); code != 200 || out["ok"] != true {
					t.Fatalf("retry: %d %v", code, out)
				}
				for _, id := range []string{"a", "b", "both"} {
					if h.extensions.reg.Get(id) != nil {
						t.Fatalf("%s still installed", id)
					}
				}
				if _, err := os.Stat(data); err != nil {
					t.Fatalf("kept data: %v", err)
				}
			})
		}
	}
}

func TestPackCannotRemoveLocalMemberAsDevice(t *testing.T) {
	h, phone, _ := marketDevices(t)
	for _, id := range []string{"a", "b", "both"} {
		installUninstallFixture(t, h, id, id == "both")
	}
	h.extensions.reg.Get("a").Record.Origin = "dir:/example/local-package"
	code, out := call(t, h.routes(), "DELETE", "/api/v1/extensions/both/-/?members=1&cascade=1", phone, "")
	if code != 403 || out["code"] != "this_machine_only" {
		t.Fatalf("%d %v", code, out)
	}
	for _, id := range []string{"a", "b", "both"} {
		if h.extensions.reg.Get(id) == nil {
			t.Fatalf("%s removed before source precheck", id)
		}
	}
}
