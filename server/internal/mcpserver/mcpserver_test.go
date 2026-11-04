package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func listRequest() request {
	return request{JSONRPC: "2.0", ID: json.RawMessage(`2`), Method: "tools/list"}
}

// fakeNode answers the n-th dial (from 1) with dial(n).
func fakeNode(t *testing.T, dial func(n int) ([]byte, error)) *int {
	t.Helper()
	oldDial, oldEvery := dialNode, retryEvery
	t.Cleanup(func() { dialNode, retryEvery = oldDial, oldEvery })
	retryEvery = time.Millisecond
	n := 0
	dialNode = func(context.Context, any) ([]byte, error) { n++; return dial(n) }
	return &n
}

func shortCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	t.Cleanup(cancel)
	return ctx
}

func TestToolsListWithoutNodeIsAnError(t *testing.T) {
	n := fakeNode(t, func(int) ([]byte, error) { return nil, errors.New("dial unix node.sock: no such file or directory") })
	res, rerr := handle(shortCtx(t), "test", listRequest())
	if rerr == nil {
		t.Fatalf("no node: want an error, got %+v", res)
	}
	if !strings.Contains(rerr.Message, "no such file") {
		t.Fatalf("error should say why: %q", rerr.Message)
	}
	if *n < 2 {
		t.Fatalf("dialed %d times, want retries", *n)
	}
}

func TestToolsListNodeErrorIsAnError(t *testing.T) {
	fakeNode(t, func(int) ([]byte, error) { return []byte(`{"tools":[],"error":"hub not connected"}`), nil })
	_, rerr := handle(shortCtx(t), "test", listRequest())
	if rerr == nil || !strings.Contains(rerr.Message, "hub not connected") {
		t.Fatalf("node reported an error: want it passed on, got %+v", rerr)
	}
}

func TestToolsListRetriesUntilTheNodeIsUp(t *testing.T) {
	fakeNode(t, func(n int) ([]byte, error) {
		switch {
		case n == 1:
			return nil, errors.New("dial unix node.sock: connection refused")
		case n == 2:
			return []byte(`{"tools":[],"error":"hub not connected"}`), nil
		}
		return []byte(`{"tools":[{"name":"phone_health_summary","description":"health"}]}`), nil
	})
	res, rerr := handle(context.Background(), "test", listRequest())
	if rerr != nil {
		t.Fatalf("unexpected error %+v", rerr)
	}
	tools := res.(map[string]any)["tools"].([]map[string]any)
	if len(tools) != 1 || tools[0]["name"] != "phone_health_summary" || tools[0]["inputSchema"] == nil {
		t.Fatalf("tools = %+v", tools)
	}
}

func TestToolsListEmptyFromAHealthyNodeIsNotAnError(t *testing.T) {
	n := fakeNode(t, func(int) ([]byte, error) { return []byte(`{"tools":[]}`), nil })
	res, rerr := handle(shortCtx(t), "test", listRequest())
	if rerr != nil {
		t.Fatalf("unexpected error %+v", rerr)
	}
	if tools := res.(map[string]any)["tools"].([]map[string]any); len(tools) != 0 || *n != 1 {
		t.Fatalf("tools = %+v after %d dials", tools, *n)
	}
}
