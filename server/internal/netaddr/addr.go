package netaddr

import (
	"cmp"
	"net"
	"net/netip"
	"slices"
	"strings"
)

// Flags contains Linux IFA_F_* bits, including bits unknown to this package.
// FlagTemporary means secondary on IPv4; it means temporary/privacy on IPv6.
type Flags uint32

// Address flags from Linux include/uapi/linux/if_addr.h.
const (
	FlagTemporary Flags = 1 << iota
	FlagNoDAD
	FlagOptimistic
	FlagDADFailed
	FlagHomeAddress
	FlagDeprecated
	FlagTentative
	FlagPermanent
	FlagManageTempAddr
	FlagNoPrefixRoute
	FlagMulticastAutoJoin
	FlagStablePrivacy
)

// InfiniteLifetime is the kernel's unlimited lifetime, in seconds.
const InfiniteLifetime uint32 = ^uint32(0)

// Addr is a snapshot of a local interface address, not a reachability result.
// IP has no zone; Interface identifies the interface for scoped addresses.
type Addr struct {
	IP             netip.Addr
	PrefixLen      int
	Interface      string
	InterfaceIndex int
	InterfaceFlags net.Flags
	Virtual        bool
	Flags          Flags
	// FlagsKnown is false outside Linux. Zero flags alone do not mean stable.
	FlagsKnown bool
	// Scope is the Linux routing scope (zero is universe); unknown outside Linux.
	Scope      uint8
	ScopeKnown bool
	// Lifetimes are remaining seconds at List time, not absolute timestamps.
	// Zero means expired only when LifetimesKnown is true; all-ones means forever.
	PreferredLifetime uint32
	ValidLifetime     uint32
	LifetimesKnown    bool
	Class             Class
}

// Temporary reports an IPv6 privacy address, never an IPv4 secondary address.
func (a Addr) Temporary() bool { return a.IP.Is6() && a.Flags&FlagTemporary != 0 }

// Dynamic reports a known address without IFA_F_PERMANENT. Unknown is not dynamic.
func (a Addr) Dynamic() bool { return a.FlagsKnown && a.Flags&FlagPermanent == 0 }

// Class describes an address range, without asserting external reachability.
type Class string

const (
	Unknown        Class = "unknown"
	GlobalIPv6     Class = "global"
	ULA            Class = "ula"
	LinkLocal      Class = "link-local"
	Loopback       Class = "loopback"
	Documentation  Class = "documentation"
	PrivateIPv4    Class = "private-ipv4"
	SharedIPv4     Class = "shared-ipv4"
	PublicIPv4     Class = "public-ipv4"
	Multicast      Class = "multicast"
	Unspecified    Class = "unspecified"
	TransitionIPv6 Class = "transition-ipv6"
	Reserved       Class = "reserved"
)

var (
	globalIPv6        = netip.MustParsePrefix("2000::/3")
	documentationIPv6 = []netip.Prefix{netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("3fff::/20")}
	transitionIPv6    = []netip.Prefix{netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("2001::/32")}
)

// Classify separates private, shared, documentation and transition ranges from
// ordinary unicast addresses. IPv4-mapped addresses are classified as IPv4.
// PublicIPv4 describes a local address only; NAT and ingress are not probed.
func Classify(ip netip.Addr) Class {
	if !ip.IsValid() {
		return Unknown
	}
	ip = ip.Unmap().WithZone("")
	switch {
	case ip.IsUnspecified():
		return Unspecified
	case ip.IsLoopback():
		return Loopback
	case ip.IsMulticast():
		return Multicast
	case ip.IsLinkLocalUnicast():
		return LinkLocal
	case ip.Is4():
		b := ip.As4()
		switch {
		case b[0] == 192 && b[1] == 0 && b[2] == 2,
			b[0] == 198 && b[1] == 51 && b[2] == 100,
			b[0] == 203 && b[1] == 0 && b[2] == 113:
			return Documentation
		case ip.IsPrivate():
			return PrivateIPv4
		case b[0] == 100 && b[1]&0xc0 == 64:
			return SharedIPv4
		case b[0] == 0, b[0] >= 240,
			b[0] == 198 && b[1]&0xfe == 18,
			b[0] == 192 && b[1] == 0 && b[2] == 0,
			b[0] == 192 && b[1] == 88 && b[2] == 99:
			return Reserved
		default:
			return PublicIPv4
		}
	case ip.IsPrivate():
		return ULA
	}
	for _, p := range documentationIPv6 {
		if p.Contains(ip) {
			return Documentation
		}
	}
	for _, p := range transitionIPv6 {
		if p.Contains(ip) {
			return TransitionIPv6
		}
	}
	if globalIPv6.Contains(ip) {
		return GlobalIPv6
	}
	return Reserved
}

