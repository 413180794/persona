package node

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/chyroc/jarvis/server/internal/transcript"
)

// The streamed background-task scan finds what parsing every short marker line finds, across a line far larger than
// its read buffer and a last line that is only half written when one scan runs.
func TestBackgroundTasksStreamed(t *testing.T) {
	for _, name := range []string{"claude-notifications.jsonl", "claude-session.jsonl", "claude-subagent.jsonl"} {
		src, err := os.ReadFile(filepath.Join("..", "transcript", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		lines := bytes.Split(bytes.TrimRight(src, "\n"), []byte("\n"))
		var want transcript.Meta
		for _, l := range lines {
			if len(l) <= 64<<10 && containsAny(l, bgMarkers) {
				transcript.ParseLine(0, l, &want)
			}
		}
		huge := append(append([]byte(`{"type":"user","note":"`), bytes.Repeat([]byte("run_in_background "), 40000)...), `"}`...)
		half := len(lines) / 2
		var head bytes.Buffer
		for _, l := range lines[:half] {
			head.Write(l)
			head.WriteByte('\n')
		}
		head.Write(huge)
		head.WriteByte('\n')
		rest := append(bytes.Join(lines[half:], []byte("\n")), '\n')

		path := filepath.Join(t.TempDir(), "s.jsonl")
		n := &Node{bgScans: map[string]*bgScan{}}
		for _, upto := range []int{len(rest) / 2, len(rest)} {
			if err := os.WriteFile(path, append(bytes.Clone(head.Bytes()), rest[:upto]...), 0o600); err != nil {
				t.Fatal(err)
			}
			n.backgroundTasks(path)
		}
		if got := n.bgScans[path].meta.Background; !reflect.DeepEqual(got, want.Background) {
			t.Errorf("%s: background tasks\n got %+v\nwant %+v", name, got, want.Background)
		}
		if st, _ := os.Stat(path); n.bgScans[path].off != st.Size() {
			t.Errorf("%s: scanned up to %d of %d bytes", name, n.bgScans[path].off, st.Size())
		}
	}
}
