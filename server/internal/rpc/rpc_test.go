package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// pair builds two Peers wired back to back through io.Pipe, the way hub and node talk over SSH stdio.
// Each Serve runs in its own goroutine and is stopped by the returned cleanup.
func pair(t *testing.T) (*Peer, *Peer) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	a2bR, a2bW := io.Pipe()
	b2aR, b2aW := io.Pipe()
	hub := New("h", a2bW)
	node := New("n", b2aW)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = hub.Serve(ctx, b2aR) }()
	go func() { defer wg.Done(); _ = node.Serve(ctx, a2bR) }()
	t.Cleanup(func() {
		cancel()
		_ = a2bW.Close()
		_ = b2aW.Close()
		wg.Wait()
	})
	return hub, node
}

func jsonArg(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("params %s: %v", raw, err)
	}
	return m
}

func TestCallBothDirections(t *testing.T) {
	hub, node := pair(t)
	node.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
		switch method {
		case "node.ping":
			return map[string]any{"pong": jsonArg(t, params)["n"]}, nil
		case "node.add":
			p := jsonArg(t, params)
			return map[string]any{"sum": p["a"].(float64) + p["b"].(float64)}, nil
		}
		return nil, errors.New("unknown method " + method)
	}
	hub.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
		if method != "hub.hello" {
			return nil, errors.New("unknown method")
		}
		return map[string]any{"who": jsonArg(t, params)["who"]}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var pong struct {
		Pong string `json:"pong"`
	}
	if err := hub.Call(ctx, "node.ping", map[string]any{"n": "hi"}, &pong); err != nil {
		t.Fatalf("hub→node call: %v", err)
	}
	if pong.Pong != "hi" {
		t.Fatalf("pong = %q", pong.Pong)
	}

	var sum struct {
		Sum float64 `json:"sum"`
	}
	if err := hub.Call(ctx, "node.add", map[string]any{"a": 2, "b": 3}, &sum); err != nil {
		t.Fatalf("node.add: %v", err)
	}
	if sum.Sum != 5 {
		t.Fatalf("sum = %v", sum.Sum)
	}

	// The reverse direction shares the same two peers.
	var hello struct {
		Who string `json:"who"`
	}
	if err := node.Call(ctx, "hub.hello", map[string]any{"who": "node"}, &hello); err != nil {
		t.Fatalf("node→hub call: %v", err)
	}
	if hello.Who != "node" {
		t.Fatalf("who = %q", hello.Who)
	}
}

func TestCallNilOutStillReturnsError(t *testing.T) {
	hub, node := pair(t)
	node.OnRequest = func(context.Context, string, json.RawMessage) (any, error) {
		return map[string]any{"ok": true}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := hub.Call(ctx, "anything", nil, nil); err != nil {
		t.Fatalf("call with nil out: %v", err)
	}
}

func TestCallPropagatesErrorMessage(t *testing.T) {
	hub, node := pair(t)
	node.OnRequest = func(context.Context, string, json.RawMessage) (any, error) {
		return nil, errors.New("herdr is not running")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := hub.Call(ctx, "session.start", nil, nil)
	if err == nil || err.Error() != "herdr is not running" {
		t.Fatalf("want the remote message, got %v", err)
	}
}

func TestNoHandlerIsAnError(t *testing.T) {
	hub, node := pair(t)
	node.OnRequest = nil
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := hub.Call(ctx, "node.info", nil, nil); err == nil {
		t.Fatal("want an error when the far side has no handler")
	}
}

func TestNotifyDeliversEvents(t *testing.T) {
	hub, node := pair(t)
	got := make(chan map[string]any, 1)
	node.OnEvent = func(event string, params json.RawMessage) {
		if event != "session.screen" {
			return
		}
		got <- jsonArg(t, params)
	}
	if err := hub.Notify("session.screen", map[string]any{"session_id": "s1", "status": "working"}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	select {
	case p := <-got:
		if p["session_id"] != "s1" || p["status"] != "working" {
			t.Fatalf("unexpected params %+v", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("event was not delivered")
	}
}

func TestNotifyWithoutHandlerDoesNotBlock(t *testing.T) {
	hub, node := pair(t)
	node.OnEvent = nil
	done := make(chan error, 1)
	go func() { done <- hub.Notify("sessions.changed", map[string]any{}) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("notify: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("notify blocked")
	}
}

func TestCallAfterPeerClosedReturnsErrClosed(t *testing.T) {
	hub := New("h", io.Discard)
	hub.Close(io.EOF)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := hub.Call(ctx, "x", nil, nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("want ErrClosed, got %v", err)
	}
	select {
	case <-hub.Done():
	default:
		t.Fatal("Done() should be closed")
	}
}

func TestServeReturnsOnEOF(t *testing.T) {
	r, w := io.Pipe()
	p := New("h", io.Discard)
	errc := make(chan error, 1)
	go func() { errc <- p.Serve(context.Background(), r) }()
	_ = w.Close()
	select {
	case err := <-errc:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("want io.EOF, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return")
	}
}

func TestCallContextCancelled(t *testing.T) {
	// The far side never answers, so the caller has to fall back to its context.
	p := New("h", io.Discard)
	p.OnRequest = nil
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := p.Call(ctx, "x", nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want the context error, got %v", err)
	}
}

func TestPeerPrefixDistinguishesIDs(t *testing.T) {
	hub, node := pair(t)
	seen := make(chan string, 2)
	node.OnRequest = func(_ context.Context, method string, _ json.RawMessage) (any, error) {
		seen <- method
		return struct{}{}, nil
	}
	hub.OnRequest = func(_ context.Context, method string, _ json.RawMessage) (any, error) {
		seen <- method
		return struct{}{}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := hub.Call(ctx, "a", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := node.Call(ctx, "b", nil, nil); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for range 2 {
		got[<-seen] = true
	}
	if !got["a"] || !got["b"] {
		t.Fatalf("missing calls: %+v", got)
	}
}

func TestUnknownJSONLinesAreSkipped(t *testing.T) {
	r, w := io.Pipe()
	p := New("h", io.Discard)
	events := make(chan string, 4)
	p.OnEvent = func(event string, _ json.RawMessage) { events <- event }
	errc := make(chan error, 1)
	go func() { errc <- p.Serve(context.Background(), r) }()
	for _, line := range []string{"garbage", `{}`, `{"event":"one"}`, `{"event":"two"}`} {
		if _, err := w.Write([]byte(line + "\n")); err != nil {
			t.Fatal(err)
		}
	}
	_ = w.Close()
	<-errc
	close(events)
	var got []string
	for e := range events {
		got = append(got, e)
	}
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("events = %+v", got)
	}
}
