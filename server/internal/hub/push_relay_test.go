package hub

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/apns"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/pushcrypto"
	"github.com/nwverse/persona/server/internal/relay"
)

// relayAPNs is APNs behind a real relay.Server.
type relayAPNs struct {
	mu   sync.Mutex
	got  []apns.Request
	gone bool
}

func (f *relayAPNs) Send(_ context.Context, r apns.Request) (apns.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, r)
	if f.gone {
		return apns.Result{Status: 410, Reason: "Unregistered"}, nil
	}
	return apns.Result{Status: 200}, nil
}

func (f *relayAPNs) wait(t *testing.T, n int) []apns.Request {
	t.Helper()
	for range 300 {
		f.mu.Lock()
		if len(f.got) >= n {
			out := append([]apns.Request(nil), f.got...)
			f.mu.Unlock()
			return out
		}
		f.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("want %d pushes at the relay's APNs, got %d", n, len(f.got))
	return nil
}

// relayHub: a hub without apns.json and a phone registered with a relay (as the app does: its grant, its key).
func relayHub(t *testing.T) (*Hub, *relayAPNs, *ecdh.PrivateKey) {
	t.Helper()
	h := newTestHub(t)
	if h.push.direct != nil {
		t.Fatal("test hub has an APNs key")
	}
	f := &relayAPNs{}
	rs, err := relay.New(relay.Config{Topics: []string{"app.example"}}, f, bytes.Repeat([]byte{3}, 32), t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(rs.Handler())
	t.Cleanup(srv.Close)
	// The phone registers with the relay itself.
	resp, err := srv.Client().Post(srv.URL+"/relay/v1/register", "application/json",
		strings.NewReader(`{"apns_token":"a1b2c3d4e5f60718a1b2c3d4e5f60718","env":"sandbox","topic":"app.example"}`))
	if err != nil {
		t.Fatal(err)
	}
	var reg struct{ Grant string }
	_ = json.NewDecoder(resp.Body).Decode(&reg)
	resp.Body.Close()
	priv, _ := ecdh.P256().GenerateKey(rand.Reader)
	h.push.register(Device{DeviceID: "phone", Name: "phone", Kind: "iphone", APNSToken: "a1b2c3d4e5f60718a1b2c3d4e5f60718", APNSEnv: "sandbox",
		Topic: "app.example", PushKey: base64.StdEncoding.EncodeToString(priv.PublicKey().Bytes()), RelayURL: srv.URL + "/relay/", RelayGrant: reg.Grant})
	return h, f, priv
}

// opened is what the phone's notification service extension sees after decrypting.
func opened(t *testing.T, priv *ecdh.PrivateKey, r apns.Request) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(r.Body, &env); err != nil {
		t.Fatal(err)
	}
	sealed, _ := base64.StdEncoding.DecodeString(env["e"].(string))
	plain, err := pushcrypto.Open(priv, sealed)
	if err != nil {
		t.Fatalf("phone cannot open: %v", err)
	}
	var p map[string]any
	_ = json.Unmarshal(plain, &p)
	return p
}

func TestRelayInteractionIsSealed(t *testing.T) {
	h, f, priv := relayHub(t)
	if !h.push.ready() {
		t.Fatal("not ready with a relayed phone")
	}
	ix := proto.Interaction{ID: "ix_secret", Kind: proto.IxPermission, MachineID: "m1", SessionID: "sess_secret", SessionTitle: "重构支付", Summary: "Bash: rm -rf build"}
	h.push.notifyInteraction(ix, "笔记本")
	r := f.wait(t, 1)[0]
	if r.PushType != "alert" || r.Token != "a1b2c3d4e5f60718a1b2c3d4e5f60718" || r.Env != "sandbox" {
		t.Fatalf("request %+v", r)
	}
	// Ids with "_", which base64 never has: "s1" turned up by chance in the ciphertext about one run in five.
	for _, leak := range []string{"重构支付", "rm -rf", "笔记本", "ix_secret", "授权", "INTERACTION", "sess_secret"} {
		if bytes.Contains(r.Body, []byte(leak)) || strings.Contains(r.CollapseID, leak) {
			t.Fatalf("relay saw %q: %s", leak, r.Body)
		}
	}
	if r.CollapseID == "" || r.CollapseID == "ix_secret" {
		t.Fatalf("collapse id %q", r.CollapseID)
	}
	p := opened(t, priv, r)
	aps := p["aps"].(map[string]any)
	alert := aps["alert"].(map[string]any)
	if alert["body"] != "Bash: rm -rf build" || aps["category"] != "INTERACTION_PERMISSION" || p["interaction_id"] != "ix_secret" || p["session_id"] != "sess_secret" {
		t.Fatalf("opened %v", p)
	}
	if iat, _ := p["iat"].(float64); time.Since(time.Unix(int64(iat), 0)) > time.Minute {
		t.Fatalf("iat %v", p["iat"])
	}
	// Which hub sent it (its group, for an app paired with several): inside the sealed payload, not before the relay.
	if p["hub_id"] != h.hubID() || bytes.Contains(r.Body, []byte(h.hubID())) {
		t.Fatalf("hub_id %v in %v", p["hub_id"], p)
	}
}

func TestRelayDoneAndWake(t *testing.T) {
	h, f, priv := relayHub(t)
	h.push.notifyDone("m1", "笔记本", "s1", "✅ 重构支付", "改好了", nil)
	h.push.wake("tool")
	got := f.wait(t, 2)
	byType := map[string]apns.Request{}
	for _, r := range got {
		byType[r.PushType] = r
	}
	if p := opened(t, priv, byType["alert"]); p["aps"].(map[string]any)["alert"].(map[string]any)["body"] != "改好了" {
		t.Fatalf("done %v", p)
	}
	bg := byType["background"]
	if bg.Priority != "5" || bytes.Contains(bg.Body, []byte("tool")) {
		t.Fatalf("wake %+v %s", bg, bg.Body)
	}
	if p := opened(t, priv, bg); p["jarvis_wake"] != "tool" {
		t.Fatalf("wake %v", p)
	}
}

func TestRelayLiveActivity(t *testing.T) {
	h, f, _ := relayHub(t)
	h.live.update("phone", func(v *db.LiveActivity) { v.PushToStartToken = "abcdef0123456789abcdef0123456789" })
	h.live.flush(FleetState{Phase: "working", Working: 1, Items: []FleetItem{{ID: "m/a", Title: "api", Kind: "working"}}}, false, false)
	r := f.wait(t, 1)[0]
	if r.PushType != "liveactivity" || r.Token != "abcdef0123456789abcdef0123456789" || !bytes.Contains(r.Body, []byte(`"event":"start"`)) {
		t.Fatalf("live activity %+v %s", r, r.Body)
	}
}

func TestRelayGoneForgetsDevice(t *testing.T) {
	h, f, _ := relayHub(t)
	f.gone = true
	h.push.notifyDone("m1", "笔记本", "s1", "t", "x", nil)
	f.wait(t, 1)
	for range 200 {
		if _, ok := h.push.device("phone"); !ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("device kept after the relay said its token is gone")
}

// Without the phone's key there is nothing to seal to: no push, never one in the clear.
func TestRelayNeedsAKey(t *testing.T) {
	h, f, _ := relayHub(t)
	d, _ := h.push.device("phone")
	d.PushKey = ""
	h.push.register(d)
	if h.push.ready() {
		t.Fatal("ready without a key")
	}
	h.push.notifyDone("m1", "笔记本", "s1", "t", "x", nil)
	d.PushKey = "bm90IGEga2V5" // garbage
	h.push.register(d)
	h.push.notifyDone("m1", "笔记本", "s1", "t", "x", nil)
	time.Sleep(200 * time.Millisecond)
	if len(f.got) != 0 {
		t.Fatalf("pushed %d", len(f.got))
	}
}

// A hub with its own key keeps pushing straight to APNs, even for a phone that also registered with a relay.
func TestDirectKeyWinsOverRelay(t *testing.T) {
	h, relayed, _ := relayHub(t)
	direct := &fakeAPNs{gone: map[string]bool{}}
	srv := httptest.NewServer(direct)
	t.Cleanup(srv.Close)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	h.push.direct = apns.NewWithKey(apnsConfig{KeyID: "K", TeamID: "T", Topic: "app.example"}, key)
	h.push.direct.Base = srv.URL
	h.push.notifyDone("m1", "笔记本", "s1", "✅ t", "改好了", nil)
	c := direct.calls(t, 1)
	if c[0].Token != "a1b2c3d4e5f60718a1b2c3d4e5f60718" || c[0].Type != "alert" {
		t.Fatalf("direct %+v", c[0])
	}
	if !strings.Contains(string(c[0].Body), `"hub_id":"`+h.hubID()+`"`) {
		t.Fatalf("direct push without the hub's hub_id: %s", c[0].Body)
	}
	time.Sleep(100 * time.Millisecond)
	if len(relayed.got) != 0 {
		t.Fatal("went through the relay too")
	}
}

func TestRegisterDropsABadRelayURL(t *testing.T) {
	p := newPusher(t.TempDir(), nil, t.Logf)
	for _, u := range []string{"ftp://x/relay", "relay", "https://x/relay?y=1", "http://relay.example/relay"} {
		p.register(Device{DeviceID: "d", RelayURL: u, RelayGrant: "g", PushKey: "k"})
		if d, _ := p.device("d"); d.RelayURL != "" || d.RelayGrant != "" {
			t.Errorf("%q kept: %+v", u, d)
		}
	}
	for _, u := range []string{"https://relay.example/relay", "http://127.0.0.1:1234/relay", "http://localhost/relay"} {
		p.register(Device{DeviceID: "d", RelayURL: u, RelayGrant: "g", PushKey: "k"})
		if d, _ := p.device("d"); d.RelayURL != u {
			t.Errorf("%q dropped", u)
		}
	}
}

// The relay's secret was replaced: the old grant gets 401, the hub registers the token again and the push goes out.
func TestRelayRenewsAStaleGrant(t *testing.T) {
	h, f, priv := relayHub(t)
	d, _ := h.push.device("phone")
	d.RelayGrant = "c3RhbGU" // not this relay's
	h.push.register(d)
	h.push.notifyDone("m1", "笔记本", "s1", "✅ t", "改好了", nil)
	r := f.wait(t, 1)[0]
	if p := opened(t, priv, r); p["aps"].(map[string]any)["alert"].(map[string]any)["body"] != "改好了" {
		t.Fatalf("opened %v", p)
	}
	d2, _ := h.push.device("phone")
	if d2.RelayGrant == "c3RhbGU" || d2.RelayGrant == "" {
		t.Fatalf("grant not renewed: %q", d2.RelayGrant)
	}
	// The app registers again with the grant it cached: the renewed one stays.
	h.push.register(d)
	if d3, _ := h.push.device("phone"); d3.RelayGrant != d2.RelayGrant {
		t.Fatalf("app's stale grant undid the renewal: %q", d3.RelayGrant)
	}
	// A grant the app newly got (it registered with the relay itself) still replaces it.
	d.RelayGrant = "bmV3"
	h.push.register(d)
	if d4, _ := h.push.device("phone"); d4.RelayGrant != "bmV3" {
		t.Fatalf("new grant ignored: %q", d4.RelayGrant)
	}
}

// A long question or command would not fit the relay's limit: the texts are shortened, the push still goes out.
func TestRelayShortensWhatDoesNotFit(t *testing.T) {
	h, f, priv := relayHub(t)
	long := strings.Repeat("很长的问题", 1200)
	h.push.notifyInteraction(proto.Interaction{ID: "ix", Kind: proto.IxQuestion, MachineID: "m1", SessionID: "s1", SessionTitle: "t",
		Questions: []proto.Question{{Question: long}}}, "笔记本")
	r := f.wait(t, 1)[0]
	p := opened(t, priv, r)
	body, _ := p["aps"].(map[string]any)["alert"].(map[string]any)["body"].(string)
	if !strings.HasPrefix(body, "很长的问题") || len(body) >= len(long) || p["interaction_id"] != "ix" {
		t.Fatalf("body %d bytes, %v", len(body), p["interaction_id"])
	}
	if len(r.Body) > 4096 {
		t.Fatalf("APNs payload %d bytes", len(r.Body))
	}
}

// TestManualRelayPush pushes through a running relay the way a hub without apns.json does, for the simulator's
// end-to-end check (scripts/test/relay_sim_e2e.sh). Opt-in: JARVIS_RELAY_URL, JARVIS_RELAY_GRANT, JARVIS_PUSH_KEY
// (the phone's public key); JARVIS_RELAY_KIND is "interaction" (default) or "done".
func TestManualRelayPush(t *testing.T) {
	url, grant, key := os.Getenv("JARVIS_RELAY_URL"), os.Getenv("JARVIS_RELAY_GRANT"), os.Getenv("JARVIS_PUSH_KEY")
	if url == "" || grant == "" || key == "" {
		t.Skip("set JARVIS_RELAY_URL, JARVIS_RELAY_GRANT and JARVIS_PUSH_KEY")
	}
	accepted := make(chan string, 4)
	p := newPusher(t.TempDir(), nil, func(f string, a ...any) {
		line := fmt.Sprintf(f, a...)
		t.Log(line)
		if strings.HasPrefix(line, "relay simulator") {
			accepted <- line
		}
	})
	p.register(Device{DeviceID: "sim", Name: "simulator", Kind: "iphone", PushKey: key, RelayURL: url, RelayGrant: grant})
	switch os.Getenv("JARVIS_RELAY_KIND") {
	case "done":
		p.notifyDone("m1", "笔记本", "s1", "✅ 重构支付模块", "改好了，测试全过", nil)
	default:
		p.notifyInteraction(proto.Interaction{ID: "ix_e2e", Kind: proto.IxPermission, MachineID: "m1", SessionID: "s1",
			SessionTitle: "重构支付模块", Summary: "Bash: swift test"}, "笔记本")
	}
	select {
	case line := <-accepted:
		if !strings.Contains(line, "accepted") {
			t.Fatal(line)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("relay did not answer")
	}
}
