package node

// The assistant on an agent runtime (docs/runtimes.md「助手」; DSH first, docs/dsh.md「助手」): a runtime that declares
// the assistant capability runs the conversation, driven with the runtime protocol's own session calls. What is the
// assistant's stays here, as for Claude and Codex: the conversation's state (launcher, directory), the live snapshot
// each message starts with, the permission mode, the shared memory, and Feishu through the lark-connector tool (the token
// never leaves the node). Nothing in this file is any one runtime's.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/nwverse/persona/server/internal/identity"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/nwverse/persona/server/internal/assistantskills"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// assistantOnRuntime is assistantAgent's answer for a launcher a runtime offers as an assistant launcher.
const assistantOnRuntime = "runtime"

// assistantCall are the calls about an assistant conversation on a runtime that the assistant answers (node.handle).
var assistantCall = map[string]bool{"session.send": true, "session.interrupt": true, "session.close": true, "session.modes": true}

// gateToolTimeout bounds one run of a gated command as a runtime's tool (ol events waits at most 300 s by its own
// flags).
var gateToolTimeout = 6 * time.Minute

// assistantRuntime is the runtime offering launcher as an assistant launcher (caps.assistant), nil when none does or
// the login shell has a launcher of that name (it runs instead, as for session.start).
func (n *Node) assistantRuntime(launcher string) *runtimeConn {
	if launcher == "" || shellLauncherNames()[launcher] {
		return nil
	}
	h := n.runtimes.launcherRuntime(launcher)
	if h == nil || !h.assistant() {
		return nil
	}
	return h
}

// runtimeAssistant: id is an assistant conversation a runtime runs. Claude's and Codex's ids are uuids (validID); a
// runtime's are its own (DSH: session-<uuid>).
func (n *Node) runtimeAssistant(id string) bool {
	return id != "" && !validID(id) && runtimeIDRe.MatchString(id) && n.isAssistantSession(id)
}

// assistantSendRuntime is assistantSend on a runtime (h, nil when it is not connected): a new conversation is started
// there (session.start with assistant), then the message goes with the snapshot as its context, the conversation's
// permission mode and its memory directory; a new conversation's first message also carries the instructions only a
// runtime needs (memory, Feishu).
func (n *Node) assistantSendRuntime(ctx context.Context, h *runtimeConn, id, cwd, launcher, text string, images []proto.Image, mode string) (string, error) {
	st, known := n.state.get(id)
	if cwd == "" {
		cwd = st.Cwd
	}
	if cwd == "" {
		cwd = identity.AssistantDir(n.home)
	}
	if launcher == "" {
		launcher = st.Launcher
	}
	if h == nil && id != "" { // its own runtime: the one that has the conversation
		var label string
		if h, label, _ = n.runtimes.belongs(id); h == nil {
			if label == "" {
				label = launcher
			}
			return "", errRuntimeAway(label)
		}
	}
	if h == nil {
		return "", errRuntimeAway(launcher)
	}
	n.assistantRuntimeSkills(cwd)
	memDir := n.memoryDir(cwd)
	turn := map[string]any{"assistant": true, "memory_dir": memDir}
	instructions := ""
	if id == "" {
		s, err := n.runtimeStartWith(ctx, h, map[string]any{"cwd": cwd, "assistant": true})
		if err != nil {
			return "", err
		}
		id = s.SessionID
		instructions = n.runtimeInstructions(cwd, memDir)
	}
	n.noteModes(id, mode, "")
	if m := n.assistantModeOf(id); m != "" {
		turn["mode"] = m
	}
	if !known || st.Launcher != launcher || st.Cwd != cwd {
		if st.StartedAt.IsZero() {
			st.StartedAt = time.Now().UTC()
		}
		st.Launcher, st.Cwd = launcher, cwd
		n.state.put(id, st)
	}
	live, commit := n.turnContext(id, n.assistantContext(id))
	if instructions != "" {
		live = strings.TrimSpace(instructions + "\n" + live)
	}
	params := map[string]any{"session_id": id, "text": text, "images": images, "client_msg_id": newUUID()}
	for k, v := range turn {
		params[k] = v
	}
	if live != "" {
		params["context"] = live
	}
	cctx, cancel := context.WithTimeout(ctx, runtimeCallTimeout)
	defer cancel()
	if err := h.peer.Call(cctx, "session.send", params, nil); err != nil {
		return "", fmt.Errorf("%s: %w", h.label(), err)
	}
	commit()
	n.notifySessions()
	return id, nil
}

