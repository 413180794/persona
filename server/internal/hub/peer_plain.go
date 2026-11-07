package hub

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"github.com/nwverse/persona/server/internal/i18n"
)

// Plaintext hub links (docs/server/mesh.md "Plaintext links", #4514): off unless hub.json lists mesh.plaintext.allow. Then pairing
// and the link may also go over http:// and ws://, in and out, but only with the addresses listed; everything else
// still needs TLS. Nothing on such a link is encrypted or pinned: the peer credential, the apps' requests a member
// passes on (with their credentials) and node streams can be read and changed on the way. The credentials each hub
// issued the other are all that is checked.

var errPlainNotAllowed = i18n.Err("hub-peer_dial.plain_not_allowed")

// plainRules is mesh.plaintext.allow parsed. Each entry is one of:
//   - "*": every address;
//   - a network or an address ("192.0.2.0/24", "2001:db8::/32", "192.0.2.7");
//   - an IPv4 address with trailing wildcard octets ("192.0.2.*", "198.51.*.*"), the same as its network;
//   - a host name ("hub.example.com") or a name wildcard ("*.example.com": any name under it, not the name itself).
//
// Addresses apply both ways; names only to dialing, since an incoming connection has an address and no name (and a
// reverse lookup says whatever its owner wants).
type plainRules struct {
	prefixes []netip.Prefix
	names    []string // lower case; "*.example.com" or "hub.example.com"
	any      bool
}

func (r plainRules) on() bool { return r.any || len(r.prefixes) > 0 || len(r.names) > 0 }

func parsePlainRules(list []string) (plainRules, error) {
	var r plainRules
	for _, raw := range list {
		s := strings.ToLower(strings.TrimSpace(raw))
		switch {
		case s == "":
		case s == "*":
			r.any = true
		case strings.Contains(s, "*") && !strings.HasPrefix(s, "*."):
			p, err := octetWildcard(s)
			if err != nil {
				return plainRules{}, err
			}
			r.prefixes = append(r.prefixes, p)
		default:
			if ps, err := parseCIDRs([]string{s}); err == nil {
				r.prefixes = append(r.prefixes, ps...)
			} else if validHostPattern(s) {
				r.names = append(r.names, strings.TrimSuffix(s, "."))
			} else {
				return plainRules{}, fmt.Errorf("not a network, an address or a host name: %q", raw)
			}
		}
	}
	return r, nil
}

