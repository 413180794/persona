package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/node"
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

func callRequest() request {
	return request{JSONRPC: "2.0", ID: json.RawMessage(`3`), Method: "tools/call", Params: json.RawMessage(`{"name":"notify_user","arguments":{}}`)}
}

// A tool call made while the resident node's socket is gone for a hub restart waits for it; one the node received
// and dropped is not sent again.
func TestToolsCallWaitsForTheNodeSocket(t *testing.T) {
	n := fakeNode(t, func(n int) ([]byte, error) {
		if n < 3 {
			return nil, fmt.Errorf("%w: dial unix node.sock: no such file or directory", node.ErrNoNode)
		}
		return []byte(`{"content":[{"type":"text","text":"sent"}]}`), nil
	})
	res, rerr := handle(context.Background(), "test", callRequest())
	if rerr != nil || *n != 3 || !strings.Contains(fmt.Sprint(res), "sent") {
		t.Fatalf("call across the gap: %+v %+v after %d dials", res, rerr, *n)
	}
	n = fakeNode(t, func(int) ([]byte, error) { return nil, errors.New("node closed connection: EOF") })
	res, _ = handle(context.Background(), "test", callRequest())
	if *n != 1 || !strings.Contains(fmt.Sprint(res), "not reachable") {
		t.Fatalf("a call the node may have run was sent %d times: %+v", *n, res)
	}
}

// syncBuf is an io.Writer the test reads while Serve writes.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); !ok(); {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// A tool call waiting for the phone holds no other request back; one Claude Code cancels (the user stopped the turn)
// hangs up on the node, so its card comes off the phone, and gets no answer.
func TestServeRunsCallsAtOnceAndCancelsThem(t *testing.T) {
	old := dialNode
	t.Cleanup(func() { dialNode = old })
	var mu sync.Mutex
	hungUp := map[string]bool{}
	dialNode = func(ctx context.Context, req any) ([]byte, error) {
		name, _ := req.(map[string]any)["name"].(string)
		if name == "mail_send" { // waits for the phone until its caller goes away
			<-ctx.Done()
			mu.Lock()
			hungUp[name] = true
			mu.Unlock()
			return nil, ctx.Err()
		}
		return []byte(`{"content":[{"type":"text","text":"here"}]}`), nil
	}
	in, w := io.Pipe()
	out := &syncBuf{}
	done := make(chan error, 1)
	go func() { done <- Serve(context.Background(), "test", in, out) }()
	send := func(s string) { _, _ = io.WriteString(w, s+"\n") }

	send(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mail_send","arguments":{}}}`)
	send(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"phone_location_current","arguments":{}}}`)
	waitFor(t, "the second call answered while the first waits", func() bool { return strings.Contains(out.String(), `"id":2`) })
	if strings.Contains(out.String(), `"id":1`) {
		t.Fatalf("the waiting call answered: %s", out.String())
	}

	send(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1,"reason":"interrupted"}}`)
	waitFor(t, "the cancelled call hung up on the node", func() bool { mu.Lock(); defer mu.Unlock(); return hungUp["mail_send"] })
	send(`{"jsonrpc":"2.0","id":3,"method":"ping"}`)
	waitFor(t, "ping", func() bool { return strings.Contains(out.String(), `"id":3`) })
	_ = w.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), `"id":1`) {
		t.Fatalf("a cancelled call was answered: %s", out.String())
	}
}

// The client going away (stdin closed) ends what still waits: no card is left on the phone for a dead process.
func TestServeEndsCallsWhenTheClientGoes(t *testing.T) {
	old := dialNode
	t.Cleanup(func() { dialNode = old })
	ended := make(chan struct{})
	dialNode = func(ctx context.Context, req any) ([]byte, error) {
		<-ctx.Done()
		close(ended)
		return nil, ctx.Err()
	}
	in := strings.NewReader(`{"jsonrpc":"2.0","id":"a","method":"tools/call","params":{"name":"ask_user","arguments":{}}}` + "\n")
	done := make(chan error, 1)
	go func() { done <- Serve(context.Background(), "test", in, &syncBuf{}) }()
	select {
	case <-ended:
	case <-time.After(3 * time.Second):
		t.Fatal("the call kept waiting after the client went away")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestIDKey(t *testing.T) {
	if idKey(json.RawMessage(` 7`)) != idKey(json.RawMessage(`7`)) || idKey(json.RawMessage(`"7"`)) == idKey(json.RawMessage(`7`)) {
		t.Fatal("ids are keyed by value, a string apart from a number")
	}
}
