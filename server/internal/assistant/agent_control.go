package assistant

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// The main conversation in charge of any agent (#5112, docs/unified-entry.md"Supervision"): stopping and redirecting one
// (agent_interrupt), taking one it did not hand out under its supervision (agent_supervise), and the agents' own way
// up to their supervisor (report_to_supervisor, one of the tools an agent gets, not the assistant; agentToolNames).

// agentToolNames are the tools of the agents (not the assistant's conversations) that are this manager's own; their
// descriptions and schemas are in prompts/mcp (docs/prompts.md). Each needs to know which agent calls it: the node
// names the session of the agent's pane for them (node.callerTools).
var agentToolNames = []string{"report_to_supervisor", "todo_decisions", "todo_decision_add"}

// AgentTools are the agents' own tools (mcpTools for an agent).
func (s *Service) AgentTools() []proto.Tool { return toolsNamed(agentToolNames) }

func isAgentTool(name string) bool {
	for _, n := range agentToolNames {
		if n == name {
			return true
		}
	}
	return false
}

// ---- agent_interrupt ----

// toolAgentInterrupt stops an agent's turn (session.interrupt) and, with then, sends it the new instruction right
// after: steering instead of agent_send's queueing behind the turn. An agent not working is not stopped; then still
// goes to it.
func (s *Service) toolAgentInterrupt(ctx context.Context, machine, self string, args json.RawMessage) (string, error) {
	var a struct {
		Machine   string `json:"machine"`
		SessionID string `json:"session_id"`
		Then      string `json:"then"`
	}
	_ = json.Unmarshal(args, &a)
	am, c, err := s.findAgent(ctx, a.Machine, a.SessionID)
	if err != nil {
		return "", err
	}
	if c.Assistant {
		return "", fmt.Errorf("that is an assistant conversation, not a coding agent: it is the user's to stop")
	}
	then := strings.TrimSpace(a.Then)
	if c.SourceKind != "" && (c.Capabilities == nil || !c.Capabilities.Interrupt || then != "" && !c.Capabilities.Send) {
		return "", fmt.Errorf("this agent can only be read: %s", c.ReadOnlyReason)
	}
	summary := i18n.T(s.lang(ctx), "assistant-tasks.interrupt_summary", i18n.Args{"agent": agentLabel(c), "machine": am.Name})
	if then != "" {
		summary += "\n\n" + then
	}
	if ok, err := s.confirmOutbound(ctx, machine, self, "agent_interrupt", summary, args); err != nil {
		return "", err
	} else if !ok {
		return "The user declined on the phone: the agent goes on as it was.", nil
	}
	// Only a turn under way is stopped: an agent still starting has none yet (then is queued for it, as agent_send does).
	working := c.Status == proto.StatusWorking
	if working {
		if err := s.call(ctx, am.ID, "session.interrupt", map[string]any{"session_id": c.SessionID}, nil); err != nil {
			return "", fmt.Errorf("stop: %w", err)
		}
		s.logf("agent_interrupt %s on %s (from %s)", short(c.SessionID), am.ID, short(self))
	}
	if then == "" {
		if !working {
			return fmt.Sprintf("%s on %s was not working: nothing to stop.", agentLabel(c), am.Name), nil
		}
		return fmt.Sprintf("Stopped %s on %s: its turn was cut off, the session stays (agent_send to go on). What it did already is not undone.",
			agentLabel(c), am.Name), nil
	}
	// The stop takes a moment to land; session.send queues the text if the agent still reads as working, and a
	// closed agent is resumed first (as agent_send does), which can take minutes.
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
	defer cancel()
	if err := s.call(sctx, am.ID, DeviceSessionSend, sendParams(c.SessionID, then, false), nil); err != nil {
		if working {
			return "", fmt.Errorf("stopped it, but sending the new instruction failed: %w", err)
		}
		return "", err
	}
	if t, ok := s.taskOf(ctx, am.ID, c.SessionID); ok && (t.Origin == self || t.Supervision != nil && t.Supervision.Supervisor == self) {
		// The conversation shows what it told its task, as for agent_send to a task (routed); marked when it cut in.
		if _, err := s.appendFeed(ctx, self, feedRouted, t.ID, map[string]any{"text": then, "interrupted": working}); err != nil {
			s.logf("task %s: routed feed: %v", t.ID, err)
		}
	}
	if !working {
		return fmt.Sprintf("%s on %s was not working: sent the new instruction.", agentLabel(c), am.Name), nil
	}
	return fmt.Sprintf("Stopped %s on %s and sent the new instruction. What it did already is not undone.", agentLabel(c), am.Name), nil
}

