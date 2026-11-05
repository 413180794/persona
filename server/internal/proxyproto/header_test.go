package proxyproto

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net/netip"
	"strings"
	"testing"
)

// v2 builds a version 2 header: command (0 LOCAL, 1 PROXY), family/transport byte, then payload.
func v2(cmd, fam byte, payload []byte) []byte {
	b := append([]byte{}, v2Signature...)
	b = append(b, 0x20|cmd, fam)
	b = binary.BigEndian.AppendUint16(b, uint16(len(payload)))
	return append(b, payload...)
}

func v2inet(src, dst netip.AddrPort) []byte {
	var p []byte
	if src.Addr().Is4() {
		p = append(p, src.Addr().AsSlice()...)
		p = append(p, dst.Addr().AsSlice()...)
	} else {
		a, b := src.Addr().As16(), dst.Addr().As16()
		p = append(append(p, a[:]...), b[:]...)
	}
	p = binary.BigEndian.AppendUint16(p, src.Port())
	return binary.BigEndian.AppendUint16(p, dst.Port())
}

func TestReadHeader(t *testing.T) {
	src4, dst4 := netip.MustParseAddrPort("203.0.113.7:51234"), netip.MustParseAddrPort("192.0.2.10:443")
	src6, dst6 := netip.MustParseAddrPort("[2001:db8::7]:51234"), netip.MustParseAddrPort("[2001:db8::10]:443")
	withTLV := append(v2inet(src4, dst4), 0x04, 0x00, 0x03, 'a', 'b', 'c') // a NOOP TLV, skipped
	for _, tc := range []struct {
		name     string
		in       []byte
		want     Header
		wantRest string
	}{
		{"v1 TCP4", []byte("PROXY TCP4 203.0.113.7 192.0.2.10 51234 443\r\nGET /"), Header{Version: 1, Source: src4, Destination: dst4}, "GET /"},
		{"v1 TCP6", []byte("PROXY TCP6 2001:db8::7 2001:db8::10 51234 443\r\n\x16\x03"), Header{Version: 1, Source: src6, Destination: dst6}, "\x16\x03"},
		{"v1 UNKNOWN", []byte("PROXY UNKNOWN\r\nrest"), Header{Version: 1}, "rest"},
		{"v1 UNKNOWN with addresses", []byte("PROXY UNKNOWN ffff::1 ffff::2 1 2\r\n"), Header{Version: 1}, ""},
		{"v1 longest", []byte("PROXY TCP6 ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff 65535 65535\r\nx"),
			Header{Version: 1, Source: netip.MustParseAddrPort("[ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff]:65535"), Destination: netip.MustParseAddrPort("[ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff]:65535")}, "x"},
		{"v2 TCP4", append(v2(1, 0x11, v2inet(src4, dst4)), "after"...), Header{Version: 2, Source: src4, Destination: dst4}, "after"},
		{"v2 TCP6", append(v2(1, 0x21, v2inet(src6, dst6)), "after"...), Header{Version: 2, Source: src6, Destination: dst6}, "after"},
		{"v2 TLVs skipped", append(v2(1, 0x11, withTLV), "after"...), Header{Version: 2, Source: src4, Destination: dst4}, "after"},
		{"v2 LOCAL", append(v2(0, 0x00, nil), "after"...), Header{Version: 2, Local: true}, "after"},
		{"v2 LOCAL ignores its addresses", v2(0, 0x11, v2inet(src4, dst4)), Header{Version: 2, Local: true}, ""},
		{"v2 PROXY UNSPEC", v2(1, 0x00, nil), Header{Version: 2}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := bufio.NewReader(bytes.NewReader(tc.in))
			got, err := Read(r)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("header = %+v, want %+v", got, tc.want)
			}
			rest, _ := io.ReadAll(r)
			if string(rest) != tc.wantRest {
				t.Fatalf("the bytes after the header = %q, want %q", rest, tc.wantRest)
			}
		})
	}
}

