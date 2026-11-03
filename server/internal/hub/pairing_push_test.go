package hub

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/apns"
)

func TestPairingPushToExistingDevice(t *testing.T) {
	h := newTestHub(t)
	h.store = newPairStore()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sent := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if json.NewDecoder(r.Body).Decode(&payload) == nil {
			select {
			case sent <- payload:
			default:
			}
		}
	}))
	t.Cleanup(srv.Close)
	h.push.direct = apns.NewWithKey(apnsConfig{KeyID: "K", TeamID: "T", Topic: "app.example"}, key)
	h.push.direct.Base = srv.URL
	h.push.devices["existing"] = Device{DeviceID: "existing", Name: "小王的手机", APNSToken: "example-token"}
	code, _ := call(t, h.routes(), "POST", "/api/v1/auth/tokens", h.cfg.Token, `{"name":"小王的新手机","kind":"phone"}`)
	if code != 200 {
		t.Fatalf("pair status %d", code)
	}
	select {
	case payload := <-sent:
		aps := payload["aps"].(map[string]any)
		alert := aps["alert"].(map[string]any)
		if alert["body"] != "有新设备配对：小王的新手机" {
			t.Fatal(alert)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pairing alert was not sent")
	}
}
