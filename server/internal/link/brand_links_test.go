package link

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestPairLinkBrandAliasesKeepTheSameSecurityInputs(t *testing.T) {
	values := url.Values{"url": {"http://hub.example.com:18660"}, "tls": {"18662"}, "fp": {"fixture-pin"},
		"token": {"jp_fixture"}, "hub": {"fixture-hub"}, "machine": {"fixture-machine"}}
	for _, host := range []string{"pair", "link"} {
		legacyToken, legacy, err := parsePairLink("jarvis://" + host + "?" + values.Encode())
		if err != nil {
			t.Fatal(err)
		}
		token, current, err := parsePairLink(" persona://" + host + "?" + values.Encode() + "\n")
		if err != nil || token != legacyToken || !reflect.DeepEqual(current, legacy) {
			t.Fatalf("brand alias changed the parsed credential or endpoint: %+v %v", current, err)
		}
		for _, key := range []string{"token", "fp", "tls", "url"} {
			incomplete := url.Values{}
			for name, entries := range values {
				incomplete[name] = append([]string(nil), entries...)
			}
			incomplete.Del(key)
			for _, scheme := range []string{"jarvis", "persona"} {
				if _, _, err := parsePairLink(scheme + "://" + host + "?" + incomplete.Encode()); err == nil {
					t.Errorf("%s missing %s was accepted", scheme, key)
				}
			}
		}
	}
	for _, raw := range []string{"other://pair?" + values.Encode(), "persona://open?" + values.Encode(), "persona://link?%zz"} {
		if _, _, err := parsePairLink(raw); err == nil {
			t.Errorf("invalid link accepted: %s", raw)
		}
	}
}

func TestConnectorBrandAliasDoesNotChangeLegacyOutput(t *testing.T) {
	addresses := []string{"[2001:db8::1]:18663", "hub.example.com:18663"}
	old := ConnectorLink(addresses, "fixture-pin", "fixture-code")
	if !strings.HasPrefix(old, "jarvis://connector?") {
		t.Fatalf("legacy output changed: %s", old)
	}
	for _, raw := range []string{old, strings.Replace(old, "jarvis://", "persona://", 1)} {
		got, fp, code, err := ParseConnectorLink(raw)
		if err != nil || !reflect.DeepEqual(got, addresses) || fp != "fixture-pin" || code != "fixture-code" {
			t.Fatalf("alias changed the parsed invite: %v %v", got, err)
		}
	}
	for _, raw := range []string{"other://connector?addr=hub.example.com:1&fp=x&code=y", "persona://pair?addr=hub.example.com:1&fp=x&code=y",
		"persona://connector?fp=x&code=y", "persona://connector?addr=hub.example.com:1&code=y", "persona://connector?addr=hub.example.com:1&fp=x"} {
		if _, _, _, err := ParseConnectorLink(raw); err == nil {
			t.Errorf("invalid invite accepted: %s", raw)
		}
	}
}
