package assistant

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// Context is the <jarvis-context> block a message to conversation p.SessionID starts with (the node asks on every
// message, node/assistant_context.go): who the user is, whether this is the main conversation, and the snapshot.
func (s *Service) Context(ctx context.Context, p ContextParams) ContextResult {
	hello := s.hello(ctx)
	text := prompts.RenderAssistantContext(prompts.AssistantContext{Snapshot: s.contextSnapshot(ctx), Owner: s.owner(ctx),
		Main: p.SessionID != "" && p.SessionID == hello.Assistant.Main})
	return ContextResult{Text: text}
}

// contextState is the last snapshot taken for the live context.
type contextState struct {
	mu       sync.Mutex
	snap     *prompts.Snapshot
	at       time.Time
	snapping chan struct{} // closed when the snapshot being taken is in
}

// contextSnapshot is the live snapshot each message to the assistant starts with: the time, which machines are up,
// agents waiting for the user, unread Feishu chats and the rest of today's meetings. It is taken on every message, so
// it must not hold the message up: the sources are read at most every snapshotFresh, a message waits for a new read
// at most snapshotWait and otherwise goes with the last one (the time is always now). Each source has a short
// deadline; a slow one is left out.
func (s *Service) contextSnapshot(ctx context.Context) string {
	cs := &s.live
	cs.mu.Lock()
	snap, at := cs.snap, cs.at
	var wait chan struct{}
	if snap == nil || time.Since(at) >= snapshotFresh {
		if wait = cs.snapping; wait == nil {
			wait = make(chan struct{})
			cs.snapping = wait
			go s.takeSnapshot(wait)
		}
	}
	cs.mu.Unlock()
	if wait != nil {
		limit := snapshotWait
		if snap == nil || time.Since(at) > snapshotStale {
			limit = snapshotSlowest // nothing usable yet: wait for the sources' own deadlines
		}
		select {
		case <-wait:
		case <-time.After(limit):
		case <-ctx.Done():
		}
		cs.mu.Lock()
		snap = cs.snap
		cs.mu.Unlock()
	}
	if snap == nil {
		return ""
	}
	out := *snap
	out.Now = time.Now().In(s.location(ctx))
	return prompts.RenderSnapshot(out)
}

const (
	snapshotFresh   = 45 * time.Second
	snapshotWait    = 1500 * time.Millisecond
	snapshotStale   = 30 * time.Minute
	snapshotSlowest = 5 * time.Second
)

// takeSnapshot reads the sources and keeps the result for contextSnapshot; done is closed when it is in.
func (s *Service) takeSnapshot(done chan struct{}) {
	ctx, cancel := context.WithTimeout(context.Background(), snapshotSlowest)
	defer cancel()
	loc := s.location(ctx)
	now := time.Now().In(loc)
	devices, phone := s.devicesAndPhone(ctx)
	snap := prompts.Snapshot{Now: now, Zone: loc.String(), PhoneOnline: phone}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		snap.Chats = s.snapshotChats(ctx)
	}()
	go func() {
		defer wg.Done()
		snap.Calendar = s.snapshotCalendar(ctx, now)
	}()
	for _, m := range devices {
		snap.Machines = append(snap.Machines, prompts.SnapshotMachine{Name: m.Name, ID: m.ID, Online: m.Online})
	}
	snap.Needs, snap.Unread, snap.Waiting, snap.Done, snap.Working = s.snapshotAgents(ctx, devices)
	snap.Tasks = s.snapshotTasks(ctx)
	var kb struct {
		Text string `json:"text"`
	}
	if s.appCall(ctx, AppKB, KBPrompt, map[string]any{"projects": 6, "chats": 8}, &kb) == nil {
		snap.KB = kb.Text
	}
	snap.Followups = s.snapshotSchedule(ctx, loc)
	wg.Wait()

	cs := &s.live
	cs.mu.Lock()
	cs.snap, cs.at, cs.snapping = &snap, time.Now(), nil
	cs.mu.Unlock()
	close(done)
}

// ProfileChanged makes the next message take a new snapshot (the time zone changed).
func (s *Service) ProfileChanged(context.Context) {
	cs := &s.live
	cs.mu.Lock()
	cs.snap, cs.at = nil, time.Time{}
	cs.mu.Unlock()
}

