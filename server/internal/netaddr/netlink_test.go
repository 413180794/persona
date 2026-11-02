package netaddr

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net/netip"
	"strings"
	"testing"
)

// Frozen wire fixtures use documentation addresses and anonymous interface
// indices. They are intentionally literal bytes, independent of our decoder.
const ipv6Wire = `
 48000000 1400 0200 01000000 00000000
 0a40 0100 02000000
 1400 0100 20010db8000000000000000000000001
 0800 0800 00090000
 1400 0600 100e0000 201c0000 00000000 00000000`
const ipv4Wire = `
 3c000000 1400 0200 01000000 00000000
 0218 8000 03000000
 0800 0100 c0000202
 0800 0200 c0000201
 1400 0600 ffffffff ffffffff 00000000 00000000`
const doneWire = `14000000 0300 0200 01000000 00000000 00000000`

func wire(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.Join(strings.Fields(s), ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseRecordedMessages(t *testing.T) {
	tests := []struct {
		name, hex        string
		order            binary.ByteOrder
		ip               string
		prefix, index    int
		flags            Flags
		known            bool
		preferred, valid uint32
	}{
		{"extended flags replace header", ipv6Wire, binary.LittleEndian, "2001:db8::1", 64, 2, FlagManageTempAddr | FlagStablePrivacy, true, 3600, 7200},
		{"point to point local wins", ipv4Wire, binary.LittleEndian, "192.0.2.1", 24, 3, FlagPermanent, true, InfiniteLifetime, InfiniteLifetime},
		{"header flags fallback", `2c000000 1400 0200 01000000 00000000 0a40 2100 02000000 1400 0100 20010db8000000000000000000000001`, binary.LittleEndian, "2001:db8::1", 64, 2, FlagTemporary | FlagDeprecated, false, 0, 0},
		{"big endian", `00000034 0014 0002 00000001 00000000 0a40 0100 00000002 0014 0001 20010db8000000000000000000000001 0008 0008 00000100`, binary.BigEndian, "2001:db8::1", 64, 2, FlagManageTempAddr, false, 0, 0},
		{"unknown padded attribute", `34000000 1400 0200 01000000 00000000 0a40 8000 02000000 0500 6300 ff000000 1400 0100 20010db8000000000000000000000001`, binary.LittleEndian, "2001:db8::1", 64, 2, FlagPermanent, false, 0, 0},
		{"zero extended flags", `34000000 1400 0200 01000000 00000000 0a40 2100 02000000 1400 0100 20010db8000000000000000000000001 0800 0800 00000000`, binary.LittleEndian, "2001:db8::1", 64, 2, 0, false, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgs, err := parseMessages(wire(t, tt.hex), tt.order)
			if err != nil || len(msgs) != 1 {
				t.Fatalf("messages: count=%d error=%v", len(msgs), err)
			}
			if msgs[0].kind != rtmNewAddr || msgs[0].seq != 1 {
				t.Fatal("wrong header")
			}
			a, err := parseAddr(msgs[0].data, tt.order)
			if err != nil {
				t.Fatal(err)
			}
			if a.IP != netip.MustParseAddr(tt.ip) || a.PrefixLen != tt.prefix || a.InterfaceIndex != tt.index || a.Flags != tt.flags || !a.FlagsKnown || !a.ScopeKnown || a.Scope != 0 || a.Class != Documentation || a.LifetimesKnown != tt.known || a.PreferredLifetime != tt.preferred || a.ValidLifetime != tt.valid {
				t.Fatal("decoded address differs from fixture expectations")
			}
		})
	}
	msgs, err := parseMessages(wire(t, ipv6Wire+ipv4Wire+doneWire), binary.LittleEndian)
	if err != nil || len(msgs) != 3 || msgs[2].kind != nlDone {
		t.Fatalf("multipart: count=%d error=%v", len(msgs), err)
	}
}

func TestMalformedNetlink(t *testing.T) {
	for _, s := range []string{"00", `0f000000 1400 0000 00000000 00000000`, `ff000000 1400 0000 00000000 00000000`, `11000000 1400 0000 00000000 00000000 0000`} {
		if _, err := parseMessages(wire(t, s), binary.LittleEndian); err == nil {
			t.Fatal("accepted malformed message")
		}
	}
	tests := []struct{ name, hex string }{
		{"short ifaddrmsg", "0a40"},
		{"prefix too long", "0aff000002000000"},
		{"missing address", "0a40000002000000"},
		{"short attribute header", "0a40000002000000ff"},
		{"zero attribute length", "0a4000000200000000000100"},
		{"oversized attribute", "0a400000020000001400010000"},
		{"wrong address length", "0a4000000200000008000100c0000201"},
		{"short flags", "0a4000000200000004000800"},
		{"short cacheinfo", "0a400000020000000800060000000000"},
		{"missing padding", "0a4000000200000005006300ff00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseAddr(wire(t, tt.hex), binary.LittleEndian); err == nil {
				t.Fatal("accepted malformed address")
			}
		})
	}
	a, err := parseAddr(wire(t, "1100000002000000"), binary.LittleEndian)
	if err != nil || a.IP.IsValid() {
		t.Fatal("unsupported address family should be skipped")
	}
}

func TestNetlinkStatus(t *testing.T) {
	tests := []struct {
		name, hex            string
		wantErr, interrupted bool
	}{
		{"done", doneWire, false, false},
		{"old done", `10000000 0300 0200 01000000 00000000`, false, false},
		{"ack", `14000000 0200 0000 01000000 00000000 00000000`, false, false},
		{"interrupted", `14000000 0300 1200 01000000 00000000 00000000`, true, true},
		{"overrun", `10000000 0400 0000 01000000 00000000`, true, true},
		{"kernel error", `14000000 0200 0000 01000000 00000000 ffffffff`, true, false},
		{"done error", `14000000 0300 0000 01000000 00000000 ffffffff`, true, false},
		{"short status", `11000000 0200 0000 01000000 00000000 00`, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgs, err := parseMessages(wire(t, tt.hex), binary.LittleEndian)
			if err != nil {
				t.Fatal(err)
			}
			err = netlinkStatus(msgs[0], binary.LittleEndian)
			if (err != nil) != tt.wantErr || errors.Is(err, errDumpInterrupted) != tt.interrupted {
				t.Fatalf("unexpected status: %v", err)
			}
		})
	}
}

func FuzzNetlink(f *testing.F) {
	f.Add([]byte{10, 64, 0, 0, 2, 0, 0, 0})
	f.Fuzz(func(t *testing.T, b []byte) {
		parseAddr(b, binary.LittleEndian)
		parseAddr(b, binary.BigEndian)
		parseMessages(b, binary.LittleEndian)
		parseMessages(b, binary.BigEndian)
	})
}
