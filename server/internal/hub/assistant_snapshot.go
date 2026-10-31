package hub

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/prompts"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// assistantSnapshot is the live context a starting assistant process gets in its system prompt
// (--append-system-prompt, node/assistant.go): the time, which machines are up, agents waiting for the user,
// unread Feishu chats and the rest of today's meetings. Each source has a short deadline; a slow one is left out.
func (h *Hub) assistantSnapshot(ctx context.Context) string {
	h.mu.Lock()
	if time.Since(h.snapAt) < 45*time.Second && h.snap != "" {
		s := h.snap
		h.mu.Unlock()
		return s
	}
	h.mu.Unlock()

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
	for _, m := range h.machines {
		snap.Machines = append(snap.Machines, prompts.SnapshotMachine{Name: m.cfg.Name, ID: m.cfg.ID, Online: m.snapshot().Online})
	}
	snap.Needs, snap.Unread, snap.Working = h.snapshotAgents()
	if e := h.kb.eng; e != nil {
		snap.KB = e.Prompt(6, 8)
	}
	snap.Followups = h.snapshotSchedule(ctx, loc)
	wg.Wait()

	out := prompts.RenderSnapshot(snap)
	h.mu.Lock()
	h.snap, h.snapAt = out, time.Now()
	h.mu.Unlock()
	return out
}

func (h *Hub) snapshotAgents() (needs, unread []prompts.SnapshotAgent, working []string) {
	for _, m := range h.machines {
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

// snapshotChats is the unread Feishu chats, nil when lark-connector does not answer in time.
func (h *Hub) snapshotChats(ctx context.Context) *prompts.SnapshotChats {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	convs, _, err := h.lark.Conversations(ctx, "inbox", "", 100)
	if err != nil {
		return nil
	}
	var out prompts.SnapshotChats
	for _, c := range convs {
		if c.Unread == 0 && !c.MarkedUnread && !c.AtMe { // an @me can outlive the unread count (read elsewhere, not answered)
			continue
		}
		if c.Muted && !c.AtMe {
			continue
		}
		if c.AtMe {
			out.AtMe = append(out.AtMe, prompts.SnapshotChat{Name: c.Name, Unread: c.Unread, Preview: clip(oneLine(c.Preview), 40)})
		} else {
			out.Other = append(out.Other, prompts.SnapshotChat{Name: c.Name, Unread: c.Unread})
		}
	}
	return &out
}

// snapshotCalendar is the rest of today's meetings, nil when lark-connector does not answer in time.
func (h *Hub) snapshotCalendar(ctx context.Context, now time.Time) *prompts.SnapshotCalendar {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	q := url.Values{"start": {day.UTC().Format("2006-01-02T15:04:05Z")}, "end": {day.AddDate(0, 0, 1).UTC().Format("2006-01-02T15:04:05Z")}}
	raw, err := h.lark.Read(ctx, "GET", "/calendar/week", q, nil)
	if err != nil {
		return nil
	}
	var res struct {
		Events []struct {
			Title              string    `json:"title"`
			StartAt            time.Time `json:"startAt"`
			EndAt              time.Time `json:"endAt"`
			AllDay             bool      `json:"allDay"`
			SelfAttendeeStatus int       `json:"selfAttendeeStatus"`
		} `json:"events"`
	}
	if json.Unmarshal(raw, &res) != nil {
		return nil
	}
	sort.Slice(res.Events, func(i, j int) bool { return res.Events[i].StartAt.Before(res.Events[j].StartAt) })
	var out prompts.SnapshotCalendar
	for _, e := range res.Events {
		if e.AllDay || e.SelfAttendeeStatus == 4 || !e.EndAt.After(now) { // declined or over
			continue
		}
		out.Events = append(out.Events, prompts.SnapshotEvent{Start: e.StartAt.In(now.Location()), End: e.EndAt.In(now.Location()), Title: e.Title})
	}
	return &out
}

func (h *Hub) snapshotSchedule(ctx context.Context, loc *time.Location) []prompts.SnapshotFollowup {
	s := h.loadSchedule(ctx)
	s.mu.Lock()
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
