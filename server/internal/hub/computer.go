package hub

// Operating the user's Macs for the assistant through Cua Driver: the built-in plugin cua, off until the user turns
// it on, then machine by machine (docs/computer-use.md). While it is off no node is asked about cua-driver, the
// assistant has no computer_* tools and the machines carry no Computer.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/plugin"
	"github.com/nwverse/persona/server/internal/proto"
)

const cuaPluginID = "cua"

// cuaConfig is the plugin's settings: the machines it is on for (turned on and off on each machine's page) and
// whether the assistant asks on the phone before it acts.
const cuaConfig = `{
  "type": "object",
  "properties": {
    "machines": {"type": "array", "title": "能操控的电脑", "description": "打开的 Mac，助手才能操控。开关即时生效，和它机器页上的「允许助手操控这台电脑」是同一个", "items": {"type": "string", "format": "machine", "x-os": ["darwin"]}, "uniqueItems": true},
    "confirm": {"type": "boolean", "title": "操作前在手机上确认", "description": "打开后，助手每次点击、输入、按键、打开或关闭 App 之前先在手机上问你；关着时直接做，记进 hub 的日志", "default": false},
    "app_approval": {"type": "boolean", "title": "第一次操作一个 App 时先问", "description": "助手第一次在一台 Mac 上操作某个 App（或看整个屏幕）时在手机上问一次，选「一直允许」后记住，在机器页可以移除；终端、系统设置、钥匙串访问、密码管理器和 Persona 每次都问", "default": true},
    "keep_awake": {"type": "boolean", "title": "操作和看画面时保持唤醒", "description": "助手操作那台 Mac、或有人在看它的画面时，它不睡、不因空闲锁屏；最后一次操作 10 分钟后、画面关掉后就撤掉，不改系统设置", "default": true}
  }
}`

func cuaManifest(h *Hub) *plugin.Manifest {
	items, _ := json.Marshal([]plugin.ComputerItem{{ID: cuaPluginID, Name: "Cua Driver"}})
	return &plugin.Manifest{ManifestVersion: plugin.ManifestVersion, ID: cuaPluginID, Name: "操控电脑（Cua）", Version: h.version,
		Description: "让助手经 Cua Driver 操控你的 Mac：看窗口和屏幕、点按、输入、截图。要在那台 Mac 上装好 Cua Driver 并给它辅助功能和屏幕录制权限。",
		Homepage:    "https://github.com/trycua/cua", Contributes: map[string]json.RawMessage{"computer": items},
		Configuration: json.RawMessage(cuaConfig)}
}

// cuaSettings is the plugin's state: On when it is on, then the machines it is on for and whether to ask first.
type cuaSettings struct {
	On       bool     `json:"-"`
	Machines []string `json:"machines"`
	Confirm  bool     `json:"confirm"`
	// KeepAwake: 「操作和看画面时保持唤醒」, on unless turned off (nil); the node holds caffeinate (node/awake.go).
	KeepAwake *bool `json:"keep_awake"`
	// AppApproval: 「第一次操作一个 App 时先问」, on unless turned off (computer_apps.go).
	AppApproval *bool `json:"app_approval"`
}

// awake: a Mac it operates, or whose live screen is open, is kept awake.
func (s cuaSettings) awake() bool { return s.KeepAwake == nil || *s.KeepAwake }

func (h *Hub) cua() cuaSettings { return h.machinePlugin(cuaPluginID) }

// machinePlugin is a built-in plugin that is turned on per Mac (cua, ego): on or not, its Macs, whether to ask first.
func (h *Hub) machinePlugin(id string) cuaSettings {
	if h.plugins == nil || h.plugins.reg == nil {
		return cuaSettings{}
	}
	p := h.plugins.reg.Get(id)
	if p == nil || !p.Record.Enabled {
		return cuaSettings{}
	}
	s := cuaSettings{On: true}
	_ = json.Unmarshal(h.plugins.reg.Config(p), &s)
	return s
}