// ---- agent_supervise ----

// toolAgentSupervise takes agent machine/session under the main conversation's supervision: a task of it already
// gets the new done_when / expect_minutes; an agent of no task becomes one (a todo of this conversation with the
// agent on it, as quietAgentTask makes); another conversation's task, or the user's own todo, stays theirs.
func (s *Service) toolAgentSupervise(ctx context.Context, self string, args json.RawMessage) (string, error) {
	var a struct {
		Machine       string   `json:"machine"`
		SessionID     string   `json:"session_id"`
		Title         string   `json:"title"`
		DoneWhen      []string `json:"done_when"`
		ExpectMinutes int      `json:"expect_minutes"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("arguments: %v", err)
	}
	if !s.isMain(ctx, self) {
		return "", fmt.Errorf("only the main conversation supervises agents: from here, talk to it with agent_send")
	}
	am, c, err := s.findAgent(ctx, a.Machine, a.SessionID)
	if err != nil {
		return "", err
	}
	if c.Assistant {
		return "", fmt.Errorf("that is an assistant conversation, not a coding agent")
	}
	if err := s.ensureTodosLoaded(ctx); err != nil {
		return "", err
	}
	// One adoption at a time: two at once would both find the agent on no task and make two.
	s.adoptMu.Lock()
	defer s.adoptMu.Unlock()
	doneWhen, expect := cleanDoneWhen(a.DoneWhen), max(0, min(a.ExpectMinutes, 24*60))
	if t, ok := s.openTodoOfAgent(am.ID, c.SessionID); ok {
		switch {
		case t.Origin == "":
			return "", fmt.Errorf("%s is on the user's own todo %q (%s): it stays theirs", agentLabel(c), t.Title, t.ID)
		case t.Origin != self && (t.Supervision == nil || t.Supervision.Supervisor != self):
			return "", fmt.Errorf("%s works for task %q (%s) of another conversation: it stays that conversation's", agentLabel(c), t.Title, t.ID)
		}
		if _, _, err := s.updateSupervision(ctx, t.ID, func(v *Supervision) bool {
			v.Supervisor = self
			if len(doneWhen) > 0 {
				v.DoneWhen = doneWhen
			}
			if expect > 0 {
				v.ExpectMinutes = expect
			}
			return true
		}); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s already works for your task %s (%q): you supervise it, done_when %s.", agentLabel(c), t.ID, t.Title,
			criteriaWords(doneWhen, t.Supervision)), nil
	}
	title := clip(oneLine(a.Title), 40)
	if title == "" {
		title = clip(oneLine(cmp.Or(strings.TrimSpace(c.Title), agentLabel(c))), 40)
	}
	notes := i18n.T(s.lang(ctx), "assistant-tasks.adopt_notes", i18n.Args{"agent": agentLabel(c), "machine": am.Name, "cwd": c.Cwd})
	todo, err := s.createTodo(ctx, title, notes, self, "")
	if err != nil {
		return "", err
	}
	if _, _, err := s.updateSupervision(ctx, todo.ID, func(v *Supervision) bool {
		v.Supervisor, v.DoneWhen, v.ExpectMinutes = self, doneWhen, expect
		return true
	}); err != nil {
		s.dropAdoption(ctx, todo.ID, err)
		return "", fmt.Errorf("supervise it: %w", err)
	}
	if err := s.linkTodoAgent(ctx, todo.ID, am.ID, c.SessionID, title, c.Cwd, c.Agent); err != nil {
		s.dropAdoption(ctx, todo.ID, err)
		return "", fmt.Errorf("link the agent: %w", err)
	}
	if _, err := s.appendFeed(ctx, self, feedTask, todo.ID, nil); err != nil {
		s.logf("task %s: feed: %v", todo.ID, err)
	}
	s.logf("agent_supervise %s on %s → task %s", short(c.SessionID), am.ID, todo.ID)
	return fmt.Sprintf("%s on %s is now your task %s (%q, its card is in the conversation): you get a supervision message (📋) when it needs you, "+
		"and close it with task_review. done_when %s. The agent was not told: if it should work to these criteria or report to you "+
		"(report_to_supervisor), tell it with agent_send.", agentLabel(c), am.Name, todo.ID, title, criteriaWords(doneWhen, nil)), nil
}

// dropAdoption closes the todo an adoption made when the agent could not be put on it (why), so no empty task stays.
func (s *Service) dropAdoption(ctx context.Context, id string, why error) {
	s.logf("agent_supervise: task %s: %v", id, why)
	if err := s.failTodo(ctx, id, i18n.T(s.lang(ctx), "assistant-tasks.adopt_failed", i18n.Args{"reason": why.Error()})); err != nil {
		s.logf("agent_supervise: close task %s: %v", id, err)
	}
}

// openTodoOfAgent is the open todo agent mid/sid is active on, whoever opened it (taskOf: only an assistant
// conversation's).
func (s *Service) openTodoOfAgent(mid, sid string) (Todo, bool) {
	s.todos.mu.RLock()
	defer s.todos.mu.RUnlock()
	t := s.todos.todos[s.todos.activeBySession[[2]string{mid, sid}]]
	if t == nil || t.DoneAt != nil {
		return Todo{}, false
	}
	return copyTodoLocked(t), true
}

// criteriaWords is done_when as the tool's answer says it: the new list, else the ledger's, else that there is none.
func criteriaWords(list []string, v *Supervision) string {
	if len(list) == 0 && v != nil {
		list = v.DoneWhen
	}
	if len(list) == 0 {
		return "not set (judge by what the task is for; set it with agent_supervise)"
	}
	return strconv.Quote(strings.Join(list, "; "))
}

// reportBody is a report feed item's body (feedReport).
type reportBody struct {
	Kind     string   `json:"kind"`
	Text     string   `json:"text"`
	Evidence []string `json:"evidence,omitempty"`
	Step     int      `json:"step,omitempty"`
	Steps    int      `json:"steps,omitempty"`
}

// ---- report_to_supervisor ----

// Report kinds an agent sends its supervisor.
const (
	upReportProgress = "progress"
	upReportQuestion = "question"
	upReportBlocked  = "blocked"
	upReportDone     = "done"
)

// reportLimit / reportWindow bound how often one agent reports (each report can start a turn of the main
// conversation: it is not to be flooded).
const (
	reportLimit  = 10
	reportWindow = 10 * time.Minute
)

// reportRate counts each session's recent reports.
type reportRate struct {
	mu   sync.Mutex
	seen map[[2]string][]time.Time
}

// allow records a report of mid/sid at now, false when it has reportLimit within reportWindow already.
func (r *reportRate) allow(mid, sid string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seen == nil {
		r.seen = map[[2]string][]time.Time{}
	}
	k := [2]string{mid, sid}
	kept := r.seen[k][:0]
	for _, at := range r.seen[k] {
		if now.Sub(at) < reportWindow {
			kept = append(kept, at)
		}
	}
	if len(kept) >= reportLimit {
		r.seen[k] = kept
		return false
	}
	r.seen[k] = append(kept, now)
	for other, list := range r.seen { // sessions that reported long ago are forgotten
		if other != k && (len(list) == 0 || now.Sub(list[len(list)-1]) >= reportWindow) {
			delete(r.seen, other)
		}
	}
	return true
}

// joinReports is two reports of the same kind waiting together (cur first, then g): what both said and showed, so
// a second question does not push the first one out.
func joinReports(cur, g superviseSignal) superviseSignal {
	g.At = cur.At
	if cur.Detail != "" && cur.Detail != g.Detail {
		g.Detail = clipMiddle(cur.Detail+"\n—\n"+g.Detail, 3000)
	}
	ev := slices.Clone(cur.Evidence)
	for _, e := range g.Evidence {
		if !slices.Contains(ev, e) && len(ev) < 12 {
			ev = append(ev, e)
		}
	}
	g.Evidence = ev
	if g.Step == 0 {
		g.Step, g.Steps = cur.Step, cur.Steps
	}
	return g
}

var errNoSupervisor = errors.New("no supervisor")

// agentTool runs one of the agents' own tools for agent session on machine.
func (s *Service) agentTool(ctx context.Context, machine, session, name string, args json.RawMessage) proto.ToolResult {
	switch name {
	case "report_to_supervisor":
		res, err := s.reportToSupervisor(ctx, machine, session, args, time.Now())
		if errors.Is(err, errNoSupervisor) {
			return proto.TextResult(res, false) // not an error: there is nobody to tell, and the agent goes on
		}
		if err != nil {
			return proto.TextResult(err.Error(), true)
		}
		return proto.TextResult(res, false)
	case "todo_decisions", "todo_decision_add": // todo_decisions.go
		res, err := s.agentDecisionTool(ctx, machine, session, name, args, time.Now())
		if err != nil {
			return proto.TextResult(err.Error(), true)
		}
		return proto.TextResult(res, false)
	}
	return proto.TextResult("unknown tool "+name, true)
}

// reportToSupervisor turns agent machine/session's report into a signal for the supervisor of the task it works
// for: in the next Supervision message (a question, a block or a finish soon: they are urgent).
func (s *Service) reportToSupervisor(ctx context.Context, machine, session string, args json.RawMessage, now time.Time) (string, error) {
	var a struct {
		Kind     string   `json:"kind"`
		Text     string   `json:"text"`
		Evidence []string `json:"evidence"`
		Step     int      `json:"step"`
		Steps    int      `json:"steps"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("arguments: %v", err)
	}
	switch a.Kind {
	case upReportProgress, upReportQuestion, upReportBlocked, upReportDone:
	default:
		return "", fmt.Errorf("kind must be progress, question, blocked or done")
	}
	text := clip(strings.TrimSpace(a.Text), 2000)
	if text == "" {
		return "", fmt.Errorf("text is empty")
	}
	if session == "" {
		return "Nobody receives this: the call came from outside an agent session.", errNoSupervisor
	}
	if err := s.ensureTodosLoaded(ctx); err != nil {
		return "", err
	}
	t, ok := s.taskOf(ctx, machine, session)
	if !ok || t.Supervision == nil || t.Supervision.Supervisor == "" {
		return "Nobody receives this: your work is not a task Persona supervises. Ask the user what you need the usual way.", errNoSupervisor
	}
	if !s.upReports.allow(machine, session, now) {
		return "", fmt.Errorf("too many reports: at most %d in %d minutes; put what is left into one report later", reportLimit, int(reportWindow.Minutes()))
	}
	evidence := make([]string, 0, len(a.Evidence))
	for _, e := range a.Evidence {
		if e = clip(strings.TrimSpace(e), 300); e != "" && len(evidence) < 8 {
			evidence = append(evidence, e)
		}
	}
	steps := max(0, min(a.Steps, 1000))
	step := max(0, min(a.Step, 1000))
	if steps > 0 {
		step = min(step, steps)
	}
	s.signal(superviseSignal{Todo: t.ID, Kind: sigReport, Key: now.UTC().Format(time.RFC3339Nano), At: now, Machine: machine,
		Session: session, Report: a.Kind, Detail: text, Evidence: evidence, Step: step, Steps: steps})
	s.noteProgress(ctx, t.ID, TaskProgress{Kind: a.Kind, Step: step, Steps: steps, Text: clip(oneLine(text), 200), At: now.UTC()})
	// What it asks, what blocks it and its finish show in the supervisor's conversation too, so the user can follow
	// the handoff; progress reports only update the card (the stream stays quiet).
	if a.Kind != upReportProgress {
		body := reportBody{Kind: a.Kind, Text: text, Evidence: evidence, Step: step, Steps: steps}
		if _, err := s.appendFeed(ctx, t.Supervision.Supervisor, feedReport, t.ID, body); err != nil {
			s.logf("task %s: report feed: %v", t.ID, err)
		}
	}
	s.logf("task %s: %s reports %s", t.ID, short(session), a.Kind)
	if a.Kind == upReportProgress {
		return "Noted for your supervisor: it reads it with its next supervision message. Go on with the work.", nil
	}
	return "Sent to your supervisor: it reads it within minutes and answers you by message. Don't wait idle for it if there is " +
		"other work in the task; stop when you cannot go on.", nil
}
