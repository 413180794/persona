package hub

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/extension/uistream"
	"github.com/nwverse/persona/server/internal/extmgr"
	"github.com/nwverse/persona/server/internal/extwire"
)

// This uses the real SDK Node program, real manager and existing app WebSocket.
// Its sanctioned injected remote process is NOT production sandbox/Deno E2E.
func TestUIStreamRealSDKManagerMainAppSocket(t *testing.T) {
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("Bun source fixture builder unavailable")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node injected source host unavailable")
	}
	h, srv, _ := newBusHub(t)
	h.public.id = "00000000-0000-4000-8000-000000000001"
	store := managerDB(t, h)
	dir := shortTempDir(t)
	program := filepath.Join(dir, "program.mjs")
	socket := filepath.Join(dir, "p.sock")
	source, err := filepath.Abs("../extmgr/testdata/ui-page-program.tsx")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(bun, "build", source, "--target=node", "--outfile="+program).CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	sourceCtx, stopSource := context.WithCancel(t.Context())
	cmd := exec.CommandContext(sourceCtx, node, program, socket)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopSource(); _ = cmd.Wait() })
	eventually(t, 3*time.Second, "source socket", func() bool { _, err := os.Stat(socket); return err == nil })
	manifest := json.RawMessage(`{"manifest_version":1,"id":"example-page","name":"Synthetic page","version":"1.0.0","engines":{"extension_protocol":1},"runtime":{"mode":"process","command":"program","rpc":"stdio"},"activation_events":["on_call"],"contributes":{"ui":[{"id":"home","title":"Synthetic page","placement":"extension_page"}]}}`)
	if _, err := extension.Parse(manifest); err != nil {
		t.Fatal(err)
	}
	if err := store.PutExtension(t.Context(), extension.Record{ID: "example-page", Source: extension.SourceInstalled, Version: "1.0.0", Manifest: manifest, Enabled: true, Remote: &extension.Remote{RPCSocket: socket}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.DeleteExtension(context.Background(), "example-page") })
	var manager *extmgr.Manager
	raw := dialRawBusWith(t, srv, busToken(t, h, assistantwire.AssistantID, "Synthetic manager"), func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		if manager != nil {
			if answer, handled, err := manager.Request(ctx, method, params); handled {
				return answer, err
			}
		}
		return map[string]any{}, nil
	})
	manager = extmgr.New(extmgr.Options{Dir: dir, Config: &extmgr.Config{}, Store: store, Hub: raw.peer, Logf: t.Logf})
	raw.peer.OnEvent = func(event string, params json.RawMessage) { manager.Event(event, params) }
	var hello json.RawMessage
	if err := raw.call(assistantwire.MethodHello, map[string]any{"id": assistantwire.AssistantID}, &hello); err != nil {
		t.Fatal(err)
	}
	manager.Hello(hello)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- manager.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(8 * time.Second):
			t.Error("manager shutdown timeout")
		}
	})
	select {
	case <-manager.Started():
	case <-time.After(5 * time.Second):
		t.Fatal("manager start timeout")
	}
	eventually(t, 3*time.Second, "manager stream capability report", func() bool {
		return slices.Contains(h.extReport().Features, extwire.FeatureUIStream)
	})
	openSocket := func() *websocket.Conn {
		t.Helper()
		url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/ws?token=" + h.cfg.Token + "&features=" + extwire.FeatureUIStream
		ws, _, err := websocket.Dial(t.Context(), url, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ws.CloseNow() })
		helloCtx, helloCancel := context.WithTimeout(t.Context(), 4*time.Second)
		defer helloCancel()
		for {
			_, frame, err := ws.Read(helloCtx)
			if err != nil {
				t.Fatal(err)
			}
			var hello struct {
				Type     string   `json:"type"`
				Features []string `json:"features"`
			}
			if json.Unmarshal(frame, &hello) != nil || hello.Type != "hello" {
				continue
			}
			if !slices.Contains(hello.Features, extwire.FeatureUIStream) {
				t.Fatalf("authenticated hello omitted manager stream support: %s", frame)
			}
			break
		}
		return ws
	}
	ws := openSocket()
	other := openSocket()
	send := func(ws *websocket.Conn, request wire.UIStreamRequest) {
		t.Helper()
		raw, err := json.Marshal(wire.UIStreamClientEnvelope{Type: "extension_ui_stream", Request: &request})
		if err != nil {
			t.Fatal(err)
		}
		if err := ws.Write(t.Context(), websocket.MessageText, raw); err != nil {
			t.Fatal(err)
		}
	}
	read := func(ws *websocket.Conn) map[string]json.RawMessage {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
		defer cancel()
		for {
			_, raw, err := ws.Read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var frame map[string]json.RawMessage
			_ = json.Unmarshal(raw, &frame)
			if string(frame["type"]) == `"extension_ui_stream"` {
				return frame
			}
		}
	}
	send(ws, wire.UIStreamRequest{Request: "open", Protocol: 1, RequestID: "open", Extension: "example-page", Contribution: "home", Context: &wire.UIContext{UIVersion: 6, Platform: "web", Stream: &wire.UIStreamCapability{Protocol: 1, Components: []string{"text", "button", "row", "code"}}}})
	first := read(ws)
	if string(first["kind"]) != `"snapshot"` {
		t.Fatalf("first frame: %s", first)
	}
	tree, err := uistream.DecodeSnapshot(first["snapshot"])
	if err != nil {
		t.Fatal(err)
	}
	snapshot := tree.Snapshot()
	callback := ""
	for _, item := range snapshot.View.Sections[0].Items {
		if item.Key == "increment" {
			callback = item.Action.ID
		}
	}
	interaction := wire.UIStreamEvent{Protocol: 1, Session: tree.Session(), Epoch: tree.Epoch(), Version: tree.Version(), EventID: "increment", Key: "increment", Action: callback, Event: "press", Value: json.RawMessage(`{}`)}
	request := wire.UIStreamRequest{Request: "event", Protocol: 1, RequestID: "increment", Interaction: &interaction}
	send(ws, request)
	deltaFrame := read(ws)
	if string(deltaFrame["kind"]) != `"delta"` {
		t.Fatalf("expected delta: %s", deltaFrame)
	}
	delta, err := uistream.DecodeDelta(deltaFrame["delta"])
	if err != nil {
		t.Fatal(err)
	}
	tree, err = uistream.Apply(tree, delta)
	if err != nil {
		t.Fatal(err)
	}
	receipt := read(ws)
	if string(receipt["kind"]) != `"receipt"` {
		t.Fatalf("expected receipt: %s", receipt)
	}
	send(ws, request)
	replay := read(ws)
	if string(replay["receipt"]) != string(receipt["receipt"]) {
		t.Fatal("WS retry did not replay receipt")
	}
	// The other authenticated connection sees public startup traffic, never this tree.
	privacyCtx, privacyCancel := context.WithTimeout(t.Context(), 75*time.Millisecond)
	defer privacyCancel()
	for {
		_, raw, err := other.Read(privacyCtx)
		if err != nil {
			break
		}
		var frame map[string]json.RawMessage
		_ = json.Unmarshal(raw, &frame)
		if string(frame["type"]) == `"extension_ui_stream"` {
			t.Fatal("private page broadcast to another connection")
		}
	}
	_ = ws.Close(websocket.StatusNormalClosure, "background")
	time.Sleep(25 * time.Millisecond)
	replacement := openSocket()
	send(replacement, wire.UIStreamRequest{Request: "resume", Protocol: 1, RequestID: "resume", Session: tree.Session(), Epoch: tree.Epoch(), Version: tree.Version()})
	resumed := read(replacement)
	if string(resumed["kind"]) != `"snapshot"` {
		t.Fatalf("resume must begin with snapshot: %s", resumed)
	}
}

