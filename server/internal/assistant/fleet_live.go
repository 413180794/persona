package assistant

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/transcript"
)

// The "Fleet" Live Activity's content (docs/live-activity.md): one activity per phone, on the lock screen and in the
// Dynamic Island, summing up every agent that needs you, is working, or finished unseen. The manager works it out
// (FleetState) from the session lists and the pending interactions, with the island's numbers (FleetStats), and
// tells the hub when it changes (fleet.live); the hub pushes it to each device's activity token at the pace Apple
// allows and gives each device its own part (its island's sides, its lock screen details, its persona).

type (
	FleetState = assistantwire.FleetState
	FleetStats = assistantwire.FleetStats
	FleetLoad  = assistantwire.FleetLoad
	FleetItem  = assistantwire.FleetItem
)

const fleetMaxItems = 3

// liveState is what was told the hub last.
type liveState struct {
	mu     sync.Mutex
	sent   string
	signal chan struct{}
}

func (l *liveState) ch() chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.signal == nil {
		l.signal = make(chan struct{}, 1)
	}
	return l.signal
}

// liveChanged: what the fleet is made of may have changed; force: tell the hub even when it did not (it may have
// restarted).
func (s *Service) liveChanged(force bool) {
	if !s.fleetOn() {
		return
	}
	if force {
		s.fleet.live.mu.Lock()
		s.fleet.live.sent = ""
		s.fleet.live.mu.Unlock()
	}
	select {
	case s.fleet.live.ch() <- struct{}{}:
	default:
	}
}

// liveLoop tells the hub the fleet when it changed, at most every 100 ms, and looks at the numbers every 15 s.
func (s *Service) liveLoop(ctx context.Context) {
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	sig := s.fleet.live.ch()
	for {
		select {
		case <-ctx.Done():
			return
		case <-sig:
			time.Sleep(100 * time.Millisecond)
		case <-tick.C:
		}
		s.tellLive()
	}
}

func (s *Service) tellLive() {
	msg := assistantwire.FleetLive{State: s.fleetSnapshot(), Stats: s.fleetStats()}
	b, _ := json.Marshal(msg)
	l := &s.fleet.live
	l.mu.Lock()
	same := l.sent == string(b)
	l.mu.Unlock()
	if same {
		return
	}
	if s.hub.Tell(assistantwire.EventFleetLive, json.RawMessage(b)) == nil {
		l.mu.Lock()
		l.sent = string(b)
		l.mu.Unlock()
	}
}

// fleetSnapshot computes the fleet from every machine's sessions and the pending interactions.
func (s *Service) fleetSnapshot() FleetState {
	var agents []fleetInput
	for _, m := range s.fleetMachines() {
		for _, sess := range s.decorated(m.ID) {
			if sess.Quiet { // a quiet task's agent: the main conversation tells about it (#1731)
				continue
			}
			agents = append(agents, fleetInput{
				// Its own space, not muted's lookup of the node's row: that scanned the machine's list once per session.
				Machine: m.Name, Session: sess, Muted: s.mutedAs(m.ID, sess.SessionID, sess.SpaceName),
				TurnStart: s.turnsBook().startOf(m.ID + "/" + sess.SessionID),
			})
		}
	}
	return buildFleetIn(s.lang(context.Background()), agents, s.pendingInteractions())
}

// round2 keeps two significant digits: 12345 → 12000, 987 → 990. The island shows "12k", so finer changes would
// only spend the push budget.
func round2(n int64) int64 {
	if n < 100 {
		return n
	}
	p := int64(1)
	for n/p >= 100 {
		p *= 10
	}
	return (n + p/2) / p * p
}

// fleetStats turns the usage summary into the island's numbers.
func (s *Service) fleetStats() FleetStats {
	if s.devs == nil {
		return FleetStats{}
	}
	u := s.devs.Usage()
	st := FleetStats{TPM: round2(u.TPM), Tokens: round2(u.Tokens), Requests: int(round2(int64(u.Requests)))}
	for _, m := range u.Machines {
		st.Loads = append(st.Loads, FleetLoad{ID: m.ID, Name: s.machineName(m.ID), Percent: int(math.Round(m.Load*20)) * 5})
	}
	return st
}

// fleetInput is one agent as the fleet sees it.
type fleetInput struct {
	Machine   string // display name
	Session   proto.Session
	Muted     bool
	TurnStart time.Time // zero: unknown
}

// buildFleet sums agents up the way the Agents list does (docs/ios.md): needs you first (oldest request first), then
// working (longest first), then finished unseen. Muted agents, idle / offline ones, and /loop agents waiting for
// their next tick are left out; the assistant only counts when it asks something.
func buildFleet(agents []fleetInput, ixs []proto.Interaction) FleetState {
	return buildFleetIn(i18n.Default, agents, ixs)
}

