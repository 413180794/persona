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

	"github.com/chyroc/jarvis/server/internal/proto"
)

// The assistant acts on its own in two ways (docs/assistant.md):
//   - routines: daily prompts the user sets up in the app (早报 on weekdays at 09:00…), each run is a new conversation;
//   - follow-ups: one-off wake-ups the assistant schedules itself (schedule_followup), sent into the conversation
//     that asked for them (or a new one).
// A finished run pushes its answer like any agent turn (Machine.notifyFinished). Both lists, and the history of runs,
// live in Postgres settings, so they outlive a hub restart: a run is marked done before it starts (no repeats), a
// follow-up missed while the hub was down runs once when it is back (and is told how late it is), a routine missed by
// more than 30 minutes is skipped and the skip shows in the history.

type Routine struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Prompt   string `json:"prompt"`
	Time     string `json:"time"`               // "HH:MM" in the user's time zone
	Weekdays []int  `json:"weekdays,omitempty"` // 1 = Monday … 7 = Sunday; empty = every day
	Enabled  bool   `json:"enabled"`
	// CreatedBy is "assistant" for routines Jarvis set up from a conversation (schedule_routine).
	CreatedBy string `json:"created_by,omitempty"`
	// Set by the hub.
	LastRun       time.Time `json:"last_run,omitempty"`
	LastSessionID string    `json:"last_session_id,omitempty"`
	LastStatus    string    `json:"last_status,omitempty"` // of the last run: ok / failed / skipped
	LastError     string    `json:"last_error,omitempty"`
	NextRun       time.Time `json:"next_run,omitempty"`
}

// ScheduleRun is one entry of the history: a routine or follow-up that ran, failed to start, or was skipped.
type ScheduleRun struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`   // routine / followup
	RefID     string    `json:"ref_id"` // the routine's or follow-up's id
	Title     string    `json:"title"`
	Due       time.Time `json:"due"` // when it was meant to run (the tap for 立即运行)
	At        time.Time `json:"at"`  // when it ran
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	Manual    bool      `json:"manual,omitempty"` // 立即运行
	MachineID string    `json:"machine_id,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
}

const (
	runOK      = "ok"
	runFailed  = "failed"
	runSkipped = "skipped"
)

// maxRuns: how much history is kept.
const maxRuns = 200

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
	runsKey      = "assistant.runs"
)

