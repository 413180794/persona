package devices

import (
	"context"
	"testing"

	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/link"
)

func TestValidHostPortTakesDigitsOnly(t *testing.T) {
	for a, want := range map[string]bool{
		"192.0.2.10:+18663": false, "192.0.2.10:-1": false, "192.0.2.10:0": false, "192.0.2.10:65536": false,
		"192.0.2.10:": false, ":443": false, "192.0.2.10": false, "[::1]:443": true, "192.0.2.10:18663": true,
		"hub.example.com:65535": true, "hub.example.com:1/x": false,
	} {
		if got := validHostPort(a); got != want {
			t.Errorf("validHostPort(%q) = %v, want %v", a, got, want)
		}
	}
}

// A pairing link (or a typed address) with a signed port is an error, not a nil request the manager dereferences (#4710).
func TestPairConnectorRejectsSignedPort(t *testing.T) {
	d := newTestDevices(t)
	for _, tc := range []struct{ name, linkAddr, address string }{
		{name: "link plus", linkAddr: "192.0.2.10:+18663"},
		{name: "link minus", linkAddr: "192.0.2.10:-1"},
		{name: "link zero", linkAddr: "192.0.2.10:0"},
		{name: "link too big", linkAddr: "192.0.2.10:65536"},
		{name: "link empty port", linkAddr: "192.0.2.10:"},
		{name: "typed plus", linkAddr: "[::1]:443", address: "192.0.2.10:+443"},
		{name: "typed empty port", linkAddr: "[::1]:443", address: "192.0.2.10:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := link.ConnectorLink([]string{tc.linkAddr}, "AAAA", "jc_code")
			_, _, err := d.PairConnector(context.Background(), devicewire.PairConnector{Link: raw, Address: tc.address, MachineID: "example"})
			if err == nil {
				t.Fatal("PairConnector took it")
			}
		})
	}
}
