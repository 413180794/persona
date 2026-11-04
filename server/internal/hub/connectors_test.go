package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/connector/github"
	"github.com/nwverse/persona/server/internal/connector/imessage"
	"github.com/nwverse/persona/server/internal/connector/proc"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/extension/host"
	"github.com/nwverse/persona/server/internal/mail"
	"github.com/nwverse/persona/server/internal/proto"
)

// exampleConnector reads and comments, on made-up data.
type exampleConnector struct{ commented []string }

func (*exampleConnector) ID() string   { return "example" }
func (*exampleConnector) Name() string { return "Example" }
func (*exampleConnector) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapStatus, connector.CapTools, connector.CapActions}}
}
func (*exampleConnector) Status(context.Context) connector.Status {
	return connector.Status{State: connector.StateOK, Account: "alice"}
}
func (*exampleConnector) Tools() []proto.Tool { return []proto.Tool{{Name: "example_read"}} }
func (*exampleConnector) Call(context.Context, string, json.RawMessage) (string, error) {
	return "example-org/example-repo#12", nil
}
func (*exampleConnector) Actions() []proto.Tool { return []proto.Tool{{Name: "example_comment"}} }
func (*exampleConnector) Describe(context.Context, string, json.RawMessage) (connector.Action, string, error) {
	return connector.Action{Visible: "example-org/example-repo 的所有人"}, "评论 example-org/example-repo#12：示例回复", nil
}
func (c *exampleConnector) Do(_ context.Context, name string, _ json.RawMessage) (string, error) {
	c.commented = append(c.commented, name)
	return "Commented.", nil
}

type namedConnector string

func (c namedConnector) ID() string                 { return string(c) }
func (c namedConnector) Name() string               { return string(c) }
func (namedConnector) Manifest() connector.Manifest { return connector.Manifest{} }

// toolConnector offers the tools named.
type toolConnector struct {
	id    string
	tools []string
}

func (c toolConnector) ID() string   { return c.id }
func (c toolConnector) Name() string { return c.id }
func (toolConnector) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapTools}}
}
func (c toolConnector) Tools() []proto.Tool {
	var out []proto.Tool
	for _, n := range c.tools {
		out = append(out, proto.Tool{Name: n})
	}
	return out
}
func (toolConnector) Call(context.Context, string, json.RawMessage) (string, error) { return "", nil }

