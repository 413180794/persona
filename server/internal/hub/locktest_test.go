package hub

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
)

func lockTestHub(t *testing.T) (*Hub, *db.DB) {
	t.Helper()
	url := os.Getenv("PERSONA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("PERSONA_TEST_DATABASE_URL not set")
	}
	d, err := db.Open(context.Background(), url, db.HubSet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	return &Hub{store: d, clients: map[*client]struct{}{}, logger: log.New(io.Discard, "", 0)}, d
}

func lockTestConnection(t *testing.T, h *Hub, device, hello string) *client {
	t.Helper()
	now := time.Now()
	id, err := h.store.InsertDeviceSession(context.Background(), db.DeviceSession{
		DeviceID: device, ConnectedAt: now, Source: "192.0.2.0/24", HeartbeatInterval: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	c := &client{device: device, deviceSessionID: id, since: now, out: make(chan []byte, 8)}
	// A hello starts fleetClientsChanged, which ranges h.clients under h.mu.
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	if hello != "" {
		h.onClientHello(c, []byte(hello))
		<-c.out
	}
	return c
}

func TestLockTestRoundTrip(t *testing.T) {
	h, d := lockTestHub(t)
	ctx := context.Background()
	device := "test-device-" + randomHex(8)
	c := lockTestConnection(t, h, device, `{"features":["lock_test"],"device_id":"untrusted-device"}`)
	legacy := lockTestConnection(t, h, device, "")
	other := lockTestConnection(t, h, "other-"+device, `{"features":["lock_test"]}`)
	mux := http.NewServeMux()
	h.lockTestRoutes(mux)
	request := func(who, method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(
			context.WithValue(ctx, principalKey{}, principal{ID: who, Role: roleFull}))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	base := "/api/v1/devices/" + device + "/lock-tests"
	for _, body := range []string{`{"delay_seconds":0}`, `{"delay_seconds":3601}`, `{"delay_seconds":"invalid"}`} {
		if w := request(device, "POST", base, body); w.Code != http.StatusBadRequest {
			t.Fatalf("invalid delay: %d %s", w.Code, w.Body.String())
		}
	}
	if w := request("other-device", "POST", base, `{}`); w.Code != http.StatusForbidden {
		t.Fatalf("cross-device scheduling: %d", w.Code)
	}
	stop, err := h.startLockTests(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	w := request(device, "POST", base, `{"delay_seconds":1}`)
	var scheduled db.LockTest
	if err := json.Unmarshal(w.Body.Bytes(), &scheduled); err != nil || w.Code != http.StatusOK || scheduled.TestID == "" {
		t.Fatalf("schedule: %d %s (%v)", w.Code, w.Body.String(), err)
	}
	path := base + "/" + scheduled.TestID
	if w := request(device, "POST", path+"/receipt", `{}`); w.Code != http.StatusConflict {
		t.Fatalf("premature receipt: %d", w.Code)
	}
	select {
	case raw := <-c.out:
		var event map[string]json.RawMessage
		if err := json.Unmarshal(raw, &event); err != nil {
			t.Fatal(err)
		}
		var testID, kind string
		_ = json.Unmarshal(event["lock_test_id"], &testID)
		_ = json.Unmarshal(event["type"], &kind)
		if testID != scheduled.TestID || kind != "lock_test" || time.Now().Before(scheduled.DueAt) {
			t.Fatalf("unexpected test event: %s", raw)
		}
		for key := range event {
			if key != "type" && !strings.HasPrefix(key, "lock_test_") {
				t.Fatalf("unscoped WS field: %s", key)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("scheduled notification did not arrive")
	}
	if len(legacy.out) != 0 || len(other.out) != 0 {
		t.Fatal("notification reached a legacy or different device connection")
	}
	if w := request("other-device", "POST", path+"/receipt", `{}`); w.Code != http.StatusForbidden {
		t.Fatalf("cross-device receipt: %d", w.Code)
	}
	w = request(device, "POST", path+"/receipt", `{"notification_shown":true}`)
	var received db.LockTest
	if err := json.Unmarshal(w.Body.Bytes(), &received); err != nil || w.Code != http.StatusOK || received.Status != "received" ||
		received.DeviceSessionID == nil || *received.DeviceSessionID != c.deviceSessionID ||
		received.ReceivedAt == nil || received.SentAt == nil || received.NotificationShown == nil || !*received.NotificationShown {
		t.Fatalf("receipt: %d %s (%v)", w.Code, w.Body.String(), err)
	}
	// A retry cannot change the first result; a fresh Hub reads it from the same store.
	w = request(device, "POST", path+"/receipt", `{"notification_shown":false}`)
	if w.Code != http.StatusOK {
		t.Fatalf("receipt retry: %d", w.Code)
	}
	restarted := &Hub{store: d}
	restartMux := http.NewServeMux()
	restarted.lockTestRoutes(restartMux)
	mux = restartMux
	w = request(device, "GET", path, "")
	var persisted db.LockTest
	if err := json.Unmarshal(w.Body.Bytes(), &persisted); err != nil || persisted.ReceivedAt == nil || !persisted.ReceivedAt.Equal(*received.ReceivedAt) ||
		persisted.NotificationShown == nil || !*persisted.NotificationShown {
		t.Fatalf("persisted result: %s (%v)", w.Body.String(), err)
	}
	if err := h.dispatchLockTests(ctx, time.Now()); err != nil || len(c.out) != 0 {
		t.Fatalf("test was sent more than once: %v", err)
	}
}

func TestLockTestUnavailableAndRecovery(t *testing.T) {
	h, d := lockTestHub(t)
	ctx, now := context.Background(), time.Now()
	device := "test-device-" + randomHex(8)
	legacy := lockTestConnection(t, h, device, "")
	wrongCapability := lockTestConnection(t, h, device, `{"features":["ws_notify"]}`)
	create := func(due time.Time) db.LockTest {
		t.Helper()
		test := db.LockTest{TestID: "lt_" + randomHex(16), DeviceID: device, CreatedAt: now.Add(-time.Hour), DueAt: due}
		if err := d.CreateLockTest(ctx, test); err != nil {
			t.Fatal(err)
		}
		return test
	}
	check := func(test db.LockTest, status string) {
		t.Helper()
		got, err := d.LockTest(ctx, device, test.TestID)
		if err != nil || got == nil || got.Status != status {
			t.Fatalf("result = %+v, %v; want %s", got, err, status)
		}
	}
	offline := create(now.Add(-time.Second))
	if err := h.dispatchLockTests(ctx, now); err != nil {
		t.Fatal(err)
	}
	check(offline, "unavailable")
	if len(legacy.out) != 0 || len(wrongCapability.out) != 0 {
		t.Fatal("notification sent without the lock_test capability")
	}
	if got, err := d.ReceiveLockTest(ctx, device, offline.TestID, nil, now); err != nil || got != nil {
		t.Fatalf("unsent test accepted receipt: %+v, %v", got, err)
	}
	interrupted := create(now.Add(-time.Second))
	future := create(now.Add(time.Hour))
	stop, err := h.startLockTests(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stop()
	check(interrupted, "interrupted")
	check(future, "scheduled")
	check(offline, "unavailable")
}
