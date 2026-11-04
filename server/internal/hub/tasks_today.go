package hub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"
)

// The 「今天」 card at the top of the day in an assistant conversation, and the kinds of tasks the user lets start
// without asking (docs/unified-entry.md「对话的形状」, decision 1).

// todayBody is what the 今天 card shows: built by the hub from what it already knows, no model in between.
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
func (h *Hub) ensureToday(ctx context.Context, conversation string) (FeedItem, bool, error) {
	loc := h.userLocation()
	now := time.Now().In(loc)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	date := day.Format("2006-01-02")
	items, _, err := h.feed(ctx, conversation, day.Add(-time.Second))
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
	if done, _, err := h.feed(ctx, conversation, since); err == nil {
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
	if h.ensureTodosLoaded(ctx) == nil {
		h.todos.mu.RLock()
		var mine []Todo
		for _, t := range h.todos.todos {
			if t.Origin == conversation && t.DoneAt == nil {
				mine = append(mine, copyTodoLocked(t))
			}
		}
		h.todos.mu.RUnlock()
		slices.SortFunc(mine, func(a, b Todo) int { return a.CreatedAt.Compare(b.CreatedAt) })
		for _, t := range mine {
			switch taskState(t, h.taskAgents(t)) {
			case "needs_you", "interrupted":
				body.Waiting = append(body.Waiting, todayTask{ID: t.ID, Title: t.Title})
			case "working":
				body.Working++
			}
		}
	}
	if h.lark == nil { // no Feishu here (unit tests): the card has the tasks only
		it, err := h.appendFeedAt(ctx, conversation, feedToday, date, body, day)
		return it, true, err
	}
	if cal := h.snapshotCalendar(ctx, now); cal != nil {
		for _, e := range cal.Events {
			body.Meetings = append(body.Meetings, todayMeeting{Start: e.Start, End: e.End, Title: e.Title})
		}
	}
	if chats := h.snapshotChats(ctx); chats != nil {
		for _, c := range chats.AtMe {
			body.AtMe = append(body.AtMe, todayChat{Name: c.Name, Unread: c.Unread, Preview: c.Preview})
		}
		body.Unread = len(chats.Other)
	}
	it, err := h.appendFeedAt(ctx, conversation, feedToday, date, body, day)
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

func (h *Hub) trustList(ctx context.Context) []trustKind {
	out := []trustKind{}
	for k := range h.trustedKinds(ctx) {
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
func (h *Hub) forgetTrust(ctx context.Context, key string) (bool, error) {
	if h.store == nil {
		return false, errors.New("no store")
	}
	have := h.trustedKinds(ctx)
	if !have[key] {
		return false, nil
	}
	delete(have, key)
	keys := make([]string, 0, len(have))
	for k := range have {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return true, h.store.PutSetting(ctx, trustSetting, keys)
}

func (h *Hub) todayRoutes(api *http.ServeMux) {
	api.HandleFunc("POST /api/v1/assistant/conversations/{id}/today", func(w http.ResponseWriter, r *http.Request) {
		if principalOf(r.Context()).Role != roleFull {
			writeErr(w, http.StatusForbidden, errors.New("read-only role cannot change conversations"))
			return
		}
		it, made, err := h.ensureToday(r.Context(), r.PathValue("id"))
		if err != nil {
			writeErr(w, http.StatusServiceUnavailable, err)
			return
		}
		writeJSON(w, map[string]any{"item": it, "new": made})
	})
	api.HandleFunc("GET /api/v1/task-trust", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"kinds": h.trustList(r.Context())})
	})
	api.HandleFunc("DELETE /api/v1/task-trust", func(w http.ResponseWriter, r *http.Request) {
		if principalOf(r.Context()).Role != roleFull {
			writeErr(w, http.StatusForbidden, errors.New("read-only role cannot change settings"))
			return
		}
		ok, err := h.forgetTrust(r.Context(), r.URL.Query().Get("key"))
		if err != nil {
			writeErr(w, http.StatusServiceUnavailable, err)
			return
		}
		if !ok {
			writeErr(w, http.StatusNotFound, errors.New("not remembered"))
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
}
