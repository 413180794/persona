package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
	"github.com/nwverse/persona/server/internal/screen"
)

// The interactions (docs/protocol.md「交互」): the cards the user answers on the phone — a hook's permission, question
// or plan (a node's interaction.create, waited for until it is answered, expires or its node goes; a resumable one
// outlives its node for its hook to claim again), a TUI dialog read off the screen (a "screen" card the manager
// answers itself with keys), a Codex question or approval the node lists, the assistant's own questions and cards,
// and what the hub's own features ask (fleet.ask). They are kept here, recorded in the interactions table, told the
// hub (fleet.interaction / fleet.interaction_resolved), pushed (fleet_push.go), and answered through the hub's
// relayed route (fleet_routes.go).

type pending struct {
	ix   proto.Interaction
	done chan proto.Resolution
	// local is set for interactions the manager resolves itself (screen dialogs, Codex cards): it acts on the answer,
	// and an error goes back to whoever answered (prompt_changed: nothing was pressed).
	local func(proto.Resolution) error
	// orphaned is set while no hook waits on a resumable interaction (its node went away, or the manager restarted):
	// it ends the interaction unless the hook claims it again in time.
	orphaned *time.Timer
	// checkedAt / checking bound transcript reads for a plan answered outside Persona.
	checkedAt time.Time
	checking  bool
	// answeredBy is who resolved it (Resolution.AnsweredBy), so the hub can say the assistant answered.
	answeredBy string
	// codex: a card a node listed for a Codex TUI; it lasts as long as the node lists it.
	codex bool
	// claimed: an answer was taken for this hook-waited card (resolve). It stays in the map until await or
	// finishInteraction ends it, but a second answer finds it no longer pending.
	claimed bool
}

// resumeGrace is how long a resumable interaction outlives its node: a little longer than hooks keep reconnecting
// (node.ResumeWait).
var resumeGrace = 3 * time.Minute

// ixMap is the pending interactions, made on first use. Callers hold s.fleet.mu.
func (s *Service) ixMapLocked() map[string]*pending {
	if s.fleet.interactions == nil {
		s.fleet.interactions = map[string]*pending{}
	}
	return s.fleet.interactions
}

// hasHooks: Persona's hooks run in the session — it sent a hook event since the manager started, or Persona started
// it (with its hooks) — so they ask for most dialogs themselves and a screen card waits for them a little.
func (s *Service) hasHooks(machine string, sess proto.Session) bool {
	s.fleet.mu.Lock()
	defer s.fleet.mu.Unlock()
	return s.fleet.hooked[[2]string{machine, sess.SessionID}] || sess.StartedByJarvis
}

// hasElicitationClient: an app that takes MCP elicitations is connected (the hub says, fleet.clients).
func (s *Service) hasElicitationClient() bool {
	s.fleet.mu.Lock()
	defer s.fleet.mu.Unlock()
	return s.fleet.clients.Elicitation
}

// ---- screen cards ----

// syncScreenInteractions turns blocked agents without a hook-driven interaction into "screen" interactions
// (folder trust, model pickers, any TUI dialog hooks cannot see), and clears them once the agent moves on.
func (s *Service) syncScreenInteractions(machine string, list []proto.Session) {
	blocked := map[string]proto.Session{}
	for _, sess := range list {
		// A session without a terminal (a source whose capabilities leave it out: Codex app-server, an agent runtime)
		// has no screen to read a dialog off: it asks through its own cards, and blocked there means it waits in its
		// own UI (docs/runtimes.md).
		if sess.Status == proto.StatusBlocked && (sess.SourceKind == "" || sess.Capabilities != nil && sess.Capabilities.Terminal) {
			blocked[sess.SessionID] = sess
		}
	}
	s.fleet.mu.Lock()
	var stale []string
	hasHook := map[string]bool{}
	for id, p := range s.fleet.interactions {
		if p.ix.MachineID != machine {
			continue
		}
		if p.ix.Kind == proto.IxScreen {
			if _, still := blocked[p.ix.SessionID]; !still {
				stale = append(stale, id)
			}
			hasHook[p.ix.SessionID] = true
		} else if coversScreen(p) {
			hasHook[p.ix.SessionID] = true
		}
	}
	s.fleet.mu.Unlock()
	for _, id := range stale {
		s.recordEnd(id, "cancelled", nil) // answered at the computer, or gone: not pending in the history either
		s.finishInteraction(id)
	}
	for sid, sess := range blocked {
		if hasHook[sid] {
			continue
		}
		go s.createScreenInteraction(machine, sess)
	}
}

