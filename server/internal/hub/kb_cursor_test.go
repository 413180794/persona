package hub

import "testing"

func TestLarkPosition(t *testing.T) {
	for p, want := range map[string]bool{"": true, "0": true, "13537": true, "-3": false, "abc": false, "9007199254740992": false, "1.5": false} {
		if got := larkPosition(p); got != want {
			t.Errorf("larkPosition(%q) = %v, want %v", p, got, want)
		}
	}
}
