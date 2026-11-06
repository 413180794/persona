package channel

import (
	"encoding/json"
	"strings"
	"testing"
)

// A permission asked in a channel with no summary shows the call's input in the chat: never an argument named like
// a credential, however deep it sits (#4734).
func TestToolBriefHidesCredentials(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{`{"command":"ls"}`, "ls"},
		{`{"api_key":"fake-key-123","repo":"demo"}`, "repo: demo"},
		{`{"Password":"fake-pass","user":"bob"}`, "user: bob"},
		{`{"token":"fake-token","count":3}`, `{"count":3,"token":"***"}`},
		{`{"opts":{"client_secret":"fake","n":2},"list":[{"x-api-key":"fake"}]}`, `{"list":[{"x-api-key":"***"}],"opts":{"client_secret":"***","n":2}}`},
		{`{"private_key":"fake-pem"}`, `{"private_key":"***"}`},
	}
	for _, c := range cases {
		got := toolBrief(json.RawMessage(c.input))
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.input, got, c.want)
		}
		if strings.Contains(got, "fake") {
			t.Errorf("%s: a credential shows: %q", c.input, got)
		}
	}
}

func TestToolBriefStaysShort(t *testing.T) {
	got := toolBrief(json.RawMessage(`{"n":1,"blob":[` + strings.Repeat(`1,`, 500) + `1]}`))
	if len([]rune(got)) > 301 || !strings.HasSuffix(got, "…") {
		t.Fatalf("not clipped: %d runes", len([]rune(got)))
	}
}
