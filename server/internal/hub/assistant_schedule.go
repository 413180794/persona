package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The assistant acts on its own in two ways (docs/assistant.md):
//   - routines: daily prompts the user sets up in the app (早报 on weekdays at 09:00…), each run is a new conversation;
//   - follow-ups: one-off wake-ups the assistant schedules itself (schedule_followup), sent into the conversation
//     that asked for them (or a new one).
// A finished run pushes its answer like any agent turn (Machine.notifyFinished). Both lists live in Postgres settings.

type Routine struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Prompt   string `json:"prompt"`
	Time     string `json:"time"`               // "HH:MM" in the user's time zone
	Weekdays []int  `json:"weekdays,omitempty"` // 1 = Monday … 7 = Sunday; empty = every day
	Enabled  bool   `json:"enabled"`
	// Set by the hub.
	LastRun       time.Time `json:"last_run,omitempty"`
	LastSessionID string    `json:"last_session_id,omitempty"`
	NextRun       time.Time `json:"next_run,omitempty"`
}

type Followup struct {
	ID        string    `json:"id"`
	At        time.Time `json:"at"`
	Title     string    `json:"title,omitempty"`
	Prompt    string    `json:"prompt"`
	MachineID string    `json:"machine_id"`
	SessionID string    `json:"session_id,omitempty"` // conversation to continue; empty = a new one
	CreatedAt time.Time `json:"created_at"`
}

const (
	routinesKey  = "assistant.routines"
	followupsKey = "assistant.followups"
)

type schedule struct {
	mu        sync.Mutex
	loaded    bool
	routines  []Routine
	followups []Followup
}

func defaultRoutines() []Routine {
	weekdays := []int{1, 2, 3, 4, 5}
	return []Routine{
		{ID: "morning", Title: "早报", Time: "09:00", Weekdays: weekdays, Enabled: true, Prompt: "给我今天的早报，一屏以内，结论先行：\n" +
			"1. 今天的日程（飞书日历 + iPhone 日历，合并去重），标出需要准备的会；\n" +
			"2. 需要我处理的：飞书里 @我 / 单聊未读里要我回复或决定的事、重要邮件；\n" +
			"3. 各台电脑上的 Agent：哪些在等我、哪些昨晚跑完了结果是什么；\n" +
			"对今天每个值得准备的会，用 schedule_followup 在会前 10 分钟安排一次会前准备（材料、上次纪要、要点）。"},
		{ID: "evening", Title: "晚间回顾", Time: "21:30", Weekdays: weekdays, Enabled: false, Prompt: "今天的晚间回顾：今天开了哪些会、" +
			"有哪些待办和承诺没落实（会议纪要、飞书消息里我答应别人的事）、各台电脑上还在跑或卡住的 Agent、明天最早的安排。最后问我要不要把待办加进提醒事项。"},
	}
}

func (h *Hub) loadSchedule(ctx context.Context) *schedule {
	s := &h.sched
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return s
	}
	s.loaded = true
	s.routines = defaultRoutines()
	if h.store == nil {
		return s
	}
	var rs []Routine
	if ok, err := h.store.Setting(ctx, routinesKey, &rs); err == nil && ok {
		s.routines = rs
	}
	var fs []Followup
	if ok, err := h.store.Setting(ctx, followupsKey, &fs); err == nil && ok {
		s.followups = fs
	}
	return s
}

// saveLocked persists both lists; s.mu is held.
func (h *Hub) saveLocked(s *schedule) {
	if h.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.store.PutSetting(ctx, routinesKey, s.routines); err != nil {
		h.logf("save routines: %v", err)
	}
	if err := h.store.PutSetting(ctx, followupsKey, s.followups); err != nil {
		h.logf("save follow-ups: %v", err)
	}
}

