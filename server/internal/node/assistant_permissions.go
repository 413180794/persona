package node

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/osproc"
)

// resetAssistantPermissions drops grants installed by older nodes inside the agent process. An
// interrupt alone is insufficient for Codex: its process and session permissions survive the turn.
// The next message resumes the transcript; the hub remains the authority for future grants.
func (n *Node) resetAssistantPermissions(ctx context.Context, id string) (restartRequired bool, err error) {
	if id == "" || !n.isAssistantSession(id) {
		return true, fmt.Errorf("not an assistant conversation")
	}
	n.mu.Lock()
	for key := range n.larkAllowed {
		if strings.HasPrefix(key, id+" ") {
			delete(n.larkAllowed, key)
		}
	}
	p := n.procs[id]
	n.mu.Unlock()
	if n.runtimeAssistant(id) {
		// The runtime protocol has no operation that clears session permissions. Do not pretend
		// session.interrupt does that or close a conversation the runtime might delete permanently.
		return true, nil
	}
	if p == nil || !p.alive() {
		return false, nil
	}
	// A graceful stop can reap the launcher before its agent child exits, leaving cached
	// permissions alive. Revocation terminates the entire process group immediately.
	if err := osproc.StopGroup(p.cmd.Process.Pid, true); err != nil && p.alive() {
		return true, fmt.Errorf("stop assistant permission cache: %w", err)
	}
	// A child forked while that signal went out can miss it (macOS): sweep until the group is empty, so nothing of
	// the conversation lives on (#5942).
	osproc.SweepGroup(p.cmd.Process.Pid, time.Second)
	_ = p.stdin.Close()
	select {
	case <-p.exited:
		n.rememberTasks(id, nil)
		return false, nil
	case <-ctx.Done():
		return true, ctx.Err()
	}
}