// operable: the plugin is on and m is a Mac (the only kind Cua Driver runs on here).
func (h *Hub) operable(m *Machine) bool {
	m.mu.Lock()
	goos := m.info.OS
	m.mu.Unlock()
	return h.cua().On && goos == "darwin"
}

// computerOf is m's proto.Machine.Computer: nil unless the plugin is on and m is a Mac; else the last status the node
// gave (for every Mac while the plugin is on, so a list of switches can say how Cua Driver is on each before it is
// turned on), with Enabled when the plugin is on for m.
func (h *Hub) computerOf(m *Machine) *proto.Computer {
	s := h.cua()
	m.mu.Lock()
	darwin, last := m.info.OS == "darwin", m.computer
	m.mu.Unlock()
	if !s.On || !darwin {
		return nil
	}
	c := proto.Computer{}
	if last != nil {
		c = *last
	}
	c.Enabled = slices.Contains(s.Machines, m.cfg.ID)
	return &c
}

// refreshComputer asks m's node what it has of Cua Driver now, when the plugin is on for m; the machine list goes out
// again when that changed.
func (h *Hub) refreshComputer(ctx context.Context, m *Machine) *proto.Computer {
	if c := h.computerOf(m); c == nil || !m.isOnline() {
		return c
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	asked := time.Now()
	var c proto.Computer
	if err := m.call(ctx, "computer.status", map[string]any{}, &c); err != nil {
		if !strings.Contains(err.Error(), "unknown method") {
			return h.computerOf(m)
		}
		c = proto.Computer{Note: "这台电脑上的 personad 太旧，还不会操控电脑：重新部署它"}
	}
	if h.keepComputer(m, c, asked) {
		h.broadcastMachines()
	}
	return h.computerOf(m)
}

// keepComputer stores what m's node answered, unless what is kept is newer: by when the node answered (its checked,
// set last), so a status asked after an install but answered before it does not undo it; an older node's answers,
// which have none, by when they were asked. True when what is kept changed.
func (h *Hub) keepComputer(m *Machine, c proto.Computer, asked time.Time) bool {
	when := c.Checked
	if when.IsZero() {
		when = asked
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if when.Before(m.computerAt) {
		return false
	}
	old := m.computer
	m.computer, m.computerAt = &c, when
	return old == nil || !sameComputer(*old, c)
}

func sameComputer(a, b proto.Computer) bool {
	aj, bj := a.Job, b.Job
	a.Checked, b.Checked, a.Job, b.Job = time.Time{}, time.Time{}, nil, nil
	return a == b && (aj == nil) == (bj == nil) && (aj == nil || *aj == *bj)
}

// computerChanged: the plugin was turned on or off, or its machines changed: ask the Macs again (every one while it is
// on: the settings' list says how Cua Driver is on each).
func (h *Hub) computerChanged(ctx context.Context) {
	for _, m := range h.machinesNow() {
		if c := h.computerOf(m); c != nil && m.isOnline() {
			go h.refreshComputer(ctx, m)
		}
	}
	h.broadcastMachines()
}

// getComputer: GET /api/v1/machines/{mid}/computer, what the machine has of Cua Driver now (the machine page asks
// when it opens). 404 while the plugin is off or it is not a Mac.
func (h *Hub) getComputer(w http.ResponseWriter, r *http.Request, m *Machine) {
	c := h.refreshComputer(r.Context(), m)
	if c == nil {
		writeErr(w, http.StatusNotFound, errors.New("操控电脑的插件没打开，或这台不是 Mac"))
		return
	}
	writeJSON(w, c)
}

// putComputer: PUT /api/v1/machines/{mid}/computer {"enabled": bool}, the machine page's switch: it goes into the
// plugin's machines.
func (h *Hub) putComputer(w http.ResponseWriter, r *http.Request, m *Machine) {
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad body: %w", err))
		return
	}
	if !h.operable(m) {
		writeErr(w, http.StatusNotFound, errors.New("操控电脑的插件没打开，或这台不是 Mac"))
		return
	}
	if err := h.setPluginMachine(r.Context(), cuaPluginID, m.cfg.ID, in.Enabled); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, h.refreshComputer(r.Context(), m))
}