// userLocation is the phone's time zone (the app reports it), else the hub's.
func (h *Hub) userLocation() *time.Location {
	h.mu.Lock()
	tz := h.userTZ
	h.mu.Unlock()
	if tz == "" && h.store != nil {
		var cfg AssistantConfig
		if ok, _ := h.store.Setting(context.Background(), "assistant", &cfg); ok && cfg.Timezone != "" {
			tz = cfg.Timezone
			h.mu.Lock()
			h.userTZ = tz
			h.mu.Unlock()
		}
	}
	if tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	return time.Local
}

var hhmm = regexp.MustCompile(`^([01]?\d|2[0-3]):([0-5]\d)$`)

// nextRun is the routine's first run strictly after `after`.
func nextRun(r Routine, after time.Time, loc *time.Location) time.Time {
	m := hhmm.FindStringSubmatch(r.Time)
	if m == nil {
		return time.Time{}
	}
	hh, _ := strconv.Atoi(m[1])
	mm, _ := strconv.Atoi(m[2])
	t := after.In(loc)
	for i := 0; i < 8; i++ {
		d := time.Date(t.Year(), t.Month(), t.Day()+i, hh, mm, 0, 0, loc)
		if !d.After(after) {
			continue
		}
		wd := int(d.Weekday())
		if wd == 0 {
			wd = 7
		}
		if len(r.Weekdays) == 0 || containsInt(r.Weekdays, wd) {
			return d
		}
	}
	return time.Time{}
}

func containsInt(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// parseWhen reads RFC 3339 or a relative "30m" / "2h" / "1d".
func parseWhen(s string, now time.Time, loc *time.Location) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02T15:04:05", s, loc); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04", s, loc); err == nil {
		return t, nil
	}
	if strings.HasSuffix(s, "d") {
		if n, err := strconv.Atoi(strings.TrimSuffix(s, "d")); err == nil {
			return now.Add(time.Duration(n) * 24 * time.Hour), nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil {
		return now.Add(d), nil
	}
	return time.Time{}, fmt.Errorf("can't read time %q: use RFC 3339 (2026-10-27T09:50:00+08:00) or 30m / 2h / 1d", s)
}

func (h *Hub) toolScheduleFollowup(ctx context.Context, m *Machine, sessionID string, args json.RawMessage) (string, error) {
	var a struct {
		At              string `json:"at"`
		Prompt          string `json:"prompt"`
		Title           string `json:"title"`
		NewConversation bool   `json:"new_conversation"`
	}
	if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.Prompt) == "" {
		return "", errors.New("at and prompt are required")
	}
	loc := h.userLocation()
	at, err := parseWhen(a.At, time.Now(), loc)
	if err != nil {
		return "", err
	}
	if at.Before(time.Now().Add(-time.Minute)) {
		return "", fmt.Errorf("%s is in the past", at.In(loc).Format("2006-01-02 15:04"))
	}
	f := Followup{ID: "fu_" + randomHex(4), At: at.UTC(), Title: a.Title, Prompt: a.Prompt, MachineID: m.cfg.ID, CreatedAt: time.Now().UTC()}
	if !a.NewConversation {
		f.SessionID = sessionID
	}
	s := h.loadSchedule(ctx)
	s.mu.Lock()
	s.followups = append(s.followups, f)
	h.saveLocked(s)
	s.mu.Unlock()
	h.broadcastSchedule()
	return fmt.Sprintf("Scheduled %s for %s (%s).", f.ID, at.In(loc).Format("01-02 15:04 Mon"), until(at)), nil
}

func until(t time.Time) string {
	d := time.Until(t).Round(time.Minute)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("in %d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("in %.1f h", d.Hours())
	}
	return fmt.Sprintf("in %d days", int(d.Hours()/24))
}

func (h *Hub) cancelFollowup(ctx context.Context, id string) bool {
	s := h.loadSchedule(ctx)
	s.mu.Lock()
	defer func() {
		s.mu.Unlock()
		h.broadcastSchedule()
	}()
	for i, f := range s.followups {
		if f.ID == id {
			s.followups = append(s.followups[:i], s.followups[i+1:]...)
			h.saveLocked(s)
			return true
		}
	}
	return false
}

