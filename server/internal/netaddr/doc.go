// Package netaddr discovers local addresses and selects stable public IPv6
// candidates. It does not probe NAT, firewall rules, TUN return routes or ingress
// reachability. Classify and List also expose IPv4 and LAN addresses for callers
// that apply their own LAN/public-IPv4 publication policies.
//
// Call Watch before the first List, then refresh on each signal. Watch uses a
// configured one-second coalescing window and a one-minute recovery poll; these
// are scheduling settings, not measured latency. Cancel its context to release
// subscription resources.
//
// Call List, then Candidates for a sorted copy, or Select with the previously
// published Addr for sticky selection. Always use fresh snapshots: lifetimes
// count down in the kernel. Linux reads full IFA_FLAGS and IFA_CACHEINFO through
// RTM_GETADDR, and resolves /sys/class/net links to identify virtual interfaces.
// Other systems use net.Interfaces and return FlagsKnown=false; Stable fails
// closed there rather than treating unknown privacy/DAD state as safe.
//
// Rules follow docs/archive/redesign/hub-changes.md section 1.2 and
// docs/archive/redesign/research/public-direct-ipv6.md section 2, plus CLAUDE.md's sysfs
// interface rule. Preferred lifetime precedes valid lifetime as required by the
// netaddr lane task. Fixed SLAAC tokens cannot be identified from RTM_GETADDR;
// user interface allowlists and publication decisions belong to the caller.
// Linux wire definitions: https://github.com/torvalds/linux/blob/master/include/uapi/linux/if_addr.h.
package netaddr
