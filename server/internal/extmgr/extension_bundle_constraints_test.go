package extmgr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/extension"
)

func bundleMarketFixture(t *testing.T, h *Manager, phone string, dirs map[string]string) string {
	t.Helper()
	assets := map[string][]byte{}
	var entries []map[string]any
	for _, id := range slices.Sorted(maps.Keys(dirs)) {
		assets[id+".tar.gz"] = tarDir(t, dirs[id])
		entries = append(entries, marketEntry(t, dirs[id], "pkg/"+id+".tar.gz", assets[id+".tar.gz"]))
	}
	index := marketIndex(t, "Example", entries...)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/market/"+extension.IndexFile {
			_, _ = w.Write(index)
			return
		}
		if b := assets[strings.TrimPrefix(r.URL.Path, "/market/pkg/")]; b != nil {
			_, _ = w.Write(b)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	h.extensions.markets.client = srv.Client()
	code, mk := call(t, h.routes(), "POST", "/api/v1/extensions/-/markets", phone, fmt.Sprintf(`{"spec":%q}`, srv.URL+"/market/"))
	if code != 200 {
		t.Fatalf("market %d %v", code, mk)
	}
	return mk["id"].(string)
}

func bundleGraphPackage(t *testing.T, id, version string, deps ...extension.Dependency) string {
	t.Helper()
	m := map[string]any{"manifest_version": 1, "id": id, "name": id, "version": version, "depends": deps, "contributes": map[string]any{"providers": []map[string]string{{"id": id + "-provider", "name": id, "base_url": "https://provider.example.com"}}}}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return declarativePackage(t, string(raw))
}

