package rpc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRuntimeRequestBudget(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := New("h", io.Discard)
	p.Limits = Limits{FrameBytes: 1024, ConcurrentRequests: 2}
	var entered atomic.Int32
	p.OnRequest = func(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
		entered.Add(1)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	frames := `{"id":"1","method":"wait"}` + "\n" + `{"id":"2","method":"wait"}` + "\n" + `{"id":"3","method":"wait"}` + "\n"
	if err := p.Serve(ctx, strings.NewReader(frames)); err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatal(err)
	}
	if entered.Load() > 2 {
		t.Fatal("unbounded handler creation")
	}
}
func TestRuntimeFrameAndPendingBudgets(t *testing.T) {
	p := New("h", io.Discard)
	p.Limits = Limits{FrameBytes: 64, PendingCalls: 1}
	if err := p.Notify("large", strings.Repeat("x", 65)); err == nil {
		t.Fatal("oversized outgoing frame accepted")
	}
	p.pending["already"] = make(chan message, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := p.Call(ctx, "next", nil, nil); err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatal(err)
	}
	if err := p.Serve(ctx, strings.NewReader(strings.Repeat("x", 65))); err == nil {
		t.Fatal("oversized incoming frame accepted")
	}
}

func TestRuntimeClosureCancelsAcceptedWork(t *testing.T) {
	for _, budgetViolation := range []bool{false, true} {
		t.Run(map[bool]string{false: "explicit-close", true: "request-budget"}[budgetViolation], func(t *testing.T) {
			p := New("h", io.Discard)
			p.Limits = Limits{ConcurrentRequests: 1}
			entered := make(chan context.Context, 1)
			finished := make(chan struct{})
			p.OnRequest = func(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
				entered <- ConnContext(ctx)
				<-ctx.Done()
				close(finished)
				return nil, ctx.Err()
			}
			reader, writer := io.Pipe()
			defer reader.Close()
			defer writer.Close()
			served := make(chan error, 1)
			go func() { served <- p.Serve(context.Background(), reader) }()
			io.WriteString(writer, "{\"id\":\"1\",\"method\":\"wait\"}\n")
			var conn context.Context
			select {
			case conn = <-entered:
			case <-time.After(time.Second):
				t.Fatal("request did not enter")
			}
			if budgetViolation {
				io.WriteString(writer, "{\"id\":\"2\",\"method\":\"wait\"}\n")
			} else {
				p.Close(ErrClosed)
			}
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("closure retained the accepted handler")
			}
			if conn.Err() == nil {
				t.Fatal("connection-scoped work remained alive")
			}
			writer.Close()
			select {
			case <-served:
			case <-time.After(time.Second):
				t.Fatal("Serve did not finish")
			}
		})
	}
}

func TestRuntimeOversizedResponseReturnsError(t *testing.T) {
	var wire bytes.Buffer
	p := New("h", &wire)
	p.Limits = Limits{FrameBytes: 256}
	p.OnRequest = func(context.Context, string, json.RawMessage) (any, error) {
		return strings.Repeat("x", 1024), nil
	}
	p.handle(context.Background(), message{ID: "request", Method: "large"})
	var reply message
	if err := json.NewDecoder(bufio.NewReader(&wire)).Decode(&reply); err != nil {
		t.Fatal(err)
	}
	if reply.ID != "request" || reply.Error == nil || !strings.Contains(reply.Error.Message, "budget") {
		t.Fatalf("missing bounded failure reply: %+v", reply)
	}
	p.Limits.FrameBytes = 16
	p.handle(context.Background(), message{ID: "request", Method: "large"})
	select {
	case <-p.Done():
	default:
		t.Fatal("unrepresentable error reply must close the peer")
	}
}