// snapshotAgents sorts the agents by what they need: the hub's work state (docs/agent-work.md) says what a stopped one
// waits for or concluded.
func (s *Service) snapshotAgents(ctx context.Context, devices []Device) (needs, unread, waiting, done []prompts.SnapshotAgent, working []string) {
	for _, m := range devices {
		list, _ := s.sessions(ctx, m.ID)
		for _, c := range list {
			if c.Assistant {
				continue
			}
			a := prompts.SnapshotAgent{Label: agentLabel(c), Machine: m.ID, Session: short(c.SessionID)}
			w := c.Work
			if w != nil && w.Phase != proto.WorkWorking {
				a.Line = w.Line
			}
			recent := time.Since(c.UpdatedAt) < 24*time.Hour
			switch {
			case c.State == proto.StateNeedsYou:
				needs = append(needs, a)
			case c.State == proto.StateWorking:
				working = append(working, agentLabel(c))
			case w != nil && w.Phase == proto.WorkBlocked && w.On == proto.WaitUser && recent:
				needs = append(needs, a) // waits for a decision the hub put no card up for
			case w != nil && w.Phase == proto.WorkBlocked && c.State != proto.StateUnread && recent:
				waiting = append(waiting, a) // the hub waits it out (an unread one is left for the user)
			case w != nil && w.Phase == proto.WorkDone && recent:
				done = append(done, a)
			case c.State == proto.StateUnread && recent:
				unread = append(unread, a)
			}
		}
	}
	return needs, unread, waiting, done, working
}

// connectorSnapshots are the live states of the hub's signed-in connectors that keep one (connector.Snapshot: a chat
// service through a plugin; app.call connectors.snapshots), waited for 3 seconds; those that do not answer in time are
// left out.
func (s *Service) connectorSnapshots(ctx context.Context) []connector.SnapshotData {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var res struct {
		Snapshots []connector.SnapshotData `json:"snapshots"`
	}
	if err := s.appCall(ctx, AppConnectors, ConnectorsSnapshots, map[string]any{"tz": s.location(ctx).String()}, &res); err != nil {
		return nil
	}
	return res.Snapshots
}

// snapshotChats is the unread chats the connectors know of, nil when none answers.
func (s *Service) snapshotChats(ctx context.Context) *prompts.SnapshotChats {
	var out *prompts.SnapshotChats
	for _, d := range s.connectorSnapshots(ctx) {
		if d.Chats == nil {
			continue
		}
		if out == nil {
			out = &prompts.SnapshotChats{}
		}
		for _, c := range d.Chats.AtMe {
			out.AtMe = append(out.AtMe, prompts.SnapshotChat{Name: c.Name, Unread: c.Unread, Preview: clip(oneLine(c.Preview), 40)})
		}
		for _, c := range d.Chats.Other {
			out.Other = append(out.Other, prompts.SnapshotChat{Name: c.Name, Unread: c.Unread})
		}
	}
	return out
}

// snapshotCalendar is the rest of today's meetings the connectors know of, nil when none answers.
func (s *Service) snapshotCalendar(ctx context.Context, now time.Time) *prompts.SnapshotCalendar {
	var out *prompts.SnapshotCalendar
	for _, d := range s.connectorSnapshots(ctx) {
		if d.Meetings == nil {
			continue
		}
		if out == nil {
			out = &prompts.SnapshotCalendar{}
		}
		for _, m := range d.Meetings {
			if m.End.After(now) {
				out.Events = append(out.Events, prompts.SnapshotEvent{Start: m.Start.In(now.Location()), End: m.End.In(now.Location()), Title: m.Title})
			}
		}
	}
	if out != nil {
		sort.SliceStable(out.Events, func(i, j int) bool { return out.Events[i].Start.Before(out.Events[j].Start) })
	}
	return out
}

func (s *Service) snapshotSchedule(ctx context.Context, loc *time.Location) []prompts.SnapshotFollowup {
	sc, err := s.lockedSchedule(ctx)
	if err != nil {
		return nil
	}
	defer sc.mu.Unlock()
	var out []prompts.SnapshotFollowup
	for _, t := range sc.tasks {
		if !t.Enabled || t.Repeat == repeatDaily {
			continue
		}
		f := prompts.SnapshotFollowup{At: t.At.In(loc), Label: t.label()}
		if t.Repeat == repeatInterval {
			f.Every = everyText(t.EveryMinutes)
		}
		out = append(out, f)
	}
	return out
}
