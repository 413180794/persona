package main

import (
	"os/exec"
	"strings"
	"testing"
)

// personad stays small: Telegram's MTProto client (gotd) lives in the Telegram extension, a program of its own (#1704;
// docs/archive/extensions/plan-telegram.md "Architecture"). Linked into personad it would add about 35 MB.
func TestPersonadDoesNotLinkTelegram(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	for _, dep := range strings.Fields(string(out)) {
		if strings.HasPrefix(dep, "github.com/gotd/") || strings.HasSuffix(dep, "/connector/telegram") {
			t.Fatalf("personad links %s: Telegram is an extension", dep)
		}
	}
}
