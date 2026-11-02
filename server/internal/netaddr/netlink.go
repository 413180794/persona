package netaddr

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
)

// Linux wire constants live here so fixtures can be replayed on other systems.
const (
	nlError           = 2
	nlDone            = 3
	nlOverrun         = 4
	nlDumpInterrupted = 0x10
	rtmNewAddr        = 20
	rtmDelAddr        = 21
	afInet            = 2
	afInet6           = 10
)

var errDumpInterrupted = errors.New("netaddr: interrupted netlink dump")

type nlMessage struct {
	kind  uint16
	flags uint16
	seq   uint32
	data  []byte
}

func align4(n int) int { return (n + 3) &^ 3 }

func parseMessages(data []byte, order binary.ByteOrder) ([]nlMessage, error) {
	var out []nlMessage
	for len(data) > 0 {
		if len(data) < 16 {
			return nil, errors.New("netaddr: truncated netlink header")
		}
		size := uint64(order.Uint32(data[:4]))
		if size < 16 || size > uint64(len(data)) {
			return nil, errors.New("netaddr: invalid netlink length")
		}
		n := int(size)
		out = append(out, nlMessage{order.Uint16(data[4:6]), order.Uint16(data[6:8]), order.Uint32(data[8:12]), data[16:n]})
		if n == len(data) {
			break
		}
		if align4(n) > len(data) {
			return nil, errors.New("netaddr: truncated netlink padding")
		}
		data = data[align4(n):]
	}
	return out, nil
}

// netlinkStatus accepts old empty DONE records as well as modern signed status.
func netlinkStatus(m nlMessage, order binary.ByteOrder) error {
	if m.flags&nlDumpInterrupted != 0 || m.kind == nlOverrun {
		return errDumpInterrupted
	}
	if m.kind != nlDone && m.kind != nlError {
		return nil
	}
	if m.kind == nlDone && len(m.data) == 0 {
		return nil
	}
	if len(m.data) < 4 {
		return errors.New("netaddr: truncated netlink status")
	}
	if code := int32(order.Uint32(m.data[:4])); code != 0 {
		return fmt.Errorf("netaddr: kernel netlink status %d", code)
	}
	return nil
}

func parseAddr(data []byte, order binary.ByteOrder) (Addr, error) {
	if len(data) < 8 {
		return Addr{}, errors.New("netaddr: truncated ifaddrmsg")
	}
	family := data[0]
	if family != afInet && family != afInet6 {
		return Addr{}, nil
	}
	a := Addr{PrefixLen: int(data[1]), Flags: Flags(data[2]), FlagsKnown: true, Scope: data[3], ScopeKnown: true, InterfaceIndex: int(order.Uint32(data[4:8]))}
	size := 16
	if family == afInet {
		size = 4
	}
	if a.PrefixLen > size*8 {
		return Addr{}, errors.New("netaddr: invalid address prefix")
	}
	var address, local []byte
	for attrs := data[8:]; len(attrs) > 0; {
		if len(attrs) < 4 {
			return Addr{}, errors.New("netaddr: truncated address attribute")
		}
		n := int(order.Uint16(attrs[:2]))
		if n < 4 || n > len(attrs) {
			return Addr{}, errors.New("netaddr: invalid address attribute length")
		}
		value := attrs[4:n]
		switch order.Uint16(attrs[2:4]) & 0x3fff {
		case 1, 2: // IFA_ADDRESS / IFA_LOCAL: local wins for point-to-point links.
			if len(value) != size {
				return Addr{}, errors.New("netaddr: invalid address size")
			}
			if order.Uint16(attrs[2:4])&0x3fff == 2 {
				local = value
			} else {
				address = value
			}
		case 8: // IFA_FLAGS replaces, rather than ORs with, the header flags.
			if len(value) != 4 {
				return Addr{}, errors.New("netaddr: invalid IFA_FLAGS")
			}
			a.Flags = Flags(order.Uint32(value))
		case 6: // IFA_CACHEINFO: preferred, valid, created, updated.
			if len(value) != 16 {
				return Addr{}, errors.New("netaddr: invalid IFA_CACHEINFO")
			}
			a.PreferredLifetime = order.Uint32(value[:4])
			a.ValidLifetime = order.Uint32(value[4:8])
			a.LifetimesKnown = true
		}
		if n == len(attrs) {
			break
		}
		if align4(n) > len(attrs) {
			return Addr{}, errors.New("netaddr: truncated attribute padding")
		}
		attrs = attrs[align4(n):]
	}
	if local != nil {
		address = local
	}
	if address == nil {
		return Addr{}, errors.New("netaddr: missing local address")
	}
	a.IP, _ = netip.AddrFromSlice(address)
	a.Class = Classify(a.IP)
	return a, nil
}

// Malformed/overrun/error messages force a refresh rather than hiding lost state.
func addressChange(data []byte, order binary.ByteOrder) bool {
	messages, err := parseMessages(data, order)
	if err != nil {
		return true
	}
	for _, m := range messages {
		switch m.kind {
		case rtmNewAddr, rtmDelAddr, 16, 17, nlOverrun: // RTM_NEWLINK / RTM_DELLINK.
			return true
		case nlError:
			if netlinkStatus(m, order) != nil {
				return true
			}
		}
	}
	return false
}
