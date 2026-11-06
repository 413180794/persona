package node

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/nwverse/persona/server/internal/identity"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// SocketPath is where hook and mcp subprocesses reach the node.
func SocketPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(identity.SocketDir(home), "node.sock")
}

// HookTimeout is how long a hook waits for the phone before handing the dialog back to the terminal.
var HookTimeout = 30 * time.Minute

// ResumeWait is how long a hook whose node went away mid-wait (a hub or node restart) keeps reconnecting to the next
// node; the hub holds its interaction at least that long (hub.resumeGrace).
var ResumeWait = 2 * time.Minute

// ErrNoNode: nothing listens on the node socket (no hub connected to this machine).
var ErrNoNode = errors.New("no jarvis node")

type sockRequest struct {
	Kind      string          `json:"kind"`
	Input     json.RawMessage `json:"input,omitempty"`
	Pane      string          `json:"pane,omitempty"`
	Name      string          `json:"name,omitempty"`
	Args      json.RawMessage `json:"args,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Assistant bool            `json:"assistant,omitempty"` // the caller is the assistant's MCP server
	// Attempt is the hook process's own id, the same on each reconnect; Resume marks those reconnects.
	Attempt string `json:"attempt,omitempty"`
	Resume  bool   `json:"resume,omitempty"`
	// AgentPID / AgentStart: the claude the hook runs under and when it started (hookAgent); 0 / "" when not found.
	AgentPID   int    `json:"agent_pid,omitempty"`
	AgentStart string `json:"agent_start,omitempty"`
	// ReportOnly: the hook is one of the global report-only entries (globalhooks.go), which never ask the phone.
	ReportOnly bool `json:"report_only,omitempty"`
}

func (n *Node) listenHookSocket(ctx context.Context) (net.Listener, error) {
	ln, err := n.listenOnce(ctx)
	if err != nil {
		return nil, err
	}
	// A node from an earlier hub connection may still be shutting down; if its cleanup (or anything else) removes
	// our socket file, hooks silently fall back to the terminal. Re-create it whenever it disappears, or when another
	// file took its place that nothing answers on (a node that started, listened and exited without unlinking: every
	// `jarvisd launch` then failed with "connection refused" and the assistant's messages stayed undelivered).
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			if why := n.sockGone(); why != "" {
				n.logger.Printf("hook socket %s, re-creating", why)
				if _, err := n.listenOnce(ctx); err != nil {
					n.logger.Printf("hook socket: %v", err)
				}
			}
		}
	}()
	return ln, nil
}

// sockGone says why the node socket needs re-creating, "" when it does not: the file is gone, or it is another file
// than the one this node listens on and nothing answers there. A live one is left alone: a newer node owns it.
func (n *Node) sockGone() string {
	fi, err := os.Stat(SocketPath())
	if os.IsNotExist(err) {
		return "vanished"
	}
	if err != nil {
		return ""
	}
	n.sockMu.Lock()
	mine := n.sock != nil && os.SameFile(fi, n.sock)
	n.sockMu.Unlock()
	if mine {
		return ""
	}
	c, err := net.DialTimeout("unix", SocketPath(), time.Second)
	if err != nil {
		return "replaced by one nothing answers on"
	}
	c.Close()
	return ""
}

func (n *Node) listenOnce(ctx context.Context) (net.Listener, error) {
	path := SocketPath()
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	// Never unlink on close: by then the path may belong to a newer node.
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = os.Chmod(path, 0o600)
	if fi, err := os.Stat(path); err == nil {
		n.sockMu.Lock()
		n.sock = fi
		n.sockMu.Unlock()
	}
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go n.serveSock(ctx, c)
		}
	}()
	return ln, nil
}

func (n *Node) serveSock(ctx context.Context, c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	line, err := r.ReadBytes('\n')
	if err != nil {
		return
	}
	var req sockRequest
	if json.Unmarshal(line, &req) != nil {
		return
	}
	req.SessionID = n.resolveAssistantKey(req.SessionID)
	// The caller going away (hook killed because the user answered in the terminal) cancels the request.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		_, _ = r.ReadByte()
		cancel()
	}()

	var resp any
	switch req.Kind {
	case "hook":
		out, err := n.handleHook(ctx, req)
		if errors.Is(err, errNodeGone) {
			// This node is going away under a pending question: the hook asks the next one (runHook).
			resp = map[string]any{"retry": true}
		} else {
			resp = map[string]any{"output": out}
		}
	case "hook.decided": // another PermissionRequest hook answered a call (hookdecided.go)
		var in hookInput
		if json.Unmarshal(req.Input, &in) == nil && in.HookEventName == "PermissionRequest" && validID(in.SessionID) {
			n.decided.decide(in, time.Now())
		}
		resp = map[string]any{"ok": true}
	case "ping": // personad node doctor: a round trip that changes nothing
		resp = map[string]any{"ok": true}
	case "ask": // personad ask (scriptcli.go)
		resp = n.handleAsk(ctx, req)
	case "notify": // personad notify (scriptcli.go)
		resp = n.handleNotify(ctx, req)
	case "launcher.env": // jarvisd launch <name>
		resp = n.launcherEnv(req.Name)
	case "mcp.tools":
		var res struct {
			Tools []proto.Tool `json:"tools"`
		}
		if err := n.peer.Call(ctx, "mcp.tools", map[string]any{"assistant": req.Assistant, "session_id": req.SessionID}, &res); err != nil {
			resp = map[string]any{"tools": []proto.Tool{}, "error": err.Error()}
		} else {
			resp = res
		}
	case "cli.call": // personad tg: the node, not the caller, says whether it is the assistant (clicall.go)
		resp = n.cliCall(ctx, c, req)
	case "mcp.call":
		var res proto.ToolResult
		if err := n.peer.Call(ctx, "mcp.call", map[string]any{"name": req.Name, "args": req.Args, "session_id": req.SessionID,
			"assistant": req.Assistant}, &res); err != nil {
			res = proto.TextResult("Persona hub error: "+err.Error(), true)
		}
		resp = res
	default:
		resp = map[string]any{"error": "unknown kind"}
	}
	b, _ := json.Marshal(resp)
	_, _ = c.Write(append(b, '\n'))
}

type hookInput struct {
	HookEventName        string          `json:"hook_event_name"`
	SessionID            string          `json:"session_id"`
	TranscriptPath       string          `json:"transcript_path"`
	Cwd                  string          `json:"cwd"`
	PermissionMode       string          `json:"permission_mode"`
	ToolName             string          `json:"tool_name"`
	ToolInput            json.RawMessage `json:"tool_input"`
	ToolUseID            string          `json:"tool_use_id"`
	Message              string          `json:"message"`
	LastAssistantMessage string          `json:"last_assistant_message"`
	StopHookActive       bool            `json:"stop_hook_active"`
	Prompt               string          `json:"prompt"`
	AgentID              string          `json:"agent_id"`
	AgentType            string          `json:"agent_type"`
	MCPServerName        string          `json:"mcp_server_name"`
	MCPConfigSource      string          `json:"mcp_config_source"`
	MCPServer            *struct {
		Name   string `json:"name"`
		Source string `json:"source"`
	} `json:"mcp_server"`
	Mode            string          `json:"mode"`
	URL             string          `json:"url"`
	ElicitationID   string          `json:"elicitation_id"`
	RequestedSchema json.RawMessage `json:"requested_schema"`
}

// errNodeGone: the hub connection ended while a hook waited (the node is going away).
var errNodeGone = errors.New("node going away")

// interactionID is the same for every attempt of one hook call, so a hook that reconnects after a restart finds
// its interaction again: the tool call it is about, else the hook process's own id.
func interactionID(in hookInput, attempt string) string {
	key := in.SessionID + "\x00" + in.HookEventName + "\x00" + in.ToolName + "\x00"
	switch {
	case in.HookEventName == "Elicitation":
		// One tool can ask multiple questions; only this hook attempt identifies a form request.
		if attempt == "" {
			return "ix_" + newUUID()[:12]
		}
		key += in.MCPServerName + "\x00" + in.ElicitationID + "\x00attempt:" + attempt
	case in.ToolUseID != "":
		key += in.ToolUseID
	case attempt != "":
		key += "attempt:" + attempt
	default: // an older hook that does not reconnect
		return "ix_" + newUUID()[:12]
	}
	sum := sha256.Sum256([]byte(key))
	return "ix_" + hex.EncodeToString(sum[:])[:16]
}

// handleHook returns the JSON Claude Code should read from the hook's stdout, or nil to let the TUI decide;
// errNodeGone when this node lost the hub before the phone answered.
func (n *Node) handleHook(ctx context.Context, req sockRequest) (any, error) {
	var in hookInput
	if json.Unmarshal(req.Input, &in) != nil {
		return nil, nil
	}
	if req.ReportOnly && !n.takesReport(in) {
		return nil, nil // before the counters: a session the plugin reports for would count each prompt twice
	}
	if repair := n.assistantViewStop(ctx, req, in); repair != nil {
		return repair, nil
	}
	stopSig := n.recordHookTurn(req, in, time.Now())
	if n.peer == nil {
		return nil, nil
	}
	if req.ReportOnly {
		n.rememberTranscript(in)
		n.notifyHookEvent(in, stopSig, true)
		return nil, nil
	}
	// An Orca pane is bound only on a machine whose engine is Orca: elsewhere nothing could drive it, and the hook
	// reported no pane for it before Orca was a host (a herdr machine behaves as it did).
	if host.ParsePane(req.Pane).Kind != host.Orca || n.engine() == proto.EngineOrca {
		n.bindPane(req.Pane, in.SessionID)
	}
	if in.HookEventName == "SessionStart" {
		n.resumeOnSessionStart(req.Pane, in.SessionID) // herdr's restore follows it to the new session (herdrresume.go)
	}
	n.rememberTranscript(in)
	if !req.Resume { // the event was reported on the first attempt
		n.notifyHookEvent(in, stopSig, false)
	}

	ix := proto.Interaction{
		ID: interactionID(in, req.Attempt), SessionID: in.SessionID, Cwd: in.Cwd, CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().Add(HookTimeout).UTC(), ToolName: in.ToolName, ToolUseID: in.ToolUseID, ToolInput: in.ToolInput,
		Resumable: req.Attempt != "",
		AgentID:   in.AgentID, AgentType: in.AgentType, MCPServerName: in.MCPServerName, MCPConfigSource: in.MCPConfigSource,
	}
	if in.MCPServer != nil {
		ix.MCPServerName, ix.MCPConfigSource = in.MCPServer.Name, in.MCPServer.Source
	}
	if p := n.transcriptPath(in.SessionID); p != "" {
		if st, err := os.Stat(p); err == nil {
			ix.SessionTitle = n.meta(p, st).title
		}
	}
	if ix.SessionTitle == "" {
		ix.SessionTitle = filepath.Base(in.Cwd)
	}
	if asksPhone(in) {
		var stopWatch func()
		ctx, stopWatch = n.watchAgent(ctx, req)
		defer stopWatch()
	}

	switch {
	case in.HookEventName == "Elicitation":
		if !elicitationInteraction(in, &ix) {
			return nil, nil
		}
		res, ok, err := n.ask(ctx, ix)
		if err != nil || !ok {
			return nil, err
		}
		return elicitationOutput(ix, res), nil

	case in.HookEventName == "PreToolUse" && in.ToolName == "Bash":
		if !n.isAssistantSession(in.SessionID) {
			return nil, nil
		}
		return n.larkGate(ctx, in, ix)

	case in.HookEventName == "PreToolUse" && in.ToolName == "AskUserQuestion":
		var q struct {
			Questions []struct {
				Question    string                 `json:"question"`
				Header      string                 `json:"header"`
				MultiSelect bool                   `json:"multiSelect"`
				Options     []proto.QuestionOption `json:"options"`
			} `json:"questions"`
		}
		if json.Unmarshal(in.ToolInput, &q) != nil || len(q.Questions) == 0 {
			return nil, nil
		}
		ix.Kind = proto.IxQuestion
		for _, qq := range q.Questions {
			ix.Questions = append(ix.Questions, proto.Question{Question: qq.Question, Header: qq.Header, MultiSelect: qq.MultiSelect, Options: qq.Options})
		}
		// Answers are keyed by question text: refuse alike-worded questions instead of silently dropping one answer.
		if proto.HasDuplicateQuestionText(ix.Questions) {
			return preToolDecision("deny", duplicateQuestionReason), nil
		}
		res, ok, err := n.ask(ctx, ix)
		if err != nil || !ok || len(res.Answers) == 0 {
			return nil, err
		}
		var input map[string]any
		_ = json.Unmarshal(in.ToolInput, &input)
		input["answers"] = res.Answers
		return map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName": "PreToolUse", "permissionDecision": "allow", "updatedInput": input,
		}}, nil

	case in.HookEventName == "PermissionRequest" && in.ToolName == "ExitPlanMode":
		var p struct {
			Plan string `json:"plan"`
		}
		_ = json.Unmarshal(in.ToolInput, &p)
		ix.Kind, ix.Plan = proto.IxPlan, p.Plan
		res, ok, err := n.ask(ctx, ix)
		if err != nil || !ok || res.Approve == nil {
			return nil, err
		}
		if !*res.Approve {
			msg := "用户在手机上打回了这个计划。"
			if strings.TrimSpace(res.Feedback) != "" {
				msg += "修改意见：" + res.Feedback
			}
			return permissionDecision(map[string]any{"behavior": "deny", "message": msg}), nil
		}
		mode := res.Mode
		if mode == "" {
			mode = "bypassPermissions"
		}
		var input map[string]any
		_ = json.Unmarshal(in.ToolInput, &input)
		// A bare allow does not close the plan dialog; it needs updatedInput plus a setMode update (2.1.310).
		return permissionDecision(map[string]any{
			"behavior": "allow", "updatedInput": input,
			"updatedPermissions": []any{map[string]any{"type": "setMode", "mode": mode, "destination": "session"}},
		}), nil

	case in.HookEventName == "PermissionRequest":
		ix.Kind, ix.Summary = proto.IxPermission, toolSummary(in.ToolName, in.ToolInput)
		// The session rule "Always allow in this session" adds: the whole tool, but for an assistant conversation's file tools only the
		// file's directory, so allowing one read does not open every file on the machine (#3282).
		rule := map[string]any{"toolName": in.ToolName}
		if n.isAssistantSession(in.SessionID) {
			alwaysKey := "tool:" + in.ToolName
			if scope := fileRuleScope(in.ToolName, in.ToolInput); scope != "" {
				// Claude Code matches path rules on Read and Edit only; Edit(path) covers every file-editing tool,
				// a Write(path) rule is never matched.
				tool := "Edit"
				if in.ToolName == "Read" {
					tool = "Read"
				}
				rule["toolName"], rule["ruleContent"] = tool, scope
				alwaysKey = "tool:" + tool + "(" + scope + ")"
			}
			// What the session rule below allows, kept by the hub past this process (a resumed one has no rules).
			ix.AlwaysKeys = []string{alwaysKey}
		}
		// Another hook may answer it first (hookdecided.go): then nothing is shown, or what was is withdrawn.
		ctx, answered, stop := n.decided.watch(ctx, permissionKey(in))
		defer stop()
		if n.decided.grace(ctx, answered) {
			return nil, nil
		}
		res, ok, err := n.ask(ctx, ix)
		if answered() {
			return nil, nil
		}
		if err != nil || !ok || res.Allow == nil {
			return nil, err
		}
		if !*res.Allow {
			msg := res.Message
			if msg == "" {
				msg = "用户在手机上拒绝了这次调用。"
			}
			return permissionDecision(map[string]any{"behavior": "deny", "message": msg}), nil
		}
		var input map[string]any
		_ = json.Unmarshal(in.ToolInput, &input)
		d := map[string]any{"behavior": "allow", "updatedInput": input}
		if res.Always {
			d["updatedPermissions"] = []any{map[string]any{
				"type": "addRules", "rules": []any{rule},
				"behavior": "allow", "destination": "session",
			}}
		}
		return permissionDecision(d), nil
	}
	return nil, nil
}

// recordHookTurn feeds the session's turn counters (hookturn.go) from a hook event and returns the signature of the
// reply a Stop ended the turn with. A subagent's events and a reconnecting hook's repeat do not count: they are not
// the main agent's turns.
func (n *Node) recordHookTurn(req sockRequest, in hookInput, now time.Time) (stopSig string) {
	if req.Resume || in.AgentID != "" || !validID(in.SessionID) {
		return ""
	}
	n.hooks.agent(in.SessionID, req.AgentPID, req.AgentStart, now) // before counting: a new process starts from zero
	switch in.HookEventName {
	case "UserPromptSubmit":
		n.hooks.prompt(in.SessionID, now)
	case "Stop":
		n.hooks.stop(in.SessionID, now)
		return stopSignature(in.LastAssistantMessage)
	}
	return ""
}

// rememberTranscript keeps the transcript path a hook names for its session.
func (n *Node) rememberTranscript(in hookInput) {
	if in.TranscriptPath == "" || !validID(in.SessionID) {
		return
	}
	n.mu.Lock()
	n.paths[in.SessionID] = in.TranscriptPath
	delete(n.pathsAt, in.SessionID) // Claude said so: no need to look again
	n.mu.Unlock()
}

// notifyHookEvent tells the hub what a hook saw. reportOnly marks an event from a global hook (globalhooks.go), so
// that the hub does not take the session for one with Persona's hooks, which would hold its screen cards back for a
// hook card that never comes (hub/machine.go, hasHooks). stopSig lets the hub tell a second completion of the same
// reply from a new one.
func (n *Node) notifyHookEvent(in hookInput, stopSig string, reportOnly bool) {
	p := map[string]any{
		"session_id": in.SessionID, "event": in.HookEventName, "tool_name": in.ToolName,
		"message": in.Message, "last_assistant_message": snippet(in.LastAssistantMessage), "cwd": in.Cwd,
		"stop_sig": stopSig,
	}
	if reportOnly {
		p["report_only"] = true
	}
	_ = n.peer.Notify("hook.event", p)
}

// reportedEvent: what a global report-only hook (globalhooks.go) may tell the hub; the tools are the cards.
func reportedEvent(in hookInput) bool {
	switch in.HookEventName {
	case "SessionStart", "UserPromptSubmit", "Stop", "Notification":
		return true
	case "PreToolUse", "PostToolUse":
		return in.ToolName == "AskUserQuestion" || in.ToolName == "ExitPlanMode"
	}
	return false
}

// takesReport says whether an event from a global report-only hook is for the hub and the counters. A session Persona
// started or the assistant reports through its own hooks (hooksplugin.go), so it is not taken twice. A report-only
// event never binds a pane (a claude that a tmux server started inherits the pane's variable without being in it, and
// would take the pane from the claude that is; the node finds the session of a pane by itself, sessions.go).
func (n *Node) takesReport(in hookInput) bool {
	if !validID(in.SessionID) || !reportedEvent(in) {
		return false
	}
	_, ours := n.state.get(in.SessionID)
	return !ours && !n.isAssistantSession(in.SessionID)
}

func permissionDecision(d map[string]any) any {
	return map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PermissionRequest", "decision": d}}
}

// duplicateQuestionReason goes back to the model when two questions share one text, telling it how to ask again.
const duplicateQuestionReason = "这次提问里有两道题的题干完全一样，我没法把两道题的答案分别交给你。" +
	"请把每道题改成不同的问法，或者合并成一道多选题，然后重新调用 AskUserQuestion。"

// agentWatch is how often a hook waiting on the phone looks whether the claude it runs under still lives.
var agentWatch = 2 * time.Second

// agentGone says the claude seen with that start is surely gone (procGone); a variable so tests can say who lives.
var agentGone = procGone

// asksPhone: the hook events that may wait on the phone (the cases of handleHook that call ask).
func asksPhone(in hookInput) bool {
	switch in.HookEventName {
	case "Elicitation", "PermissionRequest":
		return true
	case "PreToolUse":
		return in.ToolName == "AskUserQuestion" || in.ToolName == "Bash"
	}
	return false
}

// watchAgent ends ctx once the claude the hook runs under is gone (killed, crashed, out of memory). Claude Code starts
// its hooks in their own process group on Linux and macOS, so a claude that dies without cleaning up leaves its hook
// waiting, and the card on the phone, until HookTimeout (#4344). Ending ctx withdraws the card the way the hook going
// away does (ask sends interaction.cancel). Gone takes two looks in a row, so one odd read does not withdraw the card
// of a claude still waiting. Not watched: a hook whose claude was not found (AgentPID 0), or one already gone when it
// asks (a pid from another pid namespace reads as gone). stop also waits for the watcher.
func (n *Node) watchAgent(ctx context.Context, req sockRequest) (_ context.Context, stop func()) {
	if req.AgentPID <= 0 || agentGone(req.AgentPID, req.AgentStart) {
		return ctx, func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		t := time.NewTicker(agentWatch)
		defer t.Stop()
		misses := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			if !agentGone(req.AgentPID, req.AgentStart) {
				misses = 0
				continue
			}
			if misses++; misses >= 2 {
				cancel()
				return
			}
		}
	}()
	return ctx, func() { cancel(); <-exited }
}

// ask forwards an interaction to the hub and blocks until the phone answers, it expires, or the hook is cancelled.
// It fails with errNodeGone when the hub connection ends first: a resumable hook then asks the next node.
func (n *Node) ask(ctx context.Context, ix proto.Interaction) (proto.Resolution, bool, error) {
	if n.peer == nil { // no hub: nobody to ask
		return proto.Resolution{}, false, nil
	}
	ctx, cancel := context.WithDeadline(ctx, ix.ExpiresAt)
	defer cancel()
	var res proto.Resolution
	err := n.peer.Call(ctx, "interaction.create", map[string]any{"interaction": ix}, &res)
	if err != nil {
		select {
		case <-n.peer.Done():
			if ix.Resumable {
				return res, false, errNodeGone
			}
			return res, false, nil
		default:
		}
		if errors.Is(err, rpc.ErrClosed) {
			return res, false, errNodeGone
		}
		_ = n.peer.Notify("interaction.cancel", map[string]any{"id": ix.ID})
		return res, false, nil
	}
	if res.Dismiss {
		return res, false, nil
	}
	return res, true, nil
}

// fileRuleScope is the permission rule content for a file tool's call: its file's directory and below
// ("//abs/dir/**"), or "" for another tool or a path that is not absolute.
func fileRuleScope(name string, input json.RawMessage) string {
	switch name {
	case "Read", "Edit", "Write", "MultiEdit", "NotebookEdit":
	default:
		return ""
	}
	var a struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
	}
	_ = json.Unmarshal(input, &a)
	p := a.FilePath
	if p == "" {
		p = a.NotebookPath
	}
	if p == "" || !filepath.IsAbs(p) {
		return ""
	}
	dir := filepath.ToSlash(filepath.Dir(p))
	if !strings.HasPrefix(dir, "/") {
		dir = "/" + dir // a Windows drive path
	}
	return "/" + strings.TrimSuffix(dir, "/") + "/**"
}

func toolSummary(name string, input json.RawMessage) string {
	var m map[string]any
	_ = json.Unmarshal(input, &m)
	pick := func(keys ...string) string {
		for _, k := range keys {
			if s, ok := m[k].(string); ok && s != "" {
				return s
			}
		}
		return ""
	}
	switch name {
	case "Bash":
		return pick("command")
	case "Edit", "Write", "Read", "MultiEdit", "NotebookEdit":
		return pick("file_path", "notebook_path")
	case "WebFetch":
		return pick("url")
	case "WebSearch":
		return pick("query")
	}
	if s := pick("description", "command", "path", "url", "query", "prompt"); s != "" {
		return s
	}
	// The summary is a push's body, shown on the lock screen: never an argument named like a credential (#4734).
	if s := proto.ArgGist(input); s != "" {
		return snippet(s)
	}
	if s := proto.RedactArgs(input); s != "" && s != "{}" && s != "null" {
		return snippet(s)
	}
	return name
}

// HookPane is the pane a hook runs in, from its environment, as a stored PaneRef: herdr's HERDR_PANE_ID as it always
// was, else herdr-core's HC_TERMINAL_ID ("herdr-core:t7"), else Orca's ORCA_PANE_KEY (or its older ORCA_AGENT_PANE:
// "orca:<tabId>:<leafId>"); "" in none.
func HookPane(getenv func(string) string) string {
	if p := getenv(herdrPaneEnv); p != "" {
		return p
	}
	if t := getenv(herdrCorePaneEnv); t != "" {
		return host.PaneRef{Kind: host.HerdrCore, ID: t}.String()
	}
	for _, v := range []string{orcaPaneEnv, orcaLegacyPaneEnv} {
		if k := getenv(v); k != "" {
			return host.PaneRef{Kind: host.Orca, ID: k}.String()
		}
	}
	return ""
}

// Hook hands a Claude Code hook's input to the node and returns what the hook should print (nil: let the TUI decide).
// When the node goes away while the phone is deciding (a hub or node restart) it reconnects to the next node with the
// same request for up to ResumeWait: the interaction keeps its id, and the hub keeps it pending through the gap.
// No node at all: nil at once, the TUI asks as usual.
func Hook(ctx context.Context, input json.RawMessage, pane string) json.RawMessage {
	req := sockRequest{Kind: "hook", Input: input, Pane: pane, Attempt: newUUID()}
	req.AgentPID, req.AgentStart = hookAgentOf(input)
	// A live node answers by the interaction's expiry; a socket that is there but never answers must not hold Claude.
	ctx, cancel := context.WithTimeout(ctx, HookTimeout+ResumeWait)
	defer cancel()
	line, err := dialResumable(ctx, DialSocket, req)
	if err != nil {
		return nil
	}
	var resp struct {
		Output json.RawMessage `json:"output"`
	}
	_ = json.Unmarshal(line, &resp)
	if len(resp.Output) == 0 || string(resp.Output) == "null" {
		return nil
	}
	return resp.Output
}

// dialResumable sends req to the node and returns its answer line. When the node goes away while the phone is deciding
// (it answers {"retry":true}, or the connection breaks) it reconnects to the next one with the same request, marked
// Resume, for up to ResumeWait. The error is ErrNoNode when nothing listens at the first try.
func dialResumable(ctx context.Context, dial Dialer, req sockRequest) ([]byte, error) {
	line, err := dial(ctx, req)
	if errors.Is(err, ErrNoNode) {
		return nil, err
	}
	var until time.Time
	for {
		var rep struct {
			Retry bool `json:"retry"`
		}
		if err == nil && json.Unmarshal(line, &rep) == nil && !rep.Retry {
			return line, nil
		}
		if until.IsZero() {
			until = time.Now().Add(ResumeWait)
		}
		if time.Now().After(until) {
			if err == nil {
				err = errors.New("the node went away")
			}
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(resumeRetry):
		}
		req.Resume = true
		line, err = dial(ctx, req)
	}
}

// hookAgentOf finds the claude this hook process runs under (hookAgent), by the session id in its input.
func hookAgentOf(input json.RawMessage) (pid int, start string) {
	var who struct {
		SessionID string `json:"session_id"`
	}
	_ = json.Unmarshal(input, &who)
	home, _ := os.UserHomeDir()
	return hookAgent(who.SessionID, os.Getppid(), claudeDirOf(home), parentPID)
}

// reportTimeout is how long a report-only hook waits for the node; it asks for nothing, so a node that does not
// answer at once is not worth holding the process for.
const reportTimeout = 3 * time.Second

// ReportHook hands a global report-only hook's input to the node and returns at once when there is nothing to report
// to: not in a pane Persona knows, or no node socket. It prints nothing either way (the entries are async).
func ReportHook(ctx context.Context, input json.RawMessage, pane string) {
	if pane == "" {
		return
	}
	if _, err := os.Stat(SocketPath()); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, reportTimeout)
	defer cancel()
	req := sockRequest{Kind: "hook", Input: input, Pane: pane, ReportOnly: true}
	req.AgentPID, req.AgentStart = hookAgentOf(input) // the counters need it as much as a plugin hook's do
	_, _ = DialSocket(ctx, req)
}

// resumeRetry is how often a resuming hook tries the node socket.
var resumeRetry = time.Second

// DialSocket sends one request to the local node and returns the raw response line. It fails with ErrNoNode when no
// node listens.
func DialSocket(ctx context.Context, req any) ([]byte, error) {
	return dialSocketAt(ctx, SocketPath(), req)
}

// DialSocketInterruptible is DialSocket that also hangs up when ctx is cancelled (Ctrl-C in personad ask): the wait
// ends at once and, with the connection, the node withdraws the card.
func DialSocketInterruptible(ctx context.Context, req any) ([]byte, error) {
	return dialSocket(ctx, SocketPath(), req, true)
}

// dialSocketAt is DialSocket for the node socket at path.
func dialSocketAt(ctx context.Context, path string, req any) ([]byte, error) {
	return dialSocket(ctx, path, req, false)
}

func dialSocket(ctx context.Context, path string, req any, hangUpOnCancel bool) ([]byte, error) {
	var d net.Dialer
	c, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoNode, err)
	}
	defer c.Close()
	if hangUpOnCancel {
		defer context.AfterFunc(ctx, func() { c.Close() })()
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = c.SetDeadline(dl)
	}
	b, _ := json.Marshal(req)
	if _, err := c.Write(append(b, '\n')); err != nil {
		return nil, err
	}
	line, err := bufio.NewReader(c).ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("node closed connection: %w", err)
	}
	return line, nil
}
