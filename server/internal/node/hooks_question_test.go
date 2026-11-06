package node

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

func runQuestionHook(t *testing.T, n *Node, questions []map[string]any) map[string]any {
	t.Helper()
	input, _ := json.Marshal(map[string]any{"questions": questions})
	raw, _ := json.Marshal(hookInput{HookEventName: "PreToolUse", ToolName: "AskUserQuestion",
		SessionID: "s1", Cwd: "/tmp/x", ToolInput: input})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := n.handleHook(ctx, sockRequest{Input: raw})
	if err != nil {
		t.Fatal(err)
	}
	return out.(map[string]any)["hookSpecificOutput"].(map[string]any)
}

// Alike-worded questions are denied (answers are keyed by question text) with a reason for the model and no card;
// distinct questions behave as before (#4429). Trim/MultiSelect normalization is covered in proto's table test.
func TestHookAskUserQuestionDuplicateGuard(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	n2hR, n2hW := io.Pipe()
	h2nR, h2nW := io.Pipe()
	t.Cleanup(func() { cancel(); n2hW.Close(); h2nW.Close(); n2hR.Close(); h2nR.Close() })
	n, _ := testNode(t)
	n.peer = rpc.New("n", n2hW)
	hub := rpc.New("h", h2nW)
	created := 0
	reply := &proto.Resolution{}
	hub.OnRequest = func(ctx context.Context, method string, raw json.RawMessage) (any, error) {
		created++
		return *reply, nil
	}
	go hub.Serve(ctx, n2hR)
	go n.peer.Serve(ctx, h2nR)

	dup := runQuestionHook(t, n, []map[string]any{
		{"question": "确认部署？"}, {"question": " 　确认部署？　 ", "multiSelect": true},
	})
	reason, _ := dup["permissionDecisionReason"].(string)
	if dup["permissionDecision"] != "deny" || created != 0 || !strings.Contains(reason, "AskUserQuestion") {
		t.Fatalf("duplicate questions must be denied without a card, telling the model how to ask again: %v (%d)", dup, created)
	}
	*reply = proto.Resolution{Answers: map[string]string{"发给谁？": "张三", "哪些部分？": "进展"}}
	ok := runQuestionHook(t, n, []map[string]any{
		{"question": "发给谁？"}, {"question": "哪些部分？", "multiSelect": true},
	})
	if ok["permissionDecision"] != "allow" || created != 1 {
		t.Fatalf("distinct questions must be allowed, one card created: %v (created=%d)", ok, created)
	}
	updated, _ := ok["updatedInput"].(map[string]any)
	answers, _ := updated["answers"].(map[string]string)
	if len(answers) != 2 || answers["发给谁？"] != "张三" {
		t.Fatalf("answers lost: %v", updated)
	}
}
