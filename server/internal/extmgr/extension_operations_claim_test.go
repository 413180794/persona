package extmgr

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/i18n"
)

// TestInstallClaimsOperation: one install claims the operation its preview made. A second install of the same preview
// while it runs (a double click, the web and the CLI together) is refused before it reaches the registry, so it can
// neither mark the install that goes through failed nor take its preview.
func TestInstallClaimsOperation(t *testing.T) {
	h, phone, _ := marketHub(t)
	mux := h.routes()
	dir := uiPackage(t)
	v1 := tarDir(t, dir)
	gh := &fakeGitHub{release: "v1", files: map[string][]byte{}, assets: map[string][]byte{"example-ui-1.0.0.tar.gz": v1}}
	srv := httptest.NewTLSServer(gh)
	defer srv.Close()
	storage := httptest.NewTLSServer(gh)
	defer storage.Close()
	gh.storage = "https://storage.example.com"
	tr := srv.Client().Transport.(*http.Transport).Clone()
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr == "storage.example.com:443" {
			addr = storage.Listener.Addr().String()
		}
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	h.extensions.markets.client, h.extensions.markets.githubAPI = &http.Client{Transport: tr}, srv.URL
	gh.files[extension.IndexFile] = marketIndex(t, "Example plugins", marketEntry(t, dir, "release:v1/example-ui-1.0.0.tar.gz", v1))
	_, mk := call(t, mux, "POST", "/api/v1/extensions/-/markets", phone, `{"spec": "github:example/plugins"}`)
	preview := fmt.Sprintf(`{"market": %q, "extension": "example-ui", "async": true}`, mk["id"].(string))

	previewed := func() (id, previewID, digest string) {
		t.Helper()
		id = opCall(t, mux, "POST", "/api/v1/extensions/-/catalog/preview", phone, preview)
		if v := waitOp(t, h, id, opNeedsConsent, opFailed); v.State != opNeedsConsent {
			t.Fatalf("previewed: %+v", v)
		}
		_, one := call(t, mux, "GET", "/api/v1/extensions/-/operations/"+id, phone, "")
		pv, _ := one["preview"].(map[string]any)
		previewID, _ = pv["preview_id"].(string)
		digest, _ = pv["permissions_digest"].(string)
		if previewID == "" {
			t.Fatalf("no preview: %v", one)
		}
		return id, previewID, digest
	}

	// Claimed once: a second install of the same preview is refused while the first runs.
	id, previewID, _ := previewed()
	pv := h.extensions.reg.PreviewOf(previewID)
	first, err := h.opForInstall(pv, previewID, "", i18n.Default)
	if err != nil || first.v.ID != id {
		t.Fatalf("not the preview's operation %s: %v %v", id, first, err)
	}
	if v := h.snapshot(first); v.State != opRunning || v.PreviewID != "" {
		t.Fatalf("claimed but %+v", v)
	}
	if second, err := h.opForInstall(pv, previewID, "", i18n.Default); second != nil || err == nil {
		t.Fatalf("a second install goes on: %v %v", second, err)
	}
	// A claimed operation does not wait for consent any more: its preview going away does not cancel it.
	h.extensions.reg.Discard(previewID)
	h.expireOps()
	if v := h.snapshot(first); v.State != opRunning {
		t.Fatalf("a claimed operation canceled: %+v", v)
	}
	// Nor does its preview going away make it a package from elsewhere: an install that goes through uses its preview
	// up while its operation still runs, and a phone installing that preview again is told it is being installed
	// (409), not that phones only install from markets (403).
	again := fmt.Sprintf(`{"preview_id": %q}`, previewID)
	if code, out := call(t, mux, "POST", "/api/v1/extensions/-/install", phone, again); code != http.StatusConflict || out["code"] != "installing_already" {
		t.Fatalf("installing a claimed preview again: %d %v", code, out)
	}
	// Its bundle still has to come from a market: a member from elsewhere that is still waiting is refused.
	local, err := h.extensions.reg.Preview(context.Background(), "dir:"+notesPackage(t, "1.0.0"), "")
	if err != nil {
		t.Fatal(err)
	}
	withLocal := func(previewID string) string {
		return fmt.Sprintf(`{"preview_id": %q, "bundle": [{"preview_id": %q, "permissions_digest": %q}]}`, previewID, local.ID, local.Digest)
	}
	if code, out := call(t, mux, "POST", "/api/v1/extensions/-/install", phone, withLocal(previewID)); code != http.StatusForbidden || out["code"] != "this_machine_only" {
		t.Fatalf("a claimed preview with a bundle member from elsewhere: %d %v", code, out)
	}

	// Two installs of one preview at once: one installs in the preview's operation, which ends succeeded with no
	// error; the other is refused (or, when the first was done already, fails in an operation of its own).
	id, previewID, digest := previewed()
	body := fmt.Sprintf(`{"preview_id": %q, "permissions_digest": %q}`, previewID, digest)
	codes := make([]int, 2)
	outs := make([]map[string]any, 2)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Go(func() { codes[i], outs[i] = call(t, mux, "POST", "/api/v1/extensions/-/install", phone, body) })
	}
	wg.Wait()
	ok := 0
	for i, code := range codes {
		switch {
		case code == http.StatusOK:
			ok++
			if got := outs[i]["operation_id"]; got != id {
				t.Fatalf("installed in operation %v, not the preview's %s: %v", got, id, outs[i])
			}
		case code == http.StatusConflict && outs[i]["code"] == "installing_already":
		case code == http.StatusBadRequest && outs[i]["operation_id"] == nil:
		default:
			t.Fatalf("the other install: %d %v", code, outs[i])
		}
	}
	if ok != 1 {
		t.Fatalf("installs that went through: %v %v", codes, outs)
	}
	v := waitOp(t, h, id, opSucceeded, opFailed)
	if v.State != opSucceeded || v.Error != "" || v.ErrorCode != "" {
		t.Fatalf("the install that went through: %+v", v)
	}
	// Once that install is done its preview is used up: installing it again fails as such (400), still not as a package
	// from elsewhere.
	if code, out := call(t, mux, "POST", "/api/v1/extensions/-/install", phone, body); code != http.StatusBadRequest || out["code"] != "install_failed" {
		t.Fatalf("installing a used-up preview: %d %v", code, out)
	}
	// Asynchronously, in an operation of its own that the phone can follow and clear.
	code, out := call(t, mux, "POST", "/api/v1/extensions/-/install", phone, fmt.Sprintf(`{"preview_id": %q, "async": true}`, previewID))
	op, _ := out["operation"].(map[string]any)
	own, _ := op["id"].(string)
	if code != http.StatusAccepted || own == "" || own == id {
		t.Fatalf("installing a used-up preview asynchronously: %d %v", code, out)
	}
	if v := waitOp(t, h, own, opSucceeded, opFailed); v.State != opFailed {
		t.Fatalf("installed a used-up preview: %+v", v)
	}
	if code, out := call(t, mux, "GET", "/api/v1/extensions/-/operations/"+own, phone, ""); code != http.StatusOK {
		t.Fatalf("the phone's own operation: %d %v", code, out)
	}
	if code, out := call(t, mux, "DELETE", "/api/v1/extensions/-/operations/"+own, phone, ""); code != http.StatusNoContent {
		t.Fatalf("clearing the phone's own operation: %d %v", code, out)
	}
	// And a bundle member from elsewhere is still refused.
	if code, out := call(t, mux, "POST", "/api/v1/extensions/-/install", phone, withLocal(previewID)); code != http.StatusForbidden || out["code"] != "this_machine_only" {
		t.Fatalf("a used-up preview with a bundle member from elsewhere: %d %v", code, out)
	}
}