func TestUIStreamRequiresClientOptInAndAuthenticatedConnection(t *testing.T) {
	for _, tc := range []struct {
		name       string
		optIn      bool
		role       string
		connection string
	}{
		{"no opt-in", false, roleFull, "synthetic-connection"},
		{"restricted role", true, "restricted", "synthetic-connection"},
		{"no connection identity", true, roleFull, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &Hub{}
			c := &client{role: tc.role, uiConnection: tc.connection,
				urlFeatures: map[string]bool{extwire.FeatureUIStream: tc.optIn},
				uiManagers:  make(map[string]bool), uiRequests: make(chan struct{}, 1)}
			h.forwardUIStream(t.Context(), c, []byte(`{"type":"extension_ui_stream","manager":"synthetic-manager","request":{"request":"open"}}`))
			if len(c.uiManagers) != 0 || len(c.uiRequests) != 0 {
				t.Fatal("capability advertisement bypassed the client/authentication gate")
			}
		})
	}
}

func TestUIStreamUnavailableManagerKeepsCorrelation(t *testing.T) {
	h, srv, _ := newBusHub(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/ws?token=" + h.cfg.Token + "&features=" + extwire.FeatureUIStream
	ws, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	envelope := wire.UIStreamClientEnvelope{Type: "extension_ui_stream", Manager: "unavailable-manager", Request: &wire.UIStreamRequest{Request: "open", Protocol: 1, RequestID: "pending-page", Extension: "example-page", Contribution: "home", Context: &wire.UIContext{UIVersion: 6, Stream: &wire.UIStreamCapability{Protocol: 1, Components: []string{"text"}}}}}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.Write(ctx, websocket.MessageText, raw); err != nil {
		t.Fatal(err)
	}
	for {
		_, raw, err := ws.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var reply wire.UIStreamServerEnvelope
		if json.Unmarshal(raw, &reply) != nil || reply.Type != "extension_ui_stream" {
			continue
		}
		if err := wire.ValidateJSON("UIStreamServerEnvelope", raw); err != nil {
			t.Fatalf("invalid failure envelope: %v", err)
		}
		if reply.Kind != "error" || reply.RequestID != "pending-page" || reply.Manager != "unavailable-manager" {
			t.Fatalf("uncorrelated manager error: %s", raw)
		}
		return
	}
}
