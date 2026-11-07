package extmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/extension/uistream"
	"github.com/nwverse/persona/server/internal/extwire"
	"github.com/nwverse/persona/server/internal/i18n"
)

func retryOpenRequest() wire.UIStreamRequest {
	return wire.UIStreamRequest{Request: "open", Protocol: 1, RequestID: "lost-open", Extension: "example-page", Contribution: "home", Context: &wire.UIContext{UIVersion: 6, Platform: "web", Stream: &wire.UIStreamCapability{Protocol: 1, Components: []string{"text", "button", "row", "code"}}}}
}
func retryOpenTree(t *testing.T, raw json.RawMessage) *uistream.Tree {
	t.Helper()
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	tree, err := uistream.DecodeSnapshot(envelope["snapshot"])
	if err != nil {
		t.Fatalf("invalid recovery snapshot: %v", err)
	}
	return tree
}
func TestPageLostOpenSnapshotRetriesReuseAllocation(t *testing.T) {
	h, process, _ := pageProcessManager(t)
	req := retryOpenRequest()
	raw, err := pageCall(t, h, pageWho, req)
	if err != nil {
		t.Fatal(err)
	}
	first := retryOpenTree(t, raw)
	who := pageWho
	// Deliberately discard the delivered snapshot before each reconnect. Eight
	// independent pages still fit: retries did not consume their owner's quota.
	for i := 0; i < 12; i++ {
		h.pageDisconnect(who)
		who.Connection = fmt.Sprintf("reconnected-%d", i)
		raw, err = pageCall(t, h, who, req)
		if err != nil {
			t.Fatal(err)
		}
		next := retryOpenTree(t, raw)
		if next.Session() != first.Session() || next.Epoch() != first.Epoch() {
			t.Fatal("retry allocated a different page")
		}
		h.uiPages.mu.Lock()
		count := len(h.uiPages.sessions)
		h.uiPages.mu.Unlock()
		if count != 1 {
			t.Fatalf("retry retained %d allocations", count)
		}
	}
	for i := 0; i < 7; i++ {
		pageOpenTest(t, h, who)
	}
	req.RequestID = "ninth-page"
	if _, err = pageCall(t, h, who, req); err == nil {
		t.Fatal("independent pages exceeded owner quota")
	}
	if stats := pageStats(t, process); stats["mounts"] != 8 {
		t.Fatalf("mount count: %v", stats)
	}
}
func TestPageOpenDuplicateBodyAndConnectionOwnership(t *testing.T) {
	h, _, _ := pageProcessManager(t)
	req := retryOpenRequest()
	raw, err := pageCall(t, h, pageWho, req)
	if err != nil {
		t.Fatal(err)
	}
	first := retryOpenTree(t, raw)
	raw, err = pageCall(t, h, pageWho, req)
	if err != nil {
		t.Fatal(err)
	}
	if retryOpenTree(t, raw).Session() != first.Session() {
		t.Fatal("same-connection retry allocated")
	}
	changed := req
	changed.State = "changed"
	if _, err = pageCall(t, h, pageWho, changed); err == nil {
		t.Fatal("changed payload reused open ID")
	}
	other := pageWho
	other.Connection = "other-live-connection"
	if _, err = pageCall(t, h, other, req); err == nil {
		t.Fatal("second live connection stole initial snapshot")
	}
	other.Principal = "another-principal"
	raw, err = pageCall(t, h, other, req)
	if err != nil {
		t.Fatal(err)
	}
	if retryOpenTree(t, raw).Session() == first.Session() {
		t.Fatal("request ID crossed principal ownership")
	}
}
func TestPageConcurrentDuplicateOpensMountOnce(t *testing.T) {
	h, process, _ := pageProcessManager(t)
	req := retryOpenRequest()
	const n = 6
	var wg sync.WaitGroup
	results := make(chan json.RawMessage, n)
	errs := make(chan error, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			raw, _ := json.Marshal(wire.UIStreamClientEnvelope{Type: "extension_ui_stream", Request: &req})
			frame, err := h.uiStream(t.Context(), extwire.UIStreamCall{Who: pageWho, Envelope: raw})
			results <- frame
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	id := ""
	for raw := range results {
		tree := retryOpenTree(t, raw)
		if id != "" && id != tree.Session() {
			t.Fatal("duplicate mounts")
		}
		id = tree.Session()
	}
	if stats := pageStats(t, process); stats["mounts"] != 1 {
		t.Fatalf("mount count: %v", stats)
	}
}
func TestPageFailureCodesUseManagerMetadataNotText(t *testing.T) {
	for _, code := range []string{"expired", "inactive", "forbidden", "invalid", "quota", "negotiation"} {
		if got := pageFailureCode(fmt.Errorf("wrapped: %w", pageError(code))); got != code {
			t.Errorf("typed %s classified as %s", code, got)
		}
		for _, lang := range []i18n.Lang{i18n.En, i18n.ZhHans, i18n.Ja} {
			forgedText := fmt.Errorf("%s", i18n.Localize(pageError(code), lang))
			if got := pageFailureCode(forgedText); got != "rejected" {
				t.Errorf("localized text became lifecycle authority: %s", got)
			}
		}
	}
	if got := pageFailureCode(i18n.Err("extension-stream.future_code")); got != "rejected" {
		t.Errorf("unknown metadata escaped allowlist: %s", got)
	}
}

func TestPageRequestEmitsCorrelatedExpiredCode(t *testing.T) {
	h, _, transport := pageProcessManager(t)
	close(h.started)
	request := wire.UIStreamRequest{Request: "resume", Protocol: 1, RequestID: "missing-resume", Session: "missing-session", Epoch: "missing-epoch", Version: 1}
	envelope, _ := json.Marshal(wire.UIStreamClientEnvelope{Type: "extension_ui_stream", Request: &request})
	params, _ := json.Marshal(extwire.UIStreamCall{Who: pageWho, Envelope: envelope})
	if _, handled, err := h.Request(t.Context(), extwire.MethodUIStream, params); !handled || err != nil {
		t.Fatalf("request rejected outside the correlated transport: %v", err)
	}
	pageAwait(t, func() bool { return len(transport.all()) > 0 })
	var reply wire.UIStreamServerEnvelope
	if err := json.Unmarshal(transport.all()[0].Envelope, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Kind != "error" || reply.RequestID != request.RequestID || reply.Error == nil || reply.Error.Code != "expired" {
		t.Fatalf("uncorrelated lifecycle error: %+v", reply)
	}
}

func TestPageDuplicateWaitDoesNotBlockOtherReservations(t *testing.T) {
	h := New(Options{Config: &Config{}})
	req := retryOpenRequest()
	s, reused, err := h.pageReserveOpen(pageWho, req, quotaPageProcess{}, extension.UISlot{}, extension.UIParams{})
	if err != nil || reused {
		t.Fatalf("first reserve: %v %v", reused, err)
	}
	duplicate, reused, err := h.pageReserveOpen(pageWho, req, quotaPageProcess{}, extension.UISlot{}, extension.UIParams{})
	if err != nil || !reused || duplicate != s {
		t.Fatalf("duplicate reserve: %v %v", reused, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	if _, err = h.pageRetryOpen(ctx, pageWho, req, s); err != context.DeadlineExceeded {
		t.Fatalf("wait was not bounded by caller: %v", err)
	}
	req.RequestID = "independent"
	if _, reused, err = h.pageReserveOpen(pageWho, req, quotaPageProcess{}, extension.UISlot{}, extension.UIParams{}); err != nil || reused {
		t.Fatalf("another page blocked: %v", err)
	}
}
