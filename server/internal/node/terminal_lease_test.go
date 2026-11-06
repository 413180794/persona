package node

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A controlled terminal whose program keeps printing still lets go when the hub stops renewing it: the output is not
// the hub (#5450: every 256 KB of diffs renewed the lease, so a busy pane stayed at the phone's size).
func TestTerminalLeaseRunsOutWhileOutputFlows(t *testing.T) {
	dir := t.TempDir()
	bin, log, stop := filepath.Join(dir, "herdr"), filepath.Join(dir, "log"), filepath.Join(dir, "stop")
	script := fmt.Sprintf(`#!/bin/sh
case "$1 $2" in
"terminal session")
  [ "$4" = "--help" ] && exit 0
  D=$(head -c 49152 /dev/zero | base64 | tr -d '\n')
  echo '{"type":"terminal.frame","seq":1,"full":true,"width":44,"height":30,"encoding":"ansi","bytes":"RlVMTA=="}'
  exec 3<&0 # an async list's own stdin is /dev/null
  ( while read -r line <&3; do :; done; touch %[2]q ) & # stdin closing is the release
  i=2
  while [ ! -f %[2]q ]; do
    echo "{\"type\":\"terminal.frame\",\"seq\":$i,\"full\":false,\"width\":44,\"height\":30,\"encoding\":\"ansi\",\"bytes\":\"$D\"}"
    i=$((i+1))
    sleep 0.02
  done
  echo released >> %[1]q ;;
*) exit 1 ;;
esac
`, log, stop)
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	old := termLease
	termLease = 300 * time.Millisecond
	t.Cleanup(func() { termLease = old })
	n, _, events := controlNode(t, bin)
	if _, err := n.terminalWatch(context.Background(), "s1", &termControl{Cols: 44, Rows: 30}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the lease to run out", func() bool {
		for _, e := range events() {
			if e.Event == "terminal.end" && e.Control && !e.Taken {
				return true
			}
		}
		return false
	})
	waitFor(t, "the fake herdr to let go", func() bool { return strings.Contains(readLog(log), "released") })
}
