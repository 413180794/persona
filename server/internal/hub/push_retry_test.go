package hub

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"

	"github.com/nwverse/persona/server/internal/apns"
	"github.com/nwverse/persona/server/internal/proto"
)

// flakyAPNs answers the first `fail` pushes with status, then 200.
type flakyAPNs struct {
	mu     sync.Mutex
	n      int
	fail   int
	status int
}

func (f *flakyAPNs) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only pushes count: a node's port probe (GET /, node/ports.go) reaches this server too.
	if r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, "/3/device/") {
		http.NotFound(w, r)
		return
	}
	f.mu.Lock()
	f.n++
	n := f.n
	f.mu.Unlock()
	if n <= f.fail {
		w.WriteHeader(f.status)
		_, _ = w.Write([]byte(`{"reason":"ServiceUnavailable"}`))
	}
}

func (f *flakyAPNs) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.n
}

func directHub(t *testing.T, f http.Handler) *Hub {
	h := newTestHub(t)
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	h.push.direct = apns.NewWithKey(apnsConfig{KeyID: "K", TeamID: "T", Topic: "app.example"}, key)
	h.push.direct.Base = srv.URL
	h.push.retryWaits = []time.Duration{time.Millisecond, time.Millisecond}
	h.push.register(Device{DeviceID: "phone", Name: "phone", APNSToken: "devtok", Topic: "app.example"})
	return h
}

func waitCount(t *testing.T, f *flakyAPNs, want int) {
	t.Helper()
	for range 200 {
		if f.count() >= want {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(30 * time.Millisecond) // no extra tries after that
	if got := f.count(); got != want {
		t.Fatalf("APNs got %d pushes, want %d", got, want)
	}
}

// APNs busy for a moment (503): the card's push is tried again and gets through (#3989).
func TestDirectPushIsTriedAgainWhenAPNsIsBusy(t *testing.T) {
	f := &flakyAPNs{fail: 2, status: http.StatusServiceUnavailable}
	h := directHub(t, f)
	h.push.notifyInteraction(proto.Interaction{ID: "ix1", Kind: proto.IxPermission, SessionID: "s1", Summary: "rm -rf build"}, "box")
	waitCount(t, f, 3)
}

// APNs' own answer (400: a bad request) stands: not sent again.
func TestDirectPushIsNotTriedAgainOnABadRequest(t *testing.T) {
	f := &flakyAPNs{fail: 5, status: http.StatusBadRequest}
	h := directHub(t, f)
	h.push.notifyInteraction(proto.Interaction{ID: "ix2", Kind: proto.IxPermission, SessionID: "s1", Summary: "ls"}, "box")
	waitCount(t, f, 1)
}
