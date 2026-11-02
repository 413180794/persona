//go:build go1.25

package hub

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/coder/websocket"
)

type heartbeatSocket struct {
	writes chan []byte
	pings  chan time.Time
	pongs  chan struct{}
}

func (s *heartbeatSocket) Write(_ context.Context, _ websocket.MessageType, b []byte) error {
	s.writes <- b
	return nil
}
func (s *heartbeatSocket) Ping(ctx context.Context) error {
	s.pings <- time.Now()
	select {
	case <-s.pongs:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func startHeartbeatWriter(t *testing.T) (*client, *heartbeatSocket, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c := &client{out: make(chan []byte, 16), meta: clientMeta{updates: make(chan heartbeatConfig, 16), done: ctx.Done()}}
	s := &heartbeatSocket{writes: make(chan []byte, 16), pings: make(chan time.Time, 16), pongs: make(chan struct{}, 16)}
	done := make(chan error, 1)
	go func() { done <- c.writeWS(ctx, s, func(heartbeatConfig) {}) }()
	synctest.Wait()
	return c, s, done
}

func TestHeartbeatLegacyTiming(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, s, done := startHeartbeatWriter(t)
		time.Sleep(19 * time.Second)
		if len(s.pings) != 0 || len(s.writes) != 0 {
			t.Fatal("legacy connection sent early")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if len(s.pings) != 1 {
			t.Fatal("legacy ping must start at 20 seconds")
		}
		time.Sleep(14 * time.Second)
		select {
		case err := <-done:
			t.Fatalf("early timeout: %v", err)
		default:
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("timeout: %v", err)
		}
	})
}

func TestHeartbeatChangeResetsTimer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, s, _ := startHeartbeatWriter(t)
		h := &Hub{}
		time.Sleep(19 * time.Second)
		h.onClientHello(c, []byte(`{"heartbeat":{"interval":240,"pong_timeout":45}}`))
		synctest.Wait()
		checkHelloAck(t, <-s.writes, c.meta.heartbeat)
		time.Sleep(2 * time.Second)
		if len(s.pings) != 0 {
			t.Fatal("old timer survived background negotiation")
		}
		h.onClientHello(c, []byte(`{"heartbeat":{"interval":10,"pong_timeout":5}}`))
		synctest.Wait()
		checkHelloAck(t, <-s.writes, c.meta.heartbeat)
		time.Sleep(10 * time.Second)
		synctest.Wait()
		if len(s.pings) != 1 {
			t.Fatal("foreground negotiation waited for the old timer")
		}
	})
}

func TestHeartbeatChangeWhileAwaitingPong(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, s, done := startHeartbeatWriter(t)
		time.Sleep(20 * time.Second)
		synctest.Wait()
		h := &Hub{}
		h.onAppPing(c, []byte(`{"id":"pending"}`))
		synctest.Wait()
		if got := string(<-s.writes); got != `{"id":"pending","type":"pong"}` {
			t.Fatalf("app pong blocked: %s", got)
		}
		h.onClientHello(c, []byte(`{"heartbeat":{"interval":240,"pong_timeout":60}}`))
		synctest.Wait()
		checkHelloAck(t, <-s.writes, c.meta.heartbeat)
		time.Sleep(16 * time.Second)
		select {
		case err := <-done:
			t.Fatalf("old pong deadline survived: %v", err)
		default:
		}
		h.onClientHello(c, []byte(`{"heartbeat":{"interval":10,"pong_timeout":5}}`))
		synctest.Wait()
		if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("new deadline did not apply: %v", err)
		}
	})
}

func TestHeartbeatPongPreservesCadence(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, s, done := startHeartbeatWriter(t)
		time.Sleep(20 * time.Second)
		synctest.Wait()
		<-s.pings
		time.Sleep(3 * time.Second)
		s.pongs <- struct{}{}
		synctest.Wait()
		time.Sleep(17 * time.Second)
		synctest.Wait()
		if len(s.pings) != 1 {
			t.Fatal("legacy cadence changed after pong")
		}
		select {
		case err := <-done:
			t.Fatalf("completed pong timed out: %v", err)
		default:
		}
	})
}

func TestHeartbeatConnectionsNegotiateIndependently(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		background, bgSocket, _ := startHeartbeatWriter(t)
		_, legacySocket, _ := startHeartbeatWriter(t)
		(&Hub{}).onClientHello(background, []byte(`{"heartbeat":{"interval":240,"pong_timeout":60}}`))
		synctest.Wait()
		time.Sleep(20 * time.Second)
		synctest.Wait()
		if len(bgSocket.pings) != 0 || len(legacySocket.pings) != 1 {
			t.Fatal("one connection's negotiation changed another connection")
		}
	})
}
