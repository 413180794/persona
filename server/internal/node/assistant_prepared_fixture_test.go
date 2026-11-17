package node

import (
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// preparedFakeCLI is the machine's claude -p in cliTest: it appends its args to $CLI_FAKE_DIR/cli-argv and keeps its
// CLAUDE_CONFIG_DIR in cli-profile, keeps every stdin line in stdin.jsonl, answers control requests (an error from
// control-mode-error.json or control-effort-error.json, held while control-hold exists, after the lines in
// control-extra.jsonl, exiting at control-exit), and plays a fixed turn for each user message (no result while
// $CLI_FAKE_DIR/hold exists).
const preparedFakeCLI = `printf '%s\n' "$*" >> "$CLI_FAKE_DIR/cli-argv"
printf '%s\n' "$CLAUDE_CONFIG_DIR" > "$CLI_FAKE_DIR/cli-profile"
echo '{"type":"system","subtype":"init"}'
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$CLI_FAKE_DIR/stdin.jsonl"
  case "$line" in
    *'"type":"control_request"'*)
      id=$(printf '%s' "$line" | sed -n 's/.*"request_id":"\([^"]*\)".*/\1/p')
      if [ -f "$CLI_FAKE_DIR/control-extra.jsonl" ]; then cat "$CLI_FAKE_DIR/control-extra.jsonl"; fi
      if [ -f "$CLI_FAKE_DIR/control-hold" ]; then
        printf '%s\n' "$id" > "$CLI_FAKE_DIR/control-waiting"
        while [ -f "$CLI_FAKE_DIR/control-hold" ]; do sleep 0.02; done
      fi
      if [ -f "$CLI_FAKE_DIR/control-exit" ]; then exit 0; fi
      case "$line" in
        *'"subtype":"set_permission_mode"'*|*'"subtype":"apply_flag_settings"'*)
          case "$line" in
            *'"subtype":"set_permission_mode"'*) error_file="$CLI_FAKE_DIR/control-mode-error.json" ;;
            *) error_file="$CLI_FAKE_DIR/control-effort-error.json" ;;
          esac
          if [ -f "$error_file" ]; then
            printf '{"type":"control_response","response":{"subtype":"error","request_id":"%s","error":%s}}\n' "$id" "$(cat "$error_file")"
          else
            echo "{\"type\":\"control_response\",\"response\":{\"subtype\":\"success\",\"request_id\":\"$id\"}}"
          fi ;;
        *) echo "{\"type\":\"control_response\",\"response\":{\"subtype\":\"error\",\"request_id\":\"$id\",\"error\":\"unsupported\"}}" ;;
      esac ;;
    *'"type":"user"'*)
      echo '{"type":"stream_event","event":{"type":"message_start","message":{"usage":{"input_tokens":5}}},"parent_tool_use_id":null}'
      echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"hel"}},"parent_tool_use_id":null}'
      echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"lo"}},"parent_tool_use_id":null}'
      echo '{"type":"stream_event","event":{"type":"message_stop"},"parent_tool_use_id":null}'
      if [ ! -e "$CLI_FAKE_DIR/hold" ]; then echo '{"type":"result","subtype":"success","result":"hello"}'; fi ;;
  esac
done
`

type cliFixture struct {
	n   *Node
	fh  *fakeHub
	dir string // $CLI_FAKE_DIR
	log *syncBuffer
}

// cliTest is a node whose assistant conversations run preparedFakeCLI as the machine's claude, with a fake hub.
func cliTest(t *testing.T) *cliFixture {
	t.Helper()
	fakeClaudeHome(t, preparedFakeCLI)
	dir := t.TempDir()
	t.Setenv("CLI_FAKE_DIR", dir)
	n := codexTestNode(t)
	n.dir = t.TempDir()
	buf := &syncBuffer{}
	n.logger = log.New(buf, "", 0)
	fh := withFakeHub(t, n)
	// Every process it started, the warm spare too, has exited when the test ends: liveAssistants is the test binary's,
	// and a later test reading it (update_inventory.go) would find one still running.
	t.Cleanup(func() {
		n.mu.Lock()
		ids := make([]string, 0, len(n.procs))
		for id := range n.procs {
			ids = append(ids, id)
		}
		spare := n.spare
		n.spare = nil
		n.mu.Unlock()
		var procs []*assistantProc
		for _, id := range ids {
			if p := n.takeAssistant(id); p != nil {
				procs = append(procs, p)
			}
		}
		if spare != nil {
			procs = append(procs, spare)
		}
		stopAndWait(procs)
	})
	return &cliFixture{n: n, fh: fh, dir: dir, log: buf}
}

func cliWait(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(what)
}
func (f *cliFixture) file(name string) string {
	b, _ := os.ReadFile(filepath.Join(f.dir, name))
	return string(b)
}
