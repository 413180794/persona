package hub

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// stuckPrefsStore is a database whose prefs write hangs (a locked row) until the caller gives up.
type stuckPrefsStore struct {
	sharedStore
	entered chan struct{}
}

func (s *stuckPrefsStore) Devices(context.Context) ([]Device, error) { return nil, nil }

func (s *stuckPrefsStore) SetDevicePrefs(ctx context.Context, _, _ string, _ json.RawMessage) error {
	close(s.entered)
	<-ctx.Done()
	return ctx.Err()
}

// A prefs write stuck in the database holds only itself: other devices' settings still read at once, and the
// write gives up after storeTimeout without touching the settings in memory.
func TestSetPrefsStuckStoreDoesNotBlockOthers(t *testing.T) {
	store := &stuckPrefsStore{entered: make(chan struct{})}
	p := newPusher(t.TempDir(), store, t.Logf)
	p.storeTimeout = 300 * time.Millisecond
	p.devices["dev-b"] = Device{DeviceID: "dev-b"}

	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- p.setPrefs("dev-a", NotifyPrefs{Input: false, Done: DoneOff}) }()
	<-store.entered

	read := make(chan NotifyPrefs, 1)
	go func() { read <- p.prefs("dev-b") }()
	select {
	case got := <-read:
		if want := defaultPrefs(); got.Input != want.Input || got.Done != want.Done {
			t.Fatalf("dev-b prefs = %+v, want defaults", got)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("prefs for another device blocked behind a stuck prefs write")
	}

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("setPrefs err = %v, want deadline exceeded", err)
		}
		if took := time.Since(start); took < p.storeTimeout {
			t.Fatalf("setPrefs returned after %v, before the %v bound", took, p.storeTimeout)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("setPrefs did not give up after storeTimeout")
	}
	if _, ok := p.device("dev-a"); ok {
		t.Fatal("a failed prefs write still added dev-a in memory")
	}
}
