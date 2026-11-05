package assistant

import (
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// A card's conversation title made by clipping a first message inside its injected tag (an older node's).
func TestCleanTitleDropsInjectedTags(t *testing.T) {
	for title, want := range map[string]string{
		`<jarvis-context kind="context" title="接上…`:                  "demo",
		`<persona-context kind="context" title="接上旧主对话">` + "\n摘要被截": "接上旧主对话",
		"修一下登录": "修一下登录",
		"":      "",
	} {
		if got := cleanTitle(proto.Interaction{SessionTitle: title, Cwd: "/work/demo"}).SessionTitle; got != want {
			t.Errorf("cleanTitle(%q) = %q, want %q", title, got, want)
		}
	}
}
