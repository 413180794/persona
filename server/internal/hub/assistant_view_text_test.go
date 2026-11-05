package hub

import (
	"strings"
	"testing"
)

func TestAssistantCardPushUsesFallback(t *testing.T) {
	text := "**Result**\n```persona-view\n{\"fallback\":\"Build complete\",\"sections\":[]}\n```\n"
	got := plainMarkdown(text)
	if strings.Contains(got, "sections") || strings.Contains(got, "persona-view") || !strings.Contains(got, "Build complete") {
		t.Fatalf("push text %q", got)
	}
}
