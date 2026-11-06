package extmgr

import (
	"fmt"
	"testing"

	"github.com/nwverse/persona/server/internal/extension"
)

func TestBundleAutoUpgradeRequiresMemberConsent(t *testing.T) {
	h, phone, _ := marketHub(t)
	reg := h.extensions.reg
	mk := newUpgradeMarket(t)
	h.extensions.markets.client = mk.srv.Client()
	dir := bundleGraphPackage(t, "example-ui", "1.0.0")
	mk.publish(dir)
	code, out := call(t, h.routes(), "POST", "/api/v1/extensions/-/markets", phone, fmt.Sprintf(`{"spec":%q}`, mk.srv.URL+"/plugins/"))
	if code != 200 {
		t.Fatalf("market %d %v", code, out)
	}
	mid := out["id"].(string)
	code, out = call(t, h.routes(), "POST", "/api/v1/extensions/-/catalog/preview", phone, fmt.Sprintf(`{"market":%q,"extension":"example-ui"}`, mid))
	if code != 200 {
		t.Fatalf("preview %d %v", code, out)
	}
	pv := reg.PreviewOf(out["preview_id"].(string))
	if _, err := h.installExtensionPlan(t.Context(), "", pv.ID, pv.Digest, nil, "test"); err != nil {
		t.Fatal(err)
	}
	bundleMarketFixture(t, h, phone, map[string]string{"new-child": bundleGraphPackage(t, "new-child", "1.0.0")})
	// Reuse one TLS transport so both fake markets can be downloaded from.
	h.extensions.markets.client = mk.srv.Client()
	dir = bundleGraphPackage(t, "example-ui", "2.0.0", extension.Dependency{ID: "new-child"})
	mk.publish(dir)
	if _, err := h.marketRead(t.Context(), mid); err != nil {
		t.Fatal(err)
	}
	u := h.extensionUpgrades()
	u.check(t.Context())
	u.upgrade(t.Context())
	if reg.Snapshot("example-ui").Record.Version != "1.0.0" || reg.Get("new-child") != nil {
		t.Fatal("automatic upgrade installed an unconfirmed member")
	}
	if up := upgradeOf(t, h.routes(), phone); up["state"] != upgradeNeedsConsent {
		t.Fatalf("member consent not exposed: %v", up)
	}
}

func TestBundleAutoUpgradeCommitFailureUsesSharedRecovery(t *testing.T) {
	h, phone, _ := marketHub(t)
	reg := h.extensions.reg
	mk := newUpgradeMarket(t)
	h.extensions.markets.client = mk.srv.Client()
	mk.publish(bundleGraphPackage(t, "example-ui", "1.0.0"))
	code, out := call(t, h.routes(), "POST", "/api/v1/extensions/-/markets", phone, fmt.Sprintf(`{"spec":%q}`, mk.srv.URL+"/plugins/"))
	if code != 200 {
		t.Fatalf("market %d %v", code, out)
	}
	mid := out["id"].(string)
	code, out = call(t, h.routes(), "POST", "/api/v1/extensions/-/catalog/preview", phone, fmt.Sprintf(`{"market":%q,"extension":"example-ui"}`, mid))
	if code != 200 {
		t.Fatalf("preview %d %v", code, out)
	}
	pv := reg.PreviewOf(out["preview_id"].(string))
	if _, err := h.installExtensionPlan(t.Context(), "", pv.ID, pv.Digest, nil, "test"); err != nil {
		t.Fatal(err)
	}
	mk.publish(bundleGraphPackage(t, "example-ui", "2.0.0"))
	st := h.store.(*marketTestStore).extensionStore
	st.mu.Lock()
	st.failPutID = "example-ui"
	st.mu.Unlock()
	if _, err := h.marketRead(t.Context(), mid); err != nil {
		t.Fatal(err)
	}
	u := h.extensionUpgrades()
	u.check(t.Context())
	u.upgrade(t.Context())
	if reg.Snapshot("example-ui").Record.Version != "1.0.0" {
		t.Fatal("failed automatic plan replaced installed version")
	}
	if up := upgradeOf(t, h.routes(), phone); up["state"] != upgradeFailed {
		t.Fatalf("failure was not exposed: %v", up)
	}
}
