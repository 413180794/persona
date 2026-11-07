package hub

import (
	"encoding/hex"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A container's gateway is not a LAN client. Docker's userland proxy (docker-proxy) hands a container every
// connection it relays from the bridge's gateway address (172.x.0.1): the host's own, Docker Desktop's and OrbStack's
// (all of them), and on Linux the IPv6 ones when Docker's IPv6 is off. That address is private, so without this a
// request from the internet would count as the LAN (docs/operations/admin.md "Docker bridge network").

// containerGateway reports whether ip is a gateway of the container this hub runs in: the default routes' next hops
// and the first address of every network the container is on (where Docker puts a bridge's gateway). Outside a
// container it is always false.
func containerGateway(ip netip.Addr) bool {
	if !inContainer() {
		return false
	}
	return slices.Contains(cachedContainerGateways(), ip.Unmap().WithZone(""))
}

var gatewayCache struct {
	sync.Mutex
	list []netip.Addr
	at   time.Time
}

func cachedContainerGateways() []netip.Addr {
	gatewayCache.Lock()
	defer gatewayCache.Unlock()
	if gatewayCache.list == nil || time.Since(gatewayCache.at) > onLinkRefresh {
		v4, _ := os.ReadFile("/proc/net/route")
		v6, _ := os.ReadFile("/proc/net/ipv6_route")
		var prefixes []netip.Prefix
		for _, p := range localInterfacePrefixes() {
			prefixes = append(prefixes, p.prefix)
		}
		gatewayCache.list, gatewayCache.at = containerGateways(string(v4), string(v6), prefixes), time.Now()
	}
	return gatewayCache.list
}

// containerGateways reads the default routes' next hops from /proc/net/route and /proc/net/ipv6_route, and adds the
// first address of each prefix (a network's .1, Docker's gateway by default; the only source when /proc is not
// readable). Never nil.
func containerGateways(route, route6 string, prefixes []netip.Prefix) []netip.Addr {
	out := []netip.Addr{}
	add := func(a netip.Addr) {
		if a.IsValid() && !a.IsUnspecified() && !slices.Contains(out, a) {
			out = append(out, a)
		}
	}
	for _, line := range strings.Split(route, "\n") {
		// Iface Destination Gateway Flags …, addresses in little-endian hex.
		f := strings.Fields(line)
		if len(f) < 4 || f[1] != "00000000" {
			continue
		}
		flags, err := strconv.ParseUint(f[3], 16, 32)
		b, herr := hex.DecodeString(f[2])
		if err != nil || herr != nil || len(b) != 4 || flags&0x2 == 0 { // RTF_GATEWAY
			continue
		}
		add(netip.AddrFrom4([4]byte{b[3], b[2], b[1], b[0]}))
	}
	for _, line := range strings.Split(route6, "\n") {
		// dest dest_len src src_len next_hop metric refcnt use flags iface, addresses in hex.
		f := strings.Fields(line)
		if len(f) < 10 || f[1] != "00" || strings.Trim(f[0], "0") != "" {
			continue
		}
		flags, err := strconv.ParseUint(f[8], 16, 32)
		b, herr := hex.DecodeString(f[4])
		if err != nil || herr != nil || len(b) != 16 || flags&0x2 == 0 {
			continue
		}
		add(netip.AddrFrom16([16]byte(b)))
	}
	for _, p := range prefixes {
		p = p.Masked()
		if p.Addr().IsLoopback() || p.Addr().IsLinkLocalUnicast() || p.Bits() >= p.Addr().BitLen()-1 {
			continue
		}
		add(p.Addr().Next())
	}
	return out
}

// adminGateway is containerGateway, or the test's list.
func (h *Hub) adminGateway(ip netip.Addr) bool {
	if h.admin.gateways != nil {
		return slices.Contains(h.admin.gateways(), ip)
	}
	return containerGateway(ip)
}
