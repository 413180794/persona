package extmgr

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/extension"
)

// A real extension process reaches the capsule through the existing render/action routes.
func TestExtensionCapsuleCard(t *testing.T) {
	h, _ := extensionTestHub(t)
	mux := h.routes()
	master := h.cfg.Token
	dir := uiPackage(t)
	file := filepath.Join(dir, extension.ManifestFile)
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.ReplaceAll(string(raw), `"placement": "overview_card"`, `"placement": "capsule_card"`))
	if err := os.WriteFile(file, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	code, preview := call(t, mux, "POST", "/api/v1/extensions/-/preview", master, `{"source":"dir:`+dir+`"}`)
	if code != 200 || !strings.Contains(fmt.Sprint(preview["permissions"]), "Android 跨 App 悬浮胶囊里的卡片（在 App 外显示）") {
		t.Fatalf("capsule consent: %d %v", code, preview)
	}
	code, installed := call(t, mux, "POST", "/api/v1/extensions/-/install", master,
		fmt.Sprintf(`{"preview_id":%q,"permissions_digest":%q}`, preview["preview_id"], preview["permissions_digest"]))
	if code != 200 {
		t.Fatalf("install: %d %v", code, installed)
	}
	waitLong(t, "capsule extension ready", func() bool {
		hp := h.extensions.get("example-ui")
		return hp != nil && hp.proc != nil && hp.proc.Ready()
	})
	_, listed := call(t, mux, "GET", "/api/v1/extensions/-/ui?placement=capsule_card", master, "")
	places, _ := listed["ui"].([]any)
	if len(places) != 2 || !strings.Contains(fmt.Sprint(places), "size:compact") || !strings.Contains(fmt.Sprint(places), "opens:nav") {
		t.Fatalf("capsule places: %v", listed)
	}
	code, drawn := call(t, mux, "POST", "/api/v1/extensions/example-ui/-/ui/card", master, `{"context":{"platform":"android","size":"compact","ui_version":3}}`)
	if code != 200 || !strings.Contains(fmt.Sprint(drawn), "1.2M") {
		t.Fatalf("draw: %d %v", code, drawn)
	}
	code, acted := call(t, mux, "POST", "/api/v1/extensions/example-ui/-/ui/card", master, `{"action":"save","values":{"note":"胶囊"},"context":{"platform":"android","size":"compact","ui_version":3}}`)
	if code != 200 || acted["toast"] != "存好了：胶囊" {
		t.Fatalf("action: %d %v", code, acted)
	}
	if code, _ := call(t, mux, "POST", "/api/v1/extensions/example-ui/-/ui/bigcard", master, `{"context":{"platform":"android","ui_version":3}}`); code != 502 {
		t.Fatalf("oversized compact card accepted: %d", code)
	}
}
