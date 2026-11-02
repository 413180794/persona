package hub

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/connector"
	"github.com/chyroc/jarvis/server/internal/connector/github"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// exampleConnector reads and comments, on made-up data.
type exampleConnector struct{ commented []string }

func (*exampleConnector) ID() string   { return "example" }
func (*exampleConnector) Name() string { return "Example" }
func (*exampleConnector) Status(context.Context) connector.Status {
	return connector.Status{State: connector.StateOK, Account: "alice"}
}
func (*exampleConnector) Tools() []proto.Tool { return []proto.Tool{{Name: "example_read"}} }
func (*exampleConnector) Call(context.Context, string, json.RawMessage) (string, error) {
	return "example-org/example-repo#12", nil
}
func (*exampleConnector) Actions() []proto.Tool { return []proto.Tool{{Name: "example_comment"}} }
func (*exampleConnector) Describe(context.Context, string, json.RawMessage) (connector.Action, string) {
	return connector.Action{Visible: "example-org/example-repo 的所有人"}, "评论 example-org/example-repo#12：示例回复"
}
func (c *exampleConnector) Do(_ context.Context, name string, _ json.RawMessage) (string, error) {
	c.commented = append(c.commented, name)
	return "Commented.", nil
}

type namedConnector string

func (c namedConnector) ID() string   { return string(c) }
func (c namedConnector) Name() string { return string(c) }

// toolConnector offers the tools named.
type toolConnector struct {
	id    string
	tools []string
}

func (c toolConnector) ID() string   { return c.id }
func (c toolConnector) Name() string { return c.id }
func (c toolConnector) Tools() []proto.Tool {
	var out []proto.Tool
	for _, n := range c.tools {
		out = append(out, proto.Tool{Name: n})
	}
	return out
}
func (toolConnector) Call(context.Context, string, json.RawMessage) (string, error) { return "", nil }

// A connector may not offer a tool the hub has itself; one only named like them ("lark") is fine, does not take
// the hub's lark_* calls, and is not listed while it has its own row in the app.
func TestConnectorsShadowNoBuiltinTool(t *testing.T) {
	h := newTestHub(t)
	h.connectors = connector.NewRegistry(nil)
	for _, c := range []toolConnector{{"kb", []string{"kb_search"}}, {"phone", []string{"phone_contacts"}},
		{"mail", []string{"mail_send"}}, {"lark", []string{"lark_chats"}}, {"agent", []string{"agent_send"}}} {
		if err := h.addConnector(c); err == nil {
			t.Errorf("connector %q offering %v was registered", c.id, c.tools)
		}
	}
	if err := h.addConnector(namedConnector("example")); err != nil {
		t.Fatal(err)
	}
	if err := h.addConnector(namedConnector("lark")); err != nil {
		t.Fatal(err)
	}
	if h.connectors.Offers("lark_chats") {
		t.Fatal("a lark connector without tools took over lark_chats")
	}
	_, out := call(t, h.routes(), "GET", "/api/v1/connectors", h.cfg.Token, "")
	if list := out["connectors"].([]any); len(list) != 1 || list[0].(map[string]any)["id"] != "example" {
		t.Fatalf("listed %v", list)
	}
}

// Without a connector nothing changes (the MCP golden); with one, the assistant gets its tools, agents do not, a read
// runs, a write waits for the phone's card and the card says who will see it.
func TestConnectorToolsForTheAssistant(t *testing.T) {
	h := newTestHub(t)
	h.connectors = connector.NewRegistry(nil)
	before := len(h.mcpTools(true))
	c := &exampleConnector{}
	if err := h.addConnector(c); err != nil {
		t.Fatal(err)
	}
	names := func(tools []proto.Tool) []string {
		var out []string
		for _, t := range tools {
			out = append(out, t.Name)
		}
		return out
	}
	assistant := names(h.mcpTools(true))
	if len(assistant) != before+2 || !slices.Contains(assistant, "example_read") || !slices.Contains(assistant, "example_comment") {
		t.Fatalf("assistant tools %v", assistant)
	}
	if slices.Contains(names(h.mcpTools(false)), "example_read") {
		t.Fatal("an agent got a connector's tool")
	}

	m := newMachine(h, MachineConfig{ID: "box", Name: "Box", Kind: "local"})
	mcpCall := func(name string, assistant bool) proto.ToolResult {
		params, _ := json.Marshal(map[string]any{"name": name, "args": map[string]any{}, "assistant": assistant})
		out, err := m.onRequest(context.Background(), "mcp.call", params)
		if err != nil {
			t.Fatal(err)
		}
		return out.(proto.ToolResult)
	}
	if r := mcpCall("example_read", true); r.IsError || r.Content[0].Text != "example-org/example-repo#12" {
		t.Fatalf("read: %+v", r)
	}
	if r := mcpCall("example_read", false); !r.IsError {
		t.Fatalf("an agent ran a connector's tool: %+v", r)
	}

	done := make(chan proto.ToolResult, 1)
	go func() { done <- mcpCall("example_comment", true) }()
	var ix []proto.Interaction
	deadline := time.Now().Add(5 * time.Second)
	for len(ix) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		ix = h.pendingInteractions()
	}
	if len(ix) != 1 || ix[0].ToolName != "example_comment" || !strings.Contains(ix[0].Summary, "谁会看到：example-org/example-repo 的所有人") {
		t.Fatalf("card %+v", ix)
	}
	if len(c.commented) != 0 {
		t.Fatal("commented before the user allowed it")
	}
	allow := true
	if err := h.resolve(ix[0].ID, proto.Resolution{Allow: &allow}); err != nil {
		t.Fatal(err)
	}
	if r := <-done; r.IsError || len(c.commented) != 1 {
		t.Fatalf("comment: %+v %v", r, c.commented)
	}
}

func TestConnectorsEndpoint(t *testing.T) {
	h := newTestHub(t)
	h.connectors = connector.NewRegistry(nil)
	mux := h.routes()
	code, out := call(t, mux, "GET", "/api/v1/connectors", h.cfg.Token, "")
	if code != 200 || len(out["connectors"].([]any)) != 0 {
		t.Fatalf("%d %v", code, out)
	}
	_ = h.addConnector(&exampleConnector{})
	_, out = call(t, mux, "GET", "/api/v1/connectors", h.cfg.Token, "")
	list := out["connectors"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["account"] != "alice" || list[0].(map[string]any)["status"] != "ok" {
		t.Fatalf("%v", out)
	}
}

func TestGhTokenFromAMachine(t *testing.T) {
	h := newTestHub(t)
	m := newMachine(h, MachineConfig{ID: "mac", Name: "Mac", Kind: "local"})
	h.byID["mac"] = m
	if _, err := h.ghToken("mac")(context.Background(), "github.com"); !errors.Is(err, github.ErrOffline) {
		t.Fatalf("offline: %v", err)
	}
	if _, err := h.ghToken("nope")(context.Background(), "github.com"); !errors.Is(err, github.ErrNoMachine) {
		t.Fatalf("unknown machine: %v", err)
	}
}
