package proto

import (
	"strings"
	"testing"
)

// A message ref stays as the service wrote it, trimmed and clean; one over the limit is none, not a cut one.
func TestCleanMessageRef(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"a plain id", "om_1", "om_1"},
		{"an id of the service's own form", "x:123/45", "x:123/45"},
		{"trimmed", " \n om_2\t ", "om_2"},
		{"without NUL", "om_\x003", "om_3"},
		{"without invalid UTF-8", "om_\xff4", "om_4"},
		{"nothing once cleaned", " \x00 ", ""},
		{"exactly the most", strings.Repeat("长", MessageRefMost), strings.Repeat("长", MessageRefMost)},
		{"one over the most", strings.Repeat("长", MessageRefMost+1), ""},
		{"over the most before it is cleaned, within it after", strings.Repeat("a\x00", MessageRefMost), strings.Repeat("a", MessageRefMost)},
		{"over the most once cleaned", strings.Repeat("a", MessageRefMost) + " b", ""},
	} {
		if got := CleanMessageRef(c.in); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
