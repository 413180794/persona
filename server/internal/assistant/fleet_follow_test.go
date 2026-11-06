package assistant

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// followDevs is fleetDevs whose Follow a test answers (follow nil: nil) and counts.
type followDevs struct {
	*fleetDevs
	mu     sync.Mutex
	follow func(ctx context.Context, on bool) error
	asked  int
}

func (d *followDevs) Follow(ctx context.Context, _, _ string, on bool) error {
	d.mu.Lock()
	d.asked++
	fn := d.follow
	d.mu.Unlock()
	if fn == nil {
		return nil
	}
	return fn(ctx, on)
}

func (d *followDevs) askedTimes() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.asked
}

func newFollowTest(t *testing.T) (*Service, *followDevs) {
	t.Helper()
	d := &followDevs{fleetDevs: newFleetDevs()}
	d.add("mac", "Mac")
	return New(Options{Hub: newFakeHub(), Logf: t.Logf, Devices: d}), d
}

// A session.follow the devices did not take stays wanted (a reconnect's watch brings the items, and they are the
// channels'), and the next follow asks the devices again; once they took it, the one after does not.
func TestAFailedFollowIsAskedAgain(t *testing.T) {
	s, d := newFollowTest(t)
	ctx := context.Background()
	d.follow = func(context.Context, bool) error { return errNode }
	if err := s.call(ctx, "mac", DeviceSessionFollow, map[string]any{"session_id": "s1"}, nil); !errors.Is(err, errNode) {
		t.Fatalf("follow: %v, want the node's refusal", err)
	}
	if !s.followed("mac", "s1") {
		t.Fatal("no longer followed after the node refused: a reconnect's items would be dropped")
	}
	d.follow = nil
	if err := s.call(ctx, "mac", DeviceSessionFollow, map[string]any{"session_id": "s1"}, nil); err != nil {
		t.Fatal(err)
	}
	if got := d.askedTimes(); got != 2 {
		t.Fatalf("the devices were asked %d times, want 2", got)
	}
	if err := s.call(ctx, "mac", DeviceSessionFollow, map[string]any{"session_id": "s1"}, nil); err != nil || d.askedTimes() != 2 {
		t.Fatalf("followed again: %v, asked %d", err, d.askedTimes())
	}
}

// An unfollow the devices did not take is not wanted any more, and the next unfollow asks them again.
func TestAFailedUnfollowIsAskedAgain(t *testing.T) {
	s, d := newFollowTest(t)
	ctx := context.Background()
	if err := s.follow(ctx, "mac", "s1", true); err != nil {
		t.Fatal(err)
	}
	d.follow = func(context.Context, bool) error { return errNode }
	if err := s.follow(ctx, "mac", "s1", false); !errors.Is(err, errNode) {
		t.Fatalf("unfollow: %v", err)
	}
	if s.followed("mac", "s1") {
		t.Fatal("still followed after an unfollow")
	}
	d.follow = nil
	if err := s.follow(ctx, "mac", "s1", false); err != nil || d.askedTimes() != 3 {
		t.Fatalf("unfollow again: %v, asked %d", err, d.askedTimes())
	}
	if err := s.follow(ctx, "mac", "s1", false); err != nil || d.askedTimes() != 3 {
		t.Fatalf("unfollowed once more: %v, asked %d", err, d.askedTimes())
	}
}

// A second follow while the first is still out is told it is followed; when the first then fails, it stays wanted
// and the next follow asks the devices again.
func TestAFollowDuringAFailingOneStaysWanted(t *testing.T) {
	s, d := newFollowTest(t)
	ctx := context.Background()
	release := make(chan struct{})
	d.follow = func(context.Context, bool) error { <-release; return errNode }
	first := make(chan error, 1)
	go func() { first <- s.follow(ctx, "mac", "s1", true) }()
	waitFor(t, "the first follow out", func() bool { return d.askedTimes() == 1 })
	if err := s.follow(ctx, "mac", "s1", true); err != nil {
		t.Fatalf("second follow: %v", err)
	}
	close(release)
	if err := <-first; !errors.Is(err, errNode) {
		t.Fatalf("first follow: %v", err)
	}
	if !s.followed("mac", "s1") {
		t.Fatal("the second caller was told followed, and the first one's failure dropped it")
	}
	d.follow = nil
	if err := s.follow(ctx, "mac", "s1", true); err != nil || d.askedTimes() != 2 {
		t.Fatalf("follow after the failure: %v, asked %d", err, d.askedTimes())
	}
}

// The caller's deadline reaches the devices: a node that never answers session.follow does not hold the caller.
func TestFollowCarriesTheCallersDeadline(t *testing.T) {
	s, d := newFollowTest(t)
	d.follow = func(ctx context.Context, _ bool) error { <-ctx.Done(); return ctx.Err() }
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.call(ctx, "mac", DeviceSessionFollow, map[string]any{"session_id": "s1"}, nil) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("follow: %v, want the deadline", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the follow outlived its caller's deadline")
	}
}

// A channel run's follow that failed is asked again by its watcher once due, and not after it was taken or the run
// stopped following.
func TestAChannelRunsFailedFollowIsRetried(t *testing.T) {
	s, d := newFollowTest(t)
	old := followRetryEvery
	followRetryEvery = 0
	t.Cleanup(func() { followRetryEvery = old })
	ctx := context.Background()
	d.follow = func(context.Context, bool) error { return errNode }
	s.followSession(ctx, "mac", "s1")
	if d.askedTimes() != 1 {
		t.Fatalf("asked %d times, want 1", d.askedTimes())
	}
	s.retryFollow("mac", "s1")
	if d.askedTimes() != 2 {
		t.Fatalf("the failed follow was not asked again (asked %d)", d.askedTimes())
	}
	d.mu.Lock()
	d.follow = nil
	d.mu.Unlock()
	s.retryFollow("mac", "s1")
	if d.askedTimes() != 3 {
		t.Fatalf("asked %d times, want 3", d.askedTimes())
	}
	s.retryFollow("mac", "s1")
	if d.askedTimes() != 3 {
		t.Fatalf("a taken follow was asked again (asked %d)", d.askedTimes())
	}

	d.mu.Lock()
	d.follow = func(context.Context, bool) error { return errNode }
	d.mu.Unlock()
	s.followSession(ctx, "mac", "s2")
	s.unfollowSession("mac", "s2")
	n := d.askedTimes()
	s.retryFollow("mac", "s2")
	if d.askedTimes() != n {
		t.Fatal("a follow no run wants any more was asked again")
	}
}
