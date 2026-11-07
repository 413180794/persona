package serve

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/channel"
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

// The same request id again answers what the first run did, the message ref it said included.
func TestWritesReplayTheMessageRef(t *testing.T) {
	var w writes
	runs := 0
	run := func() (CallResult, error) {
		runs++
		return CallResult{Text: "sent", MessageRef: "msg-7"}, nil
	}
	for range 2 {
		if res, err := w.do("r1", run); err != nil || res.Text != "sent" || res.MessageRef != "msg-7" {
			t.Fatalf("%+v %v", res, err)
		}
	}
	if runs != 1 {
		t.Fatalf("%d runs", runs)
	}
}

// A write still running keeps its request id however many other writes finish meanwhile: the same id again waits
// for it and answers what it did, instead of running the action a second time.
func TestWritesKeepRunningWrite(t *testing.T) {
	var w writes
	started, release := make(chan struct{}), make(chan struct{})
	first := make(chan CallResult, 1)
	go func() {
		res, _ := w.do("r1", func() (CallResult, error) {
			close(started)
			<-release
			return CallResult{Text: "sent"}, nil
		})
		first <- res
	}()
	<-started
	for i := range doneMost {
		_, _ = w.do(fmt.Sprintf("other-%d", i), func() (CallResult, error) { return CallResult{}, nil })
	}
	var again atomic.Int32
	second := make(chan CallResult, 1)
	go func() {
		res, _ := w.do("r1", func() (CallResult, error) {
			again.Add(1)
			return CallResult{Text: "sent again"}, nil
		})
		second <- res
	}()
	close(release)
	if res1, res2 := <-first, <-second; res1.Text != "sent" || res2.Text != "sent" {
		t.Fatalf("first %+v, second %+v", res1, res2)
	}
	if n := again.Load(); n != 0 {
		t.Fatalf("the same request id ran %d more time(s)", n)
	}
	if len(w.runs) > doneMost || len(w.order) > doneMost {
		t.Fatalf("remembers %d writes", len(w.runs))
	}
}

// The hub sends a ref again when it timed out waiting: while the first send is still running, the second waits for
// it and answers what it did, instead of sending the message again.
func TestSentRefsWaitForRunningSend(t *testing.T) {
	var r sentRefs
	var sends atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	send := func() (channel.SendResult, error) {
		if sends.Add(1) == 1 {
			close(started)
			<-release
		}
		return channel.SendResult{MessageID: "m1"}, nil
	}
	results := make(chan channel.SendResult, 2)
	errs := make(chan error, 2)
	go func() { res, err := r.do("ref1", send); results <- res; errs <- err }()
	<-started
	go func() { res, err := r.do("ref1", send); results <- res; errs <- err }()
	// Give the retry time to reach the running send: it must wait for it, not send again or answer early.
	time.Sleep(50 * time.Millisecond)
	if n := sends.Load(); n != 1 {
		t.Fatalf("the retry sent again while the first send ran: %d send(s)", n)
	}
	select {
	case res := <-results:
		t.Fatalf("answered %+v before the first send finished", res)
	default:
	}
	close(release)
	for range 2 {
		if res, err := <-results, <-errs; err != nil || res.MessageID != "m1" {
			t.Fatalf("%+v %v", res, err)
		}
	}
	if n := sends.Load(); n != 1 {
		t.Fatalf("sent %d times", n)
	}
	if res, err := r.do("ref1", send); err != nil || res.MessageID != "m1" || sends.Load() != 1 {
		t.Fatalf("again later: %+v %v, %d send(s)", res, err, sends.Load())
	}
}

// A failed send is forgotten: the same ref may be sent again, and once it went it is not sent a third time.
func TestSentRefsRetryAfterFailure(t *testing.T) {
	var r sentRefs
	var sends atomic.Int32
	send := func() (channel.SendResult, error) {
		if sends.Add(1) == 1 {
			return channel.SendResult{}, errors.New("upstream timed out")
		}
		return channel.SendResult{MessageID: "m2"}, nil
	}
	if _, err := r.do("ref2", send); err == nil {
		t.Fatal("the first send did not fail")
	}
	if res, err := r.do("ref2", send); err != nil || res.MessageID != "m2" {
		t.Fatalf("retry: %+v %v", res, err)
	}
	if res, err := r.do("ref2", send); err != nil || res.MessageID != "m2" || sends.Load() != 2 {
		t.Fatalf("again later: %+v %v, %d send(s)", res, err, sends.Load())
	}
	for i := range doneMost + 1 {
		_, _ = r.do(string(rune('a'+i%26))+string(rune(i)), func() (channel.SendResult, error) { return channel.SendResult{}, nil })
	}
	if len(r.sent) > doneMost || len(r.order) > doneMost {
		t.Fatalf("remembers %d sends", len(r.sent))
	}
}