// buildFleetIn is buildFleet with the texts for people in l (the user's language).
func buildFleetIn(l i18n.Lang, agents []fleetInput, ixs []proto.Interaction) FleetState {
	byKey := map[string]fleetInput{}
	for _, a := range agents {
		byKey[a.Session.MachineID+"/"+a.Session.SessionID] = a
	}
	var st FleetState
	var needs, working, unread []FleetItem
	asked := map[string]bool{}
	for _, ix := range ixs { // already oldest first
		// Fleet is shared with legacy clients; its existing terminal fallback stays available.
		if ix.Kind == proto.IxElicitation {
			continue
		}
		key := ix.MachineID + "/" + ix.SessionID
		a, ok := byKey[key]
		if !ok || a.Muted || asked[key] {
			continue // one item per agent: its oldest card stands for it, as in the Agents list (#3571)
		}
		asked[key] = true
		needs = append(needs, FleetItem{
			ID: key, Title: agentTitle(a.Session), Place: agentPlace(a.Machine, a.Session), Kind: ix.Kind,
			Detail: clip(oneLine(interactionDetail(l, ix)), 60), Interaction: ix.ID, Since: unixSeconds(ix.CreatedAt),
		})
	}
	for _, a := range agents {
		sess := a.Session
		key := sess.MachineID + "/" + sess.SessionID
		if a.Muted || asked[key] {
			continue
		}
		switch sess.State {
		case proto.StateNeedsYou: // a TUI dialog with no interaction yet
			if sess.Assistant {
				continue
			}
			needs = append(needs, FleetItem{ID: key, Title: agentTitle(sess), Place: agentPlace(a.Machine, sess), Kind: proto.IxScreen, Detail: i18n.T(l, "hub-liveactivity.screen")})
		case proto.StateWorking:
			if sess.Assistant {
				st.Assistant = true
				continue
			}
			since := a.TurnStart
			if sess.BackgroundWorking {
				var started time.Time
				for _, w := range sess.Workflows {
					if w.Running && !w.StartedAt.IsZero() && (started.IsZero() || w.StartedAt.Before(started)) {
						started = w.StartedAt
					}
				}
				if !started.IsZero() {
					since = started
				}
			}
			if since.IsZero() {
				since = sess.UpdatedAt
			}
			it := FleetItem{ID: key, Title: agentTitle(sess), Place: agentPlace(a.Machine, sess), Kind: "working", Since: unixSeconds(since)}
			if sess.BackgroundWorking {
				it.Background, it.Detail = true, clip(backgroundDetail(l, sess), 60)
			}
			working = append(working, it)
		case proto.StateUnread:
			if sess.Assistant || sess.Loop != nil {
				continue
			}
			unread = append(unread, FleetItem{ID: key, Title: agentTitle(sess), Place: agentPlace(a.Machine, sess), Kind: "unread",
				Detail: clip(oneLine(firstLine(sess.LastText)), 60), Since: unixSeconds(sess.UpdatedAt)})
		}
	}
	sort.SliceStable(working, func(i, j int) bool { return working[i].Since < working[j].Since })
	sort.SliceStable(unread, func(i, j int) bool { return unread[i].Since > unread[j].Since })
	st.NeedsYou, st.Working, st.Unread = len(needs), len(working), len(unread)
	switch {
	case st.NeedsYou > 0:
		st.Phase = "needsYou"
	case st.Working > 0:
		st.Phase = "working"
	case st.Unread > 0:
		st.Phase = "done"
	}
	if len(working) > 0 {
		st.WorkingSince = working[0].Since
	}
	st.Items = append(append(append([]FleetItem{}, needs...), working...), unread...)
	if len(st.Items) > fleetMaxItems {
		st.Items = st.Items[:fleetMaxItems]
	}
	return st
}

func agentTitle(sess proto.Session) string {
	t := transcript.Title(sess.Title)
	if t == "" || t == "Claude Code" {
		t = path.Base(sess.Cwd)
	}
	return clip(oneLine(t), 24)
}

// agentPlace is "machine · space › tab", the subtitle pushes use too.
func agentPlace(machine string, sess proto.Session) string {
	place := machine
	if sess.SpaceName != "" {
		place += " · " + sess.SpaceName
		if sess.SlotName != "" && sess.SlotName != fmt.Sprint(sess.SlotIndex) {
			place += " › " + sess.SlotName
		}
	}
	return clip(oneLine(place), 32)
}

func interactionDetail(l i18n.Lang, ix proto.Interaction) string {
	switch ix.Kind {
	case proto.IxPermission:
		if ix.Summary != "" {
			return ix.Summary
		}
		return ix.ToolName
	case proto.IxPlan:
		return planTitle(ix.Plan)
	case proto.IxQuestion:
		if len(ix.Questions) > 0 {
			return ix.Questions[0].Question
		}
		return ix.Summary
	}
	return i18n.T(l, "hub-liveactivity.screen")
}

func unixSeconds(t time.Time) float64 {
	if t.IsZero() {
		return 0
	}
	return float64(t.Unix())
}

// backgroundDetail is what a background-working agent runs, in a line: its running workflow ("parity-audit · Verify ·
// 169/185"), else how many background tasks.
func backgroundDetail(l i18n.Lang, sess proto.Session) string {
	for _, w := range sess.Workflows {
		if !w.Running {
			continue
		}
		parts := []string{cmp.Or(w.Name, w.Summary, "workflow")}
		if w.Phase != "" {
			parts = append(parts, w.Phase)
		}
		parts = append(parts, fmt.Sprintf("%d/%d", w.AgentsDone, w.AgentsStarted))
		return strings.Join(parts, " · ")
	}
	n := len(sess.Background)
	for _, a := range sess.Subagents {
		if a.Running {
			n++
		}
	}
	return i18n.T(l, "hub-liveactivity.background", i18n.Args{"count": n})
}
