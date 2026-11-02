//go:build !linux

package netaddr

import (
	"fmt"
	"net"
	"net/netip"
	"slices"
)

// List returns all local unicast interface addresses, including unsuitable
// publication candidates. On non-Linux systems flags, scope and lifetimes are
// unknown, and virtual interfaces are identified by conventional names only.
func List() ([]Addr, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("netaddr: interfaces: %w", err)
	}
	var out []Addr
	for _, iface := range interfaces {
		addresses, err := iface.Addrs()
		if err != nil {
			return nil, fmt.Errorf("netaddr: interface addresses: %w", err)
		}
		for _, address := range addresses {
			p, err := netip.ParsePrefix(address.String())
			if err != nil {
				return nil, fmt.Errorf("netaddr: interface prefix: %w", err)
			}
			out = append(out, Addr{IP: p.Addr(), PrefixLen: p.Bits(), Interface: iface.Name, InterfaceIndex: iface.Index, InterfaceFlags: iface.Flags, Virtual: virtualName(iface.Name), Class: Classify(p.Addr())})
		}
	}
	slices.SortFunc(out, compareIdentity)
	return out, nil
}
