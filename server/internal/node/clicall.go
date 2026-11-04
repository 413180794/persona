package node

import (
	"context"
	"maps"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nwverse/persona/server/internal/assistantskills"
	"github.com/nwverse/persona/server/internal/proto"
)

// A connector's command line (personad tool, internal/toolcli) reaches the hub through the node's socket like the MCP
// server, but the node, not the caller, says whether the assistant is asking: only a process the node started for the assistant,
// or one of its descendants (its Bash tool), gets the assistant's tools. Any other caller (an agent, a program run by
// hand) is refused; so is a system where the other end of a socket cannot be known.

// ancestorsMost bounds the walk up the process tree.
const ancestorsMost = 64

// assistantCaller is the assistant conversation whose process is c's peer or an ancestor of it.
func (n *Node) assistantCaller(c net.Conn) (string, bool) {
	pid, err := peerPID(c)
	if err != nil {
		return "", false
	}
	return n.assistantOf(pid, parentPID)
}

// assistantOf is the assistant conversation whose process is pid or an ancestor of it (parent gives a process's).
func (n *Node) assistantOf(pid int, parent func(int) (int, error)) (string, bool) {
	roots := map[int]string{}
	n.mu.Lock()
	for id, p := range n.procs {
		if p.cmd != nil && p.cmd.Process != nil {
			roots[p.cmd.Process.Pid] = id
		}
	}
	n.mu.Unlock()
	for range ancestorsMost {
		if id, ok := roots[pid]; ok {
			return id, true
		}
		next, err := parent(pid)
		if err != nil || next <= 1 || next == pid {
			return "", false
		}
		pid = next
	}
	return "", false
}

// mayCall says whether the command lines may call tool: one of a connector signed in as of the last
// assistant.prepare ("<id>_…", the connector's own tools and its <id>_cli).
func (n *Node) mayCall(tool string) bool {
	return slices.ContainsFunc(n.signedInConnectors(), func(id string) bool { return strings.HasPrefix(tool, id+"_") })
}

// cliCall runs one of the command lines' calls (personad tool) on the hub as the assistant's, or refuses
// it.
func (n *Node) cliCall(ctx context.Context, c net.Conn, req sockRequest) proto.ToolResult {
	if !n.mayCall(req.Name) {
		return proto.TextResult(req.Name+" is not available: the command line calls only the tools of the hub's connectors that are signed in", true)
	}
	session, ok := n.assistantCaller(c)
	if !ok {
		return proto.TextResult("这个命令只能在助手的对话里用：这个进程不是 Persona 为助手起的", true)
	}
	if n.peer == nil {
		return proto.TextResult("Persona hub error: not connected", true)
	}
	var res proto.ToolResult
	if err := n.peer.Call(ctx, "mcp.call", map[string]any{"name": req.Name, "args": req.Args, "session_id": session, "assistant": true}, &res); err != nil {
		return proto.TextResult("Persona hub error: "+err.Error(), true)
	}
	return res
}

// signedInConnectors are the hub's connectors signed in as of the last assistant.prepare: whose built-in skills the
// assistant gets (assistantskills.Requires).
func (n *Node) signedInConnectors() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Clone(n.connectors)
}

// syncAssistantSkills puts the assistant's skills in its directory (cwd): the user's, and the built-in ones whose
// connector is signed in; a connector's command line is on the assistant's PATH while it is. A runtime's directory (.agents/skills) is
// kept the same once it is there.
func (n *Node) syncAssistantSkills(cwd string) {
	conns := n.signedInConnectors()
	dirs := []string{filepath.Join(cwd, ".claude", "skills")}
	if st, err := os.Stat(filepath.Join(cwd, ".agents", "skills")); err == nil && st.IsDir() {
		dirs = append(dirs, filepath.Join(cwd, ".agents", "skills"))
	}
	for _, d := range dirs {
		if err := assistantskills.Sync(d, assistantskills.UserDir(), conns, n.logger.Printf); err != nil {
			n.logger.Printf("assistant: skills: %v", err)
		}
	}
	n.writeCLIs(n.connectorCLIs())
}

// connectorCLIs are the signed-in connectors' command lines as of the last assistant.prepare (name → connector).
func (n *Node) connectorCLIs() map[string]string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return maps.Clone(n.clis)
}

// cliMark is in every command line the node writes on the assistant's PATH: one it wrote and no longer has a
// connector for goes, other files there (a plugin's gate commands) stay.
const cliMark = "# a connector's command line, on the assistant's PATH while it is signed in (node/clicall.go)"

// writeCLIs puts the signed-in connectors' command lines on the assistant's PATH (personad tool --cli <id>) and takes
// away those whose connector is not, and the built-in Telegram's tg (personad tg) it wrote before Telegram became an
// extension (#1704).
func (n *Node) writeCLIs(clis map[string]string) {
	bin := filepath.Join(n.dir, "assistant-bin")
	want := map[string]string{}
	for name, id := range clis {
		want[name] = "#!/bin/sh\n" + cliMark + "\nexec " + shellQuote(n.bin) + " tool --cli " + shellQuote(id) + " \"$@\"\n"
	}
	entries, _ := os.ReadDir(bin)
	for _, e := range entries {
		if _, keep := want[e.Name()]; keep {
			continue
		}
		p := filepath.Join(bin, e.Name())
		if b, err := os.ReadFile(p); err == nil && (strings.Contains(string(b), cliMark) || strings.Contains(string(b), "Telegram through the hub")) {
			_ = os.Remove(p)
		}
	}
	if len(want) == 0 {
		return
	}
	_ = os.MkdirAll(bin, 0o755)
	for name, wrapper := range want {
		p := filepath.Join(bin, name)
		if cur, err := os.ReadFile(p); err == nil && string(cur) == wrapper {
			continue
		}
		_ = os.WriteFile(p, []byte(wrapper), 0o755)
	}
}
