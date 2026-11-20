//go:build linux || darwin

package extmgr

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nwverse/persona/server/internal/admission"
	"github.com/nwverse/persona/server/internal/assistantwire"
)

func TestBotCardPressAdmissionKeepsUncertainCallback(t *testing.T) {
	w := newBotWorld(t)
	w.pair(t)
	dir := filepath.Join(t.TempDir(), "admission")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	gate, err := admission.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Control("init", ""); err != nil {
		t.Fatal(err)
	}
	w.h.opts.Admission = gate
	w.a.on(assistantwire.MethodFeishuAction, func(json.RawMessage) (any, error) {
		return nil, context.DeadlineExceeded
	})
	w.api.inChat("om_card", "oc_p2p")
	payload := []byte(`{"schema":"2.0","header":{"event_type":"card.action.trigger","event_id":"ev_admitted"},
		"event":{"operator":{"open_id":"ou_me"},"action":{"value":{"a":"ix","id":"ix_q","q":"0","v":"fixture"}},
		"context":{"open_message_id":"om_card"}}}`)
	if _, err := w.h.botEvent(w.ctx, payload); err != nil {
		t.Fatal(err)
	}
	if _, err := w.h.botEvent(w.ctx, payload); !errors.Is(err, admission.ErrPending) {
		t.Fatalf("uncertain Feishu card callback repeated: %v", err)
	}
	const attempt = "feishu-card-attempt"
	if _, err := gate.Control("close", attempt); err != nil {
		t.Fatal(err)
	}
	tickets, err := gate.Pending(attempt)
	if err != nil || len(tickets) != 1 || tickets[0].Kind != "feishu-card" || len(w.a.got(assistantwire.MethodFeishuAction)) != 1 {
		t.Fatalf("original Feishu callback lost or delegated twice: %+v, %v", tickets, err)
	}
}

func TestNativeFeishuRecallMissingAcknowledgementRetainsOriginal(t *testing.T) {
	w := newBotWorld(t)
	w.pair(t)
	dir := filepath.Join(t.TempDir(), "admission")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	gate, err := admission.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Control("init", ""); err != nil {
		t.Fatal(err)
	}
	w.h.opts.Admission = gate
	w.a.on(assistantwire.MethodFeishuRecall, func(json.RawMessage) (any, error) {
		return struct{}{}, nil // a transport response without the durable receipt
	})
	payload := []byte(`{"schema":"2.0","header":{"event_type":"im.message.recalled_v1","event_id":"ev_original_recall"},
		"event":{"message_id":"om_original","chat_id":"oc_p2p","recall_type":"message_owner"}}`)
	if _, err := w.h.botEvent(w.ctx, payload); err == nil {
		t.Fatal("received an acknowledgement without a durable marker")
	}
	if _, err := w.h.botEvent(w.ctx, payload); !errors.Is(err, admission.ErrPending) {
		t.Fatalf("recall accepted again: %v", err)
	}
	const attempt = "unknown-recall-attempt"
	if _, err := gate.Control("close", attempt); err != nil {
		t.Fatal(err)
	}
	tickets, err := gate.Pending(attempt)
	if err != nil || len(tickets) != 1 || tickets[0].Kind != "feishu-recall" ||
		len(w.a.got(assistantwire.MethodFeishuRecall)) != 1 {
		t.Fatalf("recall boundary lost payload or attempted twice: %+v %v", tickets, err)
	}
}
