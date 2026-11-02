package netaddr

import (
	"net"
	"net/netip"
	"reflect"
	"testing"
)

// Generate classification inputs from range bytes rather than host addresses.
func v4(a, b, c, d byte) netip.Addr { return netip.AddrFrom4([4]byte{a, b, c, d}) }
func global(id byte) netip.Addr {
	b := netip.MustParseAddr("2001:db8::").As16()
	// Flip a documentation-prefix bit for classification only; never dial it.
	b[1], b[15] = 0, id
	return netip.AddrFrom16(b)
}
func stableAddr(id byte) Addr {
	return Addr{IP: global(id), PrefixLen: 64, Interface: "eth0", InterfaceIndex: 2, InterfaceFlags: net.FlagUp, FlagsKnown: true, ScopeKnown: true, LifetimesKnown: true, PreferredLifetime: 3600, ValidLifetime: 7200, Class: GlobalIPv6}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		ip   netip.Addr
		want Class
	}{
		{"invalid", netip.Addr{}, Unknown},
		{"global", global(1), GlobalIPv6},
		{"ULA", netip.MustParseAddr("fc00::"), ULA},
		{"ULA upper", netip.MustParseAddr("fdff::"), ULA},
		{"link local", netip.MustParseAddr("fe80::1%eth0"), LinkLocal},
		{"loopback", netip.IPv6Loopback(), Loopback},
		{"unspecified", netip.IPv6Unspecified(), Unspecified},
		{"multicast", netip.MustParseAddr("ff02::1"), Multicast},
		{"documentation", netip.MustParseAddr("2001:db8::1"), Documentation},
		{"documentation new", netip.MustParseAddr("3fff::1"), Documentation},
		{"documentation upper", netip.MustParseAddr("3fff:fff::"), Documentation},
		{"documentation boundary", netip.MustParseAddr("3fff:1000::"), GlobalIPv6},
		{"6to4", netip.MustParseAddr("2002::1"), TransitionIPv6},
		{"Teredo", netip.MustParseAddr("2001::1"), TransitionIPv6},
		{"reserved v6", netip.MustParseAddr("4000::"), Reserved},
		{"doc v4", netip.MustParseAddr("192.0.2.1"), Documentation},
		{"doc v4 second", netip.MustParseAddr("198.51.100.1"), Documentation},
		{"doc v4 third", netip.MustParseAddr("203.0.113.1"), Documentation},
		{"mapped", netip.MustParseAddr("::ffff:192.0.2.1"), Documentation},
		{"private A", v4(10, 0, 0, 0), PrivateIPv4},
		{"private B", v4(172, 16, 0, 0), PrivateIPv4},
		{"private C", v4(192, 168, 0, 0), PrivateIPv4},
		{"shared start", v4(100, 64, 0, 0), SharedIPv4},
		{"shared end", v4(100, 127, 255, 255), SharedIPv4},
		{"shared before", v4(100, 63, 255, 255), PublicIPv4},
		{"shared after", v4(100, 128, 0, 0), PublicIPv4},
		{"link local v4", v4(169, 254, 0, 0), LinkLocal},
		{"loopback v4", v4(127, 0, 0, 1), Loopback},
		{"benchmark", v4(198, 18, 0, 0), Reserved},
		{"reserved v4", v4(240, 0, 0, 0), Reserved},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.ip); got != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestStable(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Addr)
		want   bool
	}{
		{"normal", func(a *Addr) {}, true},
		{"temporary", func(a *Addr) { a.Flags = FlagTemporary }, false},
		{"deprecated", func(a *Addr) { a.Flags = FlagDeprecated }, false},
		{"tentative", func(a *Addr) { a.Flags = FlagTentative }, false},
		{"DAD failed", func(a *Addr) { a.Flags = FlagDADFailed }, false},
		{"optimistic DAD", func(a *Addr) { a.Flags = FlagOptimistic | FlagTentative }, false},
		{"mngtmpaddr", func(a *Addr) { a.Flags = FlagManageTempAddr }, true},
		{"stable privacy", func(a *Addr) { a.Flags = FlagStablePrivacy }, true},
		{"static", func(a *Addr) { a.Flags = FlagPermanent }, true},
		{"unknown flags", func(a *Addr) { a.FlagsKnown = false }, false},
		{"unknown scope", func(a *Addr) { a.ScopeKnown = false }, false},
		{"wrong scope", func(a *Addr) { a.Scope = 253 }, false},
		{"virtual", func(a *Addr) { a.Virtual = true }, false},
		{"down", func(a *Addr) { a.InterfaceFlags = 0 }, false},
		{"loopback interface", func(a *Addr) { a.InterfaceFlags |= net.FlagLoopback }, false},
		{"expired preferred", func(a *Addr) { a.PreferredLifetime = 0 }, false},
		{"expired valid", func(a *Addr) { a.ValidLifetime = 0 }, false},
		{"unknown lifetime", func(a *Addr) { a.LifetimesKnown = false; a.PreferredLifetime = 0; a.ValidLifetime = 0 }, true},
		{"infinite", func(a *Addr) { a.PreferredLifetime = InfiniteLifetime; a.ValidLifetime = InfiniteLifetime }, true},
		{"ULA", func(a *Addr) { a.IP = netip.MustParseAddr("fc00::1") }, false},
		{"link local", func(a *Addr) { a.IP = netip.MustParseAddr("fe80::1") }, false},
		{"documentation", func(a *Addr) { a.IP = netip.MustParseAddr("2001:db8::1") }, false},
		{"documentation new", func(a *Addr) { a.IP = netip.MustParseAddr("3fff::1") }, false},
		{"loopback", func(a *Addr) { a.IP = netip.IPv6Loopback() }, false},
		{"transition", func(a *Addr) { a.IP = netip.MustParseAddr("2002::1") }, false},
		{"shared", func(a *Addr) { a.IP = v4(100, 64, 0, 0) }, false},
		{"public v4 requires ingress check", func(a *Addr) { a.IP = v4(100, 128, 0, 0) }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := stableAddr(1)
			tt.change(&a)
			if Stable(a) != tt.want {
				t.Fatalf("Stable=%v, want %v", Stable(a), tt.want)
			}
		})
	}
}

