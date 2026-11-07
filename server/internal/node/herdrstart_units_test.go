//go:build !windows

package node

import (
	"strings"
	"testing"
)

func TestHerdrServerUnitOfSession(t *testing.T) {
	if got := herdrUnitOf(""); got != herdrUnit {
		t.Fatalf("default unit = %q, want %q", got, herdrUnit)
	}
	for _, session := range []string{"custom-data-directory", "custom/path with.dots"} {
		if got := herdrUnitOf(session); got == herdrUnit || strings.ContainsAny(got, "./ ") {
			t.Fatalf("unit %q for session %q", got, session)
		}
	}
}
