// Package proxyproto reads the PROXY protocol header (versions 1 and 2) that a TCP proxy or port forward sends ahead of
// a connection's own bytes to say whom it relays (https://www.haproxy.org/download/2.9/doc/proxy-protocol.txt), and
// wraps a listener so that connections from trusted upstreams report that client as their RemoteAddr. Only what a TCP
// server needs is understood: TCP over IPv4 / IPv6, v1 UNKNOWN, v2 LOCAL and UNSPEC; TLVs are skipped.
package proxyproto

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"strconv"
	"strings"
)

// Header is one parsed PROXY protocol header.
type Header struct {
	Version int // 1 or 2
	// Local: the upstream opened this connection for itself (v2 LOCAL, e.g. a health check) rather than relaying a
	// client; the connection's own addresses are the right ones (the spec says so).
	Local bool
	// Source is the client and Destination the address it connected to, as the upstream saw them. Both are invalid
	// when the header carries none: Local, v1 UNKNOWN, v2 UNSPEC.
	Source, Destination netip.AddrPort
}

var (
	// ErrNoHeader: the connection does not start with a PROXY protocol header.
	ErrNoHeader = errors.New("proxyproto: no PROXY protocol header")
	// ErrMalformed: it starts like one but breaks the spec's syntax.
	ErrMalformed = errors.New("proxyproto: malformed PROXY protocol header")
	// ErrTooLong: longer than V1MaxLen or V2MaxPayload.
	ErrTooLong = errors.New("proxyproto: PROXY protocol header too long")
	// ErrUnsupported: a well-formed v2 header for something other than TCP (UDP, Unix sockets).
	ErrUnsupported = errors.New("proxyproto: unsupported PROXY protocol address family or transport")
)

const (
	// V1MaxLen is the spec's longest v1 line, CRLF included.
	V1MaxLen = 107
	// V2MaxPayload bounds what follows v2's 16 fixed bytes (addresses and TLVs). The spec allows 64 KiB; real
	// proxies send the addresses and a few short TLVs, so a header this long is not one worth waiting for.
	V2MaxPayload = 4096
)

var v2Signature = []byte("\r\n\r\n\x00\r\nQUIT\n")

// Read reads one header from r, consuming exactly its bytes: what follows stays in r. The caller bounds the time it
// may take (a deadline on the connection underneath).
func Read(r *bufio.Reader) (Header, error) {
	first, err := r.Peek(1)
	if err != nil {
		return Header{}, readErr(err)
	}
	switch first[0] {
	case 'P':
		return readV1(r)
	case v2Signature[0]:
		return readV2(r)
	}
	return Header{}, ErrNoHeader
}

// readErr says a header was cut short (EOF), keeping the cause (a deadline, a reset) for errors.Is.
func readErr(err error) error {
	if errors.Is(err, io.EOF) {
		err = io.ErrUnexpectedEOF
	}
	return fmt.Errorf("proxyproto: reading the header: %w", err)
}

func readV1(r *bufio.Reader) (Header, error) {
	line := make([]byte, 0, V1MaxLen)
	for {
		b, err := r.ReadByte()
		if err != nil {
			return Header{}, readErr(err)
		}
		line = append(line, b)
		if len(line) == 6 && string(line) != "PROXY " {
			return Header{}, ErrNoHeader
		}
		if b == '\n' {
			break
		}
		if len(line) == V1MaxLen {
			return Header{}, ErrTooLong
		}
	}
	if len(line) < 8 || line[len(line)-2] != '\r' {
		return Header{}, ErrMalformed
	}
	fields := strings.Split(string(line[6:len(line)-2]), " ")
	h := Header{Version: 1}
	var v4 bool
	switch fields[0] {
	case "UNKNOWN":
		return h, nil // the spec: whatever follows UNKNOWN on the line is ignored
	case "TCP4":
		v4 = true
	case "TCP6":
	default:
		return Header{}, ErrMalformed
	}
	if len(fields) != 5 {
		return Header{}, ErrMalformed
	}
	src, err1 := v1Addr(fields[1], v4)
	dst, err2 := v1Addr(fields[2], v4)
	sport, err3 := v1Port(fields[3])
	dport, err4 := v1Port(fields[4])
	if err := errors.Join(err1, err2, err3, err4); err != nil {
		return Header{}, ErrMalformed
	}
	h.Source, h.Destination = netip.AddrPortFrom(src, sport), netip.AddrPortFrom(dst, dport)
	return h, nil
}

