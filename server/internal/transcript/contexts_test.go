package transcript

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// The shape hub wrote Feishu /agent prompts in before the instructions moved into a block (placeholder values).
const legacyLarkPrompt = "示例用户在飞书「发布群」里用 /agent 叫你。这不是在电脑上开的会话，你只知道下面给出的上下文。\n\n" +
	"## 请求\n\n看下构建为什么挂了\n\n## 上下文\n\n" +
	"<jarvis-context kind=\"context\" title=\"请求的位置\">\n位置：群聊「发布群」\n</jarvis-context>\n" +
	"<jarvis-context kind=\"context\" title=\"项目 · 发布\">\n</jarvis-context>\n\n" +
	"知识库里这个会话的 id：lark-f1（kb_get / kb_search 查更多）。\n\n\n" +
	"## 怎么做\n\n- 能做的直接做掉。\n- 做完用 lark_agent_reply 回复一次。\n"

func outside(s string) string { return strings.TrimSpace(contextBlockRe.ReplaceAllString(s, "")) }

func TestLegacyLarkAgentPromptShowsOnlyTheRequest(t *testing.T) {
	got := legacyLarkAgent(legacyLarkPrompt)
	if outside(got) != "看下构建为什么挂了" {
		t.Fatalf("outside the blocks: %q\n%s", outside(got), got)
	}
	blocks := contextBlockRe.FindAllStringSubmatch(got, -1)
	if len(blocks) != 3 || !strings.Contains(blocks[0][1], `kind="instructions"`) {
		t.Fatalf("blocks: %q", blocks)
	}
	for _, want := range []string{"用 /agent 叫你", "## 怎么做", "lark_agent_reply", "知识库里这个会话的 id"} {
		if !strings.Contains(blocks[0][2], want) {
			t.Fatalf("instructions miss %q: %q", want, blocks[0][2])
		}
	}
	if legacyLarkAgent(got) != got {
		t.Fatal("rewriting twice changed it")
	}
}

func TestLegacyLarkAgentPromptWithoutARequest(t *testing.T) {
	old := strings.Replace(legacyLarkPrompt, "看下构建为什么挂了", "（没写具体要求：看上下文和引用的消息，判断他想让你做什么。）", 1)
	got := legacyLarkAgent(old)
	if outside(got) != "/agent" || !strings.Contains(got, "（没写具体要求") {
		t.Fatalf("got %s", got)
	}
}

func TestOtherTextIsNotRewritten(t *testing.T) {
	for _, s := range []string{"hello", "## 请求\n\nx\n\n## 上下文\n\ny\n\n## 怎么做\n\nz", "<jarvis-context kind=\"context\" title=\"t\" url=\"\">\nx\n</jarvis-context>\n看看"} {
		if got := legacyLarkAgent(s); got != s {
			t.Fatalf("%q became %q", s, got)
		}
	}
}

func TestLegacyLarkAgentPromptInATranscript(t *testing.T) {
	content, _ := json.Marshal(legacyLarkPrompt)
	items, meta := parse(t, `{"type":"user","uuid":"u1","timestamp":"2026-10-24T10:00:00Z","message":{"role":"user","content":`+string(content)+`}}`)
	it := only(t, items)
	if it.Kind != proto.KindText || outside(it.Text) != "看下构建为什么挂了" {
		t.Fatalf("item %+v", it)
	}
	if meta.FirstUser == "" {
		t.Fatal("no first user text")
	}
}

func TestLightKeepsTheRequestAndEveryTitle(t *testing.T) {
	long := strings.Repeat("一行聊天记录\n", 6000) // ~114 KB
	text := "<jarvis-context kind=\"instructions\" title=\"给 Agent 的说明\">\n说明\n</jarvis-context>\n" +
		"<jarvis-context kind=\"context\" title=\"前面的聊天\">\n" + long + "</jarvis-context>\n" +
		"<jarvis-context kind=\"doc\" title=\"文档 · 手册\" url=\"https://example.com/d\">\n" + long + "</jarvis-context>\n" +
		"看下构建为什么挂了"
	it, cut := light(proto.Item{Role: "user", Kind: proto.KindText, Text: text}, false)
	if !cut || it.FullSize != len(text) || len(it.Text) > pageTextLen {
		t.Fatalf("cut %v, full %d, len %d", cut, it.FullSize, len(it.Text))
	}
	if outside(it.Text) != "看下构建为什么挂了" {
		t.Fatalf("outside the blocks: %q", outside(it.Text))
	}
	blocks := contextBlockRe.FindAllStringSubmatch(it.Text, -1)
	if len(blocks) != 3 || strings.TrimSpace(blocks[0][2]) != "说明" || !strings.Contains(blocks[1][2], "（已截断") || !strings.Contains(blocks[2][1], `url="https://example.com/d"`) {
		t.Fatalf("blocks: %d %q", len(blocks), it.Text[:200])
	}
	// Not a user message: a plain cut, as before.
	it, _ = light(proto.Item{Role: "assistant", Kind: proto.KindText, Text: text}, false)
	if strings.HasSuffix(it.Text, "看下构建为什么挂了") {
		t.Fatal("an answer was cut like a message with context")
	}
}
