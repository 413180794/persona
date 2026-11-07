package hub

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/apns"
)

// collapseAPNs answers the first push with 503 and records every push's apns-collapse-id.
type collapseAPNs struct {
	mu  sync.Mutex
	ids []string
}

func (f *collapseAPNs) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only pushes count: a node's port probe (GET /, node/ports.go) reaches this server too.
	if r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, "/3/device/") {
		http.NotFound(w, r)
		return
	}
	f.mu.Lock()
	f.ids = append(f.ids, r.Header.Get("apns-collapse-id"))
	first := len(f.ids) == 1
	f.mu.Unlock()
	if first {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
}

func (f *collapseAPNs) wait(t *testing.T, n int) []string {
	t.Helper()
	for range 200 {
		f.mu.Lock()
		if len(f.ids) >= n {
			out := append([]string(nil), f.ids...)
			f.mu.Unlock()
			return out
		}
		f.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("want %d pushes", n)
	return nil
}

// A push without a collapse id of its own (a finished turn's) keeps one id through its retries, so one APNs took
// twice shows once; another push gets another id (#4158).
func TestRetriedPushKeepsItsCollapseID(t *testing.T) {
	h := newTestHub(t)
	f := &collapseAPNs{}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	h.push.direct = apns.NewWithKey(apnsConfig{KeyID: "K", TeamID: "T", Topic: "app.example"}, key)
	h.push.direct.Base = srv.URL
	h.push.retryWaits = []time.Duration{time.Millisecond}
	h.push.register(Device{DeviceID: "phone", Name: "phone", APNSToken: "devtok", Topic: "app.example"})

	h.push.notifyDone("m1", "box", "s1", "重构", "完成了", nil)
	ids := f.wait(t, 2)
	if ids[0] == "" || ids[0] != ids[1] || !strings.HasPrefix(ids[0], "p_") || len(ids[0]) > 64 {
		t.Fatalf("a retried push changed or lacks its collapse id: %q", ids)
	}
	h.push.notifyDone("m1", "box", "s1", "重构", "又完成了", nil)
	ids = f.wait(t, 3)
	if ids[2] == "" || ids[2] == ids[0] {
		t.Fatalf("another push shares the first one's collapse id: %q", ids)
	}
}