func TestCandidatesAndSelect(t *testing.T) {
	static := stableAddr(1)
	static.Flags = FlagPermanent
	static.PreferredLifetime = 10
	managed := stableAddr(2)
	managed.Flags = FlagManageTempAddr
	privacy := stableAddr(3)
	privacy.Flags = FlagStablePrivacy
	privacy.PreferredLifetime = InfiniteLifetime
	longer := stableAddr(4)
	longer.Flags = FlagManageTempAddr
	longer.PreferredLifetime = 7200
	valid := longer
	valid.IP = global(5)
	valid.ValidLifetime = 9000
	unknown := stableAddr(6)
	unknown.LifetimesKnown = false
	bad := stableAddr(7)
	bad.Flags = FlagTemporary
	input := []Addr{bad, unknown, privacy, managed, longer, valid, static}
	before := append([]Addr(nil), input...)
	got := Candidates(input)
	want := []Addr{static, valid, longer, managed, unknown, privacy}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("incorrect preference order")
	}
	if !reflect.DeepEqual(input, before) {
		t.Fatal("mutated input")
	}
	current := privacy
	current.PreferredLifetime = 1
	if chosen, ok := Select(input, current); !ok || chosen != privacy {
		t.Fatal("did not retain current address with fresh metadata")
	}
	if chosen, ok := Select(input, bad); !ok || chosen != static {
		t.Fatal("did not replace unsuitable current address")
	}
	if _, ok := Select([]Addr{bad}, current); ok {
		t.Fatal("selected unsuitable address")
	}
	tie := managed
	tie.IP = global(9)
	if got := Candidates([]Addr{tie, managed}); got[0] != managed {
		t.Fatal("IP tie breaker")
	}
}

func TestFlagsAndVirtualNames(t *testing.T) {
	a := stableAddr(1)
	a.Flags = FlagTemporary
	if !a.Temporary() || !a.Dynamic() {
		t.Fatal("missing IPv6 flags")
	}
	a.IP = v4(192, 0, 2, 1)
	if a.Temporary() {
		t.Fatal("IPv4 secondary is not temporary")
	}
	a.Flags = FlagPermanent
	if a.Dynamic() {
		t.Fatal("permanent is not dynamic")
	}
	a.FlagsKnown = false
	a.Flags = 0
	if a.Dynamic() {
		t.Fatal("unknown is not dynamic")
	}
	for _, name := range []string{"docker0", "veth1", "br-example", "virbr0", "tun0", "utun1", "tailscale0", "wg0", "bridge0", "vxlan0", "dummy0"} {
		if !virtualName(name) {
			t.Errorf("missed virtual %s", name)
		}
	}
	for _, name := range []string{"eth0", "en0", "enp1s0", "wlan0"} {
		if virtualName(name) {
			t.Errorf("misclassified %s", name)
		}
	}
}
