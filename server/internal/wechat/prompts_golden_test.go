package wechat

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nwverse/persona/server/internal/connector/conformance"
	"github.com/nwverse/persona/server/internal/prompts"
)

// golden compares a prompt with testdata/prompts/<name>.golden; UPDATE_GOLDEN=1 writes it (docs/prompts.md).
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "prompts", name+".golden")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (UPDATE_GOLDEN=1 writes it)", name, err)
	}
	if got != string(want) {
		t.Errorf("%s differs from %s\n--- got ---\n%s\n--- want ---\n%s", name, path, got, want)
	}
}

func TestGoldenWeChatMessage(t *testing.T) {
	golden(t, "wechat_message.text", prompts.RenderWeChatMessage(prompts.WeChatMessage{Text: "明天有什么会？"}))
	golden(t, "wechat_message.quote_unsupported", prompts.RenderWeChatMessage(prompts.WeChatMessage{Text: "看下这个",
		Quote: "张三\n合同明天要签", Unsupported: []string{"文件「合同.pdf」", "一段视频"}}))
	golden(t, "wechat_message.only_unsupported", prompts.RenderWeChatMessage(prompts.WeChatMessage{Unsupported: []string{"一段没有转写的语音"}}))
}

// The connector answers the same through the protocol as directly.
func TestConformance(t *testing.T) {
	conformance.Check(t, New(Options{Hub: NewHub("http://127.0.0.1:1", "tok")}, &memSecrets{}))
}
