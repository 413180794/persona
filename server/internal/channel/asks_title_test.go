package channel

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// A conversation titled by a clip of its first message (a node from before titles were stripped first) asked in a
// channel: the bracketed title has no injected tag, whole or cut off.
func TestAskTextTitleWithoutInjectedBlocks(t *testing.T) {
	q := []proto.Question{{Question: "Codebase 扩展 1.1.1 已经合并，要发版吗？", Header: "Codebase",
		Options: []proto.QuestionOption{{Label: "发"}, {Label: "先不发"}}}}
	input, _ := json.Marshal(map[string]any{"questions": q})
	for title, want := range map[string]string{
		`<persona-context kind="context" title="別の…`:                      "【demo】",
		`<persona-context kind="context" title="接上旧主对话" url="">` + "\n摘要": "【接上旧主对话】",
		"<persona-con…": "【demo】",
		"修一下登录":         "【修一下登录】",
	} {
		ix := proto.Interaction{ID: "ix_1", Kind: proto.IxQuestion, SessionTitle: title, Cwd: "/work/demo", ToolName: "AskUserQuestion",
			ToolInput: input, Questions: q}
		text, ok := AskText(i18n.ZhHans, ix, NewAsk("feishu", ix, time.Now()))
		if !ok || !strings.HasPrefix(text, want) || strings.Contains(text, "context") {
			t.Errorf("title %q: text = %q, want it to start with %q", title, text, want)
		}
		// An ask kept from before (its title not cleaned) reads the same.
		old := &Ask{ID: ix.ID, Kind: ix.Kind, Title: title, Questions: q}
		if text, _ := AskText(i18n.ZhHans, ix, old); strings.Contains(text, "context") {
			t.Errorf("kept ask titled %q: text = %q", title, text)
		}
	}
}