func (s *Service) createScreenInteraction(machine string, sess proto.Session) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Hook dialogs flip the agent to blocked a moment before the hook reaches us; give them a head start. A Codex
	// node reads its approvals off the screen once a second: a little longer. A Claude Code without Persona's hooks
	// (the user's own profile) asks only on the screen: its card is made at once (#751).
	if wait := screenCardWait(sess, s.hasHooks(machine, sess)); wait > 0 {
		time.Sleep(wait)
	}
	if cur, ok := s.nodeSession(machine, sess.SessionID); !ok || cur.Status != proto.StatusBlocked {
		return
	}
	var scr struct {
		Text string `json:"text"`
	}
	if err := s.nodeCall(ctx, machine, "session.screen", map[string]any{"session_id": sess.SessionID, "lines": 40}, &scr); err != nil {
		return
	}
	d := screen.ParseDialog(scr.Text)
	id := "scr_" + machine + "_" + sess.SessionID
	if d != nil {
		id += "_" + d.Rev // another dialog is another card: nothing picked on the last one carries over
	}
	ix := proto.Interaction{
		ID: id, MachineID: machine, SessionID: sess.SessionID, SessionTitle: sess.Title, Cwd: sess.Cwd, Kind: proto.IxScreen,
		CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(24 * time.Hour).UTC(), Screen: trimScreen(scr.Text), Dialog: d,
		Secret: screen.SecretPrompt(scr.Text),
	}
	s.fleet.mu.Lock()
	asking := false
	for _, p := range s.fleet.interactions {
		if p.ix.MachineID == machine && p.ix.SessionID == sess.SessionID {
			if p.ix.Kind == proto.IxScreen || coversScreen(p) {
				s.fleet.mu.Unlock()
				return
			}
			asking = true
		}
	}
	if asking && d == nil { // only the async question's input box on screen: its card says it all
		s.fleet.mu.Unlock()
		return
	}
	s.ixMapLocked()[id] = &pending{ix: ix, local: func(r proto.Resolution) error {
		if r.Dismiss {
			return nil
		}
		params, err := screenAnswer(sess.SessionID, d, r)
		if err != nil {
			return err
		}
		kctx, kcancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer kcancel()
		err = s.nodeCall(kctx, machine, "session.keys", params, nil)
		var re *rpc.Error
		if errors.As(err, &re) && re.Code == screen.PromptChanged {
			go s.createScreenInteraction(machine, sess) // the card for what is on screen now
		} else if err == nil {
			go s.recheckScreen(machine, sess.SessionID, ix.ID)
		}
		return err
	}}
	s.fleet.mu.Unlock()
	s.record(ix)
	s.announce(ix)
	if d == nil {
		go s.upgradeScreenCard(machine, sess.SessionID, id)
	}
}

// screenUpgrade: how often, and screenUpgradeFor how long, a screen card made without a dialog it could read looks
// at the screen again (upgradeScreenCard).
var screenUpgrade, screenUpgradeFor = 500 * time.Millisecond, 30 * time.Second

