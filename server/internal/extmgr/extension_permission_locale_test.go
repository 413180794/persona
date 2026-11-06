package extmgr

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
)

// These requests exercise the manager's actual preview/detail/operation/UI HTTP responses and a fake extension process.
func TestExtensionPermissionAndUIResponseLocale(t *testing.T) {
	h, _ := extensionTestHub(t)
	mux := h.routes()
	request := func(l i18n.Lang, method, path, body string) map[string]any {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("Authorization", "Bearer "+h.cfg.Token)
		r.Header.Set("Accept-Language", string(l))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("%s %s %s: %d %s", l, method, path, w.Code, w.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	pv := request(i18n.En, "POST", "/api/v1/extensions/-/preview", `{"source":"dir:`+uiPackage(t)+`"}`)
	checkPermissions := func(l i18n.Lang, out map[string]any) {
		t.Helper()
		lines := out["permissions"].([]any)
		first := lines[0].(map[string]any)
		if first["title"] != i18n.T(l, "extension-permissions.runs_code_title") {
			t.Fatalf("%s permissions: %v", l, lines)
		}
	}
	checkPermissions(i18n.En, pv)
	for _, l := range []i18n.Lang{i18n.En, i18n.ZhHans, i18n.Ja} {
		op := request(l, "GET", "/api/v1/extensions/-/operations/"+pv["operation_id"].(string), "")
		preview := op["preview"].(map[string]any)
		checkPermissions(l, preview)
		if preview["permissions_digest"] != pv["permissions_digest"] {
			t.Fatal("language switching changed consent digest")
		}
	}
	request(i18n.En, "POST", "/api/v1/extensions/-/install", fmt.Sprintf(`{"preview_id":%q,"permissions_digest":%q}`, pv["preview_id"], pv["permissions_digest"]))
	waitLong(t, "fake UI extension ready", func() bool {
		hp := h.extensions.get("example-ui")
		return hp != nil && hp.proc != nil && hp.proc.Ready()
	})
	for _, l := range []i18n.Lang{i18n.En, i18n.ZhHans, i18n.Ja} {
		for _, prefix := range []string{"extensions", "plugins"} {
			detail := request(l, "GET", "/api/v1/"+prefix+"/example-ui/-/", "")
			checkPermissions(l, detail)
			out := request(l, "POST", "/api/v1/"+prefix+"/example-ui/-/ui/list", fmt.Sprintf(`{"context":{"locale":%q,"ui_version":1}}`, l))
			view := out["view"].(map[string]any)
			items := view["sections"].([]any)[0].(map[string]any)["items"].([]any)
			seenLoading, seenMore, seenOriginal := false, false, false
			for _, item := range items {
				e := item.(map[string]any)
				seenLoading = seenLoading || e["text"] == i18n.T(l, "extension-ui-fallback.loading")
				seenMore = seenMore || e["title"] == i18n.T(l, "extension-ui-fallback.load_more")
				seenOriginal = seenOriginal || e["title"] == "周会"
			}
			if !seenLoading || !seenMore || !seenOriginal {
				t.Fatalf("%s %s fallback or original missing: %v", l, prefix, items)
			}
		}
	}
}