// assistantModeOf is the permission mode the conversation was last set to (session.modes, or a message's), "" none.
func (n *Node) assistantModeOf(id string) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.modes[id].Mode
}

// assistantInterruptRuntime stops the turn of an assistant conversation a runtime runs; nothing when it is away.
func (n *Node) assistantInterruptRuntime(ctx context.Context, id string) error {
	h, label, _ := n.runtimes.belongs(id)
	if h == nil {
		return errRuntimeAway(label)
	}
	cctx, cancel := context.WithTimeout(ctx, runtimeCallTimeout)
	defer cancel()
	return h.peer.Call(cctx, "session.interrupt", map[string]any{"session_id": id}, nil)
}

// runtimeStartWith starts a session on h with params (cwd and the rest) and lists it right away (runtimeStart).
func (n *Node) runtimeStartWith(ctx context.Context, h *runtimeConn, params map[string]any) (*proto.Session, error) {
	cctx, cancel := context.WithTimeout(ctx, runtimeCallTimeout)
	defer cancel()
	var res struct {
		Session proto.Session `json:"session"`
	}
	if err := h.peer.Call(cctx, "session.start", params, &res); err != nil {
		return nil, err
	}
	return n.listStarted(h, res.Session)
}

// runtimeInstructions is the first message's instructions for an assistant on a runtime: the memory it shares with
// Claude and Codex (memory.go), and the skills' gated commands it runs as tools here.
func (n *Node) runtimeInstructions(cwd, memDir string) string {
	r := prompts.RuntimeInstructions{Dir: memDir}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, g := range n.assistantLark(ctx).Gates {
		if g.Tool != nil && n.gateScript(cwd, &g) != "" {
			r.Gates = append(r.Gates, prompts.RuntimeGate{Title: g.Title, Command: g.Command, Tool: g.Tool.Name})
		}
	}
	index, _ := os.ReadFile(filepath.Join(memDir, "MEMORY.md"))
	if len(index) > 24*1024 {
		r.Index, r.Truncated = strings.TrimLeftFunc(string(index[:24*1024]), unicode.IsSpace), true
	} else {
		r.Index = strings.TrimSpace(string(index))
	}
	return prompts.RenderRuntimeInstructions(r)
}

// assistantRuntimeSkills puts the assistant's skills where agent runtimes look for a project's (<cwd>/.agents/skills),
// beside Claude's (.claude/skills, assistantLarkEnv).
func (n *Node) assistantRuntimeSkills(cwd string) {
	if err := assistantskills.Sync(filepath.Join(cwd, ".agents", "skills"), assistantskills.UserDir(), n.signedInConnectors(), n.logger.Printf); err != nil {
		n.logger.Printf("assistant: skills: %v", err)
	}
}

// gateScript is a command gate's script in the assistant's directory, "" when its skill is not there.
func (n *Node) gateScript(cwd string, g *proto.CommandGate) string {
	if g == nil || g.Script == "" {
		return ""
	}
	for _, dir := range []string{filepath.Join(cwd, ".agents", "skills"), filepath.Join(cwd, ".claude", "skills")} {
		p := filepath.Join(dir, filepath.FromSlash(g.Script))
		if strings.HasPrefix(filepath.Clean(p), filepath.Clean(dir)+string(os.PathSeparator)) && fileExists(p) {
			return p
		}
	}
	return ""
}

