package devices

import (
	"context"
	"maps"
	"testing"
	"time"
)

// gatedStore holds every write of "mesh.path_uids" until the test lets it through.
type gatedStore struct {
	*memStore
	arrived chan gatedWrite
}

type gatedWrite struct {
	uids    map[string]string
	release chan struct{} // closed by the test: write now
	done    chan struct{} // closed once written
}

func (s *gatedStore) PutSetting(ctx context.Context, key string, v any) error {
	if key != "mesh.path_uids" {
		return s.memStore.PutSetting(ctx, key, v)
	}
	w := gatedWrite{uids: maps.Clone(v.(map[string]string)), release: make(chan struct{}), done: make(chan struct{})}
	s.arrived <- w
	<-w.release
	defer close(w.done)
	return s.memStore.PutSetting(ctx, key, v)
}

// Two paths learnt close together: whatever order their writes are let through in, the map kept is the newest one,
// so a restart groups both paths by computer. The writes waiting together are released newest first, the order
// that lost a path when each write was an unordered goroutine.
func TestPathUIDsSavedNewestLast(t *testing.T) {
	st := &gatedStore{memStore: newMemStore(), arrived: make(chan gatedWrite, 8)}
	d := New(Options{Hub: &fakeHost{}, Store: st})
	d.learnUID(&nodePath{key: "cfg:a"}, "uid-a")
	d.learnUID(&nodePath{key: "cfg:b"}, "uid-b")

	// Each learnUID that is news makes exactly one write: wait for both, however slow the machine is. The writes that
	// arrive close together are let through together, newest (largest) first.
	const writes = 2
	deadline := time.After(10 * time.Second)
	for released := 0; released < writes; {
		var pending []gatedWrite
		select {
		case w := <-st.arrived:
			pending = append(pending, w)
		case <-deadline:
			t.Fatalf("%d of %d writes arrived", released, writes)
		}
	gather: // the others waiting with it; how long this waits changes only how they are batched, not the result
		for released+len(pending) < writes {
			select {
			case w := <-st.arrived:
				pending = append(pending, w)
			case <-time.After(200 * time.Millisecond):
				break gather
			}
		}
		for i := range pending {
			if len(pending[i].uids) > len(pending[0].uids) {
				pending[0], pending[i] = pending[i], pending[0]
			}
		}
		for _, w := range pending {
			close(w.release)
			<-w.done
		}
		released += len(pending)
	}

	var saved map[string]string
	if _, err := st.Setting(context.Background(), "mesh.path_uids", &saved); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"cfg:a": "uid-a", "cfg:b": "uid-b"}
	if !maps.Equal(saved, want) {
		t.Fatalf("saved %v, want %v", saved, want)
	}
}