func (h *Hub) describeSchedule(ctx context.Context) string {
	s := h.loadSchedule(ctx)
	loc := h.userLocation()
	s.mu.Lock()
	defer s.mu.Unlock()
	var b strings.Builder
	b.WriteString("Follow-ups:\n")
	if len(s.followups) == 0 {
		b.WriteString("- (none)\n")
	}
	for _, f := range s.followups {
		label := f.Title
		if label == "" {
			label = clip(oneLine(f.Prompt), 80)
		}
		fmt.Fprintf(&b, "- %s at %s: %s", f.ID, f.At.In(loc).Format("01-02 15:04 Mon"), label)
		if f.SessionID == "" {
			b.WriteString(" (new conversation)")
		}
		b.WriteString("\n")
	}
	b.WriteString("Routines (the user edits these in the app's settings):\n")
	for _, r := range s.routines {
		state := "off"
		if r.Enabled {
			state = "on"
		}
		fmt.Fprintf(&b, "- %s %s %s (%s)\n", r.Title, r.Time, weekdayText(r.Weekdays), state)
	}
	return b.String()
}

func weekdayText(ds []int) string {
	if len(ds) == 0 || len(ds) == 7 {
		return "每天"
	}
	if len(ds) == 5 && !containsInt(ds, 6) && !containsInt(ds, 7) {
		return "工作日"
	}
	names := []string{"", "一", "二", "三", "四", "五", "六", "日"}
	var b strings.Builder
	b.WriteString("周")
	for _, d := range ds {
		if d >= 1 && d <= 7 {
			b.WriteString(names[d])
		}
	}
	return b.String()
}

// scheduleLoop starts due routines and follow-ups.
func (h *Hub) scheduleLoop(ctx context.Context) {
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		h.runDue(ctx, time.Now())
	}
}

func (h *Hub) runDue(ctx context.Context, now time.Time) {
	s := h.loadSchedule(ctx)
	loc := h.userLocation()
	s.mu.Lock()
	var dueR []Routine
	for i := range s.routines {
		r := &s.routines[i]
		if !r.Enabled {
			continue
		}
		last := r.LastRun
		if last.IsZero() || now.Sub(last) > 36*time.Hour {
			last = now.Add(-2 * time.Minute) // never ran, or the hub was down: only catch up the last couple of minutes
		}
		if next := nextRun(*r, last, loc); !next.IsZero() && !next.After(now) {
			r.LastRun = now.UTC()
			if now.Sub(next) < 30*time.Minute { // missed by a long hub outage: skip rather than send a stale 早报 at noon
				dueR = append(dueR, *r)
			}
		}
	}
	var dueF, keep []Followup
	for _, f := range s.followups {
		if !f.At.After(now) {
			dueF = append(dueF, f)
		} else {
			keep = append(keep, f)
		}
	}
	if len(dueR) > 0 || len(dueF) > 0 {
		s.followups = keep
		h.saveLocked(s)
	}
	s.mu.Unlock()
	if len(dueR) == 0 && len(dueF) == 0 {
		return
	}
	h.broadcastSchedule()
	for _, r := range dueR {
		go h.runRoutine(r)
	}
	for _, f := range dueF {
		go h.runFollowup(f)
	}
}

func (h *Hub) runRoutine(r Routine) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	prompt := "⏰ " + r.Title + "（定时任务）\n\n" + r.Prompt
	sess, err := h.startAssistant(ctx, prompt, nil, "")
	if err != nil {
		h.logf("routine %s: %v", r.ID, err)
		return "", err
	}
	s := h.loadSchedule(ctx)
	s.mu.Lock()
	for i := range s.routines {
		if s.routines[i].ID == r.ID {
			s.routines[i].LastSessionID = sess.SessionID
		}
	}
	h.saveLocked(s)
	s.mu.Unlock()
	h.broadcastSchedule()
	return sess.SessionID, nil
}

