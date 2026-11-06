package link

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestPersonaPairLinksKeepSecurityInputs(t *testing.T) {
	values := url.Values{"url": {"http://hub.example.com:18660"}, "tls": {"18662"}, "fp": {"fixture-pin"},
		"token": {"jp_fixture"}, "hub": {"fixture-hub"}, "machine": {"fixture-machine"}}
	for _, host := range []string{"pair", "link"} {
		token, parsed, err := parsePairLink("persona://" + host + "?" + values.Encode())
		if err != nil {
			t.Fatal(err)
		}
		trimmedToken, trimmed, err := parsePairLink(" persona://" + host + "?" + values.Encode() + "\n")
		if err != nil || token != trimmedToken || !reflect.DeepEqual(parsed, trimmed) {
			t.Fatalf("whitespace changed credentials: %+v %v", trimmed, err)
		}
		for _, key := range []string{"token", "fp", "tls", "url"} {
			incomplete := url.Values{}
			for name, entries := range values {
				incomplete[name] = append([]string(nil), entries...)
			}
			incomplete.Del(key)
			if _, _, err := parsePairLink("persona://" + host + "?" + incomplete.Encode()); err == nil {
				t.Errorf("missing %s was accepted", key)
			}
		}
	}
	for _, raw := range []string{"other://pair?" + values.Encode(), "persona://open?" + values.Encode(), "persona://link?%zz"} {
		if _, _, err := parsePairLink(raw); err == nil {
			t.Errorf("invalid link accepted: %s", raw)
		}
	}
}

func TestPersonaConnectorLinksRoundTrip(t *testing.T) {
	addresses := []string{"[2001:db8::1]:18663", "hub.example.com:18663"}
	invite := ConnectorLink(addresses, "fixture-pin", "fixture-code")
	if !strings.HasPrefix(invite, "persona://connector?") {
		t.Fatalf("wrong output scheme: %s", invite)
	}
	for _, raw := range []string{invite, " \n" + invite + "\n"} {
		got, fp, code, err := ParseConnectorLink(raw)
		if err != nil || !reflect.DeepEqual(got, addresses) || fp != "fixture-pin" || code != "fixture-code" {
			t.Fatalf("invite round trip: %v %v", got, err)
		}
	}
	for _, raw := range []string{"other://connector?addr=hub.example.com:1&fp=x&code=y", "persona://pair?addr=hub.example.com:1&fp=x&code=y",
		"persona://connector?fp=x&code=y", "persona://connector?addr=hub.example.com:1&code=y", "persona://connector?addr=hub.example.com:1&fp=x"} {
		if _, _, _, err := ParseConnectorLink(raw); err == nil {
			t.Errorf("invalid invite accepted: %s", raw)
		}
	}
}
