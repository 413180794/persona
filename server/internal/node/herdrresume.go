package node

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/host"
)

// When herdr's server restarts it restores its panes and, once a client attaches, resumes the agents in them with its
// built-in command: a bare `claude --resume <id>` (session.resume_agents_on_restore, on by default). For a Claude
// Persona started that drops everything Persona started it with — the launcher (a gateway's credentials: the
// session silently moved to the user's own subscription), --settings, --mcp-config, Persona's hooks — and a session
// without a transcript stopped at Claude's login screen (2026-11-04, #1879).
//
// herdr v0.9.5 has no per-pane switch to turn its resume off, but it takes a per-pane resume command: a reporter that
// holds the pane (pane.report_agent) may attach resume_argv to its session report, and herdr runs that instead of its
// built-in one, only for that pane. So every Claude Persona starts in herdr gets one: `eval "<the command Persona
// starts it with, --resume <id>>"`, typed into the restored pane's shell like herdr's own (so ~/.zshrc launchers
// work). Claude that the user starts in a terminal is never touched: herdr resumes it as before. The user's herdr
// config is not changed. herdr keeps the command across later session reports with another id (checked against
// v0.9.5), so a Claude that moved to a new session (/clear) is reported again from its SessionStart hook.

// resumeSource is the reporter Persona holds its panes as (a custom source: herdr's own integration keeps its own).
const resumeSource = "custom:persona"

// claudeLaunch is what a Claude in a pane was started with, to start it again the same way.
type claudeLaunch struct {
	Launcher, Settings, Hooks, Model string
	Plugins                          bool // claude loads plugins: Persona's plugin directory and office rule
	ID                               string
}

// claudeCommandLine is the command that starts Claude for spec with args (--session-id / --resume …, mode flags),
// quoting with q.
func (n *Node) claudeCommandLine(spec claudeLaunch, args string, q func(string) string) string {
	cmd := n.launcherCommandQ(spec.Launcher, q) + " " + args
	if spec.Settings != "" {
		cmd += " --settings " + q(spec.Settings)
	}
	cmd += " --mcp-config " + q(filepath.Join(n.dir, "mcp.json"))
	if spec.Model != "" {
		cmd += " --model " + q(spec.Model)
	}
	if spec.Plugins {
		cmd += " --plugin-dir " + q(n.pluginDir()) + " --allowedTools " + q(officeRule(n.agentOfficeScript()))
	}
	if spec.Hooks != "" {
		cmd += " --plugin-dir " + q(spec.Hooks)
	}
	return cmd
}

