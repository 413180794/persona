package assistant

import (
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// With the conversation's page known, each message carries its own link (?at=<item id>, #2194); without it the text
// is as before.
func TestTranscriptTextLinksEachMessage(t *testing.T) {
	items := []proto.Item{
		{ID: "u1#0", Role: "user", Kind: proto.KindText, Text: "修一下 CI"},
		{ID: "a2#1", Role: "assistant", Kind: proto.KindToolUse, ToolName: "Bash"},
		{ID: "a3#0", Role: "assistant", Kind: proto.KindText, Text: "修好了"},
	}
	got := transcriptText("head", "https://hub.example.com/s/mac-a/b005", items)
	for _, want := range []string{
		"User (https://hub.example.com/s/mac-a/b005?at=u1%230): 修一下 CI",
		"Agent (https://hub.example.com/s/mac-a/b005?at=a3%230): 修好了",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "a2%231") {
		t.Fatalf("a tool call got a link:\n%s", got)
	}
	if got := transcriptText("head", "", items); strings.Contains(got, "?at=") || !strings.Contains(got, "User: 修一下 CI") {
		t.Fatalf("links without a page:\n%s", got)
	}
}
