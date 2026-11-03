// Package conformance holds a connector to the protocol (docs/connector-protocol.md): it runs the connector on the
// program's side of the protocol (connector/serve) over a pipe, calls every capability its manifest declares both
// through the wire and directly, and fails when the two differ — a field the wire loses, a type it changes. Built-in
// connectors are called directly in production; this is what keeps "in the hub is one implementation of the
// protocol" true. A third party's program can be held to the same with Program.
package conformance

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/connector/serve"
	"github.com/nwverse/persona/server/internal/rpc"
)

// Check runs c through the protocol and directly and compares what each declared capability gives back. c is
// built and running as the test has it (its Run and Watch are not started again).
func Check(t testing.TB, c connector.Connector) {
	t.Helper()
	if err := connector.Check(c); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	progR, hubW := io.Pipe()
	hubR, progW := io.Pipe()
	t.Cleanup(func() { hubW.Close(); progW.Close(); progR.Close(); hubR.Close() })
	go func() { _ = serve.Serve(ctx, progR, progW, c) }()
	hub := rpc.New("h", hubW)
	hub.OnRequest = func(context.Context, string, json.RawMessage) (any, error) { return serve.SecretData{}, nil }
	go func() { _ = hub.Serve(ctx, hubR) }()
	call := func(method string, params any) json.RawMessage {
		t.Helper()
		var raw json.RawMessage
		if err := hub.Call(ctx, method, params, &raw); err != nil {
			t.Fatalf("%s through the protocol: %v", method, err)
		}
		return raw
	}

	var man serve.Manifest
	if err := json.Unmarshal(call(serve.MInit, serve.InitParams{Protocol: serve.Protocol}), &man); err != nil {
		t.Fatal(err)
	}
	same(t, "manifest", serve.ManifestOf(c), man)

	m := c.Manifest()
	if m.Has(connector.CapStatus) {
		var got connector.Status
		decode(t, call(serve.MStatus, struct{}{}), &got)
		same(t, "status", connector.StatusOf(ctx, c), got)
	}
	if t2, ok := connector.As[connector.ReadTools](c); ok {
		var got serve.ToolsResult
		decode(t, call(serve.MTools, struct{}{}), &got)
		same(t, "tools", serve.ToolsResult{Tools: t2.Tools()}, got)
	}
	if a, ok := connector.As[connector.Actions](c); ok {
		var got serve.ToolsResult
		decode(t, call(serve.MActions, struct{}{}), &got)
		same(t, "actions", serve.ToolsResult{Tools: a.Actions()}, got)
		// Describe only: actions.do writes, and only ever after the hub's gate.
		for _, act := range a.Actions() {
			var through serve.DescribeResult
			var throughErr string
			var raw json.RawMessage
			if err := hub.Call(ctx, serve.MDescribe, serve.CallParams{Name: act.Name, Args: json.RawMessage(`{}`)}, &raw); err != nil {
				throughErr = err.Error()
			} else {
				decode(t, raw, &through)
			}
			action, summary, err := a.Describe(ctx, act.Name, json.RawMessage(`{}`))
			directErr := ""
			if err != nil {
				directErr, action, summary = err.Error(), connector.Action{}, ""
			}
			same(t, "describe "+act.Name, serve.DescribeResult{Action: action, Summary: summary}, through)
			if directErr != throughErr {
				t.Errorf("describe %s: %q directly, %q through the protocol", act.Name, directErr, throughErr)
			}
		}
	}
	if in, ok := connector.As[connector.Inbox](c); ok {
		var got serve.CollectResult
		decode(t, call(serve.MCollect, struct{}{}), &got)
		things, err := in.Collect(ctx)
		if err != nil {
			t.Fatalf("collect: %v", err)
		}
		same(t, "inbox", serve.CollectResult{Things: things}, got)
	}
	if a, ok := connector.As[connector.Archive](c); ok {
		var got serve.ChangesResult
		decode(t, call(serve.MChanges, serve.ChangesParams{Limit: 50}), &got)
		b, next, more, err := a.Changes(ctx, "", 50)
		if err != nil {
			t.Fatalf("changes: %v", err)
		}
		same(t, "changes", serve.ChangesResult{Batches: b, Next: next, More: more}, got)
	}
}

func decode(t testing.TB, raw json.RawMessage, out any) {
	t.Helper()
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
}

// same fails unless direct and through (decoded from the wire) say the same thing, as JSON.
func same(t testing.TB, what string, direct, through any) {
	t.Helper()
	a, _ := json.Marshal(direct)
	b, _ := json.Marshal(through)
	if string(a) != string(b) {
		t.Errorf("%s differs through the protocol:\ndirect  %s\nthrough %s", what, a, b)
	}
}