func (h *Hub) runFollowup(f Followup) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	label := f.Title
	if label == "" {
		label = "定时跟进"
	}
	prompt := "⏰ " + label + "（你之前安排的跟进）\n\n" + f.Prompt
	if f.SessionID != "" {
		if m := h.byID[f.MachineID]; m != nil {
			err := m.call(ctx, "assistant.send", map[string]any{"session_id": f.SessionID, "text": prompt}, nil)
			if err == nil {
				return
			}
			h.logf("follow-up %s into %s: %v; starting a new conversation", f.ID, f.SessionID, err)
		}
	}
	if _, err := h.startAssistant(ctx, prompt, nil, ""); err != nil {
		h.logf("follow-up %s: %v", f.ID, err)
	}
}

func (h *Hub) broadcastSchedule() {
	h.broadcast(map[string]any{"type": "assistant_schedule"})
}

func (h *Hub) scheduleRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/assistant/routines", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"routines": h.routinesView(r.Context())})
	})
	api.HandleFunc("PUT /api/v1/assistant/routines", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Routines []Routine `json:"routines"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		s := h.loadSchedule(r.Context())
		s.mu.Lock()
		old := map[string]Routine{}
		for _, rt := range s.routines {
			old[rt.ID] = rt
		}
		out := make([]Routine, 0, len(in.Routines))
		for _, rt := range in.Routines {
			if !hhmm.MatchString(rt.Time) || strings.TrimSpace(rt.Prompt) == "" || strings.TrimSpace(rt.Title) == "" {
				s.mu.Unlock()
				writeErr(w, http.StatusBadRequest, fmt.Errorf("routine %q needs a title, a prompt and a time HH:MM", rt.Title))
				return
			}
			if rt.ID == "" {
				rt.ID = "rt_" + randomHex(4)
			}
			prev := old[rt.ID]
			rt.LastRun, rt.LastSessionID, rt.NextRun = prev.LastRun, prev.LastSessionID, time.Time{}
			out = append(out, rt)
		}
		s.routines = out
		h.saveLocked(s)
		s.mu.Unlock()
		h.broadcastSchedule()
		writeJSON(w, map[string]any{"routines": h.routinesView(r.Context())})
	})
	api.HandleFunc("POST /api/v1/assistant/routines/{id}/run", func(w http.ResponseWriter, r *http.Request) {
		s := h.loadSchedule(r.Context())
		s.mu.Lock()
		var found *Routine
		for _, rt := range s.routines {
			if rt.ID == r.PathValue("id") {
				rt := rt
				found = &rt
			}
		}
		s.mu.Unlock()
		if found == nil {
			writeErr(w, http.StatusNotFound, errors.New("no such routine"))
			return
		}
		id, err := h.runRoutine(*found)
		if err != nil {
			callErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"session_id": id, "machine_id": h.assistantConfig(r.Context()).MachineID})
	})
	api.HandleFunc("GET /api/v1/assistant/followups", func(w http.ResponseWriter, r *http.Request) {
		s := h.loadSchedule(r.Context())
		s.mu.Lock()
		list := append([]Followup{}, s.followups...)
		s.mu.Unlock()
		sort.Slice(list, func(i, j int) bool { return list[i].At.Before(list[j].At) })
		writeJSON(w, map[string]any{"followups": list})
	})
	api.HandleFunc("DELETE /api/v1/assistant/followups/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !h.cancelFollowup(r.Context(), r.PathValue("id")) {
			writeErr(w, http.StatusNotFound, errors.New("no such follow-up"))
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
}

func (h *Hub) routinesView(ctx context.Context) []Routine {
	s := h.loadSchedule(ctx)
	loc := h.userLocation()
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]Routine{}, s.routines...)
	for i := range out {
		if out[i].Enabled {
			out[i].NextRun = nextRun(out[i], time.Now(), loc)
		}
	}
	return out
}
