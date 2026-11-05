package hub

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// The Agents business is the manager's (internal/assistant, agents_*.go; docs/agent-work.md「谁管什么」): the triage of
// an agent's turn, 继续 after a transient model error, the wakes, the cards it opens and their answers, its switches,
// state and history; the tab names of new agents; what a new or resumed agent runs on. The hub carries it: it tells
// the manager what happened (session.turn_started / turn_ended, session.seen), shows what it reports (the snapshot's
// work on the session lists, its cards with the other interactions) and asks it to fill in a start
// (agents.start_params, agents.resume_params, agents.tab_name). While the manager is not connected the lists keep
// what it reported last (a long overdue wake no longer counts as one), answers to its cards wait for it (heldActs),
// and a start waits for it a little, then goes on 模型配置 › Agents as the hub has it, with no tab name.

// AgentsConfig is hub.json "agents", the manager's defaults (Hello.Agents).
type AgentsConfig = assistantwire.AgentsConfig

// workCardTool is the tool_name of the manager's cards about an agent's turn.
const workCardTool = assistantwire.WorkCardTool

// agentWork is the manager's last work report, by (machine, session) (replaced, never written into), and whether it
// is there to ask (waitManager).
type agentWork struct {
	mu sync.RWMutex
	on bool
	by map[[2]string]proto.WorkState

	everUp            bool      // it connected since the hub started
	firstWait, goneAt time.Time // when a start first waited for it (never connected yet), when it went away
}

// takeWorkReport keeps the manager's report (nil: it could not read it; the last one stands) and says the machines
// whose sessions look different now (all of them when the triage was switched).
func (h *Hub) takeWorkReport(r *assistantwire.WorkReport) []string {
	if r == nil {
		return nil
	}
	by := make(map[[2]string]proto.WorkState, len(r.Sessions))
	for _, s := range r.Sessions {
		by[[2]string{s.MachineID, s.SessionID}] = s.Work
	}
	aw := &h.agentWork
	aw.mu.Lock()
	old, oldOn := aw.by, aw.on
	aw.by, aw.on = by, r.On
	aw.mu.Unlock()
	var out []string
	add := func(mid string) {
		if !slices.Contains(out, mid) {
			out = append(out, mid)
		}
	}
	if oldOn != r.On {
		for _, m := range h.machinesNow() {
			add(m.cfg.ID)
		}
		return out
	}
	for k, w := range by {
		if o, ok := old[k]; !ok || !sameJSON(o, w) {
			add(k[0])
		}
	}
	for k := range old {
		if _, ok := by[k]; !ok {
			add(k[0])
		}
	}
	return out
}

func sameJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// decorateWork puts on s (decorated but for its work) where it stands in its work, and the state that follows: what
// the manager reported for its turn while it is still stopped on it, what it shows while it works or asks.
// asking is the machine's pending interactions by session (an agent's own before the manager's card).
func (h *Hub) decorateWork(mid string, s *proto.Session, asking map[string]proto.Interaction) {
	aw := &h.agentWork
	aw.mu.RLock()
	on := aw.on
	reported, ok := aw.by[[2]string{mid, s.SessionID}]
	aw.mu.RUnlock()
	if !on {
		return
	}
	var e *proto.WorkState
	if ok {
		e = &reported
	}
	s.Work = workOf(*s, e, asking)
	s.State = workState(s.State, s.Work)
}

// workOf is s's work state for the lists: reported is what the manager reported for its latest turn (nil: nothing),
// asking the pending interactions of its machine, by session.
func workOf(s proto.Session, reported *proto.WorkState, asking map[string]proto.Interaction) *proto.WorkState {
	if s.Assistant || s.Status == proto.StatusOffline {
		return nil
	}
	if s.Status == proto.StatusWorking {
		return &proto.WorkState{Phase: proto.WorkWorking}
	}
	ix, asks := asking[s.SessionID]
	if asks && ix.ToolName != workCardTool { // the agent asks itself: that is what it waits for
		return &proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitUser, Line: interactionLine(ix)}
	}
	if s.Status == proto.StatusBlocked && !asks {
		return &proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitUser, Line: "终端等你处理"}
	}
	if reported == nil || reported.Phase == "" || !sameTurn(s, reported.TurnAt) {
		return nil
	}
	w := *reported
	if w.InteractionID != "" && (!asks || ix.ID != w.InteractionID) {
		w.InteractionID = "" // answered, withdrawn, or not shown yet
	}
	if w.WakeAt != nil && time.Since(*w.WakeAt) > wakeGrace {
		w.WakeAt = nil // long overdue: nobody is waking it (the manager is away), it is no wait that ends by itself
	}
	return &w
}

