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
