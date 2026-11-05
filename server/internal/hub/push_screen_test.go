package hub

import (
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/screen"
)

const permissionScreenForPush = ` Bash command

   git push origin main
   Push the release commit

 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and don't ask again for git push commands in /code/app
   3. No, and tell Claude what to do differently (esc)`

// A screen card's push says what the dialog asks (#3646): the lock screen shows the command and the question, not
// only "open Persona to see the screen".
func TestScreenPushSaysWhatItAsks(t *testing.T) {
	body := func(ix proto.Interaction) string {
		return interactionPayloadIn(i18n.ZhHans, ix, "box")["aps"].(map[string]any)["alert"].(map[string]any)["body"].(string)
	}
	generic := i18n.T(i18n.ZhHans, "push.screen_body")
	d := screen.ParseDialog(permissionScreenForPush)
	if d == nil {
		t.Fatal("dialog not parsed")
	}
	got := body(proto.Interaction{ID: "scr1", Kind: proto.IxScreen, Dialog: d})
	if !strings.Contains(got, "git push origin main") || !strings.Contains(got, "Do you want to proceed?") || got == generic {
		t.Fatalf("body %q", got)
	}
	if strings.Index(got, "git push") > strings.Index(got, "Do you want") {
		t.Fatalf("the command goes before the question: %q", got)
	}
	// No dialog read off the screen, or a password prompt: the generic line, nothing of the screen.
	if got := body(proto.Interaction{Kind: proto.IxScreen, Screen: "something"}); got != generic {
		t.Fatalf("no dialog: %q", got)
	}
	if got := body(proto.Interaction{Kind: proto.IxScreen, Dialog: d, Secret: true}); got != generic {
		t.Fatalf("secret: %q", got)
	}
	// A long command is cut, the question stays.
	longCmd := &screen.Dialog{Detail: strings.Repeat("x", 1000), Question: "Do you want to proceed?"}
	if got := body(proto.Interaction{Kind: proto.IxScreen, Dialog: longCmd}); !strings.HasSuffix(got, "Do you want to proceed?") || len([]rune(got)) > 302 {
		t.Fatalf("long command: %d runes, %q", len([]rune(got)), got[len(got)-40:])
	}
	// A dialog with nothing in it: the generic line.
	if got := body(proto.Interaction{Kind: proto.IxScreen, Dialog: &screen.Dialog{Question: "  "}}); got != generic {
		t.Fatalf("empty dialog: %q", got)
	}
	// A health conversation's card tells nothing of what it asks.
	if got := body(proto.Interaction{Kind: proto.IxScreen, Dialog: d, Cwd: "/home/me/.persona/health"}); got != i18n.T(i18n.ZhHans, "push.health_body") {
		t.Fatalf("health: %q", got)
	}
	// A long dialog is cut to fit a notification.
	long := &screen.Dialog{Question: strings.Repeat("问", 1000)}
	if got := body(proto.Interaction{Kind: proto.IxScreen, Dialog: long}); len([]rune(got)) > 301 {
		t.Fatalf("not clipped: %d runes", len([]rune(got)))
	}
}