// gateTool is the command gate offering the runtime tool name, nil when none does.
func (n *Node) gateTool(name string) *proto.CommandGate {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, g := range n.assistantLark(ctx).Gates {
		if g.Tool != nil && g.Tool.Name == name {
			return &g
		}
	}
	return nil
}

// gateTools are the runtime tools of the command gates whose skill is on this machine (the user's skills, or already
// in the assistant's directory).
func (n *Node) gateTools() []proto.Tool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var out []proto.Tool
	for _, g := range n.assistantLark(ctx).Gates {
		if g.Tool == nil || g.Tool.Name == "" {
			continue
		}
		if !fileExists(filepath.Join(assistantskills.UserDir(), filepath.FromSlash(g.Script))) && n.gateScript(identity.AssistantDir(n.home), &g) == "" {
			continue
		}
		out = append(out, proto.Tool{Name: g.Tool.Name, Description: g.Tool.Description, InputSchema: gateToolSchema(g)})
	}
	return out
}

func gateToolSchema(g proto.CommandGate) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"type": "object", "required": []string{"args"}, "properties": map[string]any{
		"args": map[string]any{"type": "array", "items": map[string]any{"type": "string"},
			"description": "The words after " + g.Command + ": METHOD PATH [JSON body] and its flags, one per item"}}})
	return raw
}

// assistantGateTool runs a gated command for an assistant conversation on a runtime: the gate decides first, as for a
// Bash call of Claude's (larkCheck: reads go, writes are confirmed on the phone, the configured rules deny), then the
// skill's script runs here with the gate's credentials in its environment only.
func (n *Node) assistantGateTool(ctx context.Context, g *proto.CommandGate, session string, raw json.RawMessage) proto.ToolResult {
	var a struct {
		Args []string `json:"args"`
	}
	if json.Unmarshal(raw, &a) != nil || len(a.Args) == 0 {
		return proto.TextResult(`args: the words after `+g.Command+`, e.g. ["GET", "/me"]`, true)
	}
	st, _ := n.state.get(session)
	cwd := st.Cwd
	if cwd == "" {
		cwd = identity.AssistantDir(n.home)
	}
	env, release := n.assistantLarkEnv(cwd) // also syncs the skills into .claude/skills
	defer release()                         // this run's address at the gate proxy
	script := n.gateScript(cwd, g)
	if script == "" {
		return proto.TextResult("这台电脑上没有 "+g.Command+" 所在的技能（"+g.Script+"）", true)
	}
	words := make([]string, len(a.Args))
	for i, w := range a.Args {
		words[i] = shellQuote(w)
	}
	command := g.Command + " " + strings.Join(words, " ")
	ix := proto.Interaction{SessionID: session, Cwd: cwd, CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(HookTimeout).UTC(),
		ToolName: g.Tool.Name, SessionTitle: filepath.Base(cwd)}
	if s, _, ok := n.runtimes.rowOf(session); ok && s.Title != "" {
		ix.SessionTitle = s.Title
	}
	decision, reason, err := n.larkCheck(ctx, session, cwd, command, ix)
	if err != nil {
		return proto.TextResult("没有执行："+err.Error(), true)
	}
	if decision == "deny" {
		return proto.TextResult("没有执行："+reason, true)
	}
	rctx, cancel := context.WithTimeout(ctx, gateToolTimeout)
	defer cancel()
	cmd := exec.CommandContext(rctx, "bash", append([]string{script}, a.Args...)...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	text := string(out)
	if len(text) > 64*1024 {
		text = text[:64*1024] + "\n…（输出太长，后面的没有放进来；用 -o 写到文件里再读）"
	}
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return proto.TextResult(g.Command+": "+err.Error(), true)
	}
	return proto.TextResult(text, err != nil)
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
