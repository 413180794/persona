package hub

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// computerCall is one computer.call a fake node got.
type computerCallSeen struct {
	Session string         `json:"session"`
	Tool    string         `json:"tool"`
	Args    map[string]any `json:"args"`
}

// computerMac is a Mac whose node answers computer.status and computer.call as a node with Cua Driver set up does, and
// says which it was asked.
func computerMac(t *testing.T, h *Hub, id string) (*Machine, func() (statuses int, calls []computerCallSeen)) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h2nR, h2nW := io.Pipe()
	n2hR, n2hW := io.Pipe()
	hubSide, nodeSide := rpc.New("h", h2nW), rpc.New("n", n2hW)
	var mu sync.Mutex
	statuses, calls := 0, []computerCallSeen{}
	var job *proto.ComputerJob // the node's install or grant, as computer.status says it
	nodeSide.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		switch method {
		case "computer.status":
			statuses++
			return proto.Computer{Installed: true, Version: "0.38.0", Daemon: true, Accessibility: "granted", ScreenRecording: "granted",
				Desktop: "active", Ready: true, Job: job}, nil
		case "computer.install", "computer.grant", "computer.cancel":
			calls = append(calls, computerCallSeen{Tool: method})
			job = &proto.ComputerJob{Kind: strings.TrimPrefix(method, "computer."), State: "running", Step: "下载安装器"}
			if method == "computer.cancel" {
				job = &proto.ComputerJob{Kind: "install", State: "cancelled"}
			}
			return proto.Computer{Job: job}, nil
		case "computer.call":
			var c computerCallSeen
			_ = json.Unmarshal(params, &c)
			calls = append(calls, c)
			switch c.Tool {
			case "run_script":
				return proto.ComputerResult{Output: json.RawMessage(`{"ok": true, "value": {"pid": 63, "window_id": 2318}}`)}, nil
			case "get_window_state", "get_desktop_state":
				return proto.ComputerResult{Output: json.RawMessage(`{"snapshot_id": "s1", "tree_markdown": "- [5] AXButton (7)"}`),
					Image: &proto.Image{Mime: "image/jpeg", Data: "AAAA", Width: 230, Height: 408}}, nil
			}
			return proto.ComputerResult{Output: json.RawMessage(`{"ok": true, "executed": 1}`)}, nil
		}
		return nil, &rpc.Error{Message: "unknown method " + method}
	}
	go func() { _ = hubSide.Serve(ctx, n2hR) }()
	go func() { _ = nodeSide.Serve(ctx, h2nR) }()
	t.Cleanup(func() { cancel(); _ = h2nW.Close(); _ = n2hW.Close() })
	m := newMachine(h, MachineConfig{ID: id, Name: id})
	m.peer, m.online, m.info = hubSide, true, proto.NodeInfo{OS: "darwin"}
	h.mu.Lock()
	h.machines, h.byID[id] = append(h.machines, m), m
	h.mu.Unlock()
	return m, func() (int, []computerCallSeen) {
		mu.Lock()
		defer mu.Unlock()
		return statuses, append([]computerCallSeen(nil), calls...)
	}
}

func toolNames(tools []proto.Tool) string {
	var names []string
	for _, t := range tools {
		names = append(names, t.Name)
	}
	return strings.Join(names, " ")
}

