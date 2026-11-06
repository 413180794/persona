package extmgr

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/extwire"
)

func TestPageRequestsWaitForManagerStartup(t *testing.T) {
	h := New(Options{Config: &Config{}})
	envelope := json.RawMessage(`{"type":"extension_ui_stream","request":{"request":"open","protocol":1,"request_id":"startup","extension":"example-page","contribution":"home","context":{"stream":{"protocol":1,"components":["text"]}}}}`)
	raw, _ := json.Marshal(extwire.UIStreamCall{Who: pageWho, Envelope: envelope})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, handled, err := h.Request(ctx, extwire.MethodUIStream, raw)
	if !handled || err == nil {
		t.Fatal("page request reached uninitialized manager state")
	}
}

type quotaPageProcess struct{}

func (quotaPageProcess) Call(context.Context, string, any, any) error { return nil }
func (quotaPageProcess) Hold() func()                                 { return func() {} }
func (quotaPageProcess) Generation() uint64                           { return 1 }

func TestPageSessionConfiguredExtensionAndRuntimeTotals(t *testing.T) {
	for _, limit := range []PageSessionLimits{{Extension: 1}, {Runtime: 1}} {
		h := New(Options{Config: &Config{UIPageSessions: limit}})
		who := pageWho
		if _, err := h.pageReserve(who, "first", "home", quotaPageProcess{}, extension.UISlot{}, extension.UIParams{}); err != nil {
			t.Fatal(err)
		}
		who.Principal = "another-device"
		ext := "first"
		if limit.Runtime == 1 {
			ext = "second"
		}
		if _, err := h.pageReserve(who, ext, "home", quotaPageProcess{}, extension.UISlot{}, extension.UIParams{}); err == nil {
			t.Fatal("configured total quota bypassed")
		}
	}
	if boundedLimit(900, 8) != 8 || boundedLimit(-1, 8) != 8 || boundedLimit(4, 8) != 4 {
		t.Fatal("unsafe quota configuration")
	}
}

func TestPageSourceProcessGenerationReplacement(t *testing.T) {
	h, process, _ := pageProcessManager(t)
	tree := pageOpenTest(t, h, pageWho)
	run := process.Generation()
	// The injected Node program exits; the real Proc independently starts a new
	// process generation. Old opaque session/event IDs cannot bind to that run.
	_ = process.Call(t.Context(), "test.restart", nil, nil)
	pageAwait(t, func() bool { return process.Generation() > run })
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
	defer cancel()
	if err := process.Call(ctx, "ping", nil, nil); err != nil {
		t.Fatal(err)
	}
	if process.Generation() == run {
		t.Fatal("process generation did not change")
	}
	if _, err := pageCall(t, h, pageWho, pageEvent(t, tree, "no-ui", "old-generation")); err == nil {
		t.Fatal("old process callback executed")
	}
	fresh := pageOpenTest(t, h, pageWho)
	if fresh.Epoch() == tree.Epoch() || fresh.Session() == tree.Session() {
		t.Fatal("new source run reused manager identity")
	}
	if _, err := pageCall(t, h, pageWho, wire.UIStreamRequest{Request: "suspend", Protocol: 1, RequestID: "hide", Session: fresh.Session(), Epoch: fresh.Epoch()}); err != nil {
		t.Fatal(err)
	}
	if _, err := pageCall(t, h, pageWho, wire.UIStreamRequest{Request: "snapshot", Protocol: 1, RequestID: "hidden-snapshot", Session: fresh.Session(), Epoch: fresh.Epoch(), Version: fresh.Version()}); err == nil {
		t.Fatal("snapshot silently resumed hidden UI")
	}
}
