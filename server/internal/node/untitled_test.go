package node

import (
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// A headless conversation titled by its first message: the context blocks it starts with are not its title.
func TestUntitledSkipsContextBlocks(t *testing.T) {
	s := proto.Session{}
	untitled(&s, quickMeta{first: `<jarvis-context kind="context" title="快照">` + "\n机器：a\n</jarvis-context>\n现在把 hub 部署到最新 master"})
	if s.Title != "现在把 hub 部署到最新 master" {
		t.Fatalf("title = %q", s.Title)
	}
	s = proto.Session{}
	untitled(&s, quickMeta{first: `<jarvis-context kind="context" title="快照">` + "\n被截断的快照"})
	if s.Title != "" {
		t.Fatalf("title of a message with only a block = %q, want none (the cwd's name)", s.Title)
	}
	// The new name (#3182), with and without attributes, and clipped.
	for first, want := range map[string]string{
		`<persona-context kind="context" title="快照">` + "\n机器：a\n</persona-context>\n现在把 hub 部署到最新 master": "现在把 hub 部署到最新 master",
		"<persona-context>\n快照\n</persona-context>\n约周五吃饭":                                                   "约周五吃饭",
		`<persona-context kind="context" title="快照">` + "\n被截断的快照":                                              "",
	} {
		s = proto.Session{}
		untitled(&s, quickMeta{first: first})
		if s.Title != want {
			t.Fatalf("title of %q = %q, want %q", first, s.Title, want)
		}
	}
}