// A connector may not offer a tool the hub has itself; one only named like them ("lark") is fine, does not take
// the hub's lark_* calls (lark_agent_reply stays the hub's), and is listed only to an app that asks for all (?all=1).
func TestConnectorsShadowNoBuiltinTool(t *testing.T) {
	h := newTestHub(t)
	h.connectors = connector.NewRegistry(nil)
	connectAssistant(t, h) // kb_search, lark_agent_reply and agent_send are on its list
	for _, c := range []toolConnector{{"kb", []string{"kb_search"}}, {"phone", []string{"phone_contacts"}},
		{"agent", []string{"agent_send"}}} {
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
	_, out := call(t, h.routes(), "GET", "/api/v1/connectors", h.cfg.Token, "")
	if list := out["connectors"].([]any); len(list) != 1 || list[0].(map[string]any)["id"] != "example" {
		t.Fatalf("listed %v", list)
	}
	// An app that draws Feishu's row from the list asks for all of them.
	_, out = call(t, h.routes(), "GET", "/api/v1/connectors?all=1", h.cfg.Token, "")
	if list := out["connectors"].([]any); len(list) != 2 || list[1].(map[string]any)["id"] != "lark" {
		t.Fatalf("listed with all=1: %v", list)
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

// What GET /api/v1/connectors?all=1 says each built-in connector can do (the app goes by these names).
func TestConnectorsListTheSameCapabilities(t *testing.T) {
	h := newTestHub(t)
	h.mail = (*mail.Service)(nil)
	h.connectors = connector.NewRegistry(nil)
	h.registerConnectors()
	if err := h.addConnector(imessage.NewRemote(imessage.Config{Machine: "mac"}, "Mac", nil)); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, info := range h.listedConnectors(context.Background(), true) {
		got = append(got, fmt.Sprintf("%s %s hosts=%v options=%v", info.ID, strings.Join(info.Capabilities, ","), info.Hosts, info.Options != nil))
	}
	want := []string{
		"mail tools,actions,archive hosts=[] options=false",
		"github status,tools,actions,events,hosts,inbox,archive,login,options,threads hosts=[github.com] options=true",
		"imessage status,tools,events,inbox,archive hosts=[] options=false",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// The coding agents get mail's tools, mail_send among them, and nobody else's.
	for name, want := range map[string]bool{"mail_send": true, "mail_search": true, "github_read": false, "lark_search": false} {
		if h.connectors.ForAgents(name) != want {
			t.Errorf("%s for agents: %v", name, !want)
		}
	}
}

// programConnector is a built-in plugin's connector whose program is at path, as the hub registers one (Telegram's
// was, until #1704): known from what it declares before the program ever runs.
func programConnector(t *testing.T, h *Hub, item extension.ConnectorItem, path string) *proc.Conn {
	t.Helper()
	items, _ := json.Marshal([]extension.ConnectorItem{item})
	m := &extension.Manifest{ManifestVersion: extension.ManifestVersion, ID: item.ID, Name: item.Name, Version: h.version,
		Engines: extension.Engines{ExtensionProtocol: extension.Protocol}, Runtime: &extension.Runtime{Command: extension.Command{Path: filepath.Base(path)}},
		Contributes: map[string]json.RawMessage{"connectors": items}}
	ps := newExtensions(h.dataDirOrTemp(), nil, h.logf)
	ps.h, ps.ctx = h, context.Background()
	if err := ps.reg.AddBuiltin(m, filepath.Dir(path), true); err != nil {
		t.Fatal(err)
	}
	hp := &hubExtension{id: item.ID}
	s, err := ps.spec(context.Background(), ps.reg.Get(item.ID), hp)
	if err != nil {
		t.Fatal(err)
	}
	pr := host.Start(context.Background(), s)
	t.Cleanup(pr.Stop)
	for _, l := range hp.links {
		l.Proc = pr
	}
	return hp.conns[0]
}

// A program's connector switched off from the start, its program never run: it can still be signed in to (the hub
// says to switch it on first) and out of (the program would run just for that; here it is not there), never "has no
// sign-in" (#666).
func TestSwitchedOffProgramSignsInAndOut(t *testing.T) {
	h := newTestHub(t)
	h.connectors = connector.NewRegistry(nil)
	item := extension.ConnectorItem{ID: "examplechat", Name: "Example Chat", Kinds: []string{"dm"},
		Capabilities: []string{"status", "tools", "actions", "events", "inbox", "archive", "login", "threads"}}
	if err := h.addConnectorOff(programConnector(t, h, item, "/nonexistent/example-chat")); err != nil {
		t.Fatal(err)
	}
	h.restoreConnectorSwitches(context.Background())
	if h.connectors.Enabled("examplechat") {
		t.Fatal("examplechat is on")
	}
	mux := h.routes()
	code, out := call(t, mux, "POST", "/api/v1/connectors/examplechat/login", h.cfg.Token, `{"step":""}`)
	if code != 200 || !strings.Contains(fmt.Sprint(out["error"]), "先打开") {
		t.Fatalf("login: %d %v", code, out)
	}
	code, out = call(t, mux, "POST", "/api/v1/connectors/examplechat/logout", h.cfg.Token, `{}`)
	if code == http.StatusConflict || out["error"] == nil {
		t.Fatalf("logout: %d %v", code, out)
	}
	_, out = call(t, mux, "GET", "/api/v1/connectors", h.cfg.Token, "")
	list := out["connectors"].([]any)
	if len(list) != 1 || fmt.Sprint(list[0].(map[string]any)["capabilities"]) != "[status tools actions events inbox archive login threads]" ||
		list[0].(map[string]any)["status"] != "disabled" {
		t.Fatalf("listed %v", list)
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
