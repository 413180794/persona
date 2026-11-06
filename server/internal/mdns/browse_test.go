package mdns

import (
	"maps"
	"net/netip"
	"slices"
	"strconv"
	"testing"
	"time"
)

type browseFixture struct {
	typ, inst, host       name
	ptr, srv, txt, a4, ll record
}

func newFixture(t *testing.T) browseFixture {
	typ := mustType(t, "_persona-hub._tcp")
	inst := append(name{"Hub A"}, typ...)
	host := name{"persona-a", "local"}
	return browseFixture{
		typ:  typ,
		inst: inst,
		host: host,
		ptr:  record{name: typ, typ: typePTR, class: classIN, ttl: 120, target: inst},
		srv:  record{name: inst, typ: typeSRV, class: classIN | topBit, ttl: 120, port: 8443, target: host},
		txt:  record{name: inst, typ: typeTXT, class: classIN | topBit, ttl: 120, txt: []string{"hub_id=a"}},
		a4:   record{name: host, typ: typeA, class: classIN | topBit, ttl: 120, addr: netip.MustParseAddr("192.0.2.1")},
		ll:   record{name: host, typ: typeAAAA, class: classIN | topBit, ttl: 120, addr: netip.MustParseAddr("fe80::1")},
	}
}

func resp(rs ...record) *message {
	return &message{flags: flagResponse | flagAA, answers: rs}
}

func qtypes(qs []question) []uint16 {
	var out []uint16
	for _, q := range qs {
		out = append(out, q.typ)
	}
	return out
}

// Records arrive in pieces: a PTR alone, then SRV + TXT, then the addresses.
func TestBrowserAggregates(t *testing.T) {
	f := newFixture(t)
	b := newBrowser(f.typ, "")
	t0 := time.Unix(1_000_000, 0)

	found, follow := b.apply(resp(f.ptr), "eth0", t0)
	if len(found) != 0 {
		t.Fatalf("PTR alone reported %+v", found)
	}
	if !slices.Equal(qtypes(follow), []uint16{typeSRV, typeTXT}) || !follow[0].name.equal(f.inst) {
		t.Fatalf("follow-up after PTR = %+v", follow)
	}
	if _, follow = b.apply(resp(f.ptr), "eth0", t0.Add(time.Second)); len(follow) != 0 {
		t.Errorf("follow-up repeated within %v: %+v", followEvery, follow)
	}

	t1 := t0.Add(6 * time.Second)
	found, follow = b.apply(resp(f.srv, f.txt), "eth0", t1)
	if len(found) != 1 {
		t.Fatalf("SRV + TXT: found %+v", found)
	}
	e := found[0]
	if e.Instance != "Hub A" || e.Host != "persona-a.local." || e.Port != 8443 || e.Addrs != nil ||
		!maps.Equal(e.TXT, map[string]string{"hub_id": "a"}) || e.TTL != 120*time.Second || !e.Seen.Equal(t1) {
		t.Errorf("SRV + TXT: got %+v", e)
	}
	if !slices.Equal(qtypes(follow), []uint16{typeA, typeAAAA}) || !follow[0].name.equal(f.host) {
		t.Errorf("follow-up after SRV = %+v", follow)
	}

	found, _ = b.apply(resp(f.a4, f.ll), "eth0", t1.Add(time.Second))
	want := []netip.Addr{netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("fe80::1%eth0")}
	if len(found) != 1 || !slices.Equal(found[0].Addrs, want) {
		t.Fatalf("addresses: got %+v, want %v", found, want)
	}

	// The same records again: nothing new to report.
	if found, follow = b.apply(resp(f.ptr, f.srv, f.txt, f.a4), "eth0", t1.Add(2*time.Second)); len(found)+len(follow) != 0 {
		t.Errorf("unchanged records reported %+v / asked %+v", found, follow)
	}

	// TXT changes: reported again.
	txt2 := f.txt
	txt2.txt = []string{"hub_id=a", "name=Lab"}
	found, _ = b.apply(resp(txt2), "eth0", t1.Add(3*time.Second))
	if len(found) != 1 || found[0].TXT["name"] != "Lab" || len(found[0].Addrs) != 2 {
		t.Errorf("TXT change: got %+v", found)
	}

	// An address goodbye removes just that address.
	gone := f.ll
	gone.ttl = 0
	found, _ = b.apply(resp(gone), "eth0", t1.Add(4*time.Second))
	if len(found) != 1 || !slices.Equal(found[0].Addrs, want[:1]) {
		t.Errorf("address goodbye: got %+v", found)
	}

	// The instance's goodbye.
	bye := f.ptr
	bye.ttl = 0
	found, _ = b.apply(resp(bye), "eth0", t1.Add(5*time.Second))
	if len(found) != 1 || found[0].TTL != 0 || found[0].Instance != "Hub A" {
		t.Fatalf("goodbye: got %+v", found)
	}
	if len(b.insts) != 0 {
		t.Errorf("goodbye left %d instances", len(b.insts))
	}
	if found, _ = b.apply(resp(bye), "eth0", t1.Add(6*time.Second)); len(found) != 0 {
		t.Errorf("second goodbye reported %+v", found)
	}
}

