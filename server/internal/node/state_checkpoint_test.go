package node

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/rpc"
)

func checkpointFixture(t *testing.T, handle rpc.Handler) *Node {
	t.Helper()
	sender, receiver := net.Pipe()
	t.Cleanup(func() { sender.Close(); receiver.Close() })
	peer := rpc.New("sender", sender)
	manager := rpc.New("manager", receiver)
	manager.OnRequest = handle
	go peer.Serve(t.Context(), sender)
	go manager.Serve(t.Context(), receiver)
	return &Node{reload: &ResidentReload{}, peer: peer, state: newState()}
}

func TestStateCheckpointReadsSamePeerAfterGuardedWork(t *testing.T) {
	started := time.Date(2026, 11, 19, 10, 2, 0, 123456789, time.UTC)
	var calls atomic.Int32
	n := checkpointFixture(t, func(_ context.Context, method string, args json.RawMessage) (any, error) {
		if method != "state.load" || string(args) != "{}" {
			return nil, errors.New("wrong RPC")
		}
		calls.Add(1)
		return map[string]any{"sessions": map[string]db.NodeSession{
			"test": {Launcher: "claude", Cwd: "/fixture", StartedAt: started.Truncate(time.Microsecond),
				PendingHandoff: json.RawMessage(`{"to":"next","from":"parent"}`)},
		}}, nil
	})
	local := sessionState{Launcher: "claude", Cwd: "/fixture", StartedAt: started, IsMain: true}
	snapshot := map[string]sessionState{"test": local}
	n.state.merge(snapshot)
	if _, err := n.checkpointState(t.Context()); err == nil || calls.Load() != 1 {
		t.Fatalf("mismatch was not detected: %v", err)
	}
	done, err := n.reload.enter()
	if err != nil {
		t.Fatalf("admission remained paused after mismatch: %v", err)
	}
	done()
}

func TestStateCheckpointRejectsNewRPCUntilSameConnectionReadback(t *testing.T) {
	readStarted, unblock := make(chan struct{}), make(chan struct{})
	started := time.Date(2026, 11, 19, 10, 2, 0, 123456789, time.UTC)
	n := checkpointFixture(t, func(ctx context.Context, method string, args json.RawMessage) (any, error) {
		if method != "state.load" || string(args) != "{}" {
			return nil, errors.New("wrong RPC")
		}
		close(readStarted)
		select {
		case <-unblock:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return map[string]any{"sessions": map[string]db.NodeSession{
			"test": {Launcher: "claude", Cwd: "/fixture", StartedAt: started.Truncate(time.Microsecond)},
		}}, nil
	})
	n.state.merge(map[string]sessionState{"test": {Launcher: "claude", Cwd: "/fixture", StartedAt: started, IsMain: true}})
	finishWork, err := n.reload.enter()
	if err != nil {
		t.Fatal(err)
	}
	call, cancel := context.WithTimeout(t.Context(), time.Second*3)
	defer cancel()
	type result struct {
		r   stateCheckpointReceipt
		err error
	}
	results := make(chan result, 1)
	go func() {
		r, err := n.checkpointState(call)
		results <- result{r, err}
	}()
	timer := time.NewTicker(time.Millisecond)
	defer timer.Stop()
	for {
		n.reload.mu.Lock()
		started := n.reload.checkpoint != nil
		n.reload.mu.Unlock()
		if started {
			break
		}
		select {
		case <-timer.C:
		case <-call.Done():
			t.Fatal("checkpoint did not pause admission")
		}
	}
	if done, err := n.reload.enter(); err == nil {
		done()
		t.Fatal("new work admitted during checkpoint")
	}
	select {
	case <-readStarted:
		t.Fatal("readback started before admitted work drained")
	default:
	}
	finishWork()
	select {
	case <-readStarted:
	case <-call.Done():
		t.Fatal("same-peer readback did not start")
	}
	if n.state.persistMu.TryLock() {
		n.state.persistMu.Unlock()
		t.Fatal("state persistence was not serialized with readback")
	}
	close(unblock)
	got := <-results
	if got.err != nil || got.r.Sessions != 1 || len(got.r.SHA256) != 64 {
		t.Fatalf("bad result: %+v", got)
	}
	done, err := n.reload.enter()
	if err != nil {
		t.Fatal("checkpoint did not release admission:", err)
	}
	done()
}

func TestStateCheckpointTimeoutAndPendingJournalKeepOriginalResident(t *testing.T) {
	unblock := make(chan struct{})
	n := checkpointFixture(t, func(_ context.Context, _ string, _ json.RawMessage) (any, error) {
		<-unblock
		return map[string]any{"sessions": map[string]db.NodeSession{}}, nil
	})
	defer close(unblock)
	call, cancel := context.WithTimeout(t.Context(), 35*time.Millisecond)
	defer cancel()
	if _, err := n.checkpointState(call); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("checkpoint timeout: %v", err)
	}
	select {
	case <-n.peer.Done():
		t.Fatal("read timeout closed the production-shaped connection")
	default:
	}
	journal := &stateJournal{path: filepath.Join(t.TempDir(), stateJournalFile)}
	if err := journal.append("retained", sessionState{Pane: "fixture"}); err != nil {
		t.Fatal(err)
	}
	n.nodeStateJournal.Store(journal)
	if _, err := n.checkpointState(t.Context()); err == nil || err.Error() != "node state journal is not confirmed empty" {
		t.Fatalf("pending journal accepted: %v", err)
	}
	if _, ok, err := journal.head(); err != nil || !ok {
		t.Fatalf("pending work changed: %t %v", ok, err)
	}
}