// wakeGrace is how late a reported wake may be and still count: the manager sends what is due within its tick, and
// tries again a minute later when the machine was away.
var wakeGrace = 5 * time.Minute

// sameTurn: s is still on the turn that ended at turnAt (a stopped transcript does not change).
func sameTurn(s proto.Session, turnAt time.Time) bool {
	d := s.UpdatedAt.Sub(turnAt)
	return d > -2*time.Second && d < 2*time.Second
}

// workState is the unified state once the work state is known: a turn that is done, or blocked on something the
// manager waits out for it, no longer counts as 完成未看 (docs/agent-work.md「模型」). A wait nobody will end (the wakes
// off, or dropped because the user went near it) stays 完成未看.
func workState(state string, w *proto.WorkState) string {
	if state != proto.StateUnread || w == nil {
		return state
	}
	if w.Phase == proto.WorkDone || hubWaits(w) {
		return proto.StateIdle
	}
	return state
}

// hubWaits: w is a wait that ends without the user — the manager wakes it (wake_at), or it wakes itself (a background
// task, a /loop: on external with nothing named to wait for).
func hubWaits(w *proto.WorkState) bool {
	if w.Phase != proto.WorkBlocked || w.On == proto.WaitUser {
		return false
	}
	return w.WakeAt != nil || w.On == proto.WaitExternal && w.Wait == "" && w.Agent == nil
}

// interactionLine is what a pending interaction waits for, in a line.
func interactionLine(ix proto.Interaction) string {
	switch ix.Kind {
	case proto.IxQuestion:
		if len(ix.Questions) > 0 {
			return clip(oneLine(ix.Questions[0].Question), 60)
		}
		return "在问你"
	case proto.IxPlan:
		return "计划等你审批"
	case proto.IxPermission:
		if ix.Summary != "" {
			return clip(oneLine("等你授权："+ix.Summary), 60)
		}
		return "等你授权"
	case proto.IxElicitation:
		return "MCP 请求等你处理"
	}
	return "终端等你处理"
}

// workLogLine is what the knowledge base's project timeline gets for a finished turn: its conclusion when the manager
// found it done, else its last words (kbAgentDone).
func workLogLine(s proto.Session) string {
	if w := s.Work; w != nil && w.Phase == proto.WorkDone && w.Line != "" {
		line := "完成：" + w.Line
		for _, a := range w.Artifacts {
			if a.Kind == "pr" || a.Kind == "doc" {
				line += " " + filepath.ToSlash(a.Ref)
			}
		}
		return line
	}
	return s.LastText
}

// decoratedSession is one of m's sessions as the apps get it.
func (h *Hub) decoratedSession(m *Machine, sid string) (proto.Session, bool) {
	for _, s := range h.decorated(m) {
		if s.SessionID == sid {
			return s, true
		}
	}
	return proto.Session{}, false
}

// mutedSessions are those of list whose pushes are off (the manager opens no card for them).
func (h *Hub) mutedSessions(mid string, list []proto.Session) []string {
	out := []string{}
	for _, s := range list {
		if h.mutedAs(mid, s.SessionID, s.SpaceName) {
			out = append(out, s.SessionID)
		}
	}
	return out
}

// ---- telling the manager, asking it ----

// workSeen tells the manager the user looked at a session (session.seen: how long a triaged turn waited for them).
func (h *Hub) workSeen(machine, session string, at time.Time) {
	h.asst().notify(assistantwire.EventSessionSeen, assistantwire.SessionSeen{MachineID: machine, SessionID: session, At: at})
}

