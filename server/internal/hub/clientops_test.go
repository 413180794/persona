package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
)

type clientOpsStore struct {
	sharedStore
	mu   sync.Mutex
	ops  map[string]db.ClientOp
	read chan struct{}
}

func (s *clientOpsStore) BeginClientOp(_ context.Context, op db.ClientOp) (*db.ClientOp, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.ops[op.ID]; ok {
		return &old, false, nil
	}
	op.State = "pending"
	s.ops[op.ID] = op
	return &op, true, nil
}

func (s *clientOpsStore) ClientOp(_ context.Context, id string) (*db.ClientOp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.read != nil {
		select {
		case s.read <- struct{}{}:
		default:
		}
	}
	op, ok := s.ops[id]
	if !ok {
		return nil, nil
	}
	return &op, nil
}

func (s *clientOpsStore) FinishClientOp(_ context.Context, id, state string, result json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	op := s.ops[id]
	op.State, op.Result = state, result
	s.ops[id] = op
	return nil
}

func TestClientOps(t *testing.T) {
	const id = "00000000-0000-4000-8000-000000000001"
	const body = "{ \"ok\": true }\n"
	store := &clientOpsStore{ops: map[string]db.ClientOp{}, read: make(chan struct{}, 1)}
	h := &Hub{store: store}
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	handler := h.withClientOp("message", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var params map[string]any
		if err := json.NewDecoder(r.Body).Decode(&params); err != nil || params["client_msg_id"] != id {
			t.Error("message id was not forwarded")
		}
		close(started)
		<-release
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(body))
	})
	request := func(ctx context.Context, params string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/sessions/m/s/messages", strings.NewReader(params)).WithContext(ctx)
		r.Header.Set("X-Persona-Op-Id", id)
		w := httptest.NewRecorder()
		handler(w, r)
		return w
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- request(ctx, `{"text":"hello","options":{"b":2,"a":1}}`) }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("handler did not start")
	}
	duplicate := make(chan *httptest.ResponseRecorder, 1)
	go func() { duplicate <- request(ctx, ` { "options": {"a":1,"b":2}, "text":"hello" } `) }()
	select {
	case <-store.read:
	case <-ctx.Done():
		t.Fatal("duplicate did not wait on the persisted operation")
	}
	select {
	case <-duplicate:
		t.Fatal("pending operation returned before completion")
	default:
	}
	close(release)
	for _, ch := range []chan *httptest.ResponseRecorder{first, duplicate} {
		select {
		case w := <-ch:
			if w.Code != http.StatusCreated || w.Body.String() != body {
				t.Fatalf("replay = %d %q", w.Code, w.Body.String())
			}
		case <-ctx.Done():
			t.Fatal("operation did not complete")
		}
	}
	// A new hub shares the receipt, without sharing any in-memory execution state.
	h = &Hub{store: store}
	replayed := h.withClientOp("message", func(http.ResponseWriter, *http.Request) { calls.Add(1) })
	r := httptest.NewRequest("POST", "/api/v1/sessions/m/s/messages", strings.NewReader(`{"text":"hello","options":{"a":1,"b":2},"client_msg_id":"`+id+`"}`))
	r.Header.Set("X-Persona-Op-Id", id) // receipts are only for clients that send the header
	w := httptest.NewRecorder()
	replayed(w, r)
	if w.Code != http.StatusCreated || w.Body.String() != body || calls.Load() != 1 {
		t.Fatalf("persisted replay = %d %q, executions = %d", w.Code, w.Body.String(), calls.Load())
	}
	w = request(ctx, `{"text":"changed"}`)
	if w.Code != http.StatusConflict || strings.TrimSpace(w.Body.String()) != `{"code":"op_id_reused"}` || calls.Load() != 1 {
		t.Fatalf("different digest = %d %s, executions = %d", w.Code, w.Body.String(), calls.Load())
	}
	for _, tc := range []struct {
		status int
		body   string
		state  string
	}{
		{400, `{"error":"invalid"}`, "failed"},
		{409, `{"code":"input_draft","delivered":"none"}`, "failed"},
		{502, `{"delivered":"unknown"}`, "unknown"},
		{409, `{"delivered":"unknown"}`, "unknown"},
		{503, `{}`, "unknown"},
	} {
		if got := clientOpState(tc.status, []byte(tc.body)); got != tc.state {
			t.Errorf("state(%d, %s) = %s", tc.status, tc.body, got)
		}
	}
}

// The cleanup loop recovers pending operations first and keeps exactly 30 UTC days.
type clientOpsCleanupStore struct {
	sharedStore
	recovered bool
	cuts      chan time.Time
}

func (s *clientOpsCleanupStore) RecoverClientOps(context.Context) error {
	s.recovered = true
	return nil
}

func (s *clientOpsCleanupStore) CleanupClientOps(_ context.Context, before time.Time) error {
	s.cuts <- before
	return nil
}

func TestClientOpsCleanupRetention(t *testing.T) {
	store := &clientOpsCleanupStore{cuts: make(chan time.Time, 1)}
	h := &Hub{store: store}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	before := time.Now().Add(-30 * 24 * time.Hour)
	if err := h.startClientOps(ctx); err != nil {
		t.Fatal(err)
	}
	if !store.recovered {
		t.Fatal("pending operations were not recovered")
	}
	select {
	case cut := <-store.cuts:
		after := time.Now().Add(-30 * 24 * time.Hour)
		if cut.Location() != time.UTC {
			t.Errorf("cleanup cut location = %v, want UTC", cut.Location())
		}
		if cut.Before(before) || cut.After(after) {
			t.Errorf("cleanup cut = %v, want between %v and %v", cut, before, after)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("client operations were not cleaned up at startup")
	}
}
