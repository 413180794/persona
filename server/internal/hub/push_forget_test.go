package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// forgetStore is a database whose device row delete either hangs until the caller gives up (a locked row) or fails.
type forgetStore struct {
	Store
	entered chan struct{}
	fail    error
}

func (s *forgetStore) Devices(context.Context) ([]Device, error) { return nil, nil }

func (s *forgetStore) SetDevicePrefs(context.Context, string, string, json.RawMessage) error {
	return nil
}

func (s *forgetStore) DeleteDevice(ctx context.Context, _ string) error {
	if s.fail != nil {
		return s.fail
	}
	close(s.entered)
	<-ctx.Done()
	return ctx.Err()
}

// logLines collects what the pusher logs.
type logLines struct {
	mu    sync.Mutex
	lines []string
}

func (l *logLines) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *logLines) has(sub string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, s := range l.lines {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// Dropping a dead token's device whose row delete hangs gives up after storeTimeout, logs it, and does not hold
// saveMu forever: another device's settings write still goes through.
func TestForgetIfCurrentStuckStoreGivesUp(t *testing.T) {
	store := &forgetStore{entered: make(chan struct{})}
	logs := &logLines{}
	p := newPusher(t.TempDir(), store, logs.logf)
	p.storeTimeout = 300 * time.Millisecond
	dead := Device{DeviceID: "dev-a", Name: "Phone A", APNSToken: "tok-a"}
	p.devices[dead.DeviceID] = dead

	done := make(chan struct{})
	go func() { p.forgetIfCurrent(dead); close(done) }()
	<-store.entered

	prefs := make(chan error, 1)
	go func() { prefs <- p.setPrefs("dev-b", NotifyPrefs{Input: false, Done: DoneOff}) }()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("forgetIfCurrent did not give up after storeTimeout")
	}
	select {
	case err := <-prefs:
		if err != nil {
			t.Fatalf("setPrefs dev-b: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("setPrefs for another device blocked behind a stuck device delete")
	}
	if _, ok := p.device("dev-a"); ok {
		t.Fatal("dev-a is still in memory after its token was gone")
	}
	if !logs.has("forget device Phone A: " + context.DeadlineExceeded.Error()) {
		t.Fatalf("no log line for the timed-out delete; logged %q", logs.lines)
	}
}

// A device row delete that fails is logged, for a dead token and for a signed-out device alike: the row stays and
// the device comes back on the next start.
func TestForgetStoreErrorIsLogged(t *testing.T) {
	for _, tc := range []struct {
		name   string
		forget func(p *Pusher, d Device)
	}{
		{"Gone", func(p *Pusher, d Device) { p.forgetIfCurrent(d) }},
		{"SignedOut", func(p *Pusher, d Device) { p.signedOut(d.Credential) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &forgetStore{fail: errors.New("row locked")}
			logs := &logLines{}
			p := newPusher(t.TempDir(), store, logs.logf)
			d := Device{DeviceID: "dev-a", Name: "Phone A", APNSToken: "tok-a", Credential: "cred-a"}
			p.devices[d.DeviceID] = d
			tc.forget(p, d)
			if _, ok := p.device("dev-a"); ok {
				t.Fatal("dev-a is still in memory")
			}
			if !logs.has("forget device Phone A: row locked") {
				t.Fatalf("no log line for the failed delete; logged %q", logs.lines)
			}
		})
	}
}
