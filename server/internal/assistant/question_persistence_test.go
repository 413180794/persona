package assistant

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

var errQuestionWrite = errors.New("question storage unavailable")

type questionFaultStore struct {
	*fleetMem
	mu                                 sync.Mutex
	failInsert, failFinish, skipFinish bool
	entered, release                   chan struct{}
	finishCount                        int
}

func (m *questionFaultStore) InsertInteraction(ctx context.Context, ix proto.Interaction) error {
	m.mu.Lock()
	fail := m.failInsert
	m.mu.Unlock()
	if fail {
		return errQuestionWrite
	}
	return m.fleetMem.InsertInteraction(ctx, ix)
}

func (m *questionFaultStore) FinishInteraction(ctx context.Context, id, status string, r *proto.Resolution) error {
	m.mu.Lock()
	fail, skip, entered, release := m.failFinish, m.skipFinish, m.entered, m.release
	m.entered, m.release = nil, nil
	m.finishCount++
	m.mu.Unlock()
	if entered != nil {
		close(entered)
		<-release
	}
	if fail {
		return errQuestionWrite
	}
	if skip {
		return nil
	}
	return m.fleetMem.FinishInteraction(ctx, id, status, r)
}

func questionPersistenceFixture(t *testing.T) (*fleetTest, *questionFaultStore, *pending) {
	t.Helper()
	ft := newFleetTest(t)
	st := &questionFaultStore{fleetMem: ft.st}
	ft.s = New(Options{Hub: ft.f, Store: func() Store { return st }, Logf: t.Logf, Devices: ft.d})
	ix := proto.Interaction{ID: "triage-durable", Kind: proto.IxQuestion, ToolName: workCardTool, CanCancel: true, ExpiresAt: time.Now().Add(time.Hour)}
	p := &pending{ix: ix, done: make(chan proto.Resolution, 1)}
	ft.s.fleet.mu.Lock()
	ft.s.ixMapLocked()[ix.ID] = p
	ft.s.fleet.mu.Unlock()
	return ft, st, p
}

func TestQuestionPersistenceFailureKeepsCardForRetry(t *testing.T) {
	for _, failure := range []string{"insert", "finish", "no update"} {
		t.Run(failure, func(t *testing.T) {
			ft, st, p := questionPersistenceFixture(t)
			st.failInsert, st.failFinish, st.skipFinish = failure == "insert", failure == "finish", failure == "no update"
			ft.s.record(p.ix) // initial insert may fail too
			if err := ft.s.resolve(p.ix.ID, proto.Resolution{Cancel: true}); err == nil {
				t.Fatal("failed durability acknowledged")
			}
			ft.s.fleet.mu.Lock()
			claimed := p.claimed
			ft.s.fleet.mu.Unlock()
			if _, pending := ft.s.pendingIx(p.ix.ID); !pending || claimed {
				t.Fatal("failed card cannot be retried")
			}
			select {
			case r := <-p.done:
				t.Fatalf("owner received unpersisted answer %+v", r)
			default:
			}
			st.mu.Lock()
			st.failInsert, st.failFinish, st.skipFinish = false, false, false
			st.mu.Unlock()
			if err := ft.s.resolve(p.ix.ID, proto.Resolution{Cancel: true}); err != nil {
				t.Fatal(err)
			}
			if r := <-p.done; !r.Cancel {
				t.Fatalf("retry: %+v", r)
			}
			status, r, ended := ft.s.outcome(p.ix.ID)
			if status != "cancelled" || !ended || !r.Cancel {
				t.Fatalf("missing durable outcome: %s %+v", status, r)
			}
		})
	}
}

func TestQuestionPersistenceHTTPFailureDoesNotAcknowledge(t *testing.T) {
	ft, st, p := questionPersistenceFixture(t)
	ft.s.record(p.ix)
	st.failFinish = true
	h := ft.s.fleetHandler(http.NotFoundHandler())
	request := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/interactions/"+p.ix.ID+"/resolve", strings.NewReader(`{"cancel":true}`)))
		return w
	}
	if w := request(); w.Code < 500 {
		t.Fatalf("write failure acknowledged: %d %s", w.Code, w.Body.String())
	}
	st.mu.Lock()
	st.failFinish = false
	st.mu.Unlock()
	if w := request(); w.Code != http.StatusOK {
		t.Fatalf("retry failed: %d %s", w.Code, w.Body.String())
	}
	if w := request(); w.Code != http.StatusOK {
		t.Fatalf("idempotent retry failed: %d %s", w.Code, w.Body.String())
	}
}