// dquote quotes s for a POSIX shell without apostrophes (herdr refuses them in resume_argv): as is when it needs no
// quoting, else in double quotes with \, ", $ and ` escaped.
func dquote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r == '/' || r == '.' || r == '-' || r == '_' || r == ':' || r == '=' || r == '+' || r == ',' ||
			r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
	}) < 0 {
		return s
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`, "`", "\\`")
	return `"` + r.Replace(s) + `"`
}

// claudeResumeArgv is the resume_argv of a Claude Persona started: eval of its start command with --resume id. ok is
// false when the command cannot be said without an apostrophe (a path with one in it): herdr would refuse it.
func (n *Node) claudeResumeArgv(spec claudeLaunch, id string) (argv []string, ok bool) {
	cmd := n.claudeCommandLine(spec, "--resume "+id, dquote)
	if strings.Contains(cmd, "'") || !validID(id) {
		return nil, false
	}
	return []string{"eval", cmd}, true
}

// holdResume gives herdr the pane's resume command and remembers how the pane's Claude was started, for a later
// SessionStart with another session id. Hosts other than herdr: nothing to do. Failures are logged: the start
// itself went through.
func (n *Node) holdResume(ctx context.Context, launch host.Launcher, pane, id string, spec claudeLaunch) {
	h, ok := launch.(*herdrHost)
	if !ok || h.c == nil {
		return
	}
	spec.ID = id
	n.mu.Lock()
	if n.launches == nil {
		n.launches = map[string]claudeLaunch{}
	}
	n.launches[pane] = spec
	n.mu.Unlock()
	n.reportResume(ctx, h, pane, spec)
}

func (n *Node) reportResume(ctx context.Context, h *herdrHost, pane string, spec claudeLaunch) {
	argv, ok := n.claudeResumeArgv(spec, spec.ID)
	if !ok {
		n.logf("herdr resume: %s (%s) not given a resume command (it cannot be said without an apostrophe)", pane, spec.ID)
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := h.c.PaneReportResume(ctx, host.ParsePane(pane).ID, resumeSource, "claude", spec.ID, argv); err != nil {
		n.logf("herdr resume: %s (%s): %v", pane, spec.ID, err)
	}
}

// resumeOnSessionStart follows a Persona-started Claude to its new session (/clear, /resume in it): herdr would
// otherwise resume the old one after a restart. Panes Persona did not start, and the same session again: nothing.
func (n *Node) resumeOnSessionStart(pane, id string) {
	if pane == "" || !validID(id) {
		return
	}
	n.mu.Lock()
	spec, ok := n.launches[pane]
	moved := ok && spec.ID != id
	if moved {
		spec.ID = id
		n.launches[pane] = spec
	}
	n.mu.Unlock()
	if !moved {
		return
	}
	h, isHerdr := n.hosts.Get(host.Herdr).(*herdrHost)
	if !isHerdr || h.c == nil || host.ParsePane(pane).Kind != host.Herdr {
		return
	}
	go n.reportResume(context.Background(), h, pane, spec)
}

// backfillResume gives herdr the resume command of Persona-started Claudes found running in herdr that have none from
// this node yet: ones started by an older node, or before this node restarted (n.launches is memory only). Once
// per pane and session; the work (settings paths, the reports) runs in the background, off the refresh.
func (n *Node) backfillResume(agents []host.Agent) {
	h, ok := n.hosts.Get(host.Herdr).(*herdrHost)
	if !ok || h.c == nil {
		return
	}
	type todo struct {
		pane string
		id   string
		st   sessionState
	}
	var work []todo
	n.mu.Lock()
	for _, a := range agents {
		if a.Agent != "claude" || a.PaneID == "" || host.ParsePane(a.PaneID).Kind != host.Herdr {
			continue
		}
		id := n.panes[a.PaneID]
		if id == "" {
			id = a.SessionID()
		}
		if !validID(id) {
			continue
		}
		if spec, ok := n.launches[a.PaneID]; ok && spec.ID == id {
			continue
		}
		st, ok := n.state.get(id)
		if !ok || st.Launcher == "" || launcherAgentType(st.Launcher) != "claude" {
			continue // not started by Persona (the user's own claude: herdr resumes it as before), or not a Claude launcher
		}
		if n.launches == nil {
			n.launches = map[string]claudeLaunch{}
		}
		n.launches[a.PaneID] = claudeLaunch{Launcher: st.Launcher, Model: st.Model, ID: id} // claimed: once
		work = append(work, todo{a.PaneID, id, st})
	}
	n.mu.Unlock()
	if len(work) == 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		for _, w := range work {
			settings, hooks := n.claudeStartSettings(ctx, w.st.Launcher, "claude", w.st.Cwd)
			spec := claudeLaunch{Launcher: w.st.Launcher, Settings: settings, Hooks: hooks, Model: w.st.Model,
				Plugins: n.claudeLoadsPlugins(ctx), ID: w.id}
			n.mu.Lock()
			if cur, ok := n.launches[w.pane]; !ok || cur.ID != w.id {
				n.mu.Unlock()
				continue // moved on meanwhile (a start, a SessionStart): that one reported
			}
			n.launches[w.pane] = spec
			n.mu.Unlock()
			n.reportResume(ctx, h, w.pane, spec)
		}
	}()
}
