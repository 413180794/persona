package assistant

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/lazyre"
	"github.com/nwverse/persona/server/internal/prompts"
)

// Keep the facts as admitted: some signals (a worker report or an answered card)
// cannot be reconstructed once queued. Text remains readable by older managers.
type queuedSupervision struct {
	Prompt  prompts.Supervise `json:"prompt"`
	Owners  map[string]string `json:"owners"`
	signals []superviseSignal // only live signals may merge or release their dedup reservations
}

func setDigest(msg *relayMsg, role prompts.Supervise, entries []prompts.SuperviseEntry, signals []superviseSignal) {
	d := &queuedSupervision{Prompt: role, Owners: map[string]string{}, signals: signals}
	for _, g := range signals {
		d.Owners[g.Todo] = g.owner
	}
	msg.Digest = d
	d.Prompt.Entries = sortEntries(entries)
	msg.Text = d.render()
	msg.Urgent = anyDecision(signals)
}

func (d *queuedSupervision) render() string {
	p := d.Prompt
	entries := p.Entries
	p.Asked = askedIn(entries)
	p.Escalated = escalatedIn(entries)
	for _, e := range entries {
		switch e.Kind {
		case sigEscalated:
			p.BackgroundEscalated = p.BackgroundEscalated || e.Background
		case sigLeaderStuck:
			p.LeaderStuck = true
		case sigOverdue:
			p.Overdue = true
		case sigParkedDue:
			p.ParkedDue = true
		case sigParkDecided:
			p.ParkDecided = true
		case sigRecheck, sigUndecidable, sigOwnerCannot:
			p.Decisions = true
		case sigInbox:
			p.Inbox = true
		}
	}
	return prompts.RenderSupervise(p)
}

func superviseTaskActionable(t Todo, kind string) bool {
	if t.Supervision == nil || t.Cancelled() {
		return false
	}
	// A start that failed can have done_at while its failure still awaits review.
	return t.DoneAt == nil || kind == sigFailed && t.StartError != "" && t.DoneBy == "" && t.Supervision.state(t) != reviewAccepted
}

// The ledger owner may differ from the addressed conversation for an escalation,
// a card routed up a rung, or a fallback when a leader is gone. Retain that route
// while its owner still holds the task. Protected runtime reports follow #10338.
func (s *Service) superviseEntryHolds(ctx context.Context, cfg Settings, to string, msg relayMsg, id, kind string) bool {
	t, ok := s.todoNow(id)
	if !ok || !superviseTaskActionable(t, kind) {
		return false
	}
	owner := t.Supervision.Supervisor
	if to == owner {
		return true
	}
	if (owner == "" || owner == cfg.Main) && (to == cfg.Main || to == cfg.BackgroundSupervisor || msg.NeedsSupervisor) {
		return true
	}
	if msg.Digest != nil && msg.Digest.Owners[id] == owner {
		return true // same ledger owner as when the explicit route or fallback was chosen
	}
	g := superviseSignal{Todo: id, Kind: kind}
	if s.agentSupervised(t) {
		if kind != sigDecide {
			return false
		}
		g.To, _ = s.supervisorAboveTask(ctx, t)
	}
	// A routed question may be at the main conversation above its leader.
	if kind == sigAsk && to == cfg.Main && slices.Contains(cfg.Leaders, owner) {
		return true
	}
	want := s.digestTo(cfg, g)
	if want == to {
		return true
	}
	if to != cfg.Main || msg.Digest != nil {
		return false
	}
	// Without a recorded owner, a rendered digest at main may be the
	// fallback for a missing side conversation. A listed current owner can
	// take its own work. A missing listing does not disprove the fallback.
	c, listed := s.session(ctx, cfg.MachineID, want)
	return !listed || !c.Assistant
}

// filterSuperviseRelay requires a successfully loaded task ledger. An empty
// result retires the message. Never mutate a queue snapshot held by a sender.
func (s *Service) filterSuperviseRelay(ctx context.Context, cfg Settings, to string, msg relayMsg) relayMsg {
	old := msg
	d := msg.Digest
	if d == nil {
		msg = s.filterLegacyDigest(ctx, cfg, to, msg)
	} else {
		var entries []prompts.SuperviseEntry
		owners := map[string]string{}
		var urgent bool
		for _, e := range d.Prompt.Entries {
			if s.superviseEntryHolds(ctx, cfg, to, msg, e.ID, e.Kind) {
				entries = append(entries, e)
				owners[e.ID] = d.Owners[e.ID]
				urgent = urgent || (superviseSignal{Kind: e.Kind, Report: e.Report}).waitsOnDecision()
			}
		}
		if len(entries) == 0 {
			return relayMsg{}
		}
		if len(entries) == len(d.Prompt.Entries) {
			return msg
		}
		next := &queuedSupervision{Prompt: d.Prompt, Owners: owners}
		next.Prompt.Entries = entries
		for _, g := range d.signals {
			if _, ok := owners[g.Todo]; ok && s.superviseEntryHolds(ctx, cfg, to, msg, g.Todo, g.Kind) {
				next.signals = append(next.signals, g)
			}
		}
		msg.Digest, msg.Text, msg.Urgent = next, next.render(), urgent
	}
	if msg.Tool != "" && msg.Text != old.Text {
		msg.OnceID = relayMsgID(to, old)
		if !msg.Urgent {
			msg.DecisionAt = time.Time{}
		}
	}
	return msg
}

