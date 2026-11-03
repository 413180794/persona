package link

import (
	"net/http"
	"testing"
)

func TestUpgradeRequiresAConnectionToken(t *testing.T) {
	for _, test := range []struct {
		name       string
		protocol   string
		connection []string
		want       bool
	}{
		{"canonical", "websocket", []string{"Upgrade"}, true},
		{"mixed case", "WebSocket", []string{"uPgRaDe"}, true},
		{"token list", "websocket", []string{"keep-alive, Upgrade"}, true},
		{"whitespace", "websocket", []string{" keep-alive ,\tUpgrade\t, "}, true},
		{"multiple fields", "websocket", []string{"keep-alive", "Upgrade"}, true},
		{"empty preceding field", "websocket", []string{"", "Upgrade"}, true},
		{"missing connection", "websocket", nil, false},
		{"other token", "websocket", []string{"keep-alive"}, false},
		{"prefix substring", "websocket", []string{"notupgrade"}, false},
		{"suffix substring", "websocket", []string{"upgrade-only"}, false},
		{"substrings in a list", "websocket", []string{"keep-alive, notupgrade, upgrade-only"}, false},
		{"missing protocol", "", []string{"Upgrade"}, false},
		{"other protocol", "h2c", []string{"Upgrade"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := &http.Request{Header: make(http.Header)}
			if test.protocol != "" {
				request.Header.Set("Upgrade", test.protocol)
			}
			for _, value := range test.connection {
				request.Header.Add("Connection", value)
			}
			if got := isUpgrade(request); got != test.want {
				t.Fatalf("isUpgrade = %v, want %v", got, test.want)
			}
		})
	}
}
