package node

import (
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// A headless conversation titled by its first message: the context blocks it starts with are not its title.
func TestUntitledSkipsContextBlocks(t *testing.T) {
	s := proto.Session{}
	untitled(&s, quickMeta{first: `<persona-context kind="context" title="快照">` + "\n机器：a\n</persona-context>\n现在把 hub 部署到最新 master"})
	if s.Title != "现在把 hub 部署到最新 master" {
		t.Fatalf("title = %q", s.Title)
	}
	s = proto.Session{}
	untitled(&s, quickMeta{first: `<persona-context kind="context" title="快照">` + "\n被截断的快照"})
	if s.Title != "快照" {
		t.Fatalf("title of a message with only a block = %q, want the block's title", s.Title)
	}
	// A main conversation handed over starts with the old one's summary: titled by the block, never by a clip of
	// its opening tag (`<persona-context kind="context" title="…`, its title saying it continues the old one).
	s = proto.Session{}
	untitled(&s, quickMeta{first: `<persona-context kind="context" title="接上旧主对话" url="">` + "\n" + strings.Repeat("旧对话的摘要。", 40)})
	if s.Title != "接上旧主对话" {
		t.Fatalf("title of a handed-over main conversation = %q", s.Title)
	}
	// The new name (#3182), with and without attributes, and clipped.
	for first, want := range map[string]string{
		`<persona-context kind="context" title="快照">` + "\n机器：a\n</persona-context>\n现在把 hub 部署到最新 master": "现在把 hub 部署到最新 master",
		"<persona-context>\n快照\n</persona-context>\n约周五吃饭":                                                 "约周五吃饭",
		`<persona-context kind="context" title="快照">` + "\n被截断的快照":                                         "快照",
		"<persona-context>\n被截断的快照":                                                                        "",
	} {
		s = proto.Session{}
		untitled(&s, quickMeta{first: first})
		if s.Title != want {
			t.Fatalf("title of %q = %q, want %q", first, s.Title, want)
		}
	}
}