// Older rows have only the rendered prompt. Trust the template's counted,
// consecutively numbered task headers, never task IDs mentioned in evidence.
// Preserve an unfamiliar or ambiguous format intact.
var legacyDigestHeader = lazyre.New(`^📋 督办：你派出去的任务有 ([0-9]+) 个要你处理\n`)
var legacyDigestItem = lazyre.New(`(?m)^([1-9][0-9]*)\. 任务「[^\n]*?」（(todo_[A-Za-z0-9_-]+)，signal=([a-z_]+)(?:，链接 [^\n]*?)?）：`)

func (s *Service) filterLegacyDigest(ctx context.Context, cfg Settings, to string, msg relayMsg) relayMsg {
	h := legacyDigestHeader.FindStringSubmatchIndex(msg.Text)
	if h == nil {
		return msg
	}
	items := legacyDigestItem.FindAllStringSubmatchIndex(msg.Text, -1)
	if len(items) == 0 || strconv.Itoa(len(items)) != msg.Text[h[2]:h[3]] {
		return msg
	}
	var body strings.Builder
	count, urgent, asked := 0, false, false
	for i, m := range items {
		if msg.Text[m[2]:m[3]] != strconv.Itoa(i+1) {
			return msg
		}
		id, kind := msg.Text[m[4]:m[5]], msg.Text[m[6]:m[7]]
		if !s.superviseEntryHolds(ctx, cfg, to, msg, id, kind) {
			continue
		}
		end := len(msg.Text)
		if i+1 < len(items) {
			end = items[i+1][0]
		}
		count++
		body.WriteString(strconv.Itoa(count))
		body.WriteString(msg.Text[m[3]:end])
		// A legacy report has no structured report kind. Preserve its priority
		// if the original digest was urgent.
		urgent = urgent || (superviseSignal{Kind: kind}).waitsOnDecision() || kind == sigReport && msg.Urgent
		asked = asked || kind == sigAsk
	}
	if count == len(items) {
		return msg
	}
	if count == 0 {
		return relayMsg{}
	}
	prefix := msg.Text[:h[2]] + strconv.Itoa(count) + msg.Text[h[3]:items[0][0]]
	if !asked {
		lines := strings.Split(prefix, "\n")
		prefix = strings.Join(slices.DeleteFunc(lines, func(l string) bool { return strings.HasPrefix(l, "Question routing:") }), "\n")
	}
	msg.Text, msg.Urgent = strings.TrimRight(prefix+body.String(), "\n"), urgent
	return msg
}

func (s *Service) recheckSuperviseRelay(ctx context.Context, to string, msg relayMsg) (relayMsg, error) {
	if err := s.ensureTodosLoaded(ctx); err != nil {
		return msg, err
	}
	cfg, ok := s.savedSettings(ctx)
	if !ok {
		return msg, errors.New("supervision routing settings could not be read")
	}
	return s.filterSuperviseRelay(ctx, s.effective(ctx, cfg), to, msg), nil
}

func (s *Service) mergeableDigest(to string) relayMsg {
	b := &s.relays
	b.mu.Lock()
	defer b.mu.Unlock()
	q := b.st.Queue[to]
	for i := len(q) - 1; i >= 0; i-- {
		m := q[i]
		if (i > 0 || !b.draining[to]) && !m.NeedsSupervisor && m.Digest != nil && len(m.Digest.signals) > 0 {
			return m
		}
	}
	return relayMsg{}
}

// Compare with the exact queued version: a drain, merge or handover can race a
// task change. A failed setting write leaves that version available for retry.
func (s *Service) replaceQueuedSupervise(to string, old, next relayMsg) bool {
	b := &s.relays
	b.mu.Lock()
	defer b.mu.Unlock()
	q := b.st.Queue[to]
	i := slices.Index(q, old)
	if i < 0 || i == 0 && b.draining[to] {
		return false
	}
	if next.Tool != "" {
		next = next.replacing(old, time.Now())
	}
	return s.editQueuedRelayLocked(to, i, next)
}

func (s *Service) editQueuedRelayLocked(to string, i int, next relayMsg) bool {
	queue := s.relays.st.Queue
	old := queue[to]
	q := slices.Clone(old)
	if next.Tool == "" {
		q = slices.Delete(q, i, i+1)
	} else {
		q[i] = next
	}
	if len(q) == 0 {
		delete(queue, to)
	} else {
		queue[to] = q
	}
	if s.saveRelayLocked() != nil {
		queue[to] = old
		return false
	}
	return true
}

// Task writes coalesce before notifying the apps; the watch also catches queued
// work restored after a restart even when its receiver is busy or absent.
func (s *Service) pruneQueuedSupervision(ctx context.Context) {
	// Rehoming a runtime changes its ledger before moving its protected evidence.
	if !s.supervise.dedupMu.TryLock() {
		return // a digest or handover is in flight; its drain or the next watch rechecks
	}
	defer s.supervise.dedupMu.Unlock()
	b := &s.relays
	b.mu.Lock()
	var candidates map[string][]relayMsg
	if s.relayLocked(ctx) {
		for to, q := range b.st.Queue {
			for i, m := range q {
				if m.Tool == superviseTool && (i > 0 || !b.draining[to]) {
					if candidates == nil {
						candidates = map[string][]relayMsg{}
					}
					candidates[to] = append(candidates[to], m)
				}
			}
		}
	}
	b.mu.Unlock()
	changed := false
	for to, q := range candidates {
		for _, old := range q {
			next, err := s.recheckSuperviseRelay(ctx, to, old)
			if err == nil && next != old && s.replaceQueuedSupervise(to, old, next) {
				changed = true
			}
		}
	}
	if changed {
		s.publish(ctx)
	}
}
