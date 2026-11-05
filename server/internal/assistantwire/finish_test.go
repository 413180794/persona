package assistantwire

import (
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// The finished push's text and whether a turn finished normally (#2396): a quiet task's conversation pushes nothing
// on a normal finish, but an error, a cut-off answer or a stuck turn is still told.
func TestFinishedTextAndNormalFinish(t *testing.T) {
	for _, c := range []struct {
		name   string
		s      proto.Session
		text   string
		normal bool
	}{
		{"last words", proto.Session{LastText: "没有新邮件。"}, "没有新邮件。", true},
		{"no words", proto.Session{}, "", true},
		{"done work line", proto.Session{LastText: "好了", Work: &proto.WorkState{Phase: proto.WorkDone, Line: "合并了 PR"}}, "合并了 PR", true},
		{"stuck", proto.Session{LastText: "等部署", Work: &proto.WorkState{Phase: proto.WorkBlocked, Line: "在等部署跑完"}}, "在等部署跑完", false},
		{"model error", proto.Session{LastText: "没有新邮件。", TurnError: "unexpected status 503"}, "没有新邮件。", false},
		{"cut off", proto.Session{LastText: "API Error: 529 overloaded"}, "API Error: 529 overloaded", false},
		{"working line is not the text", proto.Session{LastText: "查完了", Work: &proto.WorkState{Phase: proto.WorkWorking, Line: "在查"}}, "查完了", true},
	} {
		if got := FinishedText(c.s); got != c.text {
			t.Errorf("%s: text %q, want %q", c.name, got, c.text)
		}
		if got := NormalFinish(c.s); got != c.normal {
			t.Errorf("%s: normal %v, want %v", c.name, got, c.normal)
		}
	}
}

// Only a Claude Code conversation that is not the main one may go unpushed: a headless Codex's or a runtime's failed
// turn is not on the session yet, and the main conversation's turns are the user's.
func TestQuietTaskFinish(t *testing.T) {
	ok := proto.Session{SessionID: "own-q", LastText: "没有新邮件。"}
	codex, runtime := ok, ok
	codex.Agent, runtime.Agent = "codex", "dsh"
	failed := ok
	failed.TurnError = "unexpected status 503"
	for _, c := range []struct {
		name string
		s    proto.Session
		main string
		want bool
	}{
		{"claude, normal finish", ok, "main-1", true},
		{"no main conversation yet", ok, "", true},
		{"headless codex", codex, "main-1", false},
		{"a runtime", runtime, "main-1", false},
		{"made the main conversation", ok, "own-q", false},
		{"model error", failed, "main-1", false},
	} {
		if got := QuietTaskFinish(c.s, c.main); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
