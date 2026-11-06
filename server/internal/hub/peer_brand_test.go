package hub

import (
	"net/url"
	"reflect"
	"testing"
)

func TestPeerCodeBrandAliasesPreserveThePairingInputs(t *testing.T) {
	values := url.Values{"hub": {"fixture-hub"}, "name": {"Example hub"}, "fp": {"fixture-pin"}, "tls": {"18662"},
		"code": {"jx_fixture"}, "url": {"https://hub.example.com:18662", "https://[2001:db8::1]:18662"}}
	old, err := parsePeerCode("persona://peer?" + values.Encode())
	if err != nil {
		t.Fatal(err)
	}
	current, err := parsePeerCode(" persona://peer?" + values.Encode() + "\n")
	if err != nil || !reflect.DeepEqual(current, old) {
		t.Fatalf("alias changed pairing inputs: %+v %v", current, err)
	}
	for _, key := range []string{"hub", "fp", "code", "url"} {
		incomplete := url.Values{}
		for name, entries := range values {
			incomplete[name] = append([]string(nil), entries...)
		}
		incomplete.Del(key)
		for _, scheme := range []string{"persona"} {
			if _, err := parsePeerCode(scheme + "://peer?" + incomplete.Encode()); err == nil {
				t.Errorf("%s missing %s accepted", scheme, key)
			}
		}
	}
	for _, raw := range []string{"other://peer?" + values.Encode(), "persona://pair?" + values.Encode(), "persona://peer?%zz"} {
		if _, err := parsePeerCode(raw); err == nil {
			t.Errorf("invalid code accepted: %s", raw)
		}
	}
}
