package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/apierr"
)

// The 「今天」 card at the top of the day in an assistant conversation, and the kinds of tasks the user lets start
// without asking (docs/unified-entry.md「对话的形状」, decision 1).

// todayBody is what the 今天 card shows: built from what the assistant can read already, no model in between.
type todayBody struct {
	Date string `json:"date"` // the user's day, 2006-01-02
	// Done: tasks of this conversation that finished since yesterday evening.
	Done []todayTask `json:"done,omitempty"`
	// Waiting / Working: the conversation's tasks waiting for the user, and going on.
	Waiting []todayTask `json:"waiting,omitempty"`
	Working int         `json:"working,omitempty"`
	// Meetings: the rest of today's Feishu meetings; AtMe: Feishu chats that @ the user, Unread the other unread ones.
	Meetings []todayMeeting `json:"meetings,omitempty"`
	AtMe     []todayChat    `json:"at_me,omitempty"`
	Unread   int            `json:"unread,omitempty"`
}

type todayTask struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type todayMeeting struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Title string    `json:"title"`
}

type todayChat struct {
	Name    string `json:"name"`
	Unread  int    `json:"unread"`
	Preview string `json:"preview,omitempty"`
}

const feedToday = "today" // ref: the user's date; body: todayBody

// ensureToday is the conversation's 今天 card for the user's day: the one already there, or a new one.
func (s *Service) ensureToday(ctx context.Context, conversation string) (FeedItem, bool, error) {
	loc := s.location(ctx)
	now := time.Now().In(loc)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	date := day.Format("2006-01-02")
	items, _, err := s.feed(ctx, conversation, day.Add(-time.Second))
	if err != nil {
		return FeedItem{}, false, err
	}
	for _, it := range items {
		if it.Kind == feedToday && it.Ref == date {
			return it, false, nil
		}
	}
	body := todayBody{Date: date}
	since := day.Add(-6 * time.Hour) // yesterday from 18:00: what finished overnight
	if done, _, err := s.feed(ctx, conversation, since); err == nil {
		for _, it := range done {
			if it.Kind != feedReceipt {
				continue
			}
			var rb receiptBody
			if json.Unmarshal(it.Body, &rb) == nil && rb.State == "done" {
				body.Done = append(body.Done, todayTask{ID: it.Ref, Title: rb.Title})
			}
		}
	}
	if s.ensureTodosLoaded(ctx) == nil {
		var mine []Todo
		for _, t := range s.todosOf(ctx, conversation) {
			if t.Origin == conversation && t.DoneAt == nil {
				mine = append(mine, t)
			}
		}
		slices.SortFunc(mine, func(a, b Todo) int { return a.CreatedAt.Compare(b.CreatedAt) })
		for _, t := range mine {
			switch taskState(t, s.taskAgents(ctx, t)) {
			case "needs_you", "interrupted":
				body.Waiting = append(body.Waiting, todayTask{ID: t.ID, Title: t.Title})
			case "working":
				body.Working++
			}
		}
	}
	if cal := s.snapshotCalendar(ctx, now); cal != nil {
		for _, e := range cal.Events {
			body.Meetings = append(body.Meetings, todayMeeting{Start: e.Start, End: e.End, Title: e.Title})
		}
	}
	if chats := s.snapshotChats(ctx); chats != nil {
		for _, c := range chats.AtMe {
			body.AtMe = append(body.AtMe, todayChat{Name: c.Name, Unread: c.Unread, Preview: c.Preview})
		}
		body.Unread = len(chats.Other)
	}
	it, err := s.appendFeedAt(ctx, conversation, feedToday, date, body, day)
	return it, true, err
}

// ---- kinds that start without asking ----

// trustKind is a remembered kind of task (trustKey), as the settings page lists it.
type trustKind struct {
	Key      string `json:"key"`
	Machine  string `json:"machine"`
	Cwd      string `json:"cwd"`
	Readonly bool   `json:"readonly"`
}

func (s *Service) trustList(ctx context.Context) []trustKind {
	out := []trustKind{}
	for k := range s.trustedKinds(ctx) {
		parts := strings.Split(k, "|")
		if len(parts) != 3 {
			continue
		}
		out = append(out, trustKind{Key: k, Machine: parts[0], Cwd: parts[1], Readonly: parts[2] == "read"})
	}
	slices.SortFunc(out, func(a, b trustKind) int { return strings.Compare(a.Key, b.Key) })
	return out
}

// forgetTrust stops a kind from starting without asking; false when it was not remembered.
func (s *Service) forgetTrust(ctx context.Context, key string) (bool, error) {
	st := s.store()
	if st == nil {
		return false, errors.New("no store")
	}
	have := s.trustedKinds(ctx)
	if !have[key] {
		return false, nil
	}
	delete(have, key)
	keys := make([]string, 0, len(have))
	for k := range have {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return true, st.PutSetting(ctx, trustSetting, keys)
}

func (s *Service) todayRoutes() {
	s.handle("POST /api/v1/assistant/conversations/{id}/today", func(w http.ResponseWriter, r *http.Request) {
		if !fullRole(r) {
			apierr.Write(w, http.StatusForbidden, errors.New("read-only role cannot change conversations"))
			return
		}
		it, made, err := s.ensureToday(r.Context(), r.PathValue("id"))
		if err != nil {
			apierr.Write(w, http.StatusServiceUnavailable, err)
			return
		}
		apierr.WriteJSON(w, map[string]any{"item": it, "new": made})
	})
	s.handle("GET /api/v1/task-trust", func(w http.ResponseWriter, r *http.Request) {
		apierr.WriteJSON(w, map[string]any{"kinds": s.trustList(r.Context())})
	})
	s.handle("DELETE /api/v1/task-trust", func(w http.ResponseWriter, r *http.Request) {
		if !fullRole(r) {
			apierr.Write(w, http.StatusForbidden, errors.New("read-only role cannot change settings"))
			return
		}
		ok, err := s.forgetTrust(r.Context(), r.URL.Query().Get("key"))
		if err != nil {
			apierr.Write(w, http.StatusServiceUnavailable, err)
			return
		}
		if !ok {
			apierr.Write(w, http.StatusNotFound, errors.New("not remembered"))
			return
		}
		apierr.WriteJSON(w, map[string]any{"ok": true})
	})
}
