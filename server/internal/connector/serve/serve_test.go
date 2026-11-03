package serve

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

// A write runs once by its request id: asked again (at once or later), the program answers what the first run did.
func TestWritesRunOnce(t *testing.T) {
	var w writes
	var runs atomic.Int32
	run := func() (CallResult, error) {
		n := runs.Add(1)
		if n > 1 {
			return CallResult{}, errors.New("ran twice")
		}
		return CallResult{Text: "sent"}, nil
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if res, err := w.do("r1", run); err != nil || res.Text != "sent" {
				t.Errorf("%+v %v", res, err)
			}
		}()
	}
	wg.Wait()
	if res, err := w.do("r1", run); err != nil || res.Text != "sent" || runs.Load() != 1 {
		t.Fatalf("again later: %+v %v, %d run(s)", res, err, runs.Load())
	}
	if _, err := w.do("r2", run); err == nil {
		t.Fatal("another id did not run")
	}
	for i := range doneMost + 1 {
		_, _ = w.do(string(rune('a'+i%26))+string(rune(i)), func() (CallResult, error) { return CallResult{}, nil })
	}
	if len(w.runs) > doneMost || len(w.order) > doneMost {
		t.Fatalf("remembers %d writes", len(w.runs))
	}
}