// Waiting for the manager: a hub that just started takes requests (queued on its socket, the apps' replays) before its
// child has shaken hands, and one that restarts is back in a second or two. A start or a resume waits for it this
// long; while it is still not there the hub fills in what it can itself.
var (
	managerStartWait   = 60 * time.Second // after the hub started, for a manager that never connected
	managerRestartWait = 10 * time.Second // after it went away
)

// waitManager waits, within ctx (less a few seconds for the call that follows), for the manager to be connected;
// false when it is not.
func (h *Hub) waitManager(ctx context.Context) bool {
	if h.busConnOf(assistantwire.AssistantID) != nil {
		return true
	}
	aw := &h.agentWork
	now := time.Now()
	aw.mu.Lock()
	var until time.Time
	if !aw.everUp {
		if aw.firstWait.IsZero() {
			aw.firstWait = now
		}
		until = aw.firstWait.Add(managerStartWait)
	} else {
		if aw.goneAt.IsZero() {
			aw.goneAt = now
		}
		until = aw.goneAt.Add(managerRestartWait)
	}
	aw.mu.Unlock()
	if d, ok := ctx.Deadline(); ok && d.Add(-5*time.Second).Before(until) {
		until = d.Add(-5 * time.Second)
	}
	for time.Now().Before(until) && ctx.Err() == nil {
		time.Sleep(100 * time.Millisecond)
		if h.busConnOf(assistantwire.AssistantID) != nil {
			return true
		}
	}
	return h.busConnOf(assistantwire.AssistantID) != nil
}

// managerConnected: the manager's handshake went through. A build other than the hub's may not split the Agents
// business the same way (both triage, or neither): said in the log.
func (h *Hub) managerConnected() {
	aw := &h.agentWork
	aw.mu.Lock()
	aw.everUp, aw.goneAt = true, time.Time{}
	aw.mu.Unlock()
	if c := h.busConnOf(assistantwire.AssistantID); c != nil && nodeMismatch(h.version, c.presence.Version) {
		h.logf("assistant: its build %q is not the hub's %q: the Agents triage may run in both or in neither", c.presence.Version, h.version)
	}
}

// agentStartParams has the manager fill in params, an Agent the app starts on m (POST /api/v1/sessions): what it runs
// on (模型配置 › Agents, the launcher / model / effort picked for this start over it) and, from nameFrom when it is not
// "", its tab's name. An error is the start's refusal (the app's to see, 400). While the manager is not connected
// (after waiting for it a little) the hub applies 模型配置 › Agents itself, which it still has; the tab is then named
// agent-<n> by the node.
func (h *Hub) agentStartParams(ctx context.Context, m *Machine, params map[string]any, nameFrom string) error {
	var res struct {
		Params map[string]any `json:"params"`
	}
	err := errBusOffline
	if h.waitManager(ctx) {
		err = h.asst().call(ctx, assistantwire.MethodAgentStartParams, assistantwire.AgentStartParams{MachineID: m.cfg.ID,
			Params: params, NameFrom: nameFrom}, &res)
	}
	var re *rpc.Error
	switch {
	case err == nil:
		if res.Params != nil {
			clear(params)
			for k, v := range res.Params {
				params[k] = v
			}
		}
		return nil
	case errors.As(err, &re) && re.Code == assistantwire.CodeBadRequest:
		return errors.New(re.Message)
	}
	h.logf("start on %s: the manager did not say what it runs on (%v): 模型配置 › Agents as the hub has it", m.cfg.ID, err)
	if resume, _ := params["resume_session_id"].(string); resume != "" {
		return nil // a resume runs on what the node knows it ran on
	}
	return h.applyModel(ctx, "agent.new", params)
}

