//go:build linux || darwin

package admission

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func testGate(t *testing.T) *Gate {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "admission")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	g, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	control(t, g, "init", "")
	return g
}

func control(t *testing.T, g *Gate, action, attempt string) State {
	t.Helper()
	s, err := g.Control(action, attempt)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAdmissionCloseRaceAndReopen(t *testing.T) {
	g := testGate(t)
	hold, err := g.Admit("accepted")
	if err != nil {
		t.Fatal(err)
	}
	var callers sync.WaitGroup
	start := make(chan struct{})
	for range 32 {
		callers.Add(1)
		go func() {
			defer callers.Done()
			<-start
			done, err := g.Admit("racing")
			if err == nil {
				done()
			} else if !errors.Is(err, ErrClosed) {
				t.Error(err)
			}
		}()
	}
	close(start)
	control(t, g, "close", "test-attempt")
	callers.Wait()
	if s := control(t, g, "check", "test-attempt"); s.Phase != "draining" || s.Active["accepted"] != 1 {
		t.Fatalf("status with accepted work: %+v", s)
	}
	if _, err := g.Control("open", "wrong-attempt"); err == nil {
		t.Fatal("another attempt opened this one")
	}
	work := httptest.NewRecorder()
	if _, ok := g.HTTP("http", work); ok || work.Code != http.StatusServiceUnavailable || work.Header().Get("Retry-After") == "" {
		t.Fatalf("closed HTTP: %d %v", work.Code, work.Header())
	}
	hold()
	if s := control(t, g, "check", "test-attempt"); s.Phase != "drained" || len(s.Active) != 0 {
		t.Fatalf("drained status: %+v", s)
	}
	control(t, g, "open", "test-attempt")
	after, err := g.Admit("after")
	if err != nil {
		t.Fatal(err)
	}
	after()
}

func TestAdmissionCrashLeavesUnsettledOperation(t *testing.T) {
	if dir := os.Getenv("PERSONA_TEST_CRASH_GATE"); dir != "" {
		g, err := New(dir)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := g.Admit("crashed-work"); err != nil {
			t.Fatal(err)
		}
		os.Exit(43)
	}
	g := testGate(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestAdmissionCrashLeavesUnsettledOperation$")
	cmd.Env = append(os.Environ(), "PERSONA_TEST_CRASH_GATE="+g.dir)
	var failed *exec.ExitError
	if err := cmd.Run(); !errors.As(err, &failed) || failed.ExitCode() != 43 {
		t.Fatalf("unexpected subprocess result: %v", err)
	}
	control(t, g, "close", "crash-attempt")
	reopened, err := New(g.dir)
	if err != nil {
		t.Fatal(err)
	}
	if s := control(t, reopened, "check", "crash-attempt"); s.Phase != "draining" || s.Active["crashed-work"] != 1 {
		t.Fatalf("lost proof of abandoned work: %+v", s)
	}
	if _, err := reopened.Control("open", "crash-attempt"); err == nil {
		t.Fatal("reopened with an unresolved operation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := reopened.Startup(ctx, "restart"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("startup crossed a persistent fence: %v", err)
	}
}

func TestAdmissionUnreadableStateFailsClosed(t *testing.T) {
	g := testGate(t)
	if err := os.WriteFile(filepath.Join(g.dir, "state.json"), []byte(`{"attempt": "unsafe"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Admit("bad-state"); err == nil {
		t.Fatal("opened from an invalid state")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(g.dir, link); err != nil {
		t.Fatal(err)
	}
	if _, err := New(link); err == nil {
		t.Fatal("accepted a symlink as the durable admission directory")
	}
}

func TestAdmissionContinuationKeepsExactPayloadAndFence(t *testing.T) {
	g := testGate(t)
	parent, err := g.AdmitPayload("feishu-event", "event-one", []byte("original event"))
	if err != nil {
		t.Fatal(err)
	}
	control(t, g, "close", "feishu-attempt")
	if _, err := g.ContinuePayload("feishu-event", "feishu-dispatch", "another", nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("another message continued after close: %v", err)
	}
	child, err := g.ContinuePayload("feishu-event", "feishu-dispatch", "event-one", []byte("second-stage event"))
	if err != nil {
		t.Fatal(err)
	}
	parent()
	if s := control(t, g, "check", "feishu-attempt"); s.Phase != "draining" || s.Active["feishu-dispatch"] != 1 {
		t.Fatalf("handoff disappeared before its worker finished: %+v", s)
	}
	files, err := filepath.Glob(filepath.Join(g.dir, "work-feishu-dispatch.*"))
	if err != nil || len(files) != 1 {
		t.Fatalf("retained files: %v %v", files, err)
	}
	payload, err := os.ReadFile(files[0])
	if err != nil || string(payload) != "second-stage event" {
		t.Fatalf("payload: %q, %v", payload, err)
	}
	child()
	if s := control(t, g, "check", "feishu-attempt"); s.Phase != "drained" {
		t.Fatalf("completed handoff did not drain: %+v", s)
	}
}

func TestLedgerReconciliationDoesNotAcceptUnidentifiedTickets(t *testing.T) {
	g := testGate(t)
	if _, err := g.Admit("extension-ledger-record"); err != nil {
		t.Fatal(err)
	}
	const attempt = "empty-ticket"
	control(t, g, "close", attempt)
	pending, err := g.LedgerPending(attempt)
	if err != nil || pending.Unidentified != 1 || len(pending.Operations) != 0 {
		t.Fatalf("old empty ticket: %+v %v", pending, err)
	}
	const id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := g.ReconcileLedger(t.Context(), attempt, id, func(context.Context, LedgerOperation) (bool, error) {
		t.Fatal("proof callback must not run without the matching identified ticket")
		return true, nil
	}); err == nil {
		t.Fatal("accepted an unidentified ticket")
	}
	if state := control(t, g, "check", attempt); state.Phase != "draining" || state.Active["extension-ledger-record"] != 1 {
		t.Fatalf("empty ticket no longer blocks: %+v", state)
	}
}

func TestLedgerAuthorityCannotRebindUntilOpenAndSettled(t *testing.T) {
	g := testGate(t)
	if _, _, err := g.AdmitLedger(); err == nil {
		t.Fatal("ledger write admitted before the writer bound its database")
	}
	source := LedgerAuthority{SystemIdentifier: "123", DatabaseOID: 1}
	candidate := LedgerAuthority{SystemIdentifier: "123", DatabaseOID: 2}
	if err := g.BindLedgerWriter(source); err != nil {
		t.Fatal(err)
	}
	op, done, err := g.AdmitLedger()
	if err != nil || op.Authority != source {
		t.Fatalf("unbound admitted write: %+v %v", op, err)
	}
	if err := g.BindLedgerWriter(candidate); err == nil {
		t.Fatal("active ledger ticket was moved to another database")
	}
	const attempt = "frozen-authority"
	closed := control(t, g, "close", attempt)
	if closed.LedgerAuthority == nil || *closed.LedgerAuthority != source {
		t.Fatalf("wrong frozen database: %+v", closed)
	}
	done()
	control(t, g, "check", attempt)
	if err := g.BindLedgerWriter(candidate); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed gate permitted rebinding: %v", err)
	}
	control(t, g, "open", attempt)
	if err := g.BindLedgerWriter(candidate); err != nil {
		t.Fatalf("reopened gate did not accept the new Manager: %v", err)
	}
}
