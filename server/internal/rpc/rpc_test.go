package rpc

import (
	"bytes"
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

// A call refused before its request went out is ErrClosed; one whose connection closed while it was out is
// ErrDropped (the other side may have done it), which is ErrClosed too.
func TestCallDroppedWhileOutIsErrDropped(t *testing.T) {
	closed := New("h", io.Discard)
	closed.Close(io.EOF)
	if err := closed.Call(context.Background(), "x", nil, nil); errors.Is(err, ErrDropped) {
		t.Fatalf("not sent, yet dropped: %v", err)
	}
	out := New("h", io.Discard)
	go func() {
		time.Sleep(50 * time.Millisecond)
		out.Close(io.EOF)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := out.Call(ctx, "x", nil, nil)
	if !errors.Is(err, ErrDropped) || !errors.Is(err, ErrClosed) || err.Error() != ErrClosed.Error() {
		t.Fatalf("want ErrDropped (as ErrClosed), got %v", err)
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

func TestCodedErrorReachesCaller(t *testing.T) {
	a2b, b2a := io.Pipe()
	c2d, d2c := io.Pipe()
	a := New("a", b2a)
	b := New("b", d2c)
	b.OnRequest = func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		return nil, &Error{Code: "input_draft", Message: "draft there", Data: json.RawMessage(`{"draft":"hi"}`)}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = a.Serve(ctx, c2d) }()
	go func() { _ = b.Serve(ctx, a2b) }()
	err := a.Call(ctx, "x", nil, nil)
	var re *Error
	if !errors.As(err, &re) || re.Code != "input_draft" || string(re.Data) != `{"draft":"hi"}` || re.Message != "draft there" {
		t.Fatalf("got %#v", err)
	}
}

// A caller that stopped waiting before dispatch must not start work on the remote machine.
func TestCanceledCallDoesNotSend(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(map[bool]string{false: "canceled", true: "deadline"}[expired], func(t *testing.T) {
			var wire bytes.Buffer
			p := New("h", &wire)
			ctx, cancel := context.WithCancel(context.Background())
			if expired {
				cancel()
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			} else {
				cancel()
			}
			defer cancel()
			wantErr := ctx.Err()
			if err := p.Call(ctx, "session.start", map[string]string{"prompt": "hello"}, nil); !errors.Is(err, wantErr) {
				t.Fatalf("call = %v, want %v", err, wantErr)
			}
			if wire.Len() != 0 {
				t.Fatalf("canceled request reached wire: %s", wire.String())
			}
		})
	}
}

type callMarshalFunc func() ([]byte, error)

func (f callMarshalFunc) MarshalJSON() ([]byte, error) { return f() }

func TestCallCanceledDuringMarshalDoesNotSend(t *testing.T) {
	var wire bytes.Buffer
	p := New("h", &wire)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	params := callMarshalFunc(func() ([]byte, error) {
		cancel()
		return []byte(`{}`), nil
	})
	if err := p.Call(ctx, "session.start", params, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("call = %v, want canceled", err)
	}
	if wire.Len() != 0 {
		t.Fatalf("canceled request reached wire: %s", wire.String())
	}
}

func TestCallCanceledBeforeWriterAvailableDoesNotSend(t *testing.T) {
	var wire bytes.Buffer
	p := New("h", &wire)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	marshaled := make(chan struct{})
	params := callMarshalFunc(func() ([]byte, error) {
		close(marshaled)
		return []byte(`{}`), nil
	})
	// Another outbound message owns the writer until after this call loses its caller.
	p.wmu.Lock()
	done := make(chan error, 1)
	go func() { done <- p.Call(ctx, "session.start", params, nil) }()
	select {
	case <-marshaled:
	case <-time.After(time.Second):
		p.wmu.Unlock()
		t.Fatal("request did not reach serialization")
	}
	cancel()
	p.wmu.Unlock()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("call = %v, want canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled call did not return after the writer became available")
	}
	if wire.Len() != 0 {
		t.Fatalf("queued canceled request reached wire: %s", wire.String())
	}
}

// A call the caller gives up on ends on the other side too, for the methods in Cancels (#2597); the others run on
// there, as they always did.
func TestCancelReachesTheHandler(t *testing.T) {
	hub, node := pair(t)
	node.Cancels = map[string]bool{"wait": true}
	ended := make(chan string, 2)
	hub.OnRequest = func(ctx context.Context, method string, _ json.RawMessage) (any, error) {
		select {
		case <-ctx.Done():
			ended <- method + " cancelled"
		case <-time.After(300 * time.Millisecond):
			ended <- method + " ran on"
		}
		return nil, nil
	}
	for _, method := range []string{"wait", "other"} {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		if err := node.Call(ctx, method, nil, nil); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s: %v", method, err)
		}
		cancel()
		select {
		case got := <-ended:
			want := map[string]string{"wait": "wait cancelled", "other": "other ran on"}[method]
			if got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s: the handler never ended", method)
		}
	}
}