func TestStateCheckpointCanonicalJSONPreservesLargeNanosecondValues(t *testing.T) {
	makeState := func(raw json.RawMessage) map[string]db.NodeSession {
		return map[string]db.NodeSession{"fixture": {IdleCompaction: raw}}
	}
	left, _ := canonicalCheckpoint(makeState(json.RawMessage(`{"a":1795073410123456789,"b":1}`)))
	right, _ := canonicalCheckpoint(makeState(json.RawMessage(`{"b":1,"a":1795073410123456789}`)))
	changed, _ := canonicalCheckpoint(makeState(json.RawMessage(`{"b":1,"a":1795073410123456790}`)))
	if string(left) != string(right) || string(left) == string(changed) {
		t.Fatal("JSONB member order or nanosecond precision was lost")
	}
}

func TestStateCheckpointPostgresPersistence(t *testing.T) {
	dsn := os.Getenv("FAN_CHECKPOINT_TEST_DSN")
	if dsn == "" {
		t.Skip("requires an isolated PostgreSQL fixture")
	}
	database, err := db.Open(t.Context(), dsn, db.ManagerSet)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	const machine = "linux-checkpoint-fixture"
	seed := db.NodeSession{Launcher: "claude", Cwd: "/fixture", Pane: "before",
		StartedAt: time.Date(2026, 11, 19, 10, 55, 0, 123456789, time.UTC),
	}
	if err := database.PutNodeSession(t.Context(), machine, "owned", seed); err != nil {
		t.Fatal(err)
	}
	n := checkpointFixture(t, func(ctx context.Context, method string, args json.RawMessage) (any, error) {
		if method != "state.load" || string(args) != "{}" {
			return nil, errors.New("the checkpoint is allowed one ordinary read")
		}
		sessions, err := database.NodeSessions(ctx, machine)
		return map[string]any{"sessions": sessions}, err
	})
	data, err := json.Marshal(seed)
	if err != nil {
		t.Fatal(err)
	}
	var expected sessionState
	if err := json.Unmarshal(data, &expected); err != nil {
		t.Fatal(err)
	}
	n.state.merge(map[string]sessionState{"owned": expected})
	ok, err := n.checkpointState(t.Context())
	if err != nil || ok.Sessions != 1 || len(ok.SHA256) != 64 {
		t.Fatalf("persisted projection did not match: %+v, %v", ok, err)
	}
	n.state.update("owned", func(st *sessionState) bool {
		st.Pane = "write-not-in-postgres"
		return true
	})
	if _, err := n.checkpointState(t.Context()); err == nil || err.Error() != "sender and Manager state differ" {
		t.Fatalf("expected persisted state mismatch, got %v", err)
	}
	retained, err := database.NodeSessions(t.Context(), machine)
	if err != nil || retained["owned"].Pane != "before" {
		t.Fatalf("checkpoint wrote to Manager: %v", err)
	}
	t.Log("same-peer state.load matched PostgreSQL before the fault and rejected the unpersisted sender change")
}

func TestStateCheckpointLocalSocketDisabledByDefault(t *testing.T) {
	t.Setenv(stateCheckpointEnv, "")
	n := &Node{}
	client, handler := net.Pipe()
	defer client.Close()
	defer handler.Close()
	go n.serveSock(t.Context(), handler)
	if err := client.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(client).Encode(map[string]string{"kind": "state.checkpoint"}); err != nil {
		t.Fatal(err)
	}
	var result map[string]string
	if err := json.NewDecoder(client).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result["error"] != "Linux node state checkpoint is disabled" {
		t.Fatalf("checkpoint must be opt-in: %v", result)
	}
}
