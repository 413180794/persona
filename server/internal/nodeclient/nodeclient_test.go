package nodeclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const modulePath = "github.com/nwverse/persona/server/"

// The commands a computer runs for its Agents link only this client and the contracts: never the node, the manager or
// the hub (the self-contained packaging plan, step 4: the device's program owns these commands).
func TestClientCommandsDoNotLinkTheNodeOrTheManager(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go tool:", err)
	}
	forbidden := []string{"internal/node", "internal/devices", "internal/assistant", "internal/hub", "internal/manager",
		"internal/extmgr", "internal/db"}
	for _, pkg := range []string{".", "../mcpserver", "../scriptcli", "../toolcli"} {
		out, err := exec.Command(goBin, "list", "-deps", "-f", "{{.ImportPath}}", pkg).Output()
		if err != nil {
			t.Fatalf("go list -deps %s: %v", pkg, err)
		}
		for _, dep := range strings.Fields(string(out)) {
			rel, ok := strings.CutPrefix(dep, modulePath)
			if !ok {
				continue
			}
			for _, f := range forbidden {
				if rel == f || strings.HasPrefix(rel, f+"/") {
					t.Errorf("%s depends on %s: the node.sock commands reach the node only through internal/nodeclient "+
						"(move what they need into it, or into a contract package)", pkg, dep)
				}
			}
		}
	}
}

func TestDialResumableResendsTheSameAttempt(t *testing.T) {
	defer func(d, w time.Duration) { ResumeRetry, ResumeWait = d, w }(ResumeRetry, ResumeWait)
	ResumeRetry, ResumeWait = time.Millisecond, time.Second
	var seen []Request
	dial := func(_ context.Context, req any) ([]byte, error) {
		r := req.(Request)
		seen = append(seen, r)
		switch len(seen) {
		case 1:
			return []byte(`{"retry":true}` + "\n"), nil // the node is going away
		case 2:
			return nil, errors.New("node closed connection") // between nodes
		}
		return []byte(`{"output":{"ok":true}}` + "\n"), nil
	}
	line, err := DialResumable(context.Background(), dial, Request{Kind: "hook", Attempt: "a1"})
	if err != nil || !bytes.Contains(line, []byte(`"ok":true`)) {
		t.Fatalf("answer %q %v", line, err)
	}
	if len(seen) != 3 || seen[0].Resume || !seen[1].Resume || !seen[2].Resume || seen[2].Attempt != "a1" {
		t.Fatalf("requests %+v: want the first plain, then resumes of the same attempt", seen)
	}
	if _, err := DialResumable(context.Background(), func(context.Context, any) ([]byte, error) { return nil, ErrNoNode }, Request{}); !errors.Is(err, ErrNoNode) {
		t.Fatalf("no node: %v", err)
	}
}

func TestRunHookWithoutANodePrintsNothing(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no node.sock there
	for _, args := range [][]string{nil, {GlobalHookFlag}} {
		var out bytes.Buffer
		RunHook(context.Background(), args, strings.NewReader(`{"session_id":"s1","hook_event_name":"Stop"}`), &out)
		if out.Len() != 0 {
			t.Fatalf("%v: printed %q", args, out.String())
		}
	}
	var out bytes.Buffer
	RunHook(context.Background(), nil, strings.NewReader("not json"), &out)
	if out.Len() != 0 {
		t.Fatalf("bad input: printed %q", out.String())
	}
}

func TestAskRequestNormalize(t *testing.T) {
	a := AskRequest{Question: "  Deploy?  ", Options: []string{" yes ", "", "yes", "no"}}
	if err := a.Normalize(); err != nil || a.Question != "Deploy?" || len(a.Options) != 2 {
		t.Fatalf("%+v %v", a, err)
	}
	b, _ := json.Marshal(Request{Kind: "ask", Attempt: NewAttempt()})
	var back Request
	if json.Unmarshal(b, &back) != nil || len(back.Attempt) != 36 {
		t.Fatalf("round trip %s", b)
	}
}
