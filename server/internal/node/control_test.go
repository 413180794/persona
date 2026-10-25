package node

import "testing"

func TestFindBlock(t *testing.T) {
	all := []string{"a", "b", "c", "a", "b", "d"}
	if got := findBlock(all, []string{"a", "b"}); got != 3 {
		t.Fatalf("last match: %d", got)
	}
	if got := findBlock(all, []string{"c", "a "}); got != 2 {
		t.Fatalf("trailing spaces: %d", got)
	}
	if got := findBlock(all, []string{"x"}); got != -1 {
		t.Fatalf("missing: %d", got)
	}
}

func TestNearestBlockPicksTheOneWhereWeAre(t *testing.T) {
	// the same screen pushed three times; the phone has the last 4 lines → its block starts near 6
	all := []string{"x", "a", "b", "y", "a", "b", "z", "a", "b", "w"}
	if got := nearestBlock(all, []string{"a", "b"}, len(all)-4); got != 7 {
		t.Fatalf("nearest to 6: %d", got)
	}
	if got := nearestBlock(all, []string{"a", "b"}, 2); got != 1 {
		t.Fatalf("nearest to 2: %d", got)
	}
}
