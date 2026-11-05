package node

import (
	"os"

	"github.com/nwverse/persona/server/internal/host"
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
//
// The agents view opens the job the session went to (Claude's autoOpenJobId), and the pane then is that job's: its
// conversation, its prompt, its name as the terminal title. Such a pane is the background session's (the job's
// sessions/<pid>.json has jobId = the parkedJobId, and its name is the title), so it lists in its herdr tab and takes
// what is typed (2026-11-04: tab 4 of a herdr workspace was missing on the phone, its session listed apart, read-only).
//
// On the agents list (the job ended or stopped, or another one is picked) the pane is still the parked session's tab:
// it lists there, idle (herdr's state is a background session's title), with its terminal: the phone picks a session
// from the list as on the computer (arrows, enter), and the pane is then that one's. A message is not typed into it —
// the list's prompt starts a new background session. Before, such a pane listed nowhere (2026-11-05: tabs 2 and 4
// missing on the phone after their jobs ended).

const sourceClaudeBg = "claude_bg"

const claudeBgReadOnly = "Claude Code 的后台会话：手机上只能看，回复请在电脑上用 claude agents 打开它"

const claudeAgentsList = "这个标签停在 Claude Code 的 agents 列表上：打开终端，用方向键选中一个会话、回车进去后再发消息"

// claudeBgStatus maps a background session's status to the node's: waiting is a question or dialog of its own,
// answered on the computer. Anything else is unknown.
var claudeBgStatus = map[string]string{"busy": proto.StatusWorking, "idle": proto.StatusIdle, "waiting": proto.StatusBlocked}

// parkedClaudeSessions are the sessions only parked claudes have open, each with the background job it went to.
func parkedClaudeSessions(procs []claudeProc) map[string]parkedJob {
	parked, held := map[string]parkedJob{}, map[string]bool{}
	jobs := map[string]claudeProc{}
	for _, c := range procs {
		if c.Kind == "bg" && c.JobID != "" {
			jobs[c.JobID] = c
		}
	}
	for _, c := range procs {
		if c.ParkedJobID != "" {
			j := jobs[c.ParkedJobID]
			parked[c.SessionID] = parkedJob{SessionID: j.SessionID, Name: j.Name}
		} else {
			held[c.SessionID] = true
		}
	}
	for id := range held {
		delete(parked, id)
	}
	return parked
}

// parkedJob is the live background session a parked session went to: its id and name ("" when it is not running).
type parkedJob struct{ SessionID, Name string }

// shownIn is the job's session when the pane shows it (its name is the pane's title), else "".
func (j parkedJob) shownIn(a host.Agent) string {
	if j.SessionID == "" || !validID(j.SessionID) || j.Name == "" || a.Title != j.Name {
		return ""
	}
	return j.SessionID
}

// onAgentsList is a parked session's pane on the agents list, idle: the state herdr reads off the title is not its.
func onAgentsList(a host.Agent) host.Agent {
	a.AgentStatus = proto.StatusIdle
	return a
}

// agentsListRefusal refuses a message to a parked session whose pane is on the agents list, nil for any other.
func (n *Node) agentsListRefusal(id string) error {
	n.mu.Lock()
	listed := n.onAgentsList[id]
	n.mu.Unlock()
	if !listed {
		return nil
	}
	return refusal(openElsewhere, claudeAgentsList+"。这次没有发送。", nil)
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
