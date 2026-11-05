package extmgr

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/extension"
)

func bundleTestPreview(t *testing.T, h *Manager, id, version, upgrade, providerID string, members []extension.PackMember, edit func(map[string]any)) *extension.Preview {
	t.Helper()
	manifest := map[string]any{"manifest_version": 1, "id": id, "name": id, "version": version}
	if len(members) > 0 {
		manifest["extension_pack"] = members
	} else {
		if providerID == "" {
			providerID = id + "-provider"
		}
		manifest["contributes"] = map[string]any{"providers": []any{map[string]any{"id": providerID, "name": id, "base_url": "https://provider.example.com"}}}
	}
	if edit != nil {
		edit(manifest)
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	dir := declarativePackage(t, string(raw))
	pv, err := h.extensions.reg.Preview(t.Context(), dir, upgrade)
	if err != nil {
		t.Fatal(err)
	}
	return pv
}

func bundleTestRequest(t *testing.T, pv *extension.Preview, digest string) string {
	t.Helper()
	refs := make([]bundleRef, 0, len(pv.Bundle))
	for _, member := range pv.Bundle {
		refs = append(refs, bundleRef{PreviewID: member.ID, Digest: member.Digest})
	}
	raw, err := json.Marshal(map[string]any{"preview_id": pv.ID, "permissions_digest": digest, "bundle": refs})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func bundleTestEndpoint(t *testing.T, admin bool) (*Manager, *extensionStore, func(string, string) (int, map[string]any)) {
	t.Helper()
	if admin {
		h, store, client := adminExtensionHub(t) // the admin UI's session, as the hub relays it
		return h, store, func(id, body string) (int, map[string]any) {
			path := "/api/v1/admin/extensions/install"
			if id != "" {
				path = "/api/v1/admin/extensions/" + id + "/upgrade"
			}
			code, out, _ := client.do(http.MethodPost, path, body)
			return code, out
		}
	}
	h, _, _ := marketHub(t)
	mux := h.routes()
	return h, h.store.(*marketTestStore).extensionStore, func(id, body string) (int, map[string]any) {
		path := "/api/v1/extensions/-/install"
		if id != "" {
			path = "/api/v1/extensions/" + id + "/-/upgrade"
		}
		return callFrom(t, mux, http.MethodPost, path, h.cfg.Token, body, "127.0.0.1:1234")
	}
}

func TestBundleCommitFailurePreservesExistingState(t *testing.T) {
	for _, admin := range []bool{false, true} {
		for _, failed := range []string{"second", "pack"} {
			t.Run(fmt.Sprintf("admin=%v/failure=%s", admin, failed), func(t *testing.T) {
				h, store, request := bundleTestEndpoint(t, admin)
				reg := h.extensions.reg
				oldPV := bundleTestPreview(t, h, "first", "1.0.0", "", "", nil, func(m map[string]any) {
					m["configuration"] = map[string]any{"type": "object", "properties": map[string]any{"note": map[string]any{"type": "string"}}}
					m["secrets"] = []any{map[string]any{"name": "token", "title": "Example key", "required": true}}
				})
				old, err := reg.Install(t.Context(), oldPV.ID, oldPV.Digest, "test")
				if err != nil {
					t.Fatal(err)
				}
				if err = reg.SetConfig(t.Context(), "first", json.RawMessage(`{"note":"preserved example configuration"}`), "test"); err != nil {
					t.Fatal(err)
				}
				if err = reg.SetSecret(t.Context(), "first", "token", "synthetic-existing-key", "test"); err != nil {
					t.Fatal(err)
				}
				if err = reg.SetContribution(t.Context(), "first", "providers", "first-provider", false, "test"); err != nil {
					t.Fatal(err)
				}
				oldRecord := reg.Snapshot("first").Record
				oldStoredRecord := store.rows["first"]
				oldSecrets, err := store.ExtensionSecrets(t.Context(), "first")
				if err != nil {
					t.Fatal(err)
				}
				originalManifest, err := os.ReadFile(filepath.Join(old.Dir, extension.ManifestFile))
				if err != nil {
					t.Fatal(err)
				}
				dataFile := filepath.Join(reg.DataDir("first"), "user-data.txt")
				if err = os.MkdirAll(filepath.Dir(dataFile), 0o700); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(dataFile, []byte("preserved example data"), 0o600); err != nil {
					t.Fatal(err)
				}
				first := bundleTestPreview(t, h, "first", "2.0.0", "first", "", nil, func(m map[string]any) {
					m["configuration"] = map[string]any{"type": "object", "properties": map[string]any{"note": map[string]any{"type": "string"}}}
					m["secrets"] = []any{map[string]any{"name": "token", "title": "Example key", "required": true}}
				})
				second := bundleTestPreview(t, h, "second", "1.0.0", "", "", nil, nil)
				pack := bundleTestPreview(t, h, "pack", "1.0.0", "", "", []extension.PackMember{{ID: "first", Version: ">=2.0.0"}, {ID: "second"}}, nil)
				pack.Bundle = []*extension.Preview{first, second}
				beforeAudit := len(store.audit)
				var committedChanges []extension.Change
				onChange := reg.OnChange
				reg.OnChange = func(change extension.Change) { committedChanges = append(committedChanges, change); onChange(change) }
				store.failPutID = failed
				code, out := request("", bundleTestRequest(t, pack, pack.Digest))
				if code != http.StatusBadRequest {
					t.Fatalf("failure accepted: %d %v", code, out)
				}
				if got := reg.Snapshot("first").Record; !reflect.DeepEqual(got, oldRecord) {
					t.Error("failed plan changed the existing registry record")
				}
				if got := store.rows["first"]; !reflect.DeepEqual(got, oldStoredRecord) {
					t.Error("failed plan changed the existing database record")
				}
				secrets, err := store.ExtensionSecrets(t.Context(), "first")
				if err != nil || !reflect.DeepEqual(secrets, oldSecrets) {
					t.Error("failed plan changed existing sealed secrets")
				}
				if reg.Get("second") != nil || reg.Get("pack") != nil || len(store.rows) != 1 {
					t.Error("failed plan left a partially installed member or main package")
				}
				if len(store.audit) != beforeAudit {
					t.Error("failed plan emitted committed install/upgrade audit")
				}
				if len(committedChanges) != 0 {
					t.Error("failed plan published changes or activated extensions")
				}
				if hp := h.extensions.get("first"); hp == nil || hp.err != "" && strings.Contains(hp.err, "扩展包不见") {
					t.Error("original extension state was not retained")
				}
				if raw, err := os.ReadFile(filepath.Join(old.Dir, extension.ManifestFile)); err != nil || string(raw) != string(originalManifest) {
					t.Error("failed plan changed the original package")
				}
				if raw, err := os.ReadFile(dataFile); err != nil || string(raw) != "preserved example data" {
					t.Error("failed plan deleted or changed user data")
				}
				newDir := filepath.Join(filepath.Dir(old.Dir), "2.0.0")
				if _, err = os.Stat(newDir); !os.IsNotExist(err) {
					t.Error("failed plan left an upgraded package directory")
				}
				store.failPutID = ""
				code, out = request("", bundleTestRequest(t, pack, pack.Digest))
				if code != http.StatusOK {
					t.Fatalf("unchanged valid plan not retryable: %d %v", code, out)
				}
				if reg.Get("first").Record.Version != "2.0.0" || reg.Get("second") == nil || reg.Get("pack") == nil {
					t.Error("retried plan incomplete")
				}
			})
		}
	}
}

func TestBundlePrevalidatesAllMembersBeforeWriting(t *testing.T) {
	for _, admin := range []bool{false, true} {
		for _, failure := range []string{"late-preview-expired", "contribution-conflict"} {
			t.Run(fmt.Sprintf("admin=%v/%s", admin, failure), func(t *testing.T) {
				h, store, request := bundleTestEndpoint(t, admin)
				reg := h.extensions.reg
				first := bundleTestPreview(t, h, "first", "1.0.0", "", "shared", nil, nil)
				provider := "second-provider"
				if failure == "contribution-conflict" {
					provider = "shared"
				}
				second := bundleTestPreview(t, h, "second", "1.0.0", "", provider, nil, nil)
				pack := bundleTestPreview(t, h, "pack", "1.0.0", "", "", []extension.PackMember{{ID: "first"}, {ID: "second"}}, nil)
				pack.Bundle = []*extension.Preview{first, second}
				body := bundleTestRequest(t, pack, pack.Digest)
				if failure == "late-preview-expired" {
					second.ExpiresAt = time.Now().Add(-time.Second)
				}
				beforeAudit := len(store.audit)
				code, out := request("", body)
				if code != http.StatusBadRequest {
					t.Fatalf("invalid plan accepted: %d %v", code, out)
				}
				for _, id := range []string{"first", "second", "pack"} {
					if reg.Get(id) != nil {
						t.Errorf("%s written before complete plan validation", id)
					}
				}
				if len(store.rows) != 0 || len(store.audit) != beforeAudit {
					t.Error("invalid plan changed database rows or audit")
				}
				if reg.PreviewOf(first.ID) == nil {
					t.Error("valid preview consumed by a prevalidation failure")
				}
			})
		}
	}
}

func TestBundleUpgradeConfirmsMainDigestBeforeMembers(t *testing.T) {
	for _, admin := range []bool{false, true} {
		t.Run(fmt.Sprintf("admin=%v", admin), func(t *testing.T) {
			h, store, request := bundleTestEndpoint(t, admin)
			reg := h.extensions.reg
			old := bundleTestPreview(t, h, "main", "1.0.0", "", "", nil, nil)
			if _, err := reg.Install(t.Context(), old.ID, old.Digest, "test"); err != nil {
				t.Fatal(err)
			}
			main := bundleTestPreview(t, h, "main", "2.0.0", "main", "", nil, func(m map[string]any) {
				m["depends"] = []extension.Dependency{{ID: "member"}}
				m["permissions"] = map[string]any{"network": []string{"api.example.com"}}
			})
			member := bundleTestPreview(t, h, "member", "1.0.0", "", "", nil, nil)
			main.Bundle = []*extension.Preview{member}
			before := reg.Snapshot("main").Record
			code, out := request("main", bundleTestRequest(t, main, "not-the-confirmed-digest"))
			if code != http.StatusBadRequest {
				t.Fatalf("wrong digest accepted: %d %v", code, out)
			}
			if reg.Get("member") != nil || len(store.rows) != 1 {
				t.Error("member installed before main permission confirmation")
			}
			if !reflect.DeepEqual(reg.Snapshot("main").Record, before) {
				t.Error("wrong main digest changed existing state")
			}
		})
	}
}