// setPluginMachine turns a per-Mac plugin on or off for a machine: its config's machines (the machine page's switch,
// the same as the plugin page's).
func (h *Hub) setPluginMachine(ctx context.Context, plugin, machine string, on bool) error {
	s := h.machinePlugin(plugin)
	machines := slices.DeleteFunc(slices.Clone(s.Machines), func(id string) bool { return id == machine })
	if on {
		machines = append(machines, machine)
	}
	p := h.plugins.reg.Get(plugin)
	cfg := map[string]any{}
	_ = json.Unmarshal(h.plugins.reg.Config(p), &cfg)
	cfg["machines"] = machines
	raw, _ := json.Marshal(cfg)
	return h.plugins.reg.SetConfig(ctx, plugin, raw, "app")
}

// postComputerJob: POST /api/v1/machines/{mid}/computer/{install|grant|cancel}, the machine page's 安装 Cua Driver,
// 去授权 and 取消. The node runs the job in the background; its state comes back as Computer.job (the page asks
// again while it runs).
func (h *Hub) postComputerJob(w http.ResponseWriter, r *http.Request, m *Machine) {
	action := r.PathValue("action")
	if action != "install" && action != "grant" && action != "cancel" {
		writeErr(w, http.StatusNotFound, errors.New("unknown action"))
		return
	}
	if c := h.computerOf(m); c == nil || !c.Enabled {
		writeErr(w, http.StatusNotFound, errors.New("操控电脑没有对这台打开"))
		return
	}
	if !m.isOnline() {
		writeErr(w, http.StatusConflict, errors.New("机器离线"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	asked := time.Now()
	var c proto.Computer
	if err := m.call(ctx, "computer."+action, map[string]any{}, &c); err != nil {
		if strings.Contains(err.Error(), "unknown method") {
			err = errors.New("这台电脑上的 personad 太旧，还不会安装 Cua Driver：重新部署它")
		}
		writeErr(w, http.StatusConflict, err)
		return
	}
	h.logf("[%s] computer %s", m.cfg.ID, action)
	if h.keepComputer(m, c, asked) {
		h.broadcastMachines()
	}
	writeJSON(w, h.computerOf(m))
}

// ---- the assistant's tools ----

// computerTools are the assistant's tools while the plugin is on for some machine.
func (h *Hub) computerTools() []proto.Tool {
	if s := h.cua(); !s.On || len(s.Machines) == 0 {
		return nil
	}
	return []proto.Tool{mcpTool("computer_state"), mcpTool("computer_run")}
}

func isComputerTool(name string) bool { return name == "computer_state" || name == "computer_run" }

// computerMost: the most of a tool's JSON the model gets back (a window's tree runs long; it can narrow with query).
const computerMost = 60000

// callComputerTool runs computer_state or computer_run for the assistant's conversation sessionID.
func (h *Hub) callComputerTool(ctx context.Context, sessionID, name string, args json.RawMessage) proto.ToolResult {
	var a struct {
		Machine    string          `json:"machine"`
		App        string          `json:"app"`
		Query      string          `json:"query"`
		Screenshot *bool           `json:"screenshot"`
		Script     string          `json:"script"`
		Steps      json.RawMessage `json:"steps"`
		Observe    json.RawMessage `json:"observe"`
		TimeoutMS  int             `json:"timeout_ms"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return proto.TextResult("bad arguments: "+err.Error(), true)
	}
	m, err := h.computerMachine(a.Machine)
	if err != nil {
		return proto.TextResult(err.Error(), true)
	}
	session := "persona-" + convID(sessionID)
	declined := func(what string) proto.ToolResult {
		h.logf("[%s] %s declined on the phone (%s)", m.cfg.ID, name, what)
		return proto.TextResult("The user declined this on the phone. Do not retry it unless they ask.", true)
	}
	apps, whole := callApps(name, a.App, a.Script, a.Steps)
	if name == "computer_state" {
		if _, ok := h.approveApps(ctx, m, name, apps, whole, args); !ok {
			return declined("look")
		}
		shot := a.Screenshot == nil || *a.Screenshot
		return h.computerState(ctx, m, session, a.App, a.Query, shot)
	}
	var tool string
	in := map[string]any{}
	switch {
	case a.Script != "" && len(a.Steps) > 0:
		return proto.TextResult("give script or steps, not both", true)
	case a.Script != "":
		tool, in["script"] = "run_script", a.Script
		if a.TimeoutMS > 0 {
			in["timeout_ms"] = a.TimeoutMS
		}
	case len(a.Steps) > 0:
		tool, in["steps"] = "run_steps", a.Steps
		if a.App != "" {
			in["app"] = a.App
		}
		if len(a.Observe) > 0 {
			in["observe"] = a.Observe
		}
	default:
		return proto.TextResult("give script (run_script) or steps (run_steps)", true)
	}
	what := computerSummary(tool, a.Script, a.Steps)
	asked, ok := h.approveApps(ctx, m, name, apps, whole, args)
	if !ok || (h.cua().Confirm && !asked && !h.approve(ctx, m.cfg.ID, name, "在「"+m.cfg.Name+"」上"+what, args)) {
		return declined(computerLogWhat(tool, a.Script, a.Steps))
	}
	start := time.Now()
	res, err := h.computerCall(ctx, m, session, tool, in)
	h.logf("[%s] computer_run for %s: %s, %v, %s", m.cfg.ID, convID(sessionID), computerLogWhat(tool, a.Script, a.Steps),
		time.Since(start).Round(time.Millisecond), map[bool]string{true: "failed", false: "ok"}[err != nil])
	if err != nil {
		return proto.TextResult(err.Error(), true)
	}
	return computerResult(res)
}

// computerMachine is the machine the assistant named, when the plugin is on for it, it is a Mac and it is online.
func (h *Hub) computerMachine(name string) (*Machine, error) {
	m := h.machineByName(strings.TrimSpace(name))
	if m == nil {
		return nil, fmt.Errorf("no machine %q (machines: %s)", name, h.computerMachineIDs())
	}
	if c := h.computerOf(m); c == nil || !c.Enabled {
		return nil, fmt.Errorf("operating %s is not turned on (on for: %s); the user turns it on on the machine's page in the app", m.cfg.ID, h.computerMachineIDs())
	}
	if !m.isOnline() {
		return nil, fmt.Errorf("%s is offline", m.cfg.ID)
	}
	return m, nil
}

func (h *Hub) computerMachineIDs() string {
	ids := h.cua().Machines
	if len(ids) == 0 {
		return "none"
	}
	return strings.Join(ids, ", ")
}

// computerState: one window of app (its accessibility tree, and its screenshot), or without app the whole screen and
// the apps and windows on it.
func (h *Hub) computerState(ctx context.Context, m *Machine, session, app, query string, shot bool) proto.ToolResult {
	if app == "" {
		tree, err := h.computerCall(ctx, m, session, "get_accessibility_tree", map[string]any{})
		if err != nil {
			return proto.TextResult(err.Error(), true)
		}
		out := computerResult(tree)
		if shot {
			desk, err := h.computerCall(ctx, m, session, "get_desktop_state", map[string]any{})
			if err != nil {
				return proto.TextResult(err.Error(), true)
			}
			if desk.Image != nil {
				out.Content = append(out.Content, proto.Content{Type: "image", Data: desk.Image.Data, MimeType: desk.Image.Mime})
			}
		}
		return out
	}
	// The window cua-driver's own lookup picks (the app's frontmost), then its state under the same session label, so
	// its element tokens work in computer_run.
	script := "const a = await cua.getApp(" + jsString(app) + "); return {pid: a.pid, window_id: a.windowId};"
	found, err := h.computerCall(ctx, m, session, "run_script", map[string]any{"script": script})
	if err != nil {
		return proto.TextResult(err.Error(), true)
	}
	var r struct {
		OK    bool `json:"ok"`
		Value struct {
			PID      int `json:"pid"`
			WindowID int `json:"window_id"`
		} `json:"value"`
	}
	if json.Unmarshal(found.Output, &r) != nil || !r.OK || r.Value.WindowID == 0 {
		res := computerResult(found)
		res.IsError = true
		return res
	}
	in := map[string]any{"pid": r.Value.PID, "window_id": r.Value.WindowID, "include_screenshot": shot}
	if query != "" {
		in["query"] = query
	}
	state, err := h.computerCall(ctx, m, session, "get_window_state", in)
	if err != nil {
		return proto.TextResult(err.Error(), true)
	}
	return computerResult(state)
}

// computerCall runs one Cua Driver tool on m's node.
func (h *Hub) computerCall(ctx context.Context, m *Machine, session, tool string, args map[string]any) (*proto.ComputerResult, error) {
	var res proto.ComputerResult
	if err := m.call(ctx, "computer.call", map[string]any{"session": session, "tool": tool, "args": args, "keep_awake": h.cua().awake()}, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// computerResult is a tool's answer for the model: its JSON (cut at computerMost), then its screenshot.
func computerResult(r *proto.ComputerResult) proto.ToolResult {
	text := r.Text
	if len(r.Output) > 0 {
		text = string(r.Output)
	}
	if len(text) > computerMost {
		cut := computerMost
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = text[:cut] + fmt.Sprintf("\n… (%d more bytes: narrow it with query)", len(text)-cut)
	}
	out := proto.TextResult(text, false)
	if r.Image != nil {
		out.Content = append(out.Content, proto.Content{Type: "image", Data: r.Image.Data, MimeType: r.Image.Mime})
	}
	return out
}

// computerSummary is what the confirmation card says: a script's first line, or the steps' actions.
func computerSummary(tool, script string, steps json.RawMessage) string {
	if tool == "run_script" {
		line, _, _ := strings.Cut(strings.TrimSpace(script), "\n")
		return "运行脚本：" + line
	}
	return "依次执行：" + strings.Join(stepActions(steps), "、")
}

// computerLogWhat is the log's account of a run: the kind and the actions, never what is typed or clicked.
func computerLogWhat(tool, script string, steps json.RawMessage) string {
	if tool == "run_script" {
		return fmt.Sprintf("run_script (%d bytes)", len(script))
	}
	return "run_steps [" + strings.Join(stepActions(steps), " ") + "]"
}

// stepActions are the tools of run_steps' steps: {"tool": "click", …} or {"click": {…}}; a check alone is "check".
func stepActions(steps json.RawMessage) []string {
	var list []map[string]json.RawMessage
	_ = json.Unmarshal(steps, &list)
	out := make([]string, 0, len(list))
	for _, s := range list {
		var tool string
		if raw, ok := s["tool"]; ok {
			_ = json.Unmarshal(raw, &tool)
		}
		for k := range s {
			if tool == "" && k != "args" && k != "wait_for" && k != "expect" && k != "tool" {
				tool = k
			}
		}
		if tool == "" {
			tool = "check"
		}
		out = append(out, tool)
	}
	return out
}

// jsString is s as a JavaScript string literal.
func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// convID is the start of a conversation's id: the Cua Driver session label of its calls, and how the log names it.
func convID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