func TestQuestionPersistenceExpiryFailureRemainsRetryable(t *testing.T) {
	ft, st, p := questionPersistenceFixture(t)
	p.ix.ExpiresAt = time.Now().Add(-time.Second)
	ft.s.record(p.ix)
	entered, release := make(chan struct{}), make(chan struct{})
	st.entered, st.release, st.failFinish = entered, release, true
	result := make(chan error, 1)
	go func() { result <- ft.s.resolve(p.ix.ID, proto.Resolution{Cancel: true}) }()
	<-entered
	finished := make(chan error, 1)
	go func() { _, err := ft.s.await(context.Background(), p); finished <- err }()
	close(release)
	if err := <-result; err == nil {
		t.Fatal("write failure acknowledged")
	}
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("expiry overwrote failed outcome")
		}
	case <-time.After(time.Second):
		t.Fatal("expired waiter blocked")
	}
	if _, pending := ft.s.pendingIx(p.ix.ID); !pending {
		t.Fatal("expiry removed retryable card")
	}
	status, _, _ := ft.st.InteractionOutcome(context.Background(), p.ix.ID)
	if status != "pending" {
		t.Fatalf("expiry overwrote outcome: %s", status)
	}
}

func TestQuestionPersistenceDisconnectAndConcurrentResolve(t *testing.T) {
	for _, fail := range []bool{true, false} {
		t.Run(map[bool]string{true: "failed", false: "saved"}[fail], func(t *testing.T) {
			ft, st, p := questionPersistenceFixture(t)
			ft.s.record(p.ix)
			entered, release := make(chan struct{}), make(chan struct{})
			st.entered, st.release, st.failFinish = entered, release, fail
			first := make(chan error, 1)
			go func() { first <- ft.s.resolve(p.ix.ID, proto.Resolution{Cancel: true}) }()
			<-entered
			duplicate := make(chan error, 1)
			go func() {
				duplicate <- ft.s.resolve(p.ix.ID, proto.Resolution{Answers: map[string]string{"Ready?": "Yes"}})
			}()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			type outcome struct {
				r   proto.Resolution
				err error
			}
			waiter := make(chan outcome, 1)
			go func() { r, err := ft.s.await(ctx, p); waiter <- outcome{r, err} }()
			// Snapshot reads use fleet.mu and must remain responsive during the write.
			if _, ok := ft.s.pendingIx(p.ix.ID); !ok {
				t.Fatal("write removed pending card")
			}
			close(release)
			if err := <-first; (err != nil) != fail {
				t.Fatalf("first: %v", err)
			}
			if err := <-duplicate; err == nil {
				t.Fatal("concurrent answer accepted")
			}
			select {
			case got := <-waiter:
				if fail && (got.err == nil || got.r.Cancel) {
					t.Fatalf("failed write released cancellation: %+v", got)
				}
				if !fail && (got.err != nil || !got.r.Cancel) {
					t.Fatalf("persisted cancel lost: %+v", got)
				}
			case <-time.After(time.Second):
				t.Fatal("disconnected waiter blocked")
			}
			if fail {
				if _, ok := ft.s.pendingIx(p.ix.ID); !ok {
					t.Fatal("disconnect removed retryable card")
				}
				st.mu.Lock()
				st.failFinish = false
				st.mu.Unlock()
				if err := ft.s.resolve(p.ix.ID, proto.Resolution{Cancel: true}); err != nil {
					t.Fatal(err)
				}
			}
			status, r, ended := ft.s.outcome(p.ix.ID)
			if status != "cancelled" || !ended || !r.Cancel || len(r.Answers) != 0 {
				t.Fatalf("outcome: %s %+v", status, r)
			}
		})
	}
}
