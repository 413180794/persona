package toolcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// fakeNode answers the calls: a connector "example" whose example_cli plans `ex list [--limit N]`, `ex say <text|->`
// and shows list's answer as text; example_list lists; example_say is a write the gate declines when told to.
type fakeNode struct {
	calls   []string
	decline bool
	limited bool
}

func (f *fakeNode) call(_ context.Context, name string, args any) (proto.ToolResult, error) {
	b, _ := json.Marshal(args)
	f.calls = append(f.calls, name+" "+string(b))
	var in map[string]any
	_ = json.Unmarshal(b, &in)
	text := func(v any) (proto.ToolResult, error) {
		out, _ := json.Marshal(v)
		return proto.TextResult(string(out), false), nil
	}
	switch name {
	case "example_cli":
		if r, ok := in["render"]; ok {
			return text(map[string]any{"stdout": "rendered " + r.(string) + ": " + in["result"].(string)})
		}
		argv, _ := in["argv"].([]any)
		if len(argv) == 0 || argv[0] == "--help" {
			code := 0
			if len(argv) == 0 {
				code = 2
			}
			return text(map[string]any{"name": "ex", "exit": code, "stdout": "ex list | ex say <text|->"})
		}
		switch argv[0] {
		case "list":
			return text(map[string]any{"name": "ex", "call": map[string]any{"tool": "example_list", "args": map[string]any{"limit": 5}}, "render": "list"})
		case "say":
			words := argv[1].(string)
			if words == "-" {
				words = in["stdin"].(string)
			}
			return text(map[string]any{"name": "ex", "call": map[string]any{"tool": "example_say", "args": map[string]any{"text": words}}})
		}
		return text(map[string]any{"name": "ex", "exit": 2, "stderr": "no command " + argv[0].(string)})
	case "example_list":
		if f.limited {
			return proto.TextResult("Telegram 限流中，37 秒后再试", true), nil
		}
		return proto.TextResult(`{"items":[1,2]}`, false), nil
	case "example_say":
		if f.decline {
			return proto.TextResult("The user declined this on the phone. Do not retry it unless they ask.", true), nil
		}
		return proto.TextResult(`{"ok":true}`, false), nil
	case "down_cli":
		return proto.ToolResult{}, errors.New("no socket")
	}
	return proto.TextResult(name+" is not available: the command line calls only the tools of the hub's connectors that are signed in", true), nil
}

func run(t *testing.T, f *fakeNode, stdin string, args ...string) (int, string, string) {
	t.Helper()
	call = f.call
	var out, errOut bytes.Buffer
	code := Main(context.Background(), args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

// A connector's command line: the words planned by its <id>_cli, the tool it names called (so the gate sees that
// tool), the answer as JSON or shown by the connector as text; its usage and mistakes come from the plan.
func TestCommandLine(t *testing.T) {
	f := &fakeNode{}
	if code, out, _ := run(t, f, "", "--cli", "example", "list"); code != 0 || out != "{\"items\":[1,2]}\n" {
		t.Fatalf("json: %d %q", code, out)
	}
	if got := strings.Join(f.calls, "\n"); got != "example_cli {\"argv\":[\"list\"]}\nexample_list {\"limit\":5}" {
		t.Fatalf("calls:\n%s", got)
	}
	f.calls = nil
	if code, out, _ := run(t, f, "", "--cli", "example", "--format", "text", "list"); code != 0 || out != "rendered list: {\"items\":[1,2]}\n" {
		t.Fatalf("text: %d %q", code, out)
	}
	if len(f.calls) != 3 || !strings.HasPrefix(f.calls[2], `example_cli {"render":"list"`) {
		t.Fatalf("shown by the connector: %v", f.calls)
	}
	// The text of a write from stdin.
	f.calls = nil
	if code, _, _ := run(t, f, "line one\nline two", "--cli", "example", "say", "-"); code != 0 || f.calls[1] != `example_say {"text":"line one\nline two"}` {
		t.Fatalf("stdin: %d %v", code, f.calls)
	}
	// The plan's own answers: help, usage, a mistake (as JSON on stderr, or "ex: …" for people).
	if code, out, _ := run(t, f, "", "--cli", "example", "--help"); code != 0 || !strings.Contains(out, "ex list") {
		t.Fatalf("help: %d %q", code, out)
	}
	if code, out, _ := run(t, f, "", "--cli", "example"); code != 2 || !strings.Contains(out, "ex list") {
		t.Fatalf("usage: %d %q", code, out)
	}
	if code, _, errOut := run(t, f, "", "--cli", "example", "nope"); code != 2 || errOut != "{\"error\":\"no command nope\",\"kind\":\"usage\"}\n" {
		t.Fatalf("mistake: %d %q", code, errOut)
	}
	if code, _, errOut := run(t, f, "", "--cli", "example", "--format", "text", "nope"); code != 2 || errOut != "ex: no command nope\n" {
		t.Fatalf("mistake for people: %d %q", code, errOut)
	}
	// Failures by kind: declined by the user, rate limited (and for how long), a connector not signed in, no node.
	f.decline = true
	if code, _, errOut := run(t, f, "", "--cli", "example", "say", "hi"); code != exitDeclined || !strings.Contains(errOut, `"kind":"declined"`) {
		t.Fatalf("declined: %d %q", code, errOut)
	}
	f.limited = true
	if code, _, errOut := run(t, f, "", "--cli", "example", "list"); code != exitRateLimited || !strings.Contains(errOut, `"retry_after":37`) {
		t.Fatalf("limited: %d %q", code, errOut)
	}
	if code, _, _ := run(t, f, "", "--cli", "gone", "list"); code != exitUnavailable {
		t.Fatalf("not signed in: %d", code)
	}
	if code, _, _ := run(t, f, "", "--cli", "down", "list"); code != exitUnavailable {
		t.Fatalf("no node: %d", code)
	}
	if code, _, _ := run(t, f, "", "--cli", "example", "--format", "yaml", "list"); code != exitUsage {
		t.Fatalf("format: %d", code)
	}
}

// One tool by name, its arguments as JSON (or from stdin).
func TestOneTool(t *testing.T) {
	f := &fakeNode{}
	if code, out, _ := run(t, f, "", "example_list", `{"limit":5}`); code != 0 || out != "{\"items\":[1,2]}\n" || f.calls[0] != `example_list {"limit":5}` {
		t.Fatalf("%d %q %v", code, out, f.calls)
	}
	if code, _, _ := run(t, f, `{"text":"hi"}`, "example_say", "-"); code != 0 || f.calls[1] != `example_say {"text":"hi"}` {
		t.Fatalf("stdin: %d %v", code, f.calls)
	}
	if code, _, errOut := run(t, f, "", "example_list", "not json"); code != exitUsage || !strings.Contains(errOut, "not JSON") {
		t.Fatalf("bad args: %d %q", code, errOut)
	}
	if code, _, _ := run(t, f, ""); code != exitUsage {
		t.Fatalf("nothing: %d", code)
	}
}
