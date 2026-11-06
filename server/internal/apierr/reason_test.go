package apierr

import "testing"

func TestReasonPrefersTheOriginalOverTheLocalizedSummary(t *testing.T) {
	for _, c := range []struct{ body, want string }{
		{`{"error":"summary","detail":"ssh: refused","code":"internal"}`, "ssh: refused"},
		{`{"error":"ssh: refused"}`, "ssh: refused"},
		{`{"error":"summary","detail":{"kind":"x"}}`, "summary"}, // a structured detail is not a reason
		{`{"error":"summary","detail":""}`, "summary"},
		{`not json`, ""},
	} {
		if got := Reason([]byte(c.body)); got != c.want {
			t.Errorf("Reason(%s) = %q, want %q", c.body, got, c.want)
		}
	}
}
