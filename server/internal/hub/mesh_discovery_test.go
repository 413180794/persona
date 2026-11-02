package hub

import "testing"

// A hub whose TLS listens on loopback only is not advertised on the LAN: nothing there could reach it.
func TestLoopbackOnlyListen(t *testing.T) {
	for listen, want := range map[string]bool{
		"127.0.0.1:18662": true, "[::1]:18662": true, "localhost:18662": true, "LOCALHOST:1": true,
		":18662": false, "0.0.0.0:18662": false, "[::]:18662": false, "hub.example.com:18662": false, "": false, "bad": false,
	} {
		if got := loopbackOnly(listen); got != want {
			t.Errorf("loopbackOnly(%q) = %v, want %v", listen, got, want)
		}
	}
}
