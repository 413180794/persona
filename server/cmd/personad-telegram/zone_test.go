package main

import "testing"

// The user's zone from the hub, or none: never the machine's (the hub runs in UTC).
func TestUserZone(t *testing.T) {
	if z := userZone("Asia/Shanghai"); z == nil || z.String() != "Asia/Shanghai" {
		t.Fatalf("named: %v", z)
	}
	for _, name := range []string{"", "Nowhere/Example"} {
		if z := userZone(name); z != nil {
			t.Errorf("%q: %v, want none", name, z)
		}
	}
}
