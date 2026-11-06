package link

import "testing"

// A port must be plain digits: strconv.Atoi took "+18663", which url.Parse and the hub's dialer refuse (#4710).
func TestPortChecksTakeDigitsOnly(t *testing.T) {
	for _, tc := range []struct {
		name, port   string
		listen, dial bool // NormalizeListen(":"+port), ValidPort(port)
	}{
		{name: "plus", port: "+18663"},
		{name: "minus", port: "-1"},
		{name: "zero", port: "0", listen: true},
		{name: "too big", port: "65536"},
		{name: "empty", port: ""},
		{name: "six digits", port: "018663"},
		{name: "space", port: " 443"},
		{name: "low", port: "1", listen: true, dial: true},
		{name: "normal", port: "18663", listen: true, dial: true},
		{name: "high", port: "65535", listen: true, dial: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidPort(tc.port); got != tc.dial {
				t.Errorf("ValidPort(%q) = %v, want %v", tc.port, got, tc.dial)
			}
			if _, err := NormalizeListen(":" + tc.port); (err == nil) != tc.listen {
				t.Errorf("NormalizeListen(%q) err = %v, want ok %v", ":"+tc.port, err, tc.listen)
			}
			if tc.port != "" && tc.port != " 443" {
				if _, err := NormalizeListen(tc.port); (err == nil) != tc.listen {
					t.Errorf("NormalizeListen(%q) err = %v, want ok %v", tc.port, err, tc.listen)
				}
			}
			addr := "192.0.2.10:" + tc.port
			if _, err := normalizeAddr(addr, ":18663"); tc.port != "" && (err == nil) != tc.dial {
				t.Errorf("normalizeAddr(%q) err = %v, want ok %v", addr, err, tc.dial)
			}
			_, _, _, err := ParseConnectorLink(ConnectorLink([]string{addr}, "AAAA", "jc_code"))
			if (err == nil) != tc.dial {
				t.Errorf("ParseConnectorLink(addr=%q) err = %v, want ok %v", addr, err, tc.dial)
			}
		})
	}
}

func TestParseConnectorLinkDropsBadAddrs(t *testing.T) {
	raw := ConnectorLink([]string{"192.0.2.10:+18663", "[::1]:443", ":443", "192.0.2.10:-1", "hub.example.com:18663"}, "AAAA", "jc_code")
	addrs, _, _, err := ParseConnectorLink(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(addrs) != 2 || addrs[0] != "[::1]:443" || addrs[1] != "hub.example.com:18663" {
		t.Errorf("addrs = %v, want [[::1]:443 hub.example.com:18663]", addrs)
	}
	if got, err := NormalizeListen("[::1]:443"); err != nil || got != "[::1]:443" {
		t.Errorf("NormalizeListen([::1]:443) = %q %v", got, err)
	}
	if got, err := normalizeAddr("[::1]:443", ":18663"); err != nil || got != "[::1]:443" {
		t.Errorf("normalizeAddr([::1]:443) = %q %v", got, err)
	}
	if _, err := normalizeAddr("192.0.2.10", ":+18663"); err == nil {
		t.Error("normalizeAddr took the port of listen :+18663")
	}
}
