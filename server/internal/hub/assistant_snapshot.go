package hub

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// assistantSnapshot is the live context each message to the assistant starts with (a <jarvis-context> block,
// node/assistant_context.go): the time, which machines are up, agents waiting for the user, unread Feishu chats and
// the rest of today's meetings. It is taken on every message, so it must not hold the message up: the sources are
// read at most every snapshotFresh, a message waits for a new read at most snapshotWait and otherwise goes with the
// last one (the time is always now). Each source has a short deadline; a slow one is left out.
func (h *Hub) assistantSnapshot(ctx context.Context) string {
	h.mu.Lock()
	snap, at := h.snap, h.snapAt
	var wait chan struct{}
	if snap == nil || time.Since(at) >= snapshotFresh {
		if wait = h.snapping; wait == nil {
			wait = make(chan struct{})
			h.snapping = wait
			go h.takeSnapshot(wait)
		}
	}
	h.mu.Unlock()
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
		h.mu.Lock()
		snap = h.snap
		h.mu.Unlock()
	}
	if snap == nil {
		return ""
	}
	s := *snap
	s.Now = time.Now().In(h.userLocation())
	return prompts.RenderSnapshot(s)
}

const (
	snapshotFresh   = 45 * time.Second
	snapshotWait    = 1500 * time.Millisecond
	snapshotStale   = 30 * time.Minute
	snapshotSlowest = 5 * time.Second
)

// takeSnapshot reads the sources and keeps the result for assistantSnapshot; done is closed when it is in.
func (h *Hub) takeSnapshot(done chan struct{}) {
	ctx, cancel := context.WithTimeout(context.Background(), snapshotSlowest)
	defer cancel()
	loc := h.userLocation()
	now := time.Now().In(loc)
	snap := prompts.Snapshot{Now: now, Zone: loc.String(), PhoneOnline: h.phoneClient() != nil}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		snap.Chats = h.snapshotChats(ctx)
	}()
	go func() {
		defer wg.Done()
		snap.Calendar = h.snapshotCalendar(ctx, now)
	}()
	for _, m := range h.machinesNow() {
		snap.Machines = append(snap.Machines, prompts.SnapshotMachine{Name: m.cfg.Name, ID: m.cfg.ID, Online: m.snapshot().Online})
	}
	snap.Needs, snap.Unread, snap.Working = h.snapshotAgents()
	snap.Tasks = h.snapshotTasks()
	if e := h.kb.eng; e != nil {
		snap.KB = e.Prompt(6, 8)
	}
	snap.Followups = h.snapshotSchedule(ctx, loc)
	wg.Wait()

	h.mu.Lock()
	h.snap, h.snapAt, h.snapping = &snap, time.Now(), nil
	h.mu.Unlock()
	close(done)
}

// forgetSnapshotLocked makes the next message take a new snapshot (the time zone changed); h.mu is held.
func (h *Hub) forgetSnapshotLocked() { h.snap, h.snapAt = nil, time.Time{} }

func (h *Hub) snapshotAgents() (needs, unread []prompts.SnapshotAgent, working []string) {
	for _, m := range h.machinesNow() {
		for _, s := range h.decorated(m) {
			if s.Assistant {
				continue
			}
			a := prompts.SnapshotAgent{Label: agentLabel(s), Machine: m.cfg.ID, Session: short(s.SessionID)}
			switch s.State {
			case proto.StateNeedsYou:
				needs = append(needs, a)
			case proto.StateUnread:
				if time.Since(s.UpdatedAt) < 24*time.Hour {
					unread = append(unread, a)
				}
			case proto.StateWorking:
				working = append(working, agentLabel(s))
			}
		}
	}
	return needs, unread, working
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// connectorSnapshots are the live states of the signed-in connectors that keep one (connector.Snapshot: a chat service
// through a plugin), asked at once and waited for 3 seconds; those that do not answer in time are left out.
func (h *Hub) connectorSnapshots(ctx context.Context) []connector.SnapshotData {
	if h.connectors == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var mu sync.Mutex
	var wg sync.WaitGroup
	var out []connector.SnapshotData
	tz := h.userLocation().String()
	for _, c := range h.connectors.All() {
		sn, ok := connector.As[connector.Snapshot](c)
		if !ok || connector.StatusOf(ctx, c).State != connector.StateOK {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := sn.Snapshot(ctx, tz)
			if err != nil {
				return
			}
			mu.Lock()
			out = append(out, d)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

// snapshotChats is the unread chats the connectors know of, nil when none answers.
func (h *Hub) snapshotChats(ctx context.Context) *prompts.SnapshotChats {
	var out *prompts.SnapshotChats
	for _, d := range h.connectorSnapshots(ctx) {
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
func (h *Hub) snapshotCalendar(ctx context.Context, now time.Time) *prompts.SnapshotCalendar {
	var out *prompts.SnapshotCalendar
	for _, d := range h.connectorSnapshots(ctx) {
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

func (h *Hub) snapshotSchedule(ctx context.Context, loc *time.Location) []prompts.SnapshotFollowup {
	s, err := h.lockedSchedule(ctx)
	if err != nil {
		return nil
	}
	defer s.mu.Unlock()
	var out []prompts.SnapshotFollowup
	for _, t := range s.tasks {
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