func v1Addr(s string, v4 bool) (netip.Addr, error) {
	a, err := netip.ParseAddr(s)
	if err != nil || a.Zone() != "" || a.Is4() != v4 {
		return netip.Addr{}, ErrMalformed
	}
	return a, nil
}

func v1Port(s string) (uint16, error) {
	if s == "" || len(s) > 5 || strings.Trim(s, "0123456789") != "" {
		return 0, ErrMalformed
	}
	n, err := strconv.ParseUint(s, 10, 16)
	return uint16(n), err
}

func readV2(r *bufio.Reader) (Header, error) {
	var fixed [16]byte
	if got, err := io.ReadFull(r, fixed[:]); err != nil {
		if got < len(v2Signature) && !bytes.HasPrefix(v2Signature, fixed[:got]) {
			return Header{}, ErrNoHeader // a stray CR, not a signature cut short
		}
		return Header{}, readErr(err)
	}
	if !bytes.Equal(fixed[:12], v2Signature) {
		return Header{}, ErrNoHeader
	}
	verCmd, fam := fixed[12], fixed[13]
	n := int(binary.BigEndian.Uint16(fixed[14:]))
	if verCmd>>4 != 2 {
		return Header{}, ErrMalformed
	}
	if n > V2MaxPayload {
		return Header{}, ErrTooLong
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return Header{}, readErr(err)
	}
	h := Header{Version: 2}
	switch verCmd & 0x0f {
	case 0x0: // LOCAL: whatever the family, the receiver ignores the addresses
		h.Local = true
		return h, nil
	case 0x1: // PROXY
	default:
		return Header{}, ErrMalformed
	}
	family, transport := fam>>4, fam&0x0f
	switch {
	case family == 0x0: // UNSPEC: the upstream does not know whom it relays
		return h, nil
	case transport != 0x1: // not STREAM
		return Header{}, ErrUnsupported
	case family == 0x1: // INET
		if n < 12 {
			return Header{}, ErrMalformed
		}
		h.Source = netip.AddrPortFrom(netip.AddrFrom4([4]byte(payload[0:4])), binary.BigEndian.Uint16(payload[8:10]))
		h.Destination = netip.AddrPortFrom(netip.AddrFrom4([4]byte(payload[4:8])), binary.BigEndian.Uint16(payload[10:12]))
	case family == 0x2: // INET6
		if n < 36 {
			return Header{}, ErrMalformed
		}
		h.Source = netip.AddrPortFrom(netip.AddrFrom16([16]byte(payload[0:16])), binary.BigEndian.Uint16(payload[32:34]))
		h.Destination = netip.AddrPortFrom(netip.AddrFrom16([16]byte(payload[16:32])), binary.BigEndian.Uint16(payload[34:36]))
	default: // UNIX, or a family the spec does not define
		return Header{}, ErrUnsupported
	}
	return h, nil
}

// AppendV2 appends a v2 PROXY header for a TCP connection from src to dst: INET when both are IPv4 (or IPv4-mapped),
// INET6 otherwise. An invalid src or dst writes LOCAL (no addresses). Zones are not carried.
func AppendV2(b []byte, src, dst netip.AddrPort) []byte {
	b = append(b, v2Signature...)
	if !src.IsValid() || !dst.IsValid() {
		return append(b, 0x20, 0x00, 0, 0) // LOCAL, UNSPEC, no payload
	}
	s, d := src.Addr().Unmap(), dst.Addr().Unmap()
	var payload []byte
	if s.Is4() && d.Is4() {
		b = append(b, 0x21, 0x11)
		s4, d4 := s.As4(), d.As4()
		payload = append(append(payload, s4[:]...), d4[:]...)
	} else {
		b = append(b, 0x21, 0x21)
		s16, d16 := s.As16(), d.As16()
		payload = append(append(payload, s16[:]...), d16[:]...)
	}
	payload = binary.BigEndian.AppendUint16(payload, src.Port())
	payload = binary.BigEndian.AppendUint16(payload, dst.Port())
	b = binary.BigEndian.AppendUint16(b, uint16(len(payload)))
	return append(b, payload...)
}
