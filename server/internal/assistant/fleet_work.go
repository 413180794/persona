package assistant

import (
	"context"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// Where each agent stands in its work as the session lists show it (docs/agent-work.md「模型」): what the Agents
// business reported for its latest turn while it is still stopped on it, what it shows while it works or asks.

// decorateWork puts on sess (decorated but for its work) where it stands in its work, and the state that follows.
// asking is the machine's pending interactions by session (an agent's own before the manager's card).
func (s *Service) decorateWork(mid string, sess *proto.Session, asking map[string]proto.Interaction) {
	r := &s.fleet.rep
	r.mu.RLock()
	on := r.workOn
	reported, ok := r.work[[2]string{mid, sess.SessionID}]
	r.mu.RUnlock()
	if !on {
		return
	}
	var e *proto.WorkState
	if ok {
		e = &reported
	}
	sess.Work = workOf(s.lang(context.Background()), *sess, e, asking)
	sess.State = workState(sess.State, sess.Work)
}

// workOf is sess's work state for the lists: reported is what the Agents business reported for its latest turn (nil:
// nothing), asking the pending interactions of its machine, by session.
func workOf(l i18n.Lang, sess proto.Session, reported *proto.WorkState, asking map[string]proto.Interaction) *proto.WorkState {
	if sess.Assistant || sess.Status == proto.StatusOffline {
		return nil
	}
	if sess.Status == proto.StatusWorking {
		return &proto.WorkState{Phase: proto.WorkWorking}
	}
	ix, asks := asking[sess.SessionID]
	if asks && ix.ToolName != assistantwire.WorkCardTool { // the agent asks itself: that is what it waits for
		return &proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitUser, Line: interactionLine(l, ix)}
	}
	if sess.Status == proto.StatusBlocked && !asks {
		return &proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitUser, Line: i18n.T(l, "hub-agents_link.screen")}
	}
	if reported == nil || reported.Phase == "" || !sameTurn(sess, reported.TurnAt) {
		return nil
	}
	w := *reported
	if w.InteractionID != "" && (!asks || ix.ID != w.InteractionID) {
		w.InteractionID = "" // answered, withdrawn, or not shown yet
	}
	if w.WakeAt != nil && time.Since(*w.WakeAt) > wakeGrace {
		w.WakeAt = nil // long overdue: nobody is waking it, it is no wait that ends by itself
	}
	return &w
}

// wakeGrace is how late a reported wake may be and still count: the Agents business sends what is due within its
// tick, and tries again a minute later when the machine was away.
var wakeGrace = 5 * time.Minute

// workState is the unified state once the work state is known: a turn that is done, or blocked on something the
// manager waits out for it, no longer counts as 完成未看 (docs/agent-work.md「模型」). A wait nobody will end (the wakes
// off, or dropped because the user went near it) stays 完成未看.
func workState(state string, w *proto.WorkState) string {
	if state != proto.StateUnread || w == nil {
		return state
	}
	if w.Phase == proto.WorkDone || managerWaits(w) {
		return proto.StateIdle
	}
	return state
}

// managerWaits: w is a wait that ends without the user — the manager wakes it (wake_at), or it wakes itself (a
// background task, a /loop: on external with nothing named to wait for).
func managerWaits(w *proto.WorkState) bool {
	if w.Phase != proto.WorkBlocked || w.On == proto.WaitUser {
		return false
	}
	return w.WakeAt != nil || w.On == proto.WaitExternal && w.Wait == "" && w.Agent == nil
}

// interactionLine is what a pending interaction waits for, in a line, in l.
func interactionLine(l i18n.Lang, ix proto.Interaction) string {
	switch ix.Kind {
	case proto.IxQuestion:
		if len(ix.Questions) > 0 {
			return clip(oneLine(ix.Questions[0].Question), 60)
		}
		return i18n.T(l, "hub-agents_link.question")
	case proto.IxPlan:
		return i18n.T(l, "hub-agents_link.plan")
	case proto.IxPermission:
		if ix.Summary != "" {
			return clip(oneLine(i18n.T(l, "hub-agents_link.permission_for", i18n.Args{"summary": ix.Summary})), 60)
		}
		return i18n.T(l, "hub-agents_link.permission")
	case proto.IxElicitation:
		return i18n.T(l, "hub-agents_link.elicitation")
	}
	return i18n.T(l, "hub-agents_link.screen")
}
