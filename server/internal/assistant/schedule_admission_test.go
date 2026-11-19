//go:build linux || darwin

package assistant

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/admission"
	"github.com/nwverse/persona/server/internal/db"
)

func receiptGate(t *testing.T) (*admission.Gate, func(string, string) admission.State) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "admission")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	gate, err := admission.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	control := func(action, attempt string) admission.State {
		t.Helper()
		state, err := gate.Control(action, attempt)
		if err != nil {
			t.Fatal(err)
		}
		return state
	}
	control("init", "")
	return gate, control
}

func awaitReceiptDrain(t *testing.T, closed func() bool) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if closed() {
			return
		}
	}
	t.Fatal("tracked receipt did not drain")
}

func TestScheduleAdmissionHoldsLateReceiptAndKeepsUnknown(t *testing.T) {
	gate, control := receiptGate(t)
	s := newTestService(t, newFakeHub())
	s.maintenanceGate = gate
	for _, unknown := range []bool{false, true} {
		w, finish, err := s.scheduleAdmission(Task{ID: "task-fixture"}, time.Now(), false)
		if err != nil {
			t.Fatal(err)
		}
		late := w.hold()
		control("close", "schedule-attempt")
		if _, _, err := s.scheduleAdmission(Task{ID: "next-schedule"}, time.Now(), false); !errors.Is(err, admission.ErrClosed) {
			t.Fatalf("closed admission accepted a new occurrence: %v", err)
		}
		finish()
		if state := control("check", "schedule-attempt"); state.Phase != "draining" || state.Active["schedule-run"] != 1 {
			t.Fatalf("dispatch released before its watcher: %+v", state)
		}
		if unknown {
			s.verifyScheduleReceipt(w, "missing-run", runOK)
		}
		late()
		if unknown {
			if state := control("check", "schedule-attempt"); state.Phase != "draining" {
				t.Fatalf("unknown receipt was discarded: %+v", state)
			}
			return
		}
		awaitReceiptDrain(t, func() bool {
			return control("check", "schedule-attempt").Phase == "drained"
		})
		control("open", "schedule-attempt")
	}
}

func TestFeishuDispatchHoldsItsOwnTicketAfterFirstReply(t *testing.T) {
	gate, control := receiptGate(t)
	s, _ := stopCardHub(t, FeishuFeatures{Main: true})
	s.maintenanceGate = gate
	predecessor := make(chan struct{})
	s.fbot.mu.Lock()
	s.fbot.line = predecessor
	s.fbot.mu.Unlock()
	const eventID = "om_async_fixture"
	parent, err := gate.AdmitPayload("feishu-event", eventID, []byte("fixture"))
	if err != nil {
		t.Fatal(err)
	}
	control("close", "feishu-attempt")
	data := json.RawMessage(`{"message":{"message_id":"om_async_fixture","message_type":"sticker","content":"{}"}}`)
	if err := s.FeishuMessage(t.Context(), FeishuMessage{Event: data}); err != nil {
		t.Fatal(err)
	}
	parent()
	if state := control("check", "feishu-attempt"); state.Phase != "draining" || state.Active["feishu-dispatch"] != 1 {
		t.Fatalf("async dispatch released after first reply: %+v", state)
	}
	close(predecessor)
	awaitReceiptDrain(t, func() bool { return control("check", "feishu-attempt").Phase == "drained" })
}

func TestAdmissionOutboxLostReplyDoesNotReplay(t *testing.T) {
	gate, control := receiptGate(t)
	h := newOutboxHub(t)
	h.failN.Store(1)
	out := &memOutbox{}
	nf, _ := json.Marshal(push("ntf_uncertain").Notification)
	if err := out.OutboxAdd(t.Context(), db.OutboxItem{ID: "ntf_uncertain", Kind: outboxPush, Body: nf}, pushesKept, time.Time{}); err != nil {
		t.Fatal(err)
	}
	b := startOutboxManagerWithOptions(t, h, BusOptions{Admission: gate, Outbox: out})
	h.up.Store(true)
	h.waitFor(t, []string{"push ntf_uncertain"})
	if err := b.handOver(t.Context()); !errors.Is(err, admission.ErrPending) {
		t.Fatalf("lost reply did not fence repeat attempt: %v", err)
	}
	const attempt = "outbox-attempt"
	state := control("close", attempt)
	if state.Phase != "draining" || state.Active["manager-outbox"] != 1 || out.len() != 1 {
		t.Fatalf("lost reply cleared durable record: %+v, outbox rows=%d", state, out.len())
	}
	pending, err := gate.Pending(attempt)
	if err != nil || len(pending) != 1 || pending[0].Kind != "manager-outbox" || pending[0].Bytes == 0 {
		t.Fatalf("missing reconciliation receipt: %+v %v", pending, err)
	}
	if _, err := gate.Control("open", attempt); err == nil {
		t.Fatal("unknown outbox was incorrectly drained")
	}
}