// octetWildcard: "192.0.2.*" is 192.0.2.0/24, "198.51.*.*" 198.51.0.0/16; a * only after the fixed octets.
func octetWildcard(s string) (netip.Prefix, error) {
	parts := strings.Split(s, ".")
	bad := fmt.Errorf("a wildcard address is like 192.0.2.* (trailing * octets only): %q", s)
	if len(parts) != 4 {
		return netip.Prefix{}, bad
	}
	var b [4]byte
	fixed := 0
	for i, p := range parts {
		if p == "*" {
			continue
		}
		if fixed != i {
			return netip.Prefix{}, bad
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 255 || p != strconv.Itoa(n) {
			return netip.Prefix{}, bad
		}
		b[i] = byte(n)
		fixed++
	}
	return netip.PrefixFrom(netip.AddrFrom4(b), fixed*8), nil
}

// validHostPattern: a DNS name, optionally starting with "*." (and at least two labels after it).
func validHostPattern(s string) bool {
	s = strings.TrimSuffix(s, ".")
	if rest, ok := strings.CutPrefix(s, "*."); ok {
		if !strings.Contains(rest, ".") {
			return false // "*.com" would be most of the internet
		}
		s = rest
	}
	if s == "" || len(s) > 253 {
		return false
	}
	if last := s[strings.LastIndexByte(s, '.')+1:]; strings.Trim(last, "0123456789") == "" {
		return false // "192.0.2" is a mistyped address, not a name
	}
	for _, l := range strings.Split(s, ".") {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for _, c := range l {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// matchName: host (a name, not an address) matches one of the name patterns.
func (r plainRules) matchName(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	for _, n := range r.names {
		if rest, ok := strings.CutPrefix(n, "*"); ok {
			if strings.HasSuffix(host, rest) && len(host) > len(rest) {
				return true
			}
		} else if host == n {
			return true
		}
	}
	return false
}

// plainPeerRules are mesh.plaintext.allow; none (plaintext off) when any entry does not parse, so a typo never
// opens more than was meant.
func (h *Hub) plainPeerRules() plainRules {
	if len(h.cfg.Mesh.Plaintext.Allow) == 0 {
		return plainRules{}
	}
	r, err := parsePlainRules(h.cfg.Mesh.Plaintext.Allow)
	if err != nil {
		return plainRules{}
	}
	return r
}

// plainPeersOn: plaintext links are allowed with something.
func (h *Hub) plainPeersOn() bool { return h.plainPeerRules().on() }

// checkPlainPeers logs, once at start, what mesh.plaintext does.
func (h *Hub) checkPlainPeers() {
	if len(h.cfg.Mesh.Plaintext.Allow) == 0 {
		return
	}
	r, err := parsePlainRules(h.cfg.Mesh.Plaintext.Allow)
	if err != nil {
		h.logf("mesh.plaintext.allow: %v; links to other hubs stay TLS only until it is fixed", err)
		return
	}
	if r.any {
		h.logf("WARNING: mesh.plaintext.allow has \"*\": links with other hubs anywhere, the internet included, may go without TLS (unencrypted, unpinned)")
		return
	}
	h.logf("WARNING: mesh.plaintext.allow is set; links with other hubs at %s may go without TLS (unencrypted, unpinned)",
		strings.Join(h.cfg.Mesh.Plaintext.Allow, ", "))
}

// plainPeerAllowed: ip is in mesh.plaintext.allow. A container's gateway never is: it relays connections from
// anywhere (admin_gateway.go), so allowing it would allow everyone.
func (h *Hub) plainPeerAllowed(ip netip.Addr) bool {
	ip = ip.Unmap().WithZone("")
	if !ip.IsValid() || h.adminGateway(ip) {
		return false
	}
	r := h.plainPeerRules()
	if r.any {
		return true
	}
	for _, p := range r.prefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// plainPeerRequest: a plaintext request for pairing or a link (and nothing else) from an allowed address.
func (h *Hub) plainPeerRequest(r *http.Request) bool {
	if r.TLS != nil || (r.URL.Path != peerPath && r.URL.Path != peerPairPath) {
		return false
	}
	if _, passed := forwardedOf(r); passed { // a member's link stream: never a pairing or a link of its own
		return false
	}
	return h.plainPeerAllowed(remoteIP(r))
}

func isPlainEndpoint(e string) bool { return strings.HasPrefix(e, "http://") }

// plainBase is raw as an http:// base URL (scheme and host only), if it is one.
func plainBase(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "http" || u.Host == "" || u.User != nil || strings.Trim(u.Path, "/") != "" ||
		u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	return "http://" + u.Host, true
}

// plainEndpoints are the http:// addresses other hubs may use to reach this one without TLS: mesh.plaintext.advertise
// and, with no TLS listener, this hub's own plaintext addresses. None while plaintext links are off.
func (h *Hub) plainEndpoints() []string {
	if !h.plainPeersOn() {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(raw string) {
		if b, ok := plainBase(raw); ok && !seen[b] {
			seen[b] = true
			out = append(out, b)
		}
	}
	for _, raw := range h.cfg.Mesh.Plaintext.Advertise {
		add(raw)
	}
	for _, e := range h.endpointList() {
		if e.Scheme == "http" {
			add(e.URL)
		}
	}
	return out
}

// peerDialable drops the http:// candidates this hub may not dial: all of them while plaintext links are off, and
// those at an address not allowed (a name not matching a name rule is checked once it is resolved, plainPeerDial).
func (h *Hub) peerDialable(eps []string) []string {
	on := h.plainPeersOn()
	var out []string
	for _, e := range eps {
		if isPlainEndpoint(e) {
			if !on {
				continue
			}
			if u, err := url.Parse(e); err != nil {
				continue
			} else if ip, err := netip.ParseAddr(u.Hostname()); err == nil && !h.plainPeerAllowed(ip) {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}

// plainPeerDial connects to a peer's http:// address, to an allowed one only. A name matching a name rule may go to
// whatever it resolves to (that rule trusts the name, and so the DNS answer); any other name is resolved here and
// only its allowed addresses are used.
func (h *Hub) plainPeerDial(ctx context.Context, d *net.Dialer, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	var ips []netip.Addr
	byName := false
	if ip, err := netip.ParseAddr(host); err == nil {
		ips = []netip.Addr{ip}
	} else {
		if !h.plainPeersOn() {
			return nil, errPlainNotAllowed
		}
		byName = h.plainPeerRules().matchName(host)
		if ips, err = net.DefaultResolver.LookupNetIP(ctx, "ip", host); err != nil {
			return nil, err
		}
	}
	var errs []error
	for _, ip := range ips {
		if !byName && !h.plainPeerAllowed(ip) {
			continue
		}
		c, err := d.DialContext(ctx, network, net.JoinHostPort(ip.Unmap().WithZone("").String(), port))
		if err == nil {
			return c, nil
		}
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		return nil, errPlainNotAllowed
	}
	return nil, errors.Join(errs...)
}

// httpsOnly keeps the https candidates: what apps are shown (hello's mesh) never includes a plaintext address.
func httpsOnly(eps []string) []string {
	out := []string{}
	for _, e := range eps {
		if strings.HasPrefix(e, "https://") {
			out = append(out, e)
		}
	}
	return out
}