func TestReadHeaderRejects(t *testing.T) {
	src4, dst4 := netip.MustParseAddrPort("203.0.113.7:1"), netip.MustParseAddrPort("192.0.2.10:2")
	badSig := v2(1, 0x11, v2inet(src4, dst4))
	badSig[11] = 'X'
	for _, tc := range []struct {
		name string
		in   []byte
		want error
	}{
		{"TLS", []byte("\x16\x03\x01\x02\x00"), ErrNoHeader},
		{"HTTP", []byte("GET / HTTP/1.1\r\n"), ErrNoHeader},
		{"HTTP starting with P", []byte("POST / HTTP/1.1\r\n"), ErrNoHeader},
		{"stray CR", []byte("\r\nGET / HTTP/1.1\r\n\r\n"), ErrNoHeader},
		{"empty", nil, io.ErrUnexpectedEOF},
		{"v1 truncated", []byte("PROXY TCP4 203.0.113.7 192.0.2.10 1"), io.ErrUnexpectedEOF},
		{"v1 no CR", []byte("PROXY TCP4 203.0.113.7 192.0.2.10 1 2\n"), ErrMalformed},
		{"v1 too long", []byte("PROXY TCP6 " + strings.Repeat("1", 120) + "\r\n"), ErrTooLong},
		{"v1 lower case", []byte("PROXY tcp4 203.0.113.7 192.0.2.10 1 2\r\n"), ErrMalformed},
		{"v1 TCP4 with IPv6", []byte("PROXY TCP4 2001:db8::7 192.0.2.10 1 2\r\n"), ErrMalformed},
		{"v1 TCP6 with IPv4", []byte("PROXY TCP6 203.0.113.7 192.0.2.10 1 2\r\n"), ErrMalformed},
		{"v1 zone", []byte("PROXY TCP6 fe80::1%eth0 2001:db8::10 1 2\r\n"), ErrMalformed},
		{"v1 port out of range", []byte("PROXY TCP4 203.0.113.7 192.0.2.10 65536 2\r\n"), ErrMalformed},
		{"v1 signed port", []byte("PROXY TCP4 203.0.113.7 192.0.2.10 +1 2\r\n"), ErrMalformed},
		{"v1 double space", []byte("PROXY TCP4  203.0.113.7 192.0.2.10 1 2\r\n"), ErrMalformed},
		{"v1 missing port", []byte("PROXY TCP4 203.0.113.7 192.0.2.10 1\r\n"), ErrMalformed},
		{"v1 unknown protocol", []byte("PROXY UDP4 203.0.113.7 192.0.2.10 1 2\r\n"), ErrMalformed},
		{"v2 bad signature", badSig, ErrNoHeader},
		{"v2 truncated signature", v2Signature[:8], io.ErrUnexpectedEOF},
		{"v2 truncated payload", v2(1, 0x11, v2inet(src4, dst4))[:20], io.ErrUnexpectedEOF},
		{"v2 version 1", append(append([]byte{}, v2Signature...), 0x11, 0x11, 0, 0), ErrMalformed},
		{"v2 unknown command", v2(2, 0x11, v2inet(src4, dst4)), ErrMalformed},
		{"v2 short IPv4 block", v2(1, 0x11, make([]byte, 11)), ErrMalformed},
		{"v2 short IPv6 block", v2(1, 0x21, make([]byte, 35)), ErrMalformed},
		{"v2 UDP", v2(1, 0x12, v2inet(src4, dst4)), ErrUnsupported},
		{"v2 Unix socket", v2(1, 0x31, make([]byte, 216)), ErrUnsupported},
		{"v2 too long", v2(1, 0x11, make([]byte, V2MaxPayload+1)), ErrTooLong},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Read(bufio.NewReader(bytes.NewReader(tc.in)))
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// The longest v1 line the spec allows fits; one byte more does not, without reading on to a newline that may never come.
func TestReadHeaderV1Limit(t *testing.T) {
	line := "PROXY UNKNOWN ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff 65535 65535\r\n"
	if len(line) != V1MaxLen {
		t.Fatalf("longest line is %d bytes", len(line))
	}
	if h, err := Read(bufio.NewReader(strings.NewReader(line))); err != nil || h != (Header{Version: 1}) {
		t.Fatalf("longest line: %+v, %v", h, err)
	}
	endless := &countingReader{r: io.MultiReader(strings.NewReader("PROXY UNKNOWN "), neverNewline{})}
	if _, err := Read(bufio.NewReaderSize(endless, 16)); !errors.Is(err, ErrTooLong) {
		t.Fatalf("err = %v", err)
	}
	if endless.n > V1MaxLen+16 {
		t.Fatalf("read %d bytes looking for the end of a v1 line", endless.n)
	}
}

type neverNewline struct{}

func (neverNewline) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

func TestAppendV2RoundTrips(t *testing.T) {
	for _, c := range []struct{ src, dst string }{
		{"192.0.2.7:51000", "198.51.100.1:18660"},
		{"[::ffff:192.0.2.7]:51000", "[::ffff:127.0.0.1]:18660"}, // a dual-stack listener's IPv4: INET
		{"[2001:db8::7]:51000", "[2001:db8::1]:18661"},
		{"192.0.2.7:51000", "[2001:db8::1]:18661"}, // mixed: both as INET6
	} {
		src, dst := netip.MustParseAddrPort(c.src), netip.MustParseAddrPort(c.dst)
		b := append(AppendV2(nil, src, dst), "GET / HTTP/1.1\r\n"...)
		r := bufio.NewReader(bytes.NewReader(b))
		h, err := Read(r)
		if err != nil {
			t.Fatalf("%v -> %v: %v", src, dst, err)
		}
		if h.Source.Addr().Unmap() != src.Addr().Unmap() || h.Source.Port() != src.Port() ||
			h.Destination.Addr().Unmap() != dst.Addr().Unmap() || h.Destination.Port() != dst.Port() {
			t.Fatalf("%v -> %v: read %+v", src, dst, h)
		}
		if rest, _ := io.ReadAll(r); string(rest) != "GET / HTTP/1.1\r\n" {
			t.Fatalf("what follows the header: %q", rest)
		}
	}
	h, err := Read(bufio.NewReader(bytes.NewReader(AppendV2(nil, netip.AddrPort{}, netip.AddrPort{}))))
	if err != nil || !h.Local {
		t.Fatalf("no addresses: %+v %v, want LOCAL", h, err)
	}
}