// upgradeScreenCard: the agent may be called blocked before it draws its dialog (Orca does so from its PreToolUse
// hook; on Windows the hooks take seconds before AskUserQuestion's dialog shows), so the card made then has no dialog
// to answer. Look at the screen again; once a dialog shows, its card replaces this one. Stop when this card is
// answered or gone, the agent no longer blocked, or the time up (a dialog the manager cannot read: the card stays).
func (s *Service) upgradeScreenCard(machine, session, id string) {
	for end := time.Now().Add(screenUpgradeFor); time.Now().Before(end); {
		time.Sleep(screenUpgrade)
		cur, ok := s.nodeSession(machine, session)
		if !ok || cur.Status != proto.StatusBlocked {
			return
		}
		s.fleet.mu.Lock()
		_, still := s.fleet.interactions[id]
		s.fleet.mu.Unlock()
		if !still {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		var scr struct {
			Text string `json:"text"`
		}
		err := s.nodeCall(ctx, machine, "session.screen", map[string]any{"session_id": session, "lines": 40}, &scr)
		cancel()
		if err != nil {
			return
		}
		if screen.ParseDialog(scr.Text) != nil {
			s.finishInteraction(id)
			s.createScreenInteraction(machine, cur)
			return
		}
	}
}

// screenRecheck: how often, and screenRecheckFor how long, the screen is looked at again after a screen card's
// answer (recheckScreen).
var screenRecheck, screenRecheckFor = 250 * time.Millisecond, 2 * time.Second

// recheckScreen: after a screen card's answer the agent may ask again at once (the next question of an
// AskUserQuestion, the next permission) while herdr keeps calling it blocked, so no status change makes that card.
// Look again until a dialog that is not the answered one shows (its card is made), the agent works again (a new
// question then comes with its own change to blocked), or another card is up (#751).
func (s *Service) recheckScreen(machine, session, answered string) {
	for end := time.Now().Add(screenRecheckFor); time.Now().Before(end); {
		time.Sleep(screenRecheck)
		cur, ok := s.nodeSession(machine, session)
		if !ok || cur.Status != proto.StatusBlocked {
			return
		}
		s.fleet.mu.Lock()
		asked := false
		for _, p := range s.fleet.interactions {
			asked = asked || p.ix.MachineID == machine && p.ix.SessionID == session
		}
		s.fleet.mu.Unlock()
		if asked {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		var scr struct {
			Text string `json:"text"`
		}
		err := s.nodeCall(ctx, machine, "session.screen", map[string]any{"session_id": session, "lines": 40}, &scr)
		cancel()
		if err != nil {
			return
		}
		// No dialog (the screen between two) or the answered one (the keys not landed yet): look again.
		if d := screen.ParseDialog(scr.Text); d != nil && "scr_"+machine+"_"+session+"_"+d.Rev != answered {
			s.createScreenInteraction(machine, cur)
			return
		}
	}
}

// screenCardWait is how long a blocked session's screen card waits for a card its agent asks for itself.
func screenCardWait(sess proto.Session, hooks bool) time.Duration {
	switch {
	case sess.Agent == "codex" || sess.Agent == "traex":
		return 3 * time.Second
	case hooks:
		return 1500 * time.Millisecond
	}
	return 0
}

// coversScreen: the interaction stands for what the terminal waits on (a hook's, a Codex approval or overlay
// question), so no screen card is made beside it. A Codex async question does not: Codex shows no dialog for it,
// though herdr calls the agent blocked while it is open.
func coversScreen(p *pending) bool { return !(p.codex && p.ix.ToolName == "request_user_input_async") }

// screenAnswer turns an answer to a screen card into session.keys parameters. With a dialog the node checks its
// revision before pressing anything; an option's keys come from the node's fresh reading of the screen, and the
// ones worked out here are for nodes that predate that.
func screenAnswer(session string, d *screen.Dialog, r proto.Resolution) (map[string]any, error) {
	params := map[string]any{"session_id": session, "keys": r.Keys, "text": r.Text, "pre_keys": r.PreKeys}
	if d == nil {
		return params, nil
	}
	params["rev"] = d.Rev
	if picked := d.Option(r.Option); d.Multi && !(len(r.Options) == 0 && picked != nil && picked.Kind == screen.KindChat) {
		// Checked and sent by the node one key at a time (node/dialogmulti.go); "Chat about this" is pressed as it is.
		// An app from before multi-select cards names one option: that one alone (the free-text field: the text alone).
		options := r.Options
		if len(options) == 0 && r.Option != "" {
			if o := d.Option(r.Option); o != nil && o.Kind == "" {
				options = []string{o.Key}
			}
		}
		for _, k := range options {
			if o := d.Option(k); o == nil || o.Kind != "" {
				return nil, fmt.Errorf("the dialog has no option %q to check", k)
			}
		}
		if len(options) == 0 && strings.TrimSpace(r.Text) == "" {
			return nil, i18n.Err("hub-hub.pick_or_answer")
		}
		return map[string]any{"session_id": session, "rev": d.Rev, "multi": true, "options": options, "free_text": r.Text}, nil
	}
	if r.Option == "" {
		return params, nil
	}
	o := d.Option(r.Option)
	if o == nil {
		return nil, fmt.Errorf("the dialog has no option %q", r.Option)
	}
	params["option"] = o.Key
	if o.Kind == screen.KindTypeSomething {
		params["pre_keys"], params["keys"] = o.Keys, []string{"enter"}
	} else {
		params["keys"], params["text"], params["pre_keys"] = o.Keys, "", nil
	}
	return params, nil
}

func trimScreen(text string) string {
	lines := strings.Split(strings.TrimRight(text, "\n "), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	return strings.Join(lines, "\n")
}

// ---- telling ----

// announce puts a new card on the apps (through the hub), pushes it, and gives it to the channels.
func (s *Service) announce(ix proto.Interaction) {
	ix = s.withTask(ix)
	s.hub.Tell(assistantwire.EventFleetInteraction, assistantwire.FleetInteraction{Interaction: ix})
	s.publishSessions(ix.MachineID)
	if s.quietSession(ix.MachineID, ix.SessionID) {
		// A quiet task's question is no push, bot card or channel message: the main conversation is told (#1731).
		go s.InteractionRaised(context.Background(), InteractionRaised{Interaction: ix})
		return
	}
	if !s.muted(ix.MachineID, ix.SessionID) {
		s.notifyInteraction(ix, s.machineName(ix.MachineID))
	}
	s.InteractionAnnounced(context.Background(), InteractionAnnounced{Interaction: ix}) // the Feishu bot, the channels
}

// show puts a card on every app without a push (one they were told about already).
func (s *Service) show(ix proto.Interaction) {
	s.hub.Tell(assistantwire.EventFleetInteraction, assistantwire.FleetInteraction{Interaction: s.withTask(ix)})
	s.publishSessions(ix.MachineID)
}

// ---- waiting ----

// nodeInteraction is a node's interaction.create: allowed right away when 本会话总是允许 already covers it, else
// shown and waited for.
func (s *Service) nodeInteraction(ctx context.Context, ix proto.Interaction) (proto.Resolution, error) {
	if r, ok := s.alwaysAllowed(ctx, ix); ok {
		if ix.Resumable { // its hook's earlier attempt left the card pending: it is answered too
			_ = s.resolve(ix.ID, r)
		}
		return r, nil
	}
	return s.waitInteraction(ctx, ix)
}

// waitInteraction registers a hook interaction and blocks until it is resolved, cancelled or expires.
// A resumable one asked again with the same id (its hook reconnected after a manager or node restart) is the same
// interaction: the wait resumes on it, or returns the answer given meanwhile. One the history has as still pending
// (the manager restarted with it on the phone) is shown again without a second push.
func (s *Service) waitInteraction(ctx context.Context, ix proto.Interaction) (proto.Resolution, error) {
	if ix.ExpiresAt.IsZero() {
		ix.ExpiresAt = time.Now().Add(s.notifyTimings().hookTimeout()).UTC()
	}
	again := false
	if ix.Resumable {
		s.fleet.mu.Lock()
		if p := s.fleet.interactions[ix.ID]; p != nil && p.local == nil {
			if p.orphaned != nil {
				p.orphaned.Stop()
				p.orphaned = nil
			}
			s.fleet.mu.Unlock()
			if ix.Kind == proto.IxElicitation && !s.hasElicitationClient() {
				_ = s.resolve(ix.ID, proto.Resolution{Dismiss: true})
			}
			return s.await(ctx, p)
		}
		s.fleet.mu.Unlock()
		status, r, ended := s.outcome(ix.ID)
		if ended {
			return r, nil
		}
		again = status == "pending"
	}
	if ix.Kind == proto.IxElicitation && !s.hasElicitationClient() {
		return proto.Resolution{Dismiss: true}, nil
	}
	p := &pending{ix: ix, done: make(chan proto.Resolution, 1)}
	s.fleet.mu.Lock()
	// A screen interaction for the same session was a premature guess; the hook knows better.
	// Tell the app now: the wait below can last until the hook times out.
	var dropped []*pending
	m := s.ixMapLocked()
	for id, old := range m {
		if old.ix.Kind == proto.IxScreen && old.ix.MachineID == ix.MachineID && old.ix.SessionID == ix.SessionID {
			delete(m, id)
			dropped = append(dropped, old)
		}
	}
	m[ix.ID] = p
	s.fleet.mu.Unlock()
	for _, old := range dropped { // as a Codex card withdraws a screen card
		s.recordEnd(old.ix.ID, "cancelled", nil)
		s.finished(old)
	}
	if again {
		s.show(ix)
	} else {
		s.record(ix)
		s.announce(ix)
	}
	return s.await(ctx, p)
}

// await blocks on a registered interaction. When ctx ends — the node's connection is gone — a resumable
// interaction stays pending for its hook to claim again.
func (s *Service) await(ctx context.Context, p *pending) (proto.Resolution, error) {
	id := p.ix.ID
	timer := time.NewTimer(time.Until(p.ix.ExpiresAt))
	defer timer.Stop()
	select {
	case r := <-p.done:
		s.finishInteraction(id)
		return r, nil
	case <-timer.C:
		s.recordEnd(id, "expired", nil)
		s.finishInteraction(id)
		return proto.Resolution{Dismiss: true}, nil
	case <-ctx.Done():
		if p.ix.Resumable {
			s.fleet.mu.Lock()
			kept := s.fleet.interactions[id] == p && s.orphanLocked(p)
			s.fleet.mu.Unlock()
			if kept {
				return proto.Resolution{Dismiss: true}, nil // nobody reads it: the node is gone
			}
		}
		s.recordEnd(id, "cancelled", nil)
		s.finishInteraction(id)
		return proto.Resolution{Dismiss: true}, nil
	}
}

// orphanLocked starts p's grace period: it ends (cancelled) unless its hook claims it again first. Callers hold
// s.fleet.mu.
func (s *Service) orphanLocked(p *pending) bool {
	if p.orphaned != nil {
		return true
	}
	wait := min(resumeGrace, time.Until(p.ix.ExpiresAt))
	if wait <= 0 {
		return false
	}
	p.orphaned = time.AfterFunc(wait, func() {
		s.fleet.mu.Lock()
		still := s.fleet.interactions[p.ix.ID] == p && p.orphaned != nil
		s.fleet.mu.Unlock()
		if still {
			s.recordEnd(p.ix.ID, "cancelled", nil)
			s.finishInteraction(p.ix.ID)
		}
	})
	return true
}

// outcome reports how a resumable interaction no longer pending here ended: the answer given before its hook could
// read it (the manager or node restarted right then), or a dismiss when it was cancelled or expired. status is the
// history's ("pending": recorded, never ended).
func (s *Service) outcome(id string) (status string, r proto.Resolution, ended bool) {
	st := s.fleetStore()
	if st == nil {
		return "", proto.Resolution{}, false
	}
	status, res, err := st.InteractionOutcome(context.Background(), id)
	switch {
	case err != nil || status == "" || status == "pending":
		return status, proto.Resolution{}, false
	case status == "resolved" && res != nil:
		return status, *res, true
	default:
		return status, proto.Resolution{Dismiss: true}, true
	}
}

// record / recordEnd keep the interaction history in the database (best effort).
func (s *Service) record(ix proto.Interaction) {
	st := s.fleetStore()
	if st == nil {
		return
	}
	if err := st.InsertInteraction(context.Background(), ix); err != nil {
		s.logf("record interaction: %v", err)
	}
}

func (s *Service) recordEnd(id, status string, r *proto.Resolution) {
	st := s.fleetStore()
	if st == nil {
		return
	}
	if err := st.FinishInteraction(context.Background(), id, status, r); err != nil {
		s.logf("record interaction outcome: %v", err)
	}
}

// errNotPending: the interaction was answered, withdrawn or expired already.
var errNotPending = errors.New("no longer pending")

var errInvalidElicitation = errors.New("elicitation requires action accept, decline or cancel")

// resolve answers an interaction. A screen card acts right away and its error comes back (the dialog changed:
// nothing pressed, a new card follows).
func (s *Service) resolve(id string, r proto.Resolution) error {
	s.fleet.mu.Lock()
	p := s.fleet.interactions[id]
	if p != nil && p.ix.Kind == proto.IxElicitation && !r.Dismiss &&
		r.Action != "accept" && r.Action != "decline" && r.Action != "cancel" {
		s.fleet.mu.Unlock()
		return errInvalidElicitation
	}
	if p != nil && r.Allow != nil && *r.Allow {
		for _, f := range p.ix.Fields {
			if !f.Optional && strings.TrimSpace(r.Values[f.Key]) == "" {
				s.fleet.mu.Unlock()
				return i18n.Err("hub-hub.fill_in", i18n.Args{"field": f.Label})
			}
		}
	}
	if p != nil && p.claimed {
		p = nil // answered a moment ago: the first answer is the one the hook and the history get
	}
	if p != nil && p.local != nil {
		delete(s.fleet.interactions, id) // claimed: a second answer to the same card is not pending any more
	} else if p != nil {
		p.claimed = true
	}
	if p != nil {
		p.answeredBy = r.AnsweredBy // read by finished(), under this lock
	}
	s.fleet.mu.Unlock()
	if p == nil {
		return fmt.Errorf("interaction %s is %w", id, errNotPending)
	}
	status := "resolved"
	if r.Dismiss {
		status = "dismissed"
	}
	if p.local != nil {
		err := p.local(r)
		if err != nil {
			status = "failed"
			s.fleet.mu.Lock()
			back := s.fleet.interactions[id] == p
			s.fleet.mu.Unlock()
			if back { // a Codex card the node could not answer is open again (codexPending): it is not over
				go s.InteractionResolved(context.Background(), InteractionResolved{Interaction: p.ix, Resolution: r, Status: status})
				return err
			}
		} else {
			s.channelsEnded(p.ix, &r)
		}
		// What was typed into a terminal may be a password: the history keeps that something was, not what.
		kept := redacted(p.ix, r)
		if kept.Text != "" {
			kept.Text = "<redacted>"
		}
		s.recordEnd(id, status, &kept)
		s.finished(p)
		go s.InteractionResolved(context.Background(), InteractionResolved{Interaction: p.ix, Resolution: r, Status: status})
		return err
	}
	kept := redacted(p.ix, r)
	s.recordEnd(id, status, &kept)
	s.channelsEnded(p.ix, &r)
	go s.InteractionResolved(context.Background(), InteractionResolved{Interaction: p.ix, Resolution: r, Status: status})
	s.keepAlways(p.ix, r) // kept before the hook hears: its next card may follow at once
	select {
	case p.done <- r:
	default:
	}
	s.finishInteraction(id)
	return nil
}

// channelsEnded tells the channels a card is over: r is how it was answered (nil: withdrawn, expired, or over after
// the answer that was told already).
func (s *Service) channelsEnded(ix proto.Interaction, r *proto.Resolution) {
	if f := s.fleetEnded; f != nil {
		f(ix, r)
	}
	s.InteractionEnded(context.Background(), InteractionEnded{Interaction: ix, Resolution: r})
}

// redacted is the resolution as the history keeps it: secret fields say they were filled in, not what with.
func redacted(ix proto.Interaction, r proto.Resolution) proto.Resolution {
	if len(r.Values) == 0 {
		return r
	}
	secret := map[string]bool{}
	for _, f := range ix.Fields {
		secret[f.Key] = f.Secret
	}
	vals := make(map[string]string, len(r.Values))
	for k, v := range r.Values {
		if secret[k] {
			v = "<redacted>"
		}
		vals[k] = v
	}
	r.Values = vals
	return r
}

func (s *Service) finishInteraction(id string) {
	s.fleet.mu.Lock()
	p, ok := s.fleet.interactions[id]
	delete(s.fleet.interactions, id)
	if ok && p.orphaned != nil {
		p.orphaned.Stop()
	}
	s.fleet.mu.Unlock()
	if ok {
		s.finished(p)
	}
}

// finished tells everyone an interaction, already out of the pending set, is over.
func (s *Service) finished(p *pending) {
	s.fleet.mu.Lock()
	answeredBy := p.answeredBy
	s.fleet.mu.Unlock()
	s.channelsEnded(p.ix, nil)
	// the app marks a question the assistant answered (docs/assistant.md)
	s.hub.Tell(assistantwire.EventFleetResolved, assistantwire.FleetResolved{ID: p.ix.ID, AnsweredBy: answeredBy})
	s.publishSessions(p.ix.MachineID)
}

// pendingInteractions are the pending interactions, oldest first, with the task each agent works for.
func (s *Service) pendingInteractions() []proto.Interaction {
	s.fleet.mu.Lock()
	out := []proto.Interaction{}
	for _, p := range s.fleet.interactions {
		out = append(out, p.ix)
	}
	s.fleet.mu.Unlock()
	for i := range out {
		out[i] = s.withTask(out[i])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// pendingIx is one pending interaction; false when it is not pending.
func (s *Service) pendingIx(id string) (proto.Interaction, bool) {
	s.fleet.mu.Lock()
	defer s.fleet.mu.Unlock()
	if p := s.fleet.interactions[id]; p != nil {
		return p.ix, true
	}
	return proto.Interaction{}, false
}

func (s *Service) expireLoop(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s.fleet.mu.Lock()
		var plans []string
		seen := map[string]bool{}
		for _, p := range s.fleet.interactions {
			if p.ix.Kind == proto.IxPlan && !seen[p.ix.MachineID] {
				plans = append(plans, p.ix.MachineID)
				seen[p.ix.MachineID] = true
			}
		}
		s.fleet.mu.Unlock()
		for _, m := range plans {
			s.syncPlanInteractions(m) // also converge after a lost rev/status event
		}
		s.fleet.mu.Lock()
		var ids []string
		for id, p := range s.fleet.interactions {
			if p.local != nil && time.Now().After(p.ix.ExpiresAt) {
				ids = append(ids, id)
			}
		}
		s.fleet.mu.Unlock()
		for _, id := range ids {
			s.finishInteraction(id)
		}
	}
}

// askHub is fleet.ask: a card one of the hub's own features asks (a mail send, Cua's apps, jarvis_settings),
// waited for like a hook's.
func (s *Service) askHub(ctx context.Context, ix proto.Interaction) (proto.Resolution, error) {
	if ix.ID == "" {
		ix.ID = "ix_" + randomHex(6)
	}
	if ix.CreatedAt.IsZero() {
		ix.CreatedAt = time.Now().UTC()
	}
	if m, ok := s.fleetMachine(ix.MachineID); ok {
		ix.MachineID = m.ID
	}
	return s.waitInteraction(ctx, ix)
}

// ---- 本会话总是允许 (#2202, docs/assistant.md「本会话总是允许」) ----

// Claude's session rule and Codex's acceptForSession live in the agent's process, and the assistant's process is
// replaced all the time (idle, redeploys, a launcher switch): the resumed one asked about the same tool again. The
// node names what such a card grants (proto.Interaction.AlwaysKeys, only on assistant conversations); the manager
// keeps the keys once a card is answered so, and answers a later card whose keys are all kept itself, with always
// again so the new process takes its own session rule from it.

// maxAlwaysKeys bounds what one card may name (a file change's paths, a gated command's calls).
const maxAlwaysKeys = 32

func alwaysCard(ix proto.Interaction) bool {
	return ix.Kind == proto.IxPermission && ix.SessionID != "" && len(ix.AlwaysKeys) > 0 &&
		len(ix.AlwaysKeys) <= maxAlwaysKeys && len(ix.Fields) == 0
}

// alwaysAllowed: every key ix names was granted with 本会话总是允许 in its conversation.
func (s *Service) alwaysAllowed(ctx context.Context, ix proto.Interaction) (proto.Resolution, bool) {
	st := s.fleetStore()
	if st == nil || !alwaysCard(ix) {
		return proto.Resolution{}, false
	}
	ok, err := st.AlwaysAllowed(ctx, ix.SessionID, ix.AlwaysKeys)
	if err != nil {
		s.logf("[%s] 本会话总是允许: %v", ix.MachineID, err)
		return proto.Resolution{}, false
	}
	if !ok {
		return proto.Resolution{}, false
	}
	s.logf("[%s] %s allowed: 本会话总是允许 in this conversation", ix.MachineID, ix.ToolName)
	yes := true
	return proto.Resolution{Allow: &yes, Always: true}, true
}

// keepAlways keeps what a card answered with 本会话总是允许 grants, then answers the conversation's other cards
// waiting meanwhile that it covers now (parallel tool calls ask at once).
func (s *Service) keepAlways(ix proto.Interaction, r proto.Resolution) {
	st := s.fleetStore()
	if st == nil || !alwaysCard(ix) || r.Allow == nil || !*r.Allow || !r.Always {
		return
	}
	ctx := context.Background()
	if err := st.AllowAlways(ctx, ix.SessionID, ix.ToolName, ix.AlwaysKeys); err != nil {
		s.logf("[%s] keep 本会话总是允许: %v", ix.MachineID, err)
		return
	}
	s.fleet.mu.Lock()
	var waiting []proto.Interaction
	for _, p := range s.fleet.interactions {
		if p.ix.ID != ix.ID && p.ix.SessionID == ix.SessionID && p.ix.MachineID == ix.MachineID &&
			p.local == nil && !p.codex && !p.claimed && alwaysCard(p.ix) {
			waiting = append(waiting, p.ix)
		}
	}
	s.fleet.mu.Unlock()
	for _, w := range waiting {
		if res, ok := s.alwaysAllowed(ctx, w); ok {
			_ = s.resolve(w.ID, res)
		}
	}
}

// ---- Codex cards (docs/codex.md「提问和审批」) ----

// A node lists the Codex questions and approvals open on its machine whenever the list changes (codex.interactions,
// node/codexask.go); the manager keeps a card for each — a new one is announced, one no longer listed (answered at
// the computer, the turn interrupted, the agent gone) ends. An answer goes back to the node (codex.answer), which
// types it into the TUI and says whether Codex took it: that error reaches whoever answered.

// codexAnswerTimeout: the node checks the screen, types, and waits for Codex to take the answer (its codexConfirm).
var codexAnswerTimeout = time.Minute

// codexAnnouncedFor: which cards were announced (pushed) lately is remembered this long: a node that reconnects
// lists them again, and the phone should not ring twice for one question.
const codexAnnouncedFor = 24 * time.Hour

func (s *Service) syncCodexInteractions(machine string, params json.RawMessage) {
	var p struct {
		Interactions []proto.Interaction `json:"interactions"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	listed := map[string]proto.Interaction{}
	for _, ix := range p.Interactions {
		ix.MachineID = machine
		listed[ix.ID] = ix
	}
	s.fleet.mu.Lock()
	m := s.ixMapLocked()
	var gone []string
	var screens []*pending
	for id, pd := range m {
		if !pd.codex || pd.ix.MachineID != machine {
			continue
		}
		if _, ok := listed[id]; ok {
			delete(listed, id) // already a card
		} else {
			gone = append(gone, id)
		}
	}
	var added []proto.Interaction
	for _, ix := range listed {
		if m[ix.ID] != nil {
			continue
		}
		pd := s.codexPending(machine, ix)
		// A screen card for the same session read the terminal without knowing Codex: an approval or an overlay
		// question card is that dialog read properly. An async question is not on the terminal's keyboard at all.
		for id, old := range m {
			if coversScreen(pd) && old.ix.Kind == proto.IxScreen && old.ix.MachineID == ix.MachineID && old.ix.SessionID == ix.SessionID {
				delete(m, id)
				screens = append(screens, old)
			}
		}
		m[ix.ID] = pd
		added = append(added, ix)
	}
	now := time.Now()
	quiet := map[string]bool{}
	if s.fleet.codexAnnounced == nil {
		s.fleet.codexAnnounced = map[string]time.Time{}
	}
	for id, at := range s.fleet.codexAnnounced {
		if now.Sub(at) > codexAnnouncedFor {
			delete(s.fleet.codexAnnounced, id)
		}
	}
	for _, ix := range added {
		if _, ok := s.fleet.codexAnnounced[ix.ID]; ok {
			quiet[ix.ID] = true
		}
		s.fleet.codexAnnounced[ix.ID] = now
	}
	s.fleet.mu.Unlock()
	for _, old := range screens {
		s.recordEnd(old.ix.ID, "cancelled", nil)
		s.finished(old)
	}
	for _, id := range gone {
		s.recordEnd(id, "cancelled", nil)
		s.finishInteraction(id)
	}
	for _, ix := range added {
		s.record(ix)
		if quiet[ix.ID] {
			s.show(ix)
		} else {
			s.announce(ix)
		}
	}
}

// codexPending is the manager's side of a Codex card: answering it asks the node. When the node could not type the
// answer (a draft in the box, the TUI elsewhere) the card comes back, still open on the computer; when the node says
// Codex no longer asks it (prompt_changed), the node's next list has what is there now.
func (s *Service) codexPending(machine string, ix proto.Interaction) *pending {
	p := &pending{ix: ix, codex: true}
	p.local = func(r proto.Resolution) error {
		ctx, cancel := context.WithTimeout(context.Background(), codexAnswerTimeout)
		defer cancel()
		err := s.nodeCall(ctx, machine, "codex.answer", map[string]any{"session_id": ix.SessionID, "id": ix.ID, "resolution": r}, nil)
		var re *rpc.Error
		if err != nil && !r.Dismiss && !(errors.As(err, &re) && re.Code == screen.PromptChanged) {
			s.fleet.mu.Lock()
			if m := s.ixMapLocked(); m[ix.ID] == nil {
				m[ix.ID] = p
			}
			s.fleet.mu.Unlock()
			s.show(ix)
		}
		return err
	}
	return p
}

// ---- plans answered in the terminal ----

// A plan answered in the terminal can outlive its PermissionRequest hook. Reconcile the pending card with the
// completed ExitPlanMode call, not the agent's status or words: working is not proof that anyone approved a plan.
func (s *Service) syncPlanInteractions(machine string) {
	now := time.Now()
	s.fleet.mu.Lock()
	var check []*pending
	for _, p := range s.fleet.interactions {
		if p.ix.MachineID != machine || p.ix.Kind != proto.IxPlan || p.ix.ToolName != "ExitPlanMode" || p.local != nil ||
			p.checking || now.Sub(p.checkedAt) < 2*time.Second {
			continue
		}
		p.checking, p.checkedAt = true, now
		check = append(check, p)
	}
	s.fleet.mu.Unlock()
	for _, p := range check {
		go s.checkPlanInteraction(machine, p)
	}
}

func (s *Service) checkPlanInteraction(machine string, p *pending) {
	defer func() {
		s.fleet.mu.Lock()
		p.checking = false
		s.fleet.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var items []proto.Item
	before := 0
	// No transcript-wide scan: most plans are in the latest two turns. A few older pages cover a user continuing
	// after answering in the terminal; pages have the node's item and parse budgets too.
	for page := 0; page < 3; page++ {
		var res struct {
			Items   []proto.Item `json:"items"`
			HasMore bool         `json:"has_more"`
		}
		if err := s.nodeCall(ctx, machine, "session.items", map[string]any{"session_id": p.ix.SessionID, "turns": 2,
			"before_seq": before, "lazy_images": true}, &res); err != nil || len(res.Items) == 0 {
			return
		}
		items = append(items, res.Items...)
		if planCallFinished(p.ix, items) {
			s.finishExternalPlan(p)
			return
		}
		next := res.Items[0].Seq
		if !res.HasMore || next <= 0 || (before > 0 && next >= before) {
			return
		}
		before = next
	}
}

// PermissionRequest in some Claude Code versions omits tool_use_id. In that case the call immediately preceding
// this hook is identified by its timestamp; ambiguous calls, missing timestamps and earlier results fail closed.
// The result means only that the call ended (approval, rejection or interruption); no resolution is invented.
func planCallFinished(ix proto.Interaction, items []proto.Item) bool {
	id := ix.ToolUseID
	if id == "" {
		if ix.CreatedAt.IsZero() {
			return false
		}
		for _, it := range items {
			if it.Role != "assistant" || it.Kind != proto.KindToolUse || it.ToolName != "ExitPlanMode" || it.ToolUseID == "" ||
				it.TS.IsZero() || it.TS.After(ix.CreatedAt) || ix.CreatedAt.Sub(it.TS) > 30*time.Second {
				continue
			}
			if id != "" && id != it.ToolUseID {
				return false // two candidate plans: neither can be safely attributed to this hook
			}
			id = it.ToolUseID
		}
	}
	if id == "" {
		return false
	}
	for _, it := range items {
		if it.Role == "user" && it.Kind == proto.KindToolResult && it.ToolUseID == id &&
			!it.TS.IsZero() && !it.TS.Before(ix.CreatedAt) {
			return true
		}
	}
	return false
}

func (s *Service) finishExternalPlan(p *pending) {
	s.fleet.mu.Lock()
	if s.fleet.interactions[p.ix.ID] != p {
		s.fleet.mu.Unlock()
		return // the phone answered or this card was replaced while the read was in flight
	}
	delete(s.fleet.interactions, p.ix.ID)
	if p.orphaned != nil {
		p.orphaned.Stop()
	}
	s.fleet.mu.Unlock()
	s.recordEnd(p.ix.ID, "dismissed", nil)
	select {
	case p.done <- proto.Resolution{Dismiss: true}:
	default:
	}
	s.finished(p)
}

// ---- the interaction queue as the assistant's own (its interaction_answer tool, the channels) ----

// answerQuestion resolves the pending question id of machine's session with answers, as the assistant answered it;
// false when it is no longer pending.
func (s *Service) answerQuestion(machine, session, id string, answers map[string]string) (bool, error) {
	ix, ok := s.pendingIx(id)
	if !ok {
		return false, nil
	}
	if ix.Kind != proto.IxQuestion {
		return false, fmt.Errorf("interaction %s is a %s, not a question", id, ix.Kind)
	}
	if ix.MachineID != machine || ix.SessionID != session {
		return false, fmt.Errorf("interaction %s is not machine=%s session_id=%s's", id, machine, session)
	}
	if err := s.resolve(id, proto.Resolution{Answers: answers, AnsweredBy: proto.AnsweredByAssistant}); err != nil {
		if errors.Is(err, errNotPending) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// interactionOutcome is how an interaction that is no longer pending ended, from the history: resolved, dismissed,
// expired, cancelled; "" when the history has none.
func (s *Service) interactionOutcome(ctx context.Context, id string) string {
	st := s.fleetStore()
	if st == nil {
		return ""
	}
	status, _, err := st.InteractionOutcome(ctx, id)
	if err != nil {
		return ""
	}
	return status
}

// ---- the assistant's own questions and cards ----

// askOnPhone shows the questions the assistant asks and its cards about agents' turns (its snapshot's interactions)
// like any agent's card: recorded, announced, pushed, expired (waitInteraction); the answer goes to its Action, or
// waits when it cannot take it (keepAnswer). A question that left the snapshot is withdrawn, as a hook's that went
// away, and nothing is said about it. One it still reports a while after its answer went, with no answer waiting for
// it, is shown again: the answer did not take.
func (s *Service) askOnPhone(list []proto.Interaction) {
	f := &s.fleet
	f.askMu.Lock()
	defer f.askMu.Unlock()
	if f.asked == nil {
		f.asked = map[string]*askedOne{}
	}
	now := map[string]bool{}
	for _, ix := range list {
		now[ix.ID] = true
		if q := f.asked[ix.ID]; q != nil {
			if q.ended.IsZero() || time.Since(q.ended) < askAgainAfter || s.answerKept(ix.ID) {
				continue
			}
			q.cancel()
		}
		ctx, cancel := context.WithCancel(context.Background())
		q := &askedOne{cancel: cancel}
		f.asked[ix.ID] = q
		go func(ix proto.Interaction) {
			r, err := s.waitInteraction(ctx, ix)
			if err != nil || r.Dismiss && ctx.Err() != nil {
				return // withdrawn: it knows
			}
			answer, _ := json.Marshal(r)
			act := Action{ActionID: "act_" + randomHex(6), SessionID: ix.SessionID, Op: "answer", InteractionID: ix.ID,
				InteractionVersion: 1, Answer: answer}
			if _, err := s.Action(context.Background(), act); err != nil && !s.keepAnswer(act, err, ix.ExpiresAt) {
				s.logf("assistant: answer %s: %v", ix.ID, err)
			}
			f.askMu.Lock()
			q.ended = time.Now()
			f.askMu.Unlock()
		}(ix)
	}
	for id, q := range f.asked {
		if !now[id] {
			q.cancel()
			delete(f.asked, id)
		}
	}
}

// askedOne is a question or card of the assistant's on the phone: cancel withdraws it; ended is when its answer (or
// its expiry) went to its Action.
type askedOne struct {
	cancel context.CancelFunc
	ended  time.Time
}

// askAgainAfter is how long a question the assistant still reports after its answer went may stay off the phone.
var askAgainAfter = 30 * time.Second

// heldActs are the answers to the assistant's questions and cards its Action did not take yet, by interaction id:
// they go to it again after each snapshot (deliverAnswers) until it takes them, says they are for nothing it has
// any more (CodeGone), or their card would have expired.
type heldActs struct {
	mu   sync.Mutex
	acts map[string]heldAct
	busy bool
}

type heldAct struct {
	act         Action
	until, next time.Time
}

// answerRetryAfter is how long an answer the Action could not take waits before it is tried again.
var answerRetryAfter = 10 * time.Second

// keepAnswer holds act, which the Action did not take (err), until until; false when err is final (its question or
// card is gone): nothing is kept.
func (s *Service) keepAnswer(act Action, err error, until time.Time) bool {
	var re *rpc.Error
	if errors.As(err, &re) && re.Code == assistantwire.CodeGone {
		return false
	}
	now := time.Now()
	if until.Before(now) {
		until = now.Add(24 * time.Hour) // as long as a card of the manager's lasts
	}
	u := &s.fleet.undelivered
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.acts == nil {
		u.acts = map[string]heldAct{}
	}
	u.acts[act.InteractionID] = heldAct{act: act, until: until, next: now.Add(answerRetryAfter)}
	return true
}

// answerKept: an answer to interaction id is waiting for the Action.
func (s *Service) answerKept(id string) bool {
	u := &s.fleet.undelivered
	u.mu.Lock()
	defer u.mu.Unlock()
	_, ok := u.acts[id]
	return ok
}

// deliverAnswers gives the Action the answers kept that are due (after a snapshot). One delivery at a time.
func (s *Service) deliverAnswers() {
	u := &s.fleet.undelivered
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
			s.logf("assistant: answer %s: dropped, its card expired before it was taken", id)
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
		if _, err := s.Action(context.Background(), a.act); err != nil && !s.keepAnswer(a.act, err, a.until) {
			s.logf("assistant: answer %s: %v", a.act.InteractionID, err)
		}
	}
}

// NodeDetached: a machine's node connection ended (devices.Options.Detached): the cards waiting on it that cannot be
// claimed again end (resumable ones wait for their hook through the next connection), its list shows it.
func (s *Service) NodeDetached(machine string) {
	if !s.fleetOn() {
		return
	}
	s.cancelMachineInteractions(machine)
	s.sessionsChanged(machine)
}

func (s *Service) cancelMachineInteractions(machineID string) {
	s.fleet.mu.Lock()
	var ids []string
	for id, p := range s.fleet.interactions {
		// Resumable ones wait for their hook to come back through the next node (await).
		if p.ix.MachineID == machineID && !p.ix.Resumable {
			ids = append(ids, id)
		}
	}
	s.fleet.mu.Unlock()
	for _, id := range ids {
		s.finishInteraction(id)
	}
}

// restoreInteractions takes back, as the manager starts, the resumable cards the last one left pending: their hooks
// reconnect through the next node and claim them again (waitInteraction), without a second push; the others it left
// are over (cancelled in the history).
func (s *Service) restoreInteractions(ctx context.Context) {
	st, ok := s.store().(interface {
		ExpireStaleInteractions(ctx context.Context) ([]proto.Interaction, error)
	})
	if !ok {
		return
	}
	resumable, err := func() (out []proto.Interaction, err error) {
		defer guard(&err) // a test's store may not keep the table
		return st.ExpireStaleInteractions(ctx)
	}()
	if err != nil {
		s.logf("expire stale interactions: %v", err)
		return
	}
	s.fleet.mu.Lock()
	m := s.ixMapLocked()
	for _, ix := range resumable {
		if m[ix.ID] == nil {
			m[ix.ID] = &pending{ix: ix, done: make(chan proto.Resolution, 1)}
			s.orphanLocked(m[ix.ID])
		}
	}
	s.fleet.mu.Unlock()
}
