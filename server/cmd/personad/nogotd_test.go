package main

import (
	"os/exec"
	"strings"
	"testing"
)

// jarvisd stays small: Telegram's MTProto client (gotd) is only in jarvisd-telegram, which the hub runs as a program
// of its own (docs/plan-telegram.md「架构」). Linked into jarvisd it would add about 35 MB.
func TestJarvisdDoesNotLinkTelegram(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	for _, dep := range strings.Fields(string(out)) {
		if strings.HasPrefix(dep, "github.com/gotd/") || strings.HasSuffix(dep, "/connector/telegram") {
			t.Fatalf("jarvisd links %s: keep it in jarvisd-telegram", dep)
		}
	}
}