// applyResumeLauncher adds 新建 Agent's launcher and model to p, a message that resumes sid on m (the manager's
// agents.resume_params: asked with no params, what it answers goes over p's). While the manager is not connected (after
// waiting for it a little) the hub applies 模型配置 › Agents itself; when that cannot run as a launcher the send goes
// as before (plain claude).
func (h *Hub) applyResumeLauncher(ctx context.Context, m *Machine, sid string, p map[string]any) {
	var res struct {
		Params map[string]any `json:"params"`
	}
	err := errBusOffline
	if h.waitManager(ctx) {
		err = h.asst().call(ctx, assistantwire.MethodAgentResumeParams, assistantwire.AgentResumeParams{MachineID: m.cfg.ID,
			SessionID: sid, Params: map[string]any{}}, &res)
	}
	if err != nil {
		h.logf("resume %s: the manager did not say what it runs on (%v): 模型配置 › Agents as the hub has it", sid, err)
		res.Params = map[string]any{}
		if err := h.applyModel(ctx, "agent.new", res.Params); err != nil {
			h.logf("resume %s: 新建 Agent's launcher not applied: %v", sid, err)
			return
		}
	}
	for _, k := range []string{"launcher", "model", "effort", "model_fingerprint"} {
		if v, ok := res.Params[k]; ok {
			p[k] = v
		}
	}
}

// autoTabName is a short name for the herdr tab of a new agent, from its first message (the manager's
// agents.tab_name). "" when there is no message, or no name came: the node names it agent-<n>.
func (h *Hub) autoTabName(ctx context.Context, prompt string) string {
	var res struct {
		Name string `json:"name"`
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if !h.waitManager(cctx) {
		return ""
	}
	if err := h.asst().call(cctx, assistantwire.MethodAgentTabName, map[string]any{"prompt": prompt}, &res); err != nil {
		h.logf("tab name: %v", err)
		return ""
	}
	return res.Name
}

// ---- answers that wait for the manager ----

// heldActs are the answers to the manager's questions and cards it did not take yet (it was not connected, or could
// not take them then), by interaction id: they go to it again after each snapshot it reports (deliverAnswers) until it
// takes them, says they are for nothing it has any more (CodeGone), or their card would have expired.
type heldActs struct {
	mu   sync.Mutex
	acts map[string]heldAct
	busy bool
}

type heldAct struct {
	act         assistantwire.Action
	until, next time.Time
}

// answerRetryAfter is how long an answer the manager could not take (while connected) waits before it is tried again.
var answerRetryAfter = 10 * time.Second

// keepAnswer holds act, which the manager did not take (err), until until; false when err is final (its question or
// card is gone): nothing is kept.
func (h *Hub) keepAnswer(act assistantwire.Action, err error, until time.Time) bool {
	var re *rpc.Error
	if errors.As(err, &re) && re.Code == assistantwire.CodeGone {
		return false
	}
	now := time.Now()
	if until.Before(now) {
		until = now.Add(24 * time.Hour) // as long as a card of the manager's lasts
	}
	next := now // not connected: the next snapshot is when it is back
	if !errors.As(err, &re) || re.Code != assistantwire.CodeAssistantOffline {
		next = now.Add(answerRetryAfter) // it could not take it now: not after every snapshot
	}
	u := &h.undelivered
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.acts == nil {
		u.acts = map[string]heldAct{}
	}
	u.acts[act.InteractionID] = heldAct{act: act, until: until, next: next}
	return true
}

// answerKept: an answer to interaction id is waiting for the manager.
func (h *Hub) answerKept(id string) bool {
	u := &h.undelivered
	u.mu.Lock()
	defer u.mu.Unlock()
	_, ok := u.acts[id]
	return ok
}

// deliverAnswers gives the manager the answers kept for it that are due (after a snapshot: it is there, its work read
// back). One delivery at a time.
func (h *Hub) deliverAnswers() {
	u := &h.undelivered
	u.mu.Lock()
	if u.busy || len(u.acts) == 0 {
		u.mu.Unlock()
		return
	}
	u.busy = true
	now := time.Now()
	var due []heldAct
	for id, a := range u.acts {
		switch {
		case now.After(a.until):
			delete(u.acts, id)
			h.logf("assistant: answer %s: dropped, its card expired before it was taken", id)
		case !a.next.After(now):
			delete(u.acts, id)
			due = append(due, a)
		}
	}
	u.mu.Unlock()
	defer func() {
		u.mu.Lock()
		u.busy = false
		u.mu.Unlock()
	}()
	for _, a := range due {
		if _, err := h.asst().Action(context.Background(), a.act); err != nil && !h.keepAnswer(a.act, err, a.until) {
			h.logf("assistant: answer %s: %v", a.act.InteractionID, err)
		}
	}
}