type schedule struct {
	mu        sync.Mutex
	loaded    bool
	routines  []Routine
	followups []Followup
	runs      []ScheduleRun // newest last
	wake      chan struct{} // the lists changed: scheduleLoop recomputes when to wake up
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
	var runs []ScheduleRun
	if ok, err := h.store.Setting(ctx, runsKey, &runs); err == nil && ok {
		s.runs = runs
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
	b.WriteString("Routines (recurring; schedule_routine / delete_routine, or the user edits them in the app):\n")
	if len(s.routines) == 0 {
		b.WriteString("- (none)\n")
	}
	for _, r := range s.routines {
		state := "off"
		if r.Enabled {
			state = "on, next " + nextRun(r, time.Now(), loc).Format("01-02 15:04 Mon")
		}
		fmt.Fprintf(&b, "- %s %s: %s %s (%s)", r.ID, r.Title, weekdayText(r.Weekdays), r.Time, state)
		if r.LastStatus != "" {
			fmt.Fprintf(&b, ", last run %s %s", r.LastRun.In(loc).Format("01-02 15:04"), r.LastStatus)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// toolScheduleRoutine adds a recurring routine from a conversation ("每天 9 点给我早报"), or changes one by id.
func (h *Hub) toolScheduleRoutine(ctx context.Context, args json.RawMessage) (string, error) {
	var a struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Prompt   string `json:"prompt"`
		Time     string `json:"time"`
		Weekdays []int  `json:"weekdays"`
		Enabled  *bool  `json:"enabled"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", err
	}
	s := h.loadSchedule(ctx)
	loc := h.userLocation()
	s.mu.Lock()
	var r Routine
	i := -1
	if a.ID != "" {
		for j := range s.routines {
			if s.routines[j].ID == a.ID {
				i, r = j, s.routines[j]
			}
		}
		if i < 0 {
			s.mu.Unlock()
			return "", fmt.Errorf("no routine %q (list_followups lists them)", a.ID)
		}
	} else {
		r = Routine{ID: "rt_" + randomHex(4), Enabled: true, CreatedBy: "assistant"}
	}
	if a.Title != "" {
		r.Title = strings.TrimSpace(a.Title)
	}
	if a.Prompt != "" {
		r.Prompt = a.Prompt
	}
	if a.Time != "" {
		r.Time = strings.TrimSpace(a.Time)
	}
	if a.Weekdays != nil {
		r.Weekdays = nil
		for _, d := range a.Weekdays {
			if d < 1 || d > 7 {
				s.mu.Unlock()
				return "", fmt.Errorf("weekday %d: use 1 = Monday … 7 = Sunday", d)
			}
			if !containsInt(r.Weekdays, d) {
				r.Weekdays = append(r.Weekdays, d)
			}
		}
		sort.Ints(r.Weekdays)
		if len(r.Weekdays) == 7 {
			r.Weekdays = nil
		}
	}
	if a.Enabled != nil {
		r.Enabled = *a.Enabled
	}
	if !hhmm.MatchString(r.Time) || strings.TrimSpace(r.Prompt) == "" || r.Title == "" {
		s.mu.Unlock()
		return "", errors.New("a routine needs a title, a prompt and a time HH:MM")
	}
	if i < 0 {
		s.routines = append(s.routines, r)
	} else {
		s.routines[i] = r
	}
	h.saveLocked(s)
	s.mu.Unlock()
	h.broadcastSchedule()
	state := "off"
	if r.Enabled {
		next := nextRun(r, time.Now(), loc)
		state = "next run " + next.In(loc).Format("01-02 15:04 Mon") + " (" + until(next) + ")"
	}
	return fmt.Sprintf("Saved routine %s「%s」: %s %s, %s. Each run is a new conversation and the user gets a push with the answer; "+
		"they see and edit it in the app (Jarvis › 定时任务).", r.ID, r.Title, weekdayText(r.Weekdays), r.Time, state), nil
}

func (h *Hub) deleteRoutine(ctx context.Context, id string) bool {
	s := h.loadSchedule(ctx)
	s.mu.Lock()
	found := false
	for i, r := range s.routines {
		if r.ID == id {
			s.routines = append(s.routines[:i], s.routines[i+1:]...)
			found = true
			break
		}
	}
	if found {
		h.saveLocked(s)
	}
	s.mu.Unlock()
	if found {
		h.broadcastSchedule()
	}
	return found
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

// scheduleLoop starts due routines and follow-ups. It sleeps until the next one is due (at most scheduleIdle, so
// a time zone change is picked up) and wakes early when the lists change; the old 20-second poll ran follow-ups
// up to 20 seconds late (measured: 21 s on 2026-10-30).
func (h *Hub) scheduleLoop(ctx context.Context) {
	wake := h.sched.wakeup()
	for {
		now := time.Now()
		h.runDue(ctx, now)
		wait := scheduleIdle
		if next := h.nextDue(ctx, now); !next.IsZero() && next.Sub(now) < wait {
			wait = max(next.Sub(now), 50*time.Millisecond)
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-wake:
			t.Stop()
		case <-t.C:
		}
	}
}

const scheduleIdle = 30 * time.Second

func (s *schedule) wakeup() chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.wake == nil {
		s.wake = make(chan struct{}, 1)
	}
	return s.wake
}

// nextDue is the earliest moment a follow-up or an enabled routine is due; zero when nothing is scheduled.
func (h *Hub) nextDue(ctx context.Context, now time.Time) time.Time {
	s := h.loadSchedule(ctx)
	loc := h.userLocation()
	s.mu.Lock()
	defer s.mu.Unlock()
	var next time.Time
	earlier := func(t time.Time) {
		if !t.IsZero() && (next.IsZero() || t.Before(next)) {
			next = t
		}
	}
	for _, f := range s.followups {
		earlier(f.At)
	}
	for _, r := range s.routines {
		if r.Enabled {
			earlier(nextRun(r, now, loc))
		}
	}
	return next
}

func (h *Hub) runDue(ctx context.Context, now time.Time) {
	s := h.loadSchedule(ctx)
	loc := h.userLocation()
	s.mu.Lock()
	type dueRoutine struct {
		r   Routine
		due time.Time
	}
	var dueR []dueRoutine
	var skipped []ScheduleRun
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
			r.LastRun = now.UTC() // saved before it starts: a restart in between loses the run rather than repeating it
			if now.Sub(next) < 30*time.Minute {
				dueR = append(dueR, dueRoutine{*r, next})
			} else { // missed by a long hub outage: skip rather than send a stale 早报 at noon
				r.LastStatus, r.LastError = runSkipped, "Jarvis 停机，错过了时间"
				skipped = append(skipped, ScheduleRun{Kind: "routine", RefID: r.ID, Title: r.Title, Due: next.UTC(), At: now.UTC(),
					Status: runSkipped, Error: r.LastError})
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
	if len(dueR) > 0 || len(dueF) > 0 || len(skipped) > 0 {
		s.followups = keep
		h.saveLocked(s)
	}
	s.mu.Unlock()
	if len(dueR) == 0 && len(dueF) == 0 && len(skipped) == 0 {
		return
	}
	for _, r := range skipped {
		h.recordRun(r)
	}
	h.broadcastSchedule()
	for _, d := range dueR {
		go func() { _, _ = h.runRoutine(d.r, d.due, false) }()
	}
	for _, f := range dueF {
		go func() { _, _ = h.runFollowup(f, now, false) }()
	}
}

// recordRun appends to the history (and the routine's last status).
func (h *Hub) recordRun(r ScheduleRun) {
	r.ID = "run_" + randomHex(4)
	s := h.loadSchedule(context.Background())
	s.mu.Lock()
	s.runs = append(s.runs, r)
	if len(s.runs) > maxRuns {
		s.runs = append([]ScheduleRun(nil), s.runs[len(s.runs)-maxRuns:]...)
	}
	if r.Kind == "routine" {
		for i := range s.routines {
			if s.routines[i].ID == r.RefID {
				s.routines[i].LastStatus, s.routines[i].LastError = r.Status, r.Error
				if r.SessionID != "" {
					s.routines[i].LastSessionID = r.SessionID
				}
			}
		}
	}
	runs := append([]ScheduleRun(nil), s.runs...)
	h.saveLocked(s)
	s.mu.Unlock()
	if h.store != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.store.PutSetting(ctx, runsKey, runs); err != nil {
			h.logf("save schedule runs: %v", err)
		}
	}
	h.broadcastSchedule()
}

// runRoutine starts the routine's conversation (due: its scheduled time); manual is 立即运行 in the app.
func (h *Hub) runRoutine(r Routine, due time.Time, manual bool) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	start := time.Now()
	prompt := "⏰ " + r.Title + "（定时任务）\n\n" + r.Prompt
	run := ScheduleRun{Kind: "routine", RefID: r.ID, Title: r.Title, Due: due.UTC(), At: start.UTC(), Manual: manual, Status: runOK}
	sess, err := h.startAssistant(ctx, prompt, nil, "", "", "")
	if err != nil {
		h.logf("routine %s: %v", r.ID, err)
		run.Status, run.Error = runFailed, err.Error()
		h.recordRun(run)
		return "", err
	}
	run.MachineID, run.SessionID = sess.MachineID, sess.SessionID
	h.logf("routine %s ran %s after its time (conversation %s)", r.ID, lateness(run.Due, start), short(sess.SessionID))
	h.recordRun(run)
	return sess.SessionID, nil
}

// runFollowup sends the follow-up into its conversation, or a new one when that fails or there is none.
// now is when the scheduler noticed it was due: a follow-up that waited for the hub to come back is told how late it is.
func (h *Hub) runFollowup(f Followup, now time.Time, manual bool) (proto.Session, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	label := followupLabel(f)
	prompt := followupPrompt(f, now, h.userLocation(), manual)
	run := ScheduleRun{Kind: "followup", RefID: f.ID, Title: label, Due: f.At, At: time.Now().UTC(), Manual: manual, Status: runOK}
	if f.SessionID != "" {
		if m := h.byID[f.MachineID]; m != nil {
			err := m.call(ctx, "assistant.send", map[string]any{"session_id": f.SessionID, "text": prompt}, nil)
			if err == nil {
				run.MachineID, run.SessionID = f.MachineID, f.SessionID
				h.logf("follow-up %s ran %s after its time", f.ID, lateness(f.At, time.Now()))
				h.recordRun(run)
				return proto.Session{MachineID: f.MachineID, SessionID: f.SessionID}, nil
			}
			h.logf("follow-up %s into %s: %v; starting a new conversation", f.ID, f.SessionID, err)
		}
	}
	sess, err := h.startAssistant(ctx, prompt, nil, "", "", "")
	if err != nil {
		h.logf("follow-up %s: %v", f.ID, err)
		run.Status, run.Error = runFailed, err.Error()
		h.recordRun(run)
		return proto.Session{}, err
	}
	run.MachineID, run.SessionID = sess.MachineID, sess.SessionID
	h.logf("follow-up %s ran %s after its time (new conversation %s)", f.ID, lateness(f.At, time.Now()), short(sess.SessionID))
	h.recordRun(run)
	return *sess, nil
}

func followupLabel(f Followup) string {
	if f.Title == "" {
		return "定时跟进"
	}
	return f.Title
}

// followupPrompt is what the conversation gets; one that waited for the hub to come back says how late it is.
func followupPrompt(f Followup, now time.Time, loc *time.Location, manual bool) string {
	prompt := "⏰ " + followupLabel(f) + "（你之前安排的跟进）"
	if late := now.Sub(f.At); !manual && late > 2*time.Minute {
		prompt += fmt.Sprintf("\n（本该 %s 运行，Jarvis 当时没在运行，晚了 %s；看看还有没有必要按原计划做。）",
			f.At.In(loc).Format("01-02 15:04"), humanDuration(late))
	}
	return prompt + "\n\n" + f.Prompt
}

// lateness for the log: "0.4s".
func lateness(due, at time.Time) string {
	return at.Sub(due).Round(100 * time.Millisecond).String()
}

// humanDuration: "25 分钟" / "3 小时" / "2 天".
func humanDuration(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d 分钟", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d 小时", int(d.Hours()))
	}
	return fmt.Sprintf("%d 天", int(d.Hours()/24))
}

func (h *Hub) broadcastSchedule() {
	select { // the scheduler re-reads the lists
	case h.sched.wakeup() <- struct{}{}:
	default:
	}
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
		id, err := h.runRoutine(*found, time.Now(), true)
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
	api.HandleFunc("POST /api/v1/assistant/followups/{id}/run", func(w http.ResponseWriter, r *http.Request) {
		s := h.loadSchedule(r.Context())
		s.mu.Lock()
		var found *Followup
		for i, f := range s.followups {
			if f.ID == r.PathValue("id") {
				found = &f
				s.followups = append(s.followups[:i], s.followups[i+1:]...)
				h.saveLocked(s)
				break
			}
		}
		s.mu.Unlock()
		if found == nil {
			writeErr(w, http.StatusNotFound, errors.New("no such follow-up"))
			return
		}
		h.broadcastSchedule()
		sess, err := h.runFollowup(*found, time.Now(), true)
		if err != nil {
			callErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"session_id": sess.SessionID, "machine_id": sess.MachineID})
	})
	api.HandleFunc("GET /api/v1/assistant/runs", func(w http.ResponseWriter, r *http.Request) {
		s := h.loadSchedule(r.Context())
		s.mu.Lock()
		runs := make([]ScheduleRun, 0, len(s.runs))
		for i := len(s.runs) - 1; i >= 0; i-- { // newest first
			if ref := r.URL.Query().Get("ref"); ref == "" || s.runs[i].RefID == ref {
				runs = append(runs, s.runs[i])
			}
		}
		s.mu.Unlock()
		writeJSON(w, map[string]any{"runs": runs})
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