// The plugin is off until the user turns it on, then machine by machine: until then no node is asked, the assistant
// has no tools for it and the machines say nothing of it.
func TestComputerPluginIsOffUntilTurnedOnForAMachine(t *testing.T) {
	h, _ := pluginTestHub(t)
	ctx := context.Background()
	mux := h.routes()
	mac, seen := computerMac(t, h, "mac")
	box := newMachine(h, MachineConfig{ID: "box", Name: "box"})
	box.online, box.info = true, proto.NodeInfo{OS: "linux"}
	h.machines, h.byID["box"] = append(h.machines, box), box

	if p := h.plugins.reg.Get(cuaPluginID); p == nil || p.Record.Enabled {
		t.Fatalf("the built-in plugin is there and off: %+v", p)
	}
	if mac.snapshot().Computer != nil || strings.Contains(toolNames(h.mcpTools(true)), "computer_") {
		t.Fatal("off: no Computer, no tools")
	}
	h.refreshComputer(ctx, mac)
	if code, _ := call(t, mux, "GET", "/api/v1/machines/mac/computer", h.cfg.Token, ""); code != 404 {
		t.Fatalf("off: %d", code)
	}
	if n, _ := seen(); n != 0 {
		t.Fatal("the node is not asked while the plugin is off")
	}

	if err := h.plugins.reg.SetEnabled(ctx, cuaPluginID, true, "test"); err != nil {
		t.Fatal(err)
	}
	if c := mac.snapshot().Computer; c == nil || c.Enabled {
		t.Fatalf("on, not yet for this Mac: %+v", c)
	}
	if box.snapshot().Computer != nil {
		t.Fatal("not a Mac")
	}
	if tools := h.mcpTools(true); strings.Contains(toolNames(tools), "computer_") {
		t.Fatal("no tools before some machine is on")
	}
	if code, _ := call(t, mux, "PUT", "/api/v1/machines/box/computer", h.cfg.Token, `{"enabled": true}`); code != 404 {
		t.Fatalf("a Linux machine: %d", code)
	}

	code, out := call(t, mux, "PUT", "/api/v1/machines/mac/computer", h.cfg.Token, `{"enabled": true}`)
	if code != 200 || out["enabled"] != true || out["ready"] != true || out["version"] != "0.38.0" {
		t.Fatalf("turned on for the Mac: %d %v", code, out)
	}
	if got := toolNames(h.mcpTools(true)); !strings.Contains(got, "computer_state") || !strings.Contains(got, "computer_run") {
		t.Fatalf("the assistant's tools: %s", got)
	}
	if strings.Contains(toolNames(h.mcpTools(false)), "computer_") {
		t.Fatal("the assistant's only: agents use Cua Driver's own MCP on their machine")
	}
	if c := mac.snapshot().Computer; c == nil || !c.Enabled || !c.Ready {
		t.Fatalf("the machine list says it: %+v", c)
	}

	if !h.portAllowed(mac, screenPort) || h.portAllowed(box, screenPort) || h.portAllowed(mac, 5901) {
		t.Fatal("the live screen's port is open for a Mac it is on for, and only that port")
	}

	code, out = call(t, mux, "PUT", "/api/v1/machines/mac/computer", h.cfg.Token, `{"enabled": false}`)
	if code != 200 || out["enabled"] != false || h.computerTools() != nil {
		t.Fatalf("off again: %d %v", code, out)
	}
	if err := h.plugins.reg.SetEnabled(ctx, cuaPluginID, false, "test"); err != nil {
		t.Fatal(err)
	}
	if mac.snapshot().Computer != nil {
		t.Fatal("the plugin off: nothing of it")
	}
	if h.portAllowed(mac, screenPort) {
		t.Fatal("the plugin off: no live screen")
	}
}

// computer_state and computer_run reach the node as Cua Driver calls under the conversation's session label; the
// screenshot comes back as an image beside the JSON.
func TestComputerToolsCallTheNode(t *testing.T) {
	h, _ := pluginTestHub(t)
	ctx := context.Background()
	_, seen := computerMac(t, h, "mac")
	if err := h.plugins.reg.SetEnabled(ctx, cuaPluginID, true, "test"); err != nil {
		t.Fatal(err)
	}
	if res := h.callComputerTool(ctx, "conv1234-5678", "computer_state", json.RawMessage(`{"machine": "mac"}`)); !res.IsError ||
		!strings.Contains(res.Content[0].Text, "not turned on") {
		t.Fatalf("not on for the Mac yet: %+v", res)
	}
	if err := h.plugins.reg.SetConfig(ctx, cuaPluginID, json.RawMessage(`{"machines": ["mac"]}`), "test"); err != nil {
		t.Fatal(err)
	}

	res := h.callComputerTool(ctx, "conv1234-5678", "computer_state", json.RawMessage(`{"machine": "mac", "app": "Calculator", "query": "7"}`))
	if res.IsError || len(res.Content) != 2 || !strings.Contains(res.Content[0].Text, "AXButton (7)") ||
		res.Content[1].Type != "image" || res.Content[1].MimeType != "image/jpeg" {
		t.Fatalf("a window's tree and screenshot: %+v", res)
	}
	_, calls := seen()
	if len(calls) != 2 || calls[0].Tool != "run_script" || !strings.Contains(calls[0].Args["script"].(string), `cua.getApp("Calculator")`) {
		t.Fatalf("the window is found by Cua Driver's own lookup: %+v", calls)
	}
	if w := calls[1]; w.Tool != "get_window_state" || w.Args["window_id"] != float64(2318) || w.Args["query"] != "7" || w.Session != "persona-conv1234" {
		t.Fatalf("then read under the conversation's session: %+v", w)
	}

	steps := `[{"click": {"app": "Calculator", "role": "button", "name": "6"}}, {"tool": "press_key", "args": {"key": "return"}}]`
	res = h.callComputerTool(ctx, "conv1234-5678", "computer_run", json.RawMessage(`{"machine": "mac", "steps": `+steps+`}`))
	if res.IsError || !strings.Contains(res.Content[0].Text, `"executed":1`) {
		t.Fatalf("%+v", res)
	}
	_, calls = seen()
	if run := calls[len(calls)-1]; run.Tool != "run_steps" || len(run.Args["steps"].([]any)) != 2 || run.Session != "persona-conv1234" {
		t.Fatalf("steps go to run_steps: %+v", run)
	}
	if got := stepActions(json.RawMessage(steps)); strings.Join(got, " ") != "click press_key" {
		t.Fatalf("the log names the actions only: %v", got)
	}
	if res := h.callComputerTool(ctx, "c", "computer_run", json.RawMessage(`{"machine": "mac"}`)); !res.IsError {
		t.Fatal("neither script nor steps")
	}

	// Asking first, when the user set it: unanswered (or declined), nothing runs.
	if err := h.plugins.reg.SetConfig(ctx, cuaPluginID, json.RawMessage(`{"machines": ["mac"], "confirm": true}`), "test"); err != nil {
		t.Fatal(err)
	}
	_, before := seen()
	short, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	res = h.callComputerTool(short, "c", "computer_run", json.RawMessage(`{"machine": "mac", "script": "return 1"}`))
	if _, after := seen(); !res.IsError || !strings.Contains(res.Content[0].Text, "declined") || len(after) != len(before) {
		t.Fatalf("not confirmed: %+v", res)
	}
}