func TestBrowserGoodbyeBeforeReport(t *testing.T) {
	f := newFixture(t)
	b := newBrowser(f.typ, "")
	now := time.Unix(1_000_000, 0)
	b.apply(resp(f.ptr), "", now)
	bye := f.ptr
	bye.ttl = 0
	if found, _ := b.apply(resp(bye), "", now); len(found) != 0 {
		t.Errorf("never-reported instance said goodbye: %+v", found)
	}
}

func TestBrowserIgnoresSelf(t *testing.T) {
	f := newFixture(t)
	b := newBrowser(f.typ, "HUB a")
	found, follow := b.apply(resp(f.ptr, f.srv, f.txt, f.a4), "", time.Now())
	if len(found)+len(follow)+len(b.insts) != 0 {
		t.Errorf("own records were used: %+v %+v", found, follow)
	}
}

func TestBrowserOtherTypeIgnored(t *testing.T) {
	f := newFixture(t)
	b := newBrowser(mustType(t, "_other._tcp"), "")
	if found, follow := b.apply(resp(f.ptr, f.srv, f.txt, f.a4), "", time.Now()); len(found)+len(follow) != 0 {
		t.Errorf("other type: %+v %+v", found, follow)
	}
}

func TestBrowserExpiry(t *testing.T) {
	f := newFixture(t)
	b := newBrowser(f.typ, "")
	t0 := time.Unix(1_000_000, 0)
	short := f.a4
	short.ttl = 10
	found, _ := b.apply(resp(f.ptr, f.srv, f.txt, short, f.ll), "eth0", t0)
	if len(found) != 1 || len(found[0].Addrs) != 2 {
		t.Fatalf("got %+v", found)
	}
	if next := b.next(); !next.Equal(t0.Add(10 * time.Second)) {
		t.Errorf("next = %v, want +10s", next.Sub(t0))
	}
	if found = b.sweep(t0.Add(9 * time.Second)); len(found) != 0 {
		t.Errorf("early sweep reported %+v", found)
	}
	found = b.sweep(t0.Add(10 * time.Second))
	if len(found) != 1 || len(found[0].Addrs) != 1 || found[0].Addrs[0] != netip.MustParseAddr("fe80::1%eth0") {
		t.Errorf("address expiry: got %+v", found)
	}
	found = b.sweep(t0.Add(120 * time.Second))
	if len(found) != 1 || found[0].TTL != 0 {
		t.Errorf("instance expiry: got %+v", found)
	}
	if len(b.insts) != 0 || !b.next().IsZero() {
		t.Errorf("expired instance still cached")
	}
}

func TestBrowserHostMove(t *testing.T) {
	f := newFixture(t)
	b := newBrowser(f.typ, "")
	now := time.Unix(1_000_000, 0)
	b.apply(resp(f.ptr, f.srv, f.a4), "", now)
	moved := f.srv
	moved.target = name{"persona-b", "local"}
	found, _ := b.apply(resp(moved), "", now.Add(time.Second))
	if len(found) != 1 || found[0].Host != "persona-b.local." || len(found[0].Addrs) != 0 {
		t.Errorf("host move: got %+v", found)
	}
}

func TestBrowserLimits(t *testing.T) {
	f := newFixture(t)
	b := newBrowser(f.typ, "")
	var rs []record
	for i := range maxInstances + 50 {
		r := f.ptr
		r.target = append(name{"hub-" + strconv.Itoa(i)}, f.typ...)
		rs = append(rs, r)
	}
	b.apply(resp(rs...), "", time.Now())
	if len(b.insts) != maxInstances {
		t.Errorf("cached %d instances, want %d", len(b.insts), maxInstances)
	}

	b = newBrowser(f.typ, "")
	rs = []record{f.ptr, f.srv}
	for i := range maxHostAddrs + 10 {
		a := f.a4
		a.addr = netip.AddrFrom4([4]byte{192, 0, 2, byte(i + 1)})
		rs = append(rs, a)
	}
	found, _ := b.apply(resp(rs...), "", time.Now())
	if len(found) != 1 || len(found[0].Addrs) != maxHostAddrs {
		t.Errorf("addresses kept: %d", len(found[0].Addrs))
	}
}

func TestBrowserClampsTTL(t *testing.T) {
	f := newFixture(t)
	b := newBrowser(f.typ, "")
	long := f.ptr
	long.ttl = 0xffffffff
	srv := f.srv
	srv.ttl = 0xffffffff
	found, _ := b.apply(resp(long, srv), "", time.Now())
	if len(found) != 1 || found[0].TTL != maxTTL {
		t.Errorf("got %+v", found)
	}
}
