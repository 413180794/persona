package hub

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
)

// macHub is testLive's hub (an iPhone "devtok" on a fake APNs) plus a Mac registered for pushes ("mactok"),
// wired to the hub's desktop presence like New does.
func macHub(t *testing.T) (*Hub, *fakeAPNs) {
	t.Helper()
	h, f := testLive(t)
	h.push.desktopOnline, h.push.desktopOpen = h.desktopOnline, h.desktopOpen
	h.push.register(Device{DeviceID: "mac", Name: "mac", Kind: kindMac, APNSToken: "mactok", Topic: "app.example"})
	return h, f
}

// pushedTo waits for n APNs calls after the first skip ones, then a little more for any that should not come, and
// returns their tokens, sorted.
func pushedTo(t *testing.T, f *fakeAPNs, skip, n int) []string {
	t.Helper()
	got := f.calls(t, skip+n)
	time.Sleep(150 * time.Millisecond)
	f.mu.Lock()
	got = append(got[:0:0], f.got...)
	f.mu.Unlock()
	var out []string
	for _, c := range got[skip:] {
		out = append(out, c.Token)
	}
	sort.Strings(out)
	return out
}

func same(a []string, b ...string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// While the Mac app is open it posts the agents' notifications itself (MacAttentionWatcher), so the hub does not
// push those to it as well; what was asked for explicitly still goes; once it closes, the hub pushes it everything.
func TestOpenMacLeavesAgentAlertsToTheApp(t *testing.T) {
	h, f := macHub(t)
	mac := &client{out: make(chan []byte, 16), subs: map[[2]string]bool{}, device: "dev_mac", role: roleFull}
	h.mu.Lock()
	h.clients[mac] = struct{}{}
	h.mu.Unlock()
	h.onClientMessage(context.Background(), mac, []byte(`{"type":"register_desktop","device_name":"MacBook","device_id":"mac"}`))

	input := func(p NotifyPrefs) bool { return p.Input }
	h.push.notifyDone("m1", "笔记本", "s1", "需要你", "要授权", input)
	if got := pushedTo(t, f, 0, 1); !same(got, "devtok") {
		t.Fatalf("an agent's alert with the Mac open went to %v", got)
	}
	h.push.notifyDone("m1", "Persona", "s1", "提醒", "该走了", nil)
	if got := pushedTo(t, f, 1, 2); !same(got, "devtok", "mactok") {
		t.Fatalf("an explicit reminder went to %v", got)
	}

	h.dropClient(mac)
	h.push.notifyDone("m1", "笔记本", "s1", "需要你", "要授权", input)
	if got := pushedTo(t, f, 3, 2); !same(got, "devtok", "mactok") {
		t.Fatalf("with the Mac app closed: %v", got)
	}
}

// An older Mac app registers as a desktop without its push id: nothing to match, so it is not taken for open.
func TestDesktopWithoutPushIDIsNotOpen(t *testing.T) {
	h, _ := macHub(t)
	mac := &client{out: make(chan []byte, 16), subs: map[[2]string]bool{}, device: "dev_mac", role: roleFull}
	h.mu.Lock()
	h.clients[mac] = struct{}{}
	h.mu.Unlock()
	h.onClientMessage(context.Background(), mac, []byte(`{"type":"register_desktop","device_name":"MacBook"}`))
	if h.desktopOpen("mac") || h.desktopOpen("") {
		t.Fatal("open without a push id")
	}
	if !h.desktopOnline() {
		t.Fatal("still a desktop")
	}
}

// The silent wake push is for the phone_* tools, which run on the iPhone: never to a Mac.
func TestWakeGoesToPhonesOnly(t *testing.T) {
	h, f := macHub(t)
	h.push.wake("tool")
	if got := pushedTo(t, f, 0, 1); !same(got, "devtok") {
		t.Fatalf("wake went to %v", got)
	}
	if c := f.calls(t, 1)[0]; c.Type != "background" {
		t.Fatalf("wake pushed as %q", c.Type)
	}
}

// Signing a Mac out (its device credential revoked) stops its pushes: the hub forgets the device it registered.
func TestRevokedCredentialGetsNoPushes(t *testing.T) {
	h, f := macHub(t)
	h.store = &tokenStore{memStore: *newMemStore(), rows: map[string]db.DeviceToken{}, rev: map[string]bool{}}
	mux := h.routes()
	code, cred := call(t, mux, "POST", "/api/v1/auth/tokens", h.cfg.Token, `{"name":"MacBook","kind":"desktop"}`)
	if code != 200 {
		t.Fatal(code, cred)
	}
	tok := cred["token"].(string)
	// The device id a request names is the app's own; which credential sent it is the hub's to record.
	body := `{"device_id":"mac2","name":"MacBook","kind":"mac","apns_token":"mac2tok","topic":"app.example","credential":"dev_other"}`
	if code, _ := call(t, mux, "POST", "/api/v1/devices", tok, body); code != 200 {
		t.Fatal(code)
	}
	if d, _ := h.push.device("mac2"); d.Credential != cred["id"] {
		t.Fatalf("registered by %q, want %q", d.Credential, cred["id"])
	}
	h.push.notifyDone("m1", "Persona", "s1", "提醒", "该走了", nil)
	// The new credential was announced to the devices already there ("New device paired"): two more before the reminder's three.
	if got := pushedTo(t, f, 0, 5); !same(got, "devtok", "devtok", "mac2tok", "mactok", "mactok") {
		t.Fatalf("before signing out: %v", got)
	}

	if code, _ := call(t, mux, "DELETE", "/api/v1/auth/tokens/"+cred["id"].(string), h.cfg.Token, ""); code != 200 {
		t.Fatal(code)
	}
	if _, ok := h.push.device("mac2"); ok {
		t.Fatal("the signed-out Mac is still registered")
	}
	h.push.notifyDone("m1", "Persona", "s1", "提醒", "该走了", nil)
	if got := pushedTo(t, f, 5, 2); !same(got, "devtok", "mactok") {
		t.Fatalf("after signing out: %v", got)
	}
}
