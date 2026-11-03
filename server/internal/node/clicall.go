package node

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nwverse/persona/server/internal/assistantskills"
	"github.com/nwverse/persona/server/internal/proto"
)

// The tg CLI (internal/tgcli, the telegram skill) reaches the hub through the node's socket like the MCP server, but
// the node, not the caller, says whether the assistant is asking: only a process the node started for the assistant,
// or one of its descendants (its Bash tool), gets the assistant's tools. Any other caller (an agent, a program run by
// hand) is refused; so is a system where the other end of a socket cannot be known.

// cliPrefix is what the CLI may call: Telegram's tools and actions (the hub's gate still decides each write).
const cliPrefix = "telegram_"

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

// cliCall runs one of the tg CLI's calls on the hub as the assistant's, or refuses it.
func (n *Node) cliCall(ctx context.Context, c net.Conn, req sockRequest) proto.ToolResult {
	if !strings.HasPrefix(req.Name, cliPrefix) {
		return proto.TextResult("tg only calls Telegram's tools", true)
	}
	session, ok := n.assistantCaller(c)
	if !ok {
		return proto.TextResult("tg 只能在助手的对话里用：这个进程不是 Persona 为助手起的", true)
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
// connector is signed in; tg is on the assistant's PATH while Telegram is. A runtime's directory (.agents/skills) is
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
	n.writeTG(slices.Contains(conns, "telegram"))
}

// writeTG puts tg (personad tg) on the assistant's PATH, or takes it away.
func (n *Node) writeTG(on bool) {
	bin := filepath.Join(n.dir, "assistant-bin")
	tg := filepath.Join(bin, "tg")
	if !on {
		_ = os.Remove(tg)
		return
	}
	_ = os.MkdirAll(bin, 0o755)
	wrapper := "#!/bin/sh\n# Telegram through the hub (the telegram skill), on the assistant's PATH while it is signed in (node/clicall.go).\nexec " +
		shellQuote(n.bin) + " tg \"$@\"\n"
	if cur, err := os.ReadFile(tg); err == nil && string(cur) == wrapper {
		return
	}
	_ = os.WriteFile(tg, []byte(wrapper), 0o755)
}