// A long tree is cut where a character starts, saying how much is left.
func TestComputerResultCutsLongOutput(t *testing.T) {
	long := `"` + strings.Repeat("界", computerMost) + `"`
	res := computerResult(&proto.ComputerResult{Output: json.RawMessage(long)})
	text := res.Content[0].Text
	if len(text) > computerMost+100 || !strings.Contains(text, "more bytes") || !strings.HasPrefix(text, `"界`) {
		t.Fatalf("%d %q", len(text), text[len(text)-60:])
	}
	if strings.ContainsRune(text, '�') {
		t.Fatal("cut inside a character")
	}
}

// The machine page's 安装 Cua Driver, 去授权 and 取消 start and stop the node's job; its state comes back on the machine.
func TestComputerJobs(t *testing.T) {
	h, _ := pluginTestHub(t)
	ctx := context.Background()
	mux := h.routes()
	mac, seen := computerMac(t, h, "mac")
	if code, _ := call(t, mux, "POST", "/api/v1/machines/mac/computer/install", h.cfg.Token, ""); code != 404 {
		t.Fatalf("the plugin off: %d", code)
	}
	if err := h.plugins.reg.SetEnabled(ctx, cuaPluginID, true, "test"); err != nil {
		t.Fatal(err)
	}
	if code, _ := call(t, mux, "POST", "/api/v1/machines/mac/computer/install", h.cfg.Token, ""); code != 404 {
		t.Fatalf("not on for this Mac: %d", code)
	}
	if err := h.plugins.reg.SetConfig(ctx, cuaPluginID, json.RawMessage(`{"machines": ["mac"]}`), "test"); err != nil {
		t.Fatal(err)
	}
	if code, _ := call(t, mux, "POST", "/api/v1/machines/mac/computer/reboot", h.cfg.Token, ""); code != 404 {
		t.Fatalf("only the three: %d", code)
	}
	code, out := call(t, mux, "POST", "/api/v1/machines/mac/computer/install", h.cfg.Token, "")
	job, _ := out["job"].(map[string]any)
	if code != 200 || out["enabled"] != true || job["kind"] != "install" || job["state"] != "running" || job["step"] != "下载安装器" {
		t.Fatalf("%d %v", code, out)
	}
	if c := mac.snapshot().Computer; c == nil || c.Job == nil || c.Job.State != "running" {
		t.Fatalf("the machine list says it: %+v", c)
	}
	if _, out = call(t, mux, "POST", "/api/v1/machines/mac/computer/cancel", h.cfg.Token, ""); out["job"].(map[string]any)["state"] != "cancelled" {
		t.Fatalf("%v", out)
	}
	call(t, mux, "POST", "/api/v1/machines/mac/computer/grant", h.cfg.Token, "")
	var asked []string
	_, calls := seen()
	for _, c := range calls {
		asked = append(asked, c.Tool)
	}
	if strings.Join(asked, " ") != "computer.install computer.cancel computer.grant" {
		t.Fatalf("%v", asked)
	}
	same := proto.Computer{Job: &proto.ComputerJob{Kind: "install", State: "running"}}
	again := proto.Computer{Job: &proto.ComputerJob{Kind: "install", State: "running"}}
	if !sameComputer(same, again) || sameComputer(same, proto.Computer{}) {
		t.Fatal("jobs compare by value")
	}
	mac.mu.Lock()
	mac.online = false
	mac.mu.Unlock()
	if code, _ := call(t, mux, "POST", "/api/v1/machines/mac/computer/install", h.cfg.Token, ""); code != 409 {
		t.Fatalf("offline: %d", code)
	}
}

// An answer to an older question never replaces a newer one: a page's refresh can land after the install it raced.
func TestKeepComputerKeepsTheNewest(t *testing.T) {
	h := newTestHub(t)
	m := newMachine(h, MachineConfig{ID: "mac", Name: "mac"})
	early, late := time.Now(), time.Now().Add(time.Second)
	running := proto.Computer{Job: &proto.ComputerJob{Kind: "install", State: "running"}}
	cancelled := proto.Computer{Job: &proto.ComputerJob{Kind: "install", State: "cancelled"}}
	if !h.keepComputer(m, cancelled, late) || h.keepComputer(m, running, early) {
		t.Fatal("the late answer to the early question is dropped")
	}
	if m.computer.Job.State != "cancelled" {
		t.Fatalf("%+v", m.computer.Job)
	}
	if h.keepComputer(m, cancelled, late.Add(time.Second)) {
		t.Fatal("the same again is no change")
	}
}