func TestBundleResolverChoosesVersionSatisfyingEveryMarketBranch(t *testing.T) {
	for _, order := range [][]string{{"loose", "strict"}, {"strict", "loose"}} {
		t.Run(strings.Join(order, "-"), func(t *testing.T) {
			h, phone, _ := marketHub(t)
			pack := declarativePackage(t, fmt.Sprintf(`{"manifest_version":1,"id":"pack","name":"Pack","version":"1.0.0","extension_pack":[{"id":%q},{"id":%q}]}`, order[0], order[1]))
			mid := bundleMarketFixture(t, h, phone, map[string]string{"pack": pack, "shared": bundleGraphPackage(t, "shared", "1.5.0"), "loose": bundleGraphPackage(t, "loose", "1.0.0", extension.Dependency{ID: "shared", Version: ">=1.0.0"}), "strict": bundleGraphPackage(t, "strict", "1.0.0", extension.Dependency{ID: "shared", Version: ">=2.0.0"})})
			bundleMarketFixture(t, h, phone, map[string]string{"shared": bundleGraphPackage(t, "shared", "2.5.0")})
			code, out := call(t, h.routes(), "POST", "/api/v1/extensions/-/catalog/preview", phone, fmt.Sprintf(`{"market":%q,"extension":"pack"}`, mid))
			if code != 200 {
				t.Fatalf("preview %d %v", code, out)
			}
			pv := h.extensions.reg.PreviewOf(out["preview_id"].(string))
			if len(pv.Unresolved) > 0 {
				t.Fatal(pv.Unresolved)
			}
			count := 0
			var refs []bundleRef
			for _, member := range pv.Bundle {
				if member.Manifest.ID == "shared" {
					count++
					if member.Manifest.Version != "2.5.0" {
						t.Fatal("wrong branch-selected version")
					}
				}
				refs = append(refs, bundleRef{PreviewID: member.ID, Digest: member.Digest})
			}
			if count != 1 {
				t.Fatal("shared package not unique")
			}
			if _, err := h.installExtensionPlan(t.Context(), "", pv.ID, pv.Digest, refs, "test"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBundleResolverTraversesDisabledInstalledMembers(t *testing.T) {
	h, phone, _ := marketHub(t)
	reg := h.extensions.reg
	branch, err := reg.Preview(t.Context(), bundleGraphPackage(t, "branch", "1.0.0", extension.Dependency{ID: "child", Version: ">=2.0.0"}), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reg.Install(t.Context(), branch.ID, branch.Digest, "test"); err != nil {
		t.Fatal(err)
	}
	if err = reg.SetEnabled(t.Context(), "branch", false, "test"); err != nil {
		t.Fatal(err)
	}
	before := reg.Snapshot("branch").Record
	pack := declarativePackage(t, `{"manifest_version":1,"id":"pack","name":"Pack","version":"1.0.0","extension_pack":[{"id":"branch"}]}`)
	mid := bundleMarketFixture(t, h, phone, map[string]string{"pack": pack, "child": bundleGraphPackage(t, "child", "2.0.0")})
	code, out := call(t, h.routes(), "POST", "/api/v1/extensions/-/catalog/preview", phone, fmt.Sprintf(`{"market":%q,"extension":"pack"}`, mid))
	if code != 200 {
		t.Fatalf("preview %d %v", code, out)
	}
	pv := reg.PreviewOf(out["preview_id"].(string))
	if len(pv.Unresolved) > 0 || len(pv.Bundle) != 1 || pv.Bundle[0].Manifest.ID != "child" {
		t.Fatalf("installed branch children omitted: %+v", pv)
	}
	if _, err := h.installExtensionPlan(t.Context(), "", pv.ID, pv.Digest, []bundleRef{{PreviewID: pv.Bundle[0].ID, Digest: pv.Bundle[0].Digest}}, "test"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, reg.Snapshot("branch").Record) {
		t.Fatal("installed disabled member changed")
	}
}

func TestBundleResolverJointUpgradeReplacesOldDependencyEdges(t *testing.T) {
	h, phone, _ := marketHub(t)
	reg := h.extensions.reg
	for _, id := range []string{"b", "a"} {
		var deps []extension.Dependency
		if id == "a" {
			deps = []extension.Dependency{{ID: "b"}}
		}
		pv, err := reg.Preview(t.Context(), bundleGraphPackage(t, id, "1.0.0", deps...), "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = reg.Install(t.Context(), pv.ID, pv.Digest, "test"); err != nil {
			t.Fatal(err)
		}
	}
	mid := bundleMarketFixture(t, h, phone, map[string]string{"a": bundleGraphPackage(t, "a", "2.0.0"), "b": bundleGraphPackage(t, "b", "2.0.0", extension.Dependency{ID: "a", Version: ">=2.0.0"})})
	code, out := call(t, h.routes(), "POST", "/api/v1/extensions/-/catalog/preview", phone, fmt.Sprintf(`{"market":%q,"extension":"b","upgrade":"b"}`, mid))
	if code != 200 {
		t.Fatalf("valid joint transition refused against old graph: %d %v", code, out)
	}
	pv := reg.PreviewOf(out["preview_id"].(string))
	if len(pv.Unresolved) > 0 || len(pv.Bundle) != 1 {
		t.Fatal("joint candidate not complete:", pv.Unresolved)
	}
	if _, err := h.installExtensionPlan(t.Context(), "b", pv.ID, pv.Digest, []bundleRef{{PreviewID: pv.Bundle[0].ID, Digest: pv.Bundle[0].Digest}}, "test"); err != nil {
		t.Fatal(err)
	}
	if reg.Snapshot("a").Record.Version != "2.0.0" || reg.Snapshot("b").Record.Version != "2.0.0" {
		t.Fatal("joint graph not committed")
	}
}

func TestBundleResolverCollectsConstraintsBeyondInstalledDepthLimit(t *testing.T) {
	h, phone, _ := marketHub(t)
	reg := h.extensions.reg
	for i := 5; i >= 1; i-- {
		id := fmt.Sprintf("deep%d", i)
		dep := extension.Dependency{ID: fmt.Sprintf("deep%d", i+1)}
		if i == 5 {
			dep = extension.Dependency{ID: "shared", Version: ">=2.0.0"}
		}
		pv, err := reg.Preview(t.Context(), bundleGraphPackage(t, id, "1.0.0", dep), "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = reg.Install(t.Context(), pv.ID, pv.Digest, "test"); err != nil {
			t.Fatal(err)
		}
	}
	pack := declarativePackage(t, `{"manifest_version":1,"id":"pack","name":"Pack","version":"1.0.0","extension_pack":[{"id":"loose"},{"id":"deep1"}]}`)
	mid := bundleMarketFixture(t, h, phone, map[string]string{"pack": pack, "loose": bundleGraphPackage(t, "loose", "1.0.0", extension.Dependency{ID: "shared", Version: ">=1.0.0"}), "shared": bundleGraphPackage(t, "shared", "1.5.0")})
	code, out := call(t, h.routes(), "POST", "/api/v1/extensions/-/catalog/preview", phone, fmt.Sprintf(`{"market":%q,"extension":"pack"}`, mid))
	if code != 200 {
		t.Fatalf("preview %d %v", code, out)
	}
	pv := reg.PreviewOf(out["preview_id"].(string))
	if !strings.Contains(strings.Join(pv.Unresolved, " "), ">=2.0.0") {
		t.Fatal("installed depth limit hid a stricter shared constraint:", pv.Unresolved)
	}
	store := h.store.(*marketTestStore).extensionStore
	store.mu.Lock()
	beforeRows := maps.Clone(store.rows)
	beforeAudit := slices.Clone(store.audit)
	store.mu.Unlock()
	files := map[string][]byte{}
	for id := range beforeRows {
		data, err := os.ReadFile(filepath.Join(reg.Snapshot(id).Dir, extension.ManifestFile))
		if err != nil {
			t.Fatal(err)
		}
		files[id] = data
	}
	var refs []bundleRef
	for _, member := range pv.Bundle {
		refs = append(refs, bundleRef{PreviewID: member.ID, Digest: member.Digest})
	}
	if _, err := h.installExtensionPlan(t.Context(), "", pv.ID, pv.Digest, refs, "test"); err == nil {
		t.Fatal("unresolved deep constraint was installed")
	}
	store.mu.Lock()
	same := reflect.DeepEqual(beforeRows, store.rows) && reflect.DeepEqual(beforeAudit, store.audit)
	store.mu.Unlock()
	if !same {
		t.Fatal("deep prevalidation failure changed persisted records or successful audit")
	}
	for _, id := range []string{"loose", "shared", "pack"} {
		if reg.Snapshot(id) != nil {
			t.Fatal("deep prevalidation installed:", id)
		}
		if _, err := os.Stat(filepath.Join(h.extensions.root, "installed", id)); !os.IsNotExist(err) {
			t.Fatal("deep prevalidation changed installed packages:", id, err)
		}
	}
	for id, before := range files {
		after, err := os.ReadFile(filepath.Join(reg.Snapshot(id).Dir, extension.ManifestFile))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("deep prevalidation changed existing package:", id, err)
		}
	}
}

func TestBundleResolverChecksEveryBranchConstraint(t *testing.T) {
	for _, order := range [][]string{{"loose", "strict"}, {"strict", "loose"}} {
		t.Run(strings.Join(order, "-"), func(t *testing.T) {
			h, phone, _ := marketHub(t)
			dirs := map[string]string{}
			dirs["shared"] = declarativePackage(t, `{"manifest_version":1,"id":"shared","name":"Shared","version":"1.5.0","contributes":{"providers":[{"id":"shared-provider","name":"Shared","base_url":"https://provider.example.com"}]}}`)
			for name, version := range map[string]string{"loose": ">=1.0.0", "strict": ">=2.0.0"} {
				dirs[name] = declarativePackage(t, fmt.Sprintf(`{"manifest_version":1,"id":%q,"name":%q,"version":"1.0.0","depends":[{"id":"shared","version":%q}],"contributes":{"providers":[{"id":%q,"name":"Example","base_url":"https://provider.example.com"}]}}`, name, name, version, name+"-provider"))
			}
			dirs["pack"] = declarativePackage(t, fmt.Sprintf(`{"manifest_version":1,"id":"pack","name":"Pack","version":"1.0.0","extension_pack":[{"id":%q},{"id":%q}]}`, order[0], order[1]))
			assets := map[string][]byte{}
			var entries []map[string]any
			for _, id := range []string{"shared", "loose", "strict", "pack"} {
				assets[id+".tar.gz"] = tarDir(t, dirs[id])
				entries = append(entries, marketEntry(t, dirs[id], "release:v1/"+id+".tar.gz", assets[id+".tar.gz"]))
			}
			gh := &fakeGitHub{release: "v1", files: map[string][]byte{}, assets: assets}
			srv := httptest.NewTLSServer(gh)
			defer srv.Close()
			gh.storage = srv.URL
			h.extensions.markets.client, h.extensions.markets.githubAPI = srv.Client(), srv.URL
			gh.files[extension.IndexFile] = marketIndex(t, "Example", entries...)
			code, mk := call(t, h.routes(), "POST", "/api/v1/extensions/-/markets", phone, `{"spec":"github:example/plugins"}`)
			if code != 200 {
				t.Fatalf("market %d %v", code, mk)
			}
			code, pv := call(t, h.routes(), "POST", "/api/v1/extensions/-/catalog/preview", phone, fmt.Sprintf(`{"market":%q,"extension":"pack"}`, mk["id"]))
			if code != 200 {
				t.Fatalf("preview %d %v", code, pv)
			}
			if !strings.Contains(fmt.Sprint(pv["unresolved"]), ">=2.0.0") {
				t.Error("a later branch's stricter constraint was ignored:", pv["unresolved"])
			}
			for _, id := range []string{"shared", "loose", "strict", "pack"} {
				if h.extensions.reg.Get(id) != nil {
					t.Errorf("preview installed %s", id)
				}
			}
		})
	}
}
