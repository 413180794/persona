package extmgr

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/extension"
)

func TestExtensionDependencyBundleGraph(t *testing.T) {
	for _, mode := range []string{"cycle", "installed-unready-dependency"} {
		t.Run(mode, func(t *testing.T) {
			h, phone, _ := marketHub(t)
			mux := h.routes()
			depends := map[string]string{"top": `[{"id":"middle"}]`, "middle": `[{"id":"base"}]`, "base": `[]`}
			if mode == "cycle" {
				depends["base"] = `[{"id":"top"}]`
			}
			dirs := map[string]string{}
			assets := map[string][]byte{}
			var entries []map[string]any
			for _, id := range []string{"base", "middle", "top"} {
				dir := declarativePackage(t, fmt.Sprintf(`{"manifest_version":1,"id":%q,"name":%q,"version":"1.0.0","depends":%s,"contributes":{"providers":[{"id":%q,"name":"Example","base_url":"https://provider.example.com"}]}}`, id, id, depends[id], id+"-provider"))
				dirs[id] = dir
				assets[id+".tar.gz"] = tarDir(t, dir)
				entries = append(entries, marketEntry(t, dir, "release:v1/"+id+".tar.gz", assets[id+".tar.gz"]))
			}
			gh := &fakeGitHub{release: "v1", files: map[string][]byte{}, assets: assets}
			srv := httptest.NewTLSServer(gh)
			defer srv.Close()
			gh.storage = srv.URL
			h.extensions.markets.client, h.extensions.markets.githubAPI = srv.Client(), srv.URL
			gh.files[extension.IndexFile] = marketIndex(t, "Example", entries...)
			code, mk := call(t, mux, "POST", "/api/v1/extensions/-/markets", phone, `{"spec":"github:example/plugins"}`)
			if code != 200 {
				t.Fatalf("market %d %v", code, mk)
			}
			if mode != "cycle" {
				pv, err := h.extensions.reg.Preview(t.Context(), dirs["middle"], "")
				if err != nil {
					t.Fatal(err)
				}
				if _, err = h.extensions.reg.Install(t.Context(), pv.ID, pv.Digest, "test"); err != nil {
					t.Fatal(err)
				}
			}
			code, pv := call(t, mux, "POST", "/api/v1/extensions/-/catalog/preview", phone, fmt.Sprintf(`{"market":%q,"extension":"top"}`, mk["id"]))
			if code != 200 {
				t.Fatalf("preview %d %v", code, pv)
			}
			if mode == "cycle" {
				if !strings.Contains(fmt.Sprint(pv["unresolved"]), "环") {
					t.Fatal("whole candidate cycle not diagnosed:", pv)
				}
				var refs []string
				for _, item := range pv["bundle"].([]any) {
					b := item.(map[string]any)
					refs = append(refs, fmt.Sprintf(`{"preview_id":%q,"permissions_digest":%q}`, b["preview_id"], b["permissions_digest"]))
				}
				code, out := call(t, mux, "POST", "/api/v1/extensions/-/install", phone, fmt.Sprintf(`{"preview_id":%q,"permissions_digest":%q,"bundle":[%s]}`, pv["preview_id"], pv["permissions_digest"], strings.Join(refs, ",")))
				if code != 400 {
					t.Fatalf("cyclic plan installed: %d %v", code, out)
				}
				for _, id := range []string{"top", "middle", "base"} {
					if h.extensions.reg.Get(id) != nil {
						t.Errorf("cyclic preview changed %s", id)
					}
				}
			} else {
				bundle := pv["bundle"].([]any)
				if len(bundle) != 1 || bundle[0].(map[string]any)["manifest"].(map[string]any)["id"] != "base" {
					t.Fatal("did not resolve the installed dependency's missing child:", bundle)
				}
				if h.extensions.reg.Get("middle").Record.Version != "1.0.0" {
					t.Fatal("reinstalled an existing unready dependency")
				}
			}
		})
	}
}