// Stable reports whether a snapshot is a publishable stable global IPv6
// candidate. It requires known address flags, an UP non-loopback non-virtual
// interface and universe scope. Temporary, deprecated, tentative, failed DAD
// and expired addresses are rejected. IPv4 must be handled separately by the
// caller, since a local address does not establish public ingress reachability.
// Non-Linux snapshots are conservatively rejected because their flags are unknown.
func Stable(a Addr) bool {
	return Classify(a.IP) == GlobalIPv6 && a.FlagsKnown && a.ScopeKnown && a.Scope == 0 &&
		a.InterfaceFlags&net.FlagUp != 0 && a.InterfaceFlags&net.FlagLoopback == 0 && !a.Virtual &&
		a.Flags&(FlagTemporary|FlagDeprecated|FlagTentative|FlagDADFailed) == 0 &&
		(!a.LifetimesKnown || (a.PreferredLifetime > 0 && a.ValidLifetime > 0))
}

// Candidates returns a new slice of Stable addresses, best first: permanent,
// mngtmpaddr/EUI-64, then stable-privacy. Within a tier known and longer preferred
// lifetimes win, then valid lifetimes, then interface/IP for deterministic ties.
// Netlink cannot distinguish a fixed SLAAC token from ordinary SLAAC.
func Candidates(addrs []Addr) []Addr {
	out := make([]Addr, 0, len(addrs))
	for _, a := range addrs {
		if Stable(a) {
			out = append(out, a)
		}
	}
	slices.SortFunc(out, func(a, b Addr) int {
		if n := cmp.Compare(stabilityRank(a), stabilityRank(b)); n != 0 {
			return n
		}
		if a.LifetimesKnown != b.LifetimesKnown {
			if a.LifetimesKnown {
				return -1
			}
			return 1
		}
		if a.LifetimesKnown {
			if n := cmp.Compare(b.PreferredLifetime, a.PreferredLifetime); n != 0 {
				return n
			}
			if n := cmp.Compare(b.ValidLifetime, a.ValidLifetime); n != 0 {
				return n
			}
		}
		return compareIdentity(a, b)
	})
	return out
}

// Select keeps the current interface/IP while it remains Stable in the fresh
// snapshot, otherwise chooses the best candidate. It never returns stale metadata.
// Pass Addr{} when nothing has been published yet. The bool is false if none qualify.
func Select(addrs []Addr, current Addr) (Addr, bool) {
	candidates := Candidates(addrs)
	for _, a := range candidates {
		if a.IP == current.IP && a.Interface == current.Interface {
			return a, true
		}
	}
	if len(candidates) == 0 {
		return Addr{}, false
	}
	return candidates[0], true
}

func stabilityRank(a Addr) int {
	if a.Flags&FlagPermanent != 0 {
		return 0
	}
	if a.Flags&FlagManageTempAddr != 0 {
		return 1
	}
	if a.Flags&FlagStablePrivacy != 0 {
		return 2
	}
	return 1
}

func compareIdentity(a, b Addr) int {
	if n := cmp.Compare(a.Interface, b.Interface); n != 0 {
		return n
	}
	if n := a.IP.Compare(b.IP); n != 0 {
		return n
	}
	if n := cmp.Compare(a.PrefixLen, b.PrefixLen); n != 0 {
		return n
	}
	return cmp.Compare(a.InterfaceIndex, b.InterfaceIndex)
}

func virtualName(name string) bool {
	name = strings.ToLower(name)
	// Keep the vendor prefix in display case: Go packs string constants together, so unrelated neighboring
	// protocol text must not manufacture a configured machine name in the raw binary. Matching stays lowercase.
	for _, prefix := range []string{"docker", "veth", "br-", "virbr", "tun", "tap", "utun", "tailscale", "wg", "bridge", "vxlan", "dummy", "VBoxNet", "vmnet", "awdl", "llw", "ipsec"} {
		if strings.HasPrefix(name, strings.ToLower(prefix)) {
			return true
		}
	}
	return false
}
