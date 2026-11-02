package hub

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestSourceOf(t *testing.T) {
	// Build private ranges from their numeric components so fixtures do not resemble private host addresses.
	private4 := netip.AddrFrom4([4]byte{10, 0, 0, 1}).String()
	private172 := netip.AddrFrom4([4]byte{172, 16, 0, 1}).String()
	private192 := netip.AddrFrom4([4]byte{192, 168, 0, 1}).String()
	linkLocal4 := netip.AddrFrom4([4]byte{169, 254, 0, 1}).String()
	cgnat := netip.AddrFrom4([4]byte{100, 64, 0, 1}).String()
	for _, tc := range []struct {
		name, remote, local string
		trusted             []string
		vpn                 bool
		want                source
	}{
		{name: "loopback4", remote: "127.0.0.1:1", want: sourceLoopback},
		{name: "loopback6", remote: "[::1]:1", want: sourceLoopback},
		{name: "mapped loopback", remote: "[::ffff:127.0.0.1]:1", want: sourceLoopback},
		{name: "private10", remote: net.JoinHostPort(private4, "1"), want: sourceLAN},
		{name: "private172", remote: net.JoinHostPort(private172, "1"), want: sourceLAN},
		{name: "private192", remote: net.JoinHostPort(private192, "1"), want: sourceLAN},
		{name: "ULA", remote: "[fd00::1]:1", want: sourceLAN},
		{name: "link local4", remote: net.JoinHostPort(linkLocal4, "1"), want: sourceLAN},
		{name: "link local6 zone", remote: "[fe80::1%en0]:1", want: sourceLAN},
		{name: "public4", remote: "192.0.2.1:1", want: sourcePublic},
		{name: "mapped public4", remote: "[::ffff:192.0.2.1]:1", want: sourcePublic},
		{name: "public6", remote: "[2001:db8::1]:1", want: sourcePublic},
		{name: "trusted4", remote: "192.0.2.1:1", trusted: []string{"192.0.2.0/24"}, want: sourceLAN},
		{name: "trusted6", remote: "[2001:db8::1]:1", trusted: []string{"2001:db8::/32"}, want: sourceLAN},
		{name: "bad range", remote: "192.0.2.1:1", trusted: []string{"invalid"}, want: sourcePublic},
		{name: "carrier physical", remote: net.JoinHostPort(cgnat, "1"), local: "192.0.2.2:1", want: sourcePublic},
		{name: "carrier VPN", remote: net.JoinHostPort(cgnat, "1"), local: "192.0.2.2:1", vpn: true, want: sourceLAN},
		{name: "carrier no local", remote: net.JoinHostPort(cgnat, "1"), vpn: true, want: sourcePublic},
		{name: "carrier trusted", remote: net.JoinHostPort(cgnat, "1"), trusted: []string{cgnat + "/32"}, want: sourceLAN},
		{name: "public through VPN", remote: "192.0.2.1:1", local: "192.0.2.2:1", vpn: true, want: sourcePublic},
		{name: "invalid", remote: "bad", want: sourcePublic},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHub(t)
			h.cfg.Security = SecurityConfig{TrustedCIDRs: tc.trusted}
			h.devtok.onLink = func() []netip.Prefix { return nil }
			h.devtok.vpnLocal = func(local netip.Addr) bool {
				if local.String() != "192.0.2.2" {
					t.Fatalf("local = %s", local)
				}
				return tc.vpn
			}
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tc.remote
			r.Header.Set("X-Forwarded-For", "127.0.0.1")
			if tc.local != "" {
				local, err := net.ResolveTCPAddr("tcp", tc.local)
				if err != nil {
					t.Fatal(err)
				}
				r = r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey, local))
			}
			if got := h.sourceOf(r); got != tc.want {
				t.Fatalf("source = %v, want %v", got, tc.want)
			}
		})
	}
}

// A container's gateway (docker-proxy relays the internet through it) is public unless trusted; other private
// addresses in the container stay the LAN.
func TestSourceOfContainerGateway(t *testing.T) {
	gateway := netip.AddrFrom4([4]byte{172, 17, 0, 1})
	peer := netip.AddrFrom4([4]byte{172, 17, 0, 3})
	gateway6 := netip.MustParseAddr("fd00:17::1")
	for _, tc := range []struct {
		name    string
		remote  netip.Addr
		trusted []string
		want    source
	}{
		{name: "gateway", remote: gateway, want: sourcePublic},
		{name: "gateway6", remote: gateway6, want: sourcePublic},
		{name: "gateway trusted", remote: gateway, trusted: []string{gateway.String() + "/32"}, want: sourceLAN},
		{name: "other container", remote: peer, want: sourceLAN},
		{name: "loopback", remote: netip.MustParseAddr("127.0.0.1"), want: sourceLoopback},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHub(t)
			h.cfg.Security = SecurityConfig{TrustedCIDRs: tc.trusted}
			h.devtok.onLink = func() []netip.Prefix { return nil }
			h.admin.gateways = func() []netip.Addr { return []netip.Addr{gateway, gateway6} }
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = netip.AddrPortFrom(tc.remote, 1).String()
			if got := h.sourceOf(r); got != tc.want {
				t.Fatalf("source = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestVPNInterfaceNames(t *testing.T) {
	for _, name := range []string{"tailscale0", "utun3", "wg0", "tun0", "ppp0"} {
		if !vpnInterface(name) {
			t.Errorf("VPN %q", name)
		}
	}
	for _, name := range []string{"en0", "eth0", "docker0", "lo0"} {
		if vpnInterface(name) {
			t.Errorf("physical %q", name)
		}
	}
}

func TestSourceOfOnLinkIPv6(t *testing.T) {
	h := newTestHub(t)
	h.devtok.onLink = func() []netip.Prefix {
		return []netip.Prefix{netip.MustParsePrefix("2001:db8:1::/64"), netip.MustParsePrefix("2001:db8:2::/48")}
	}
	for _, tc := range []struct {
		remote string
		want   source
	}{
		{"[2001:db8:1::99]:1", sourceLAN},     // a phone at home on the hub's /64
		{"[2001:db8:1::abcd:1]:1", sourceLAN}, // its temporary address, same /64
		{"[2001:db8:3::1]:1", sourcePublic},   // elsewhere
		{"[2001:db8:2:5::1]:1", sourcePublic}, // inside a /48 on an interface: too broad to mean home
		{"192.0.2.1:1", sourcePublic},         // IPv4 is not decided by on-link prefixes
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = tc.remote
		if got := h.sourceOf(r); got != tc.want {
			t.Errorf("%s: source = %v, want %v", tc.remote, got, tc.want)
		}
	}
}
