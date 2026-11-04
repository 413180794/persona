package node

import (
	"os"

	"github.com/nwverse/persona/server/internal/proto"
)

// Claude Code's background sessions (2.1.30x: /fork, a session sent to the background) run in Claude's daemon, not
// in a terminal: `claude daemon` hosts each in its own claude process, whose sessions/<pid>.json says kind "bg" and
// what the session does (busy / idle / waiting). No pane runs one and no SessionStart hook reaches herdr, so they
// listed as offline while they worked. The node lists every live one as running, read-only (source claude_bg): no
// pane to type into, and Claude's peer socket is not spoken here. Their transcripts stream like any session's.
//
// An interactive claude that sent its own session to the background keeps that session in its sessions/<pid>.json
// with parkedJobId set, and shows Claude's agents view in its pane until it comes back. herdr still names the pane
// after the session its SessionStart hook reported and reads its state off the terminal title — a background
// session's — so the parked session looked busy with another one's work, and a message to it went into the agents
// view. As in Claude Code (a parked claude does not hold its session), such a pane is not taken for that session.

const sourceClaudeBg = "claude_bg"

const claudeBgReadOnly = "Claude Code 的后台会话：手机上只能看，回复请在电脑上用 claude agents 打开它"

// claudeBgStatus maps a background session's status to the node's: waiting is a question or dialog of its own,
// answered on the computer. Anything else is unknown.
var claudeBgStatus = map[string]string{"busy": proto.StatusWorking, "idle": proto.StatusIdle, "waiting": proto.StatusBlocked}

// parkedClaudeSessions are the sessions only parked claudes have open.
func parkedClaudeSessions(procs []claudeProc) map[string]bool {
	parked, held := map[string]bool{}, map[string]bool{}
	for _, c := range procs {
		if c.ParkedJobID != "" {
			parked[c.SessionID] = true
		} else {
			held[c.SessionID] = true
		}
	}
	for id := range held {
		delete(parked, id)
	}
	return parked
}

// claudeBgSessions lists the live background sessions not in seen that have a transcript.
func (n *Node) claudeBgSessions(procs []claudeProc, seen map[string]bool) []proto.Session {
	var out []proto.Session
	listed := map[string]bool{}
	for _, c := range procs {
		id := c.SessionID
		if c.Kind != "bg" || c.Spare || !validID(id) || seen[id] || listed[id] {
			continue
		}
		p := n.transcriptPath(id)
		if p == "" {
			continue
		}
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		listed[id] = true
		m := n.meta(p, st)
		status, ok := claudeBgStatus[c.Status]
		if !ok {
			status = proto.StatusUnknown
		}
		s := proto.Session{
			SessionID: id, Cwd: c.Cwd, Status: status, Agent: "claude", Title: m.title, SourceKind: sourceClaudeBg,
			Capabilities: &proto.SessionCapabilities{Read: true}, ReadOnlyReason: claudeBgReadOnly,
			Model: m.model, LastText: snippet(m.lastText), UpdatedAt: lastUsed(m, st), Rev: st.Size(),
		}
		if s.Cwd == "" {
			s.Cwd = m.cwd
		}
		if s.Title == "" {
			s.Title = c.Name
		}
		untitled(&s, m)
		s.Activity, s.ActivityAt = m.activityOf()
		s.PermissionMode, s.Effort = n.currentModes(id, m)
		for _, t := range m.bg { // as for a pane's session: background subagents are listed as subagents
			if t.Kind != "agent" {
				s.Background = append(s.Background, t)
			}
		}
		s.Loop = m.loop
		s.Subagents = n.subagents(id)
		n.decorate(&s)
		out = append(out, s)
	}
	return out
}
