package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestCodexUnknownItemsAreNotUserInputOrProof(t *testing.T) {
	line := `{"timestamp":"2026-11-01T00:00:00Z","type":"event_msg","payload":{"type":"item_completed","item":{"id":"unknown-1","type":"UnknownVerification","proof":"synthetic-proof"}}}`
	path := filepath.Join(t.TempDir(), "rollout-synthetic.jsonl")
	if err := os.WriteFile(path, []byte(line+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f := NewFile(path)
	items, err := f.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Role != "system" || items[0].Kind != proto.KindNotice || strings.Contains(items[0].Text, "synthetic-proof") {
		t.Fatalf("unknown event boundary: %+v", items)
	}
}
