package node

import (
	"strings"
	"testing"
)

func TestAssistantCardPreviewUsesFallback(t *testing.T) {
	got := snippet("Conclusion\n```persona-view\n{\"fallback\":\"Build complete\",\"sections\":[]}\n```\n")
	if strings.Contains(got, "sections") || strings.Contains(got, "persona-view") || !strings.Contains(got, "Build complete") {
		t.Fatalf("preview %q", got)
	}
}
