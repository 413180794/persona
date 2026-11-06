package mdns

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunRejectsBadOptions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	bad := testService()
	bad.Port = 0
	for desc, o := range map[string]Options{
		"nothing to do":  {},
		"bad service":    {Advertise: &bad},
		"bad browse":     {Browse: "persona"},
		"bad both":       {Advertise: &bad, Browse: "_persona-hub._tcp"},
		"browse invalid": {Browse: "_a._b._tcp"},
	} {
		if err := Run(ctx, o); err == nil {
			t.Errorf("%s: no error", desc)
		}
	}
}

// Run binds 5353 next to the system responder, sends nothing (no interfaces), and stops cleanly.
func TestRunStops(t *testing.T) {
	before := runtime.NumGoroutine()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	svc := testService()
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{Advertise: &svc, Browse: svc.Type, Interfaces: []net.Interface{}, Logf: t.Logf})
	}()
	select {
	case err := <-done:
		if err != nil {
			if strings.Contains(err.Error(), "no multicast socket") {
				t.Skipf("cannot bind the mDNS port here: %v", err)
			}
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after ctx ended")
	}
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<16)
			t.Fatalf("goroutines: %d before, %d after\n%s", before, runtime.NumGoroutine(), buf[:runtime.Stack(buf, true)])
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// liveRun is a Run in the background for TestLive.
type liveRun struct {
	found chan Entry
	done  chan error
	stop  context.CancelFunc
}

func startLive(t *testing.T, o Options, inst string) *liveRun {
	ctx, cancel := context.WithCancel(context.Background())
	lr := &liveRun{found: make(chan Entry, 32), done: make(chan error, 1), stop: cancel}
	o.Logf = t.Logf
	o.Found = func(e Entry) {
		if e.Instance == inst {
			select {
			case lr.found <- e:
			default:
			}
		}
	}
	go func() { lr.done <- Run(ctx, o) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-lr.done:
		case <-time.After(5 * time.Second):
			t.Error("Run did not stop")
		}
	})
	return lr
}

// await waits for an entry that satisfies ok.
func (lr *liveRun) await(t *testing.T, what string, timeout time.Duration, ok func(Entry) bool) Entry {
	t.Helper()
	deadline := time.After(timeout)
	var last Entry
	for {
		select {
		case e := <-lr.found:
			if ok(e) {
				return e
			}
			last = e
		case err := <-lr.done:
			t.Fatalf("%s: Run stopped: %v", what, err)
		case <-deadline:
			t.Fatalf("%s: nothing within %v (last: %+v)", what, timeout, last)
		}
	}
}

func (lr *liveRun) halt(t *testing.T, what string) time.Duration {
	t.Helper()
	start := time.Now()
	lr.stop()
	select {
	case err := <-lr.done:
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		lr.done <- nil // for the cleanup
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not stop", what)
	}
	return time.Since(start)
}

// TestLive runs a real advertiser and real browsers on this machine's interfaces. It sends a few mDNS packets on the
// LAN, so it only runs with PERSONA_MDNS_LIVE=1. It checks: announcements reach a browser that was already running,
// a browser started later finds the instance through its query and the answer, a legacy unicast query gets a
// unicast reply, and stopping the advertiser sends a goodbye.
func TestLive(t *testing.T) {
	if os.Getenv("PERSONA_MDNS_LIVE") != "1" {
		t.Skip("set PERSONA_MDNS_LIVE=1 to send real mDNS packets on this machine's interfaces")
	}
	const typ = "_persona-test._tcp"
	rnd := make([]byte, 4)
	rand.Read(rnd)
	inst := "live test " + hex.EncodeToString(rnd)
	live := func(e Entry) bool { return e.TTL > 0 && len(e.Addrs) > 0 }

	early := startLive(t, Options{Browse: typ, QueryEvery: 2 * time.Second}, inst)
	start := time.Now()
	adv := startLive(t, Options{Advertise: &Service{Instance: inst, Type: typ, Port: 18443, TXT: map[string]string{"hub_id": inst, "v": "test"}}}, inst)

	e := early.await(t, "announcement", 10*time.Second, live)
	t.Logf("announcement: found after %v: host %s, port %d, %d addresses, TXT %v, TTL %v",
		time.Since(start).Round(time.Millisecond), e.Host, e.Port, len(e.Addrs), e.TXT, e.TTL)
	if e.Port != 18443 || e.TXT["hub_id"] != inst || e.TXT["v"] != "test" {
		t.Errorf("entry = %+v", e)
	}

	// After both announcements (1 s apart), a new browser can only learn about the instance by asking.
	time.Sleep(time.Until(start.Add(1500 * time.Millisecond)))
	lateStart := time.Now()
	late := startLive(t, Options{Browse: typ}, inst)
	late.await(t, "query and answer", 10*time.Second, live)
	t.Logf("query and answer: found after %v", time.Since(lateStart).Round(time.Millisecond))

	// Legacy unicast: a query from an ordinary port gets a direct reply with the query id.
	if err := legacyQuery(t, typ, inst); err != nil {
		t.Errorf("legacy unicast query: %v", err)
	}

	t.Logf("advertiser stopped in %v", adv.halt(t, "advertiser").Round(time.Millisecond))
	early.await(t, "goodbye", 5*time.Second, func(e Entry) bool { return e.TTL == 0 })
	late.await(t, "goodbye (late browser)", 5*time.Second, func(e Entry) bool { return e.TTL == 0 })
}

func legacyQuery(t *testing.T, typ, inst string) error {
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
	if err != nil {
		return err
	}
	defer c.Close()
	tn, err := parseType(typ)
	if err != nil {
		return err
	}
	q, err := packMessage(&message{id: 0x4a52, questions: []question{{name: tn, typ: typePTR, class: classIN}}})
	if err != nil {
		return err
	}
	if _, err := c.WriteToUDP(q, groupV4); err != nil {
		return err
	}
	start := time.Now()
	buf := make([]byte, maxPacket)
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		n, _, err := c.ReadFromUDP(buf)
		if err != nil {
			return fmt.Errorf("no reply naming the instance: %w", err)
		}
		m, err := unpackMessage(buf[:n])
		if err != nil || m.id != 0x4a52 || !m.isResponse() || len(m.questions) != 1 {
			continue
		}
		for _, r := range m.answers {
			if r.typ == typePTR && len(r.target) > 0 && r.target[0] == inst {
				if r.ttl > 10 || r.class&topBit != 0 {
					return fmt.Errorf("legacy reply has TTL %d, class %#x", r.ttl, r.class)
				}
				t.Logf("legacy unicast: reply after %v", time.Since(start).Round(time.Millisecond))
				return nil
			}
		}
	}
}
