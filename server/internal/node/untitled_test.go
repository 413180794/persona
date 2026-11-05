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
}
