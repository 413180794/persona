package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// What the assistant does on its own (docs/assistant.md「定时任务」) is one list of tasks, each with a schedule:
//   - once: a follow-up the assistant scheduled from a conversation (schedule_followup), or the user in the app;
//   - daily: at a time on some weekdays (早报 on weekdays at 09:00…), set up in the app or with schedule_routine;
//   - interval: every N minutes (a 30-minute patrol), from schedule_followup's `every` or the app.
// A run goes into the conversation the task came from (SessionID), else into the task's own conversation
// (RunSessionID: the first run starts it, later ones continue it so each run sees the ones before), with the task's
// own launcher, permission mode and effort ("" = the assistant's defaults). A finished run pushes its answer like any agent turn
// (Machine.notifyFinished). The list and the history of runs live in Postgres settings, so they outlive a hub
// restart: a run is marked before it starts (no repeats), a once / interval task missed while the hub was down runs
// once when it is back (and is told how late it is), a daily one missed by more than 30 minutes is skipped and the
// skip shows in the history.
//
// Until 2026-10-31 routines and follow-ups were two lists; a follow-up vanished from the app the moment it started,
// so a patrol re-scheduling itself at the end of each run was nowhere to be seen (or stopped) while a run was stuck
// on an approval. loadSchedule moves the old lists over once, ids and times kept.

type Task struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Prompt string `json:"prompt"`
	Repeat string `json:"repeat"` // once / daily / interval
	// once: when; interval: the next run (the hub moves it on after each run).
	At           time.Time `json:"at,omitempty"`
	Time         string    `json:"time,omitempty"`     // daily: "HH:MM" in the user's time zone
	Weekdays     []int     `json:"weekdays,omitempty"` // daily: 1 = Monday … 7 = Sunday; empty = every day
	EveryMinutes int       `json:"every_minutes,omitempty"`
	Enabled      bool      `json:"enabled"`
	// The conversation it came from (on MachineID), runs go into it; empty = the task's own conversation.
	MachineID string `json:"machine_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	// The task's own conversation, set by the hub (2026-11-01: each run used to start a new one, so 「和昨天比」 had
	// nothing to compare with): the first run starts it on the assistant's machine with RunLauncher, later runs are
	// sent into it. Empty = the next run starts one (the app's 下次开新对话); so does a conversation that is gone or
	// can't be continued (another launcher or machine), and the new one is kept.
	RunMachineID string `json:"run_machine_id,omitempty"`
	RunSessionID string `json:"run_session_id,omitempty"`
	RunLauncher  string `json:"run_launcher,omitempty"`
	// How claude runs, chosen in the app only (the assistant can't raise its own permissions): "" = the assistant's
	// launcher, Claude Code's default mode, the launcher's effort.
	Launcher       string `json:"launcher,omitempty"`
	Model          string `json:"model,omitempty"`
	ModelRevision  int64  `json:"model_revision,omitempty"`
	PermissionMode string `json:"permission_mode,omitempty"`
	Effort         string `json:"effort,omitempty"`
	// CreatedBy is "assistant" for tasks Persona set up from a conversation.
	CreatedBy string    `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	// Set by the hub.
	LastRun       time.Time `json:"last_run,omitempty"`
	LastSessionID string    `json:"last_session_id,omitempty"`
	LastStatus    string    `json:"last_status,omitempty"` // of the last run: ok / failed / skipped
	LastError     string    `json:"last_error,omitempty"`
	NextRun       time.Time `json:"next_run,omitempty"`
}

const (
	repeatOnce     = "once"
	repeatDaily    = "daily"
	repeatInterval = "interval"
)

// minEvery: an interval task runs at most this often (each run is a whole claude turn).
const minEvery = 5

// doneKeep: a once task that ran stays this long (hidden in the app), so a run re-scheduling itself keeps its
// settings (scheduleOwn).
const doneKeep = 24 * time.Hour

// kind is what the history calls it (the app's 运行记录, from before tasks were one list).
func (t Task) kind() string {
	if t.Repeat == repeatOnce {
		return "followup"
	}
	return "routine"
}

func (t Task) label() string {
	if t.Title != "" {
		return t.Title
	}
	if t.Repeat == repeatOnce {
		return "定时跟进"
	}
	return clip(oneLine(t.Prompt), 30)
}

// done: a once task that already ran.
func (t Task) done() bool { return t.Repeat == repeatOnce && !t.Enabled && !t.LastRun.IsZero() }

// next is when the task runs next after `after`; zero when it is off.
func (t Task) next(after time.Time, loc *time.Location) time.Time {
	if !t.Enabled {
		return time.Time{}
	}
	if t.Repeat == repeatDaily {
		return nextRun(t, after, loc)
	}
	return t.At
}

// Routine and Followup are the lists from before 2026-10-31, read once to move them into tasks.
type Routine struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	Prompt        string    `json:"prompt"`
	Time          string    `json:"time"`
	Weekdays      []int     `json:"weekdays,omitempty"`
	Enabled       bool      `json:"enabled"`
	CreatedBy     string    `json:"created_by,omitempty"`
	LastRun       time.Time `json:"last_run,omitempty"`
	LastSessionID string    `json:"last_session_id,omitempty"`
	LastStatus    string    `json:"last_status,omitempty"`
	LastError     string    `json:"last_error,omitempty"`
}

type Followup struct {
	ID        string    `json:"id"`
	At        time.Time `json:"at"`
	Title     string    `json:"title,omitempty"`
	Prompt    string    `json:"prompt"`
	MachineID string    `json:"machine_id"`
	SessionID string    `json:"session_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// ScheduleRun is one entry of the history: a task that ran, failed to start, or was skipped.
type ScheduleRun struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`   // routine / followup (Task.kind)
	RefID     string    `json:"ref_id"` // the task's id
	Title     string    `json:"title"`
	Due       time.Time `json:"due"` // when it was meant to run (the tap for 立即运行)
	At        time.Time `json:"at"`  // when it ran
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	Manual    bool      `json:"manual,omitempty"` // 立即运行
	MachineID string    `json:"machine_id,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
	// Retried: this run is the automatic retry after a failure (so a retry that fails again does not retry forever).
	Retried bool `json:"retried,omitempty"`
	// of is when the scheduler started this run (the task's LastRun then; 立即运行's tap): a run that finishes after a
	// newer one started is history only, it does not overwrite the newer one's status (recordRun).
	of time.Time
}

const (
	runOK      = "ok"
	runFailed  = "failed"
	runSkipped = "skipped"
)

// maxRuns: how much history is kept.
const maxRuns = 200

const (
	tasksKey     = "assistant.tasks"
	routinesKey  = "assistant.routines"  // before 2026-10-31
	followupsKey = "assistant.followups" // before 2026-10-31
	runsKey      = "assistant.runs"
)

type schedule struct {
	mu     sync.Mutex
	loaded bool // false after a failed read: nothing is saved over what Postgres has, the next call reads again
	tasks  []Task
	runs   []ScheduleRun // newest last
	wake   chan struct{} // the list changed: scheduleLoop recomputes when to wake up
	// sending: tasks whose run is being sent right now (runTask), so two can't go into one conversation at once.
	sending map[string]bool
	// inflight: runs runDue started and that have not recorded their result yet.
	inflight sync.WaitGroup
}

func defaultRoutines() []Routine {
	weekdays := []int{1, 2, 3, 4, 5}
	return []Routine{
		{ID: "morning", Title: "早报", Time: "09:00", Weekdays: weekdays, Enabled: true, Prompt: prompts.RenderMorningRoutine()},
		{ID: "evening", Title: "晚间回顾", Time: "21:30", Weekdays: weekdays, Enabled: false, Prompt: prompts.RenderEveningRoutine()},
	}
}

// tasksFrom moves the old lists into one, ids, times and last runs kept.
func tasksFrom(routines []Routine, followups []Followup) []Task {
	out := make([]Task, 0, len(routines)+len(followups))
	for _, r := range routines {
		out = append(out, Task{ID: r.ID, Title: r.Title, Prompt: r.Prompt, Repeat: repeatDaily, Time: r.Time, Weekdays: r.Weekdays,
			Enabled: r.Enabled, CreatedBy: r.CreatedBy, LastRun: r.LastRun, LastSessionID: r.LastSessionID, LastStatus: r.LastStatus,
			LastError: r.LastError})
	}
	for _, f := range followups {
		out = append(out, Task{ID: f.ID, Title: f.Title, Prompt: stripScheduleHeader(f.Prompt), Repeat: repeatOnce, At: f.At, Enabled: true,
			MachineID: f.MachineID, SessionID: f.SessionID, CreatedBy: "assistant", CreatedAt: f.CreatedAt})
	}
	return out
}

// errScheduleUnavailable: the scheduled tasks could not be read from the database yet.
var errScheduleUnavailable = errors.New("暂时读不到定时任务（数据库没有响应），过一会儿再试")

// loadSchedule reads the task list and the run history once. A failed read leaves the schedule unloaded, so the next
// call reads again and saveLocked writes nothing: saving the defaults then would overwrite the stored tasks.
func (s *Service) loadSchedule(ctx context.Context) *schedule {
	sc := &s.sched
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.loaded {
		return sc
	}
	st := s.store()
	if st == nil {
		sc.loaded = true
		sc.tasks = tasksFrom(defaultRoutines(), nil)
		return sc
	}
	var tasks []Task
	ok, err := st.Setting(ctx, tasksKey, &tasks)
	if err != nil {
		s.logf("load tasks: %v", err)
		return sc
	}
	if !ok || tasks == nil {
		// First start with one list: move the routines and follow-ups over, then empty the old follow-ups so an
		// older hub started again can't run them a second time.
		routines := defaultRoutines()
		var rs []Routine
		ok, err := st.Setting(ctx, routinesKey, &rs)
		if err != nil {
			s.logf("load routines: %v", err)
			return sc
		}
		if ok {
			routines = rs
		}
		var fs []Followup
		if _, err := st.Setting(ctx, followupsKey, &fs); err != nil {
			s.logf("load follow-ups: %v", err)
			return sc
		}
		tasks = tasksFrom(routines, fs)
		if err := st.PutSetting(ctx, tasksKey, tasks); err != nil {
			s.logf("save tasks: %v", err)
			return sc
		}
		if len(fs) > 0 {
			if err := st.PutSetting(ctx, followupsKey, []Followup{}); err != nil {
				s.logf("empty the old follow-ups: %v", err)
			}
		}
		s.logf("schedule: %d routines and %d follow-ups moved into one task list", len(routines), len(fs))
	}
	var runs []ScheduleRun
	if _, err := st.Setting(ctx, runsKey, &runs); err != nil {
		s.logf("load schedule runs: %v", err)
		return sc
	}
	sc.tasks, sc.runs, sc.loaded = tasks, runs, true
	return sc
}

// lockedSchedule returns the schedule locked, or errScheduleUnavailable (and nothing locked) while it could not be
// read: a change made then would only live in memory and be lost on the next successful read, and a view of it would
// show no tasks at all. Writes fail (503 for the app, an error for the assistant's tools), the scheduler skips its turn.
func (s *Service) lockedSchedule(ctx context.Context) (*schedule, error) {
	sc := s.loadSchedule(ctx)
	sc.mu.Lock()
	if !sc.loaded {
		sc.mu.Unlock()
		return nil, errScheduleUnavailable
	}
	return sc, nil
}

// scheduleReadable is lockedSchedule for a view: nil once the schedule was read (it stays read).
func (s *Service) scheduleReadable(ctx context.Context) error {
	sc, err := s.lockedSchedule(ctx)
	if err == nil {
		sc.mu.Unlock()
	}
	return err
}

// saveLocked persists the list; sc.mu is held.
func (s *Service) saveLocked(sc *schedule) {
	st := s.store()
	if st == nil || !sc.loaded {
		return
	}
	if sc.tasks == nil {
		sc.tasks = []Task{} // null would read as "never moved over"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := st.PutSetting(ctx, tasksKey, sc.tasks); err != nil {
		s.logf("save tasks: %v", err)
	}
}

func (sc *schedule) indexLocked(id string) int {
	return slices.IndexFunc(sc.tasks, func(t Task) bool { return t.ID == id })
}

var hhmm = regexp.MustCompile(`^([01]?\d|2[0-3]):([0-5]\d)$`)

// nextRun is a daily task's first run strictly after `after`.
func nextRun(t Task, after time.Time, loc *time.Location) time.Time {
	m := hhmm.FindStringSubmatch(t.Time)
	if m == nil {
		return time.Time{}
	}
	hh, _ := strconv.Atoi(m[1])
	mm, _ := strconv.Atoi(m[2])
	at := after.In(loc)
	for i := 0; i < 8; i++ {
		d := time.Date(at.Year(), at.Month(), at.Day()+i, hh, mm, 0, 0, loc)
		if !d.After(after) {
			continue
		}
		wd := int(d.Weekday())
		if wd == 0 {
			wd = 7
		}
		if len(t.Weekdays) == 0 || slices.Contains(t.Weekdays, wd) {
			return d
		}
	}
	return time.Time{}
}

// relative reads "30m" / "2h" / "1d".
func relative(s string) (time.Duration, bool) {
	s = strings.TrimSpace(s)
	if n, ok := strings.CutSuffix(s, "d"); ok {
		if d, err := strconv.Atoi(n); err == nil {
			return time.Duration(d) * 24 * time.Hour, true
		}
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d, true
	}
	return 0, false
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
	if d, ok := relative(s); ok {
		return now.Add(d), nil
	}
	return time.Time{}, fmt.Errorf("can't read time %q: use RFC 3339 (2026-10-27T09:50:00+08:00) or 30m / 2h / 1d", s)
}

// scheduleHeader is the first line of a run's message (taskPrompt, prompts/assistant/task_run.md) and the notes under it. A patrol told to
// "schedule the next one with the same prompt" copies them along; they are dropped so they don't pile up.
var scheduleHeader = regexp.MustCompile(`^(?:⏰[^\n]*（[^\n]*(?:跟进|定时任务)[^\n]*）|（本该[^\n]*）|（(?:Jarvis|Persona) 会[^\n]*）)\s*`)

func stripScheduleHeader(p string) string {
	p = strings.TrimSpace(p)
	for {
		loc := scheduleHeader.FindStringIndex(p)
		if loc == nil {
			return p
		}
		p = strings.TrimSpace(p[loc[1]:])
	}
}

// everyText: "30 分钟" / "2 小时".
func everyText(minutes int) string {
	if minutes%60 == 0 {
		return fmt.Sprintf("%d 小时", minutes/60)
	}
	return fmt.Sprintf("%d 分钟", minutes)
}

// ruleText is the schedule in words, for the tools' replies and list_followups.
func ruleText(t Task, loc *time.Location) string {
	switch t.Repeat {
	case repeatDaily:
		return weekdayText(t.Weekdays) + " " + t.Time
	case repeatInterval:
		return "every " + everyText(t.EveryMinutes)
	}
	return "once at " + t.At.In(loc).Format("01-02 15:04 Mon")
}

func sameText(a, b string) bool {
	return a != "" && oneLine(stripScheduleHeader(a)) == oneLine(stripScheduleHeader(b))
}

func (s *Service) toolScheduleFollowup(ctx context.Context, machine, sessionID string, args json.RawMessage) (string, error) {
	var a struct {
		At              string `json:"at"`
		Every           string `json:"every"`
		Prompt          string `json:"prompt"`
		Title           string `json:"title"`
		NewConversation bool   `json:"new_conversation"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", err
	}
	prompt := stripScheduleHeader(a.Prompt)
	if prompt == "" {
		return "", errors.New("prompt is required")
	}
	loc, now := s.location(ctx), time.Now()
	every := 0
	if a.Every != "" {
		d, ok := relative(a.Every)
		if !ok || d < minEvery*time.Minute {
			return "", fmt.Errorf("every %q: use a duration of at least %dm, like 30m or 2h", a.Every, minEvery)
		}
		every = int(d.Minutes())
	}
	var at time.Time
	switch {
	case a.At != "":
		var err error
		if at, err = parseWhen(a.At, now, loc); err != nil {
			return "", err
		}
	case every > 0:
		at = now.Add(time.Duration(every) * time.Minute)
	default:
		return "", errors.New("at is required (or every, for something that repeats)")
	}
	if at.Before(now.Add(-time.Minute)) {
		return "", fmt.Errorf("%s is in the past", at.In(loc).Format("2006-01-02 15:04"))
	}
	if out, ok := s.scheduleOwn(ctx, sessionID, a.Title, prompt, a.At, at, every); ok {
		return out, nil
	}
	t := Task{ID: "fu_" + randomHex(4), Title: strings.TrimSpace(a.Title), Prompt: prompt, Repeat: repeatOnce, At: at.UTC(), Enabled: true,
		MachineID: machine, CreatedBy: "assistant", CreatedAt: now.UTC()}
	if every > 0 {
		t.Repeat, t.EveryMinutes = repeatInterval, every
	}
	if !a.NewConversation {
		t.SessionID = sessionID
	}
	sc, err := s.lockedSchedule(ctx)
	if err != nil {
		return "", err
	}
	sc.tasks = append(sc.tasks, t)
	s.saveLocked(sc)
	sc.mu.Unlock()
	s.broadcastSchedule()
	if every > 0 {
		return fmt.Sprintf("Scheduled %s every %s, first at %s (%s). It repeats by itself: don't schedule the next run from a run.",
			t.ID, everyText(every), at.In(loc).Format("01-02 15:04 Mon"), until(at)), nil
	}
	return fmt.Sprintf("Scheduled %s for %s (%s).", t.ID, at.In(loc).Format("01-02 15:04 Mon"), until(at)), nil
}

// scheduleOwn handles a run scheduling its own next run ("最后用 schedule_followup（at=30m，prompt 和本条相同）
// 安排下一次"): the same task is kept, with the launcher and permission mode the user picked for it, instead of a new
// follow-up per run. A one-off rescheduled by a relative time becomes a task every that long; one that already
// repeats stays as it is (a changed prompt is taken). ok is false when this conversation isn't one of the task's runs
// or the title and prompt are another task's.
func (s *Service) scheduleOwn(ctx context.Context, sessionID, title, prompt, atArg string, at time.Time, every int) (string, bool) {
	if sessionID == "" {
		return "", false
	}
	loc := s.location(ctx)
	sc, err := s.lockedSchedule(ctx)
	if err != nil {
		return "", false // the caller's own write says why
	}
	i := -1
	for j := len(sc.runs) - 1; j >= 0 && i < 0; j-- {
		if sc.runs[j].SessionID == sessionID {
			i = sc.indexLocked(sc.runs[j].RefID)
		}
	}
	if i < 0 || !(sameText(title, sc.tasks[i].Title) || sameText(prompt, sc.tasks[i].Prompt)) {
		sc.mu.Unlock()
		return "", false
	}
	t := &sc.tasks[i]
	if title = strings.TrimSpace(title); title != "" {
		t.Title = title
	}
	t.Prompt = prompt
	var out string
	switch {
	case t.Repeat != repeatOnce && !t.Enabled:
		out = fmt.Sprintf("%s「%s」 is a repeating task the user turned off; it stays off (they can turn it on in the app).", t.ID, t.label())
	case t.Repeat != repeatOnce:
		next := t.next(time.Now(), loc)
		out = fmt.Sprintf("%s「%s」 already repeats (%s); next run %s (%s). Kept its prompt up to date. Don't schedule its next run yourself.",
			t.ID, t.label(), ruleText(*t, loc), next.In(loc).Format("01-02 15:04 Mon"), until(next))
	default:
		if d, ok := relative(atArg); every == 0 && ok && d >= minEvery*time.Minute {
			every = int(d.Minutes())
		}
		t.At, t.Enabled = at.UTC(), true
		if every > 0 {
			t.Repeat, t.EveryMinutes = repeatInterval, every
			out = fmt.Sprintf("%s「%s」 now repeats every %s by itself (next %s, %s); the user sees it in Persona › 定时任务. "+
				"Don't schedule its next run from now on.", t.ID, t.label(), everyText(every), at.In(loc).Format("01-02 15:04 Mon"), until(at))
		} else {
			out = fmt.Sprintf("Rescheduled %s「%s」 for %s (%s).", t.ID, t.label(), at.In(loc).Format("01-02 15:04 Mon"), until(at))
		}
	}
	s.saveLocked(sc)
	sc.mu.Unlock()
	s.broadcastSchedule()
	return out, true
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

// deleteTask removes a task (cancel_followup, delete_routine, the app); false when there is none.
func (s *Service) deleteTask(ctx context.Context, id string) (bool, error) {
	sc, err := s.lockedSchedule(ctx)
	if err != nil {
		return false, err
	}
	i := sc.indexLocked(id)
	if i >= 0 {
		sc.tasks = slices.Delete(sc.tasks, i, i+1)
		s.saveLocked(sc)
	}
	sc.mu.Unlock()
	if i >= 0 {
		s.broadcastSchedule()
	}
	return i >= 0, nil
}

func (s *Service) describeSchedule(ctx context.Context) string {
	loc := s.location(ctx)
	now := time.Now()
	sc, err := s.lockedSchedule(ctx)
	if err != nil {
		return err.Error()
	}
	defer sc.mu.Unlock()
	var b strings.Builder
	b.WriteString("Scheduled tasks (the user sees and edits them in Persona › 定时任务; launcher / permission mode are theirs to set):\n")
	n := 0
	for _, t := range sc.tasks {
		if t.done() {
			continue
		}
		n++
		fmt.Fprintf(&b, "- %s 「%s」: %s", t.ID, t.label(), ruleText(t, loc))
		if !t.Enabled {
			b.WriteString(", off")
		} else if next := t.next(now, loc); !next.IsZero() && t.Repeat != repeatOnce {
			b.WriteString(", next " + next.In(loc).Format("01-02 15:04 Mon"))
		}
		switch {
		case t.SessionID != "":
			b.WriteString(", into conversation " + short(t.SessionID))
		case t.RunSessionID != "":
			b.WriteString(", in its own conversation " + short(t.RunSessionID))
		default:
			b.WriteString(", its own conversation starts on the next run")
		}
		if t.LastStatus != "" {
			fmt.Fprintf(&b, ", last run %s %s", t.LastRun.In(loc).Format("01-02 15:04"), t.LastStatus)
		}
		b.WriteString("\n")
	}
	if n == 0 {
		b.WriteString("- (none)\n")
	}
	return b.String()
}

// toolScheduleRoutine adds a daily task from a conversation ("每天 9 点给我早报"), or changes one by id.
func (s *Service) toolScheduleRoutine(ctx context.Context, args json.RawMessage) (string, error) {
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
	loc := s.location(ctx)
	sc, err := s.lockedSchedule(ctx)
	if err != nil {
		return "", err
	}
	var t Task
	i := -1
	if a.ID != "" {
		if i = sc.indexLocked(a.ID); i < 0 {
			sc.mu.Unlock()
			return "", fmt.Errorf("no task %q (list_followups lists them)", a.ID)
		}
		t = sc.tasks[i]
	} else {
		t = Task{ID: "rt_" + randomHex(4), Repeat: repeatDaily, Enabled: true, CreatedBy: "assistant", CreatedAt: time.Now().UTC()}
	}
	if a.Title != "" {
		t.Title = strings.TrimSpace(a.Title)
	}
	if a.Prompt != "" {
		t.Prompt = stripScheduleHeader(a.Prompt)
	}
	if a.Time != "" || a.Weekdays != nil {
		t.Repeat = repeatDaily
	}
	if a.Time != "" {
		t.Time = strings.TrimSpace(a.Time)
	}
	if a.Weekdays != nil {
		t.Weekdays = a.Weekdays
	}
	if a.Enabled != nil {
		t.Enabled = *a.Enabled
	}
	if err := checkTask(&t, time.Now()); err != nil {
		sc.mu.Unlock()
		return "", err
	}
	if i < 0 {
		sc.tasks = append(sc.tasks, t)
	} else {
		sc.tasks[i] = t
	}
	s.saveLocked(sc)
	sc.mu.Unlock()
	s.broadcastSchedule()
	state := "off"
	if next := t.next(time.Now(), loc); !next.IsZero() {
		state = "next run " + next.In(loc).Format("01-02 15:04 Mon") + " (" + until(next) + ")"
	}
	return fmt.Sprintf("Saved %s「%s」: %s, %s. Each run is a new conversation and the user gets a push with the answer; "+
		"they see and edit it in the app (Persona › 定时任务).", t.ID, t.label(), ruleText(t, loc), state), nil
}

var launcherName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

// checkTask validates what the app or a tool set and tidies it (weekdays sorted, every day = none); the messages
// are for the app.
func checkTask(t *Task, now time.Time) error {
	t.Title, t.Prompt = strings.TrimSpace(t.Title), strings.TrimSpace(t.Prompt)
	if t.Prompt == "" {
		return errors.New("提示词不能为空")
	}
	switch t.Repeat {
	case repeatDaily:
		if !hhmm.MatchString(t.Time) {
			return fmt.Errorf("时间 %q 要写成 HH:MM", t.Time)
		}
		if t.Title == "" {
			return errors.New("每天运行的任务要有标题")
		}
		var days []int
		for _, d := range t.Weekdays {
			if d < 1 || d > 7 {
				return fmt.Errorf("weekday %d: use 1 = Monday … 7 = Sunday", d)
			}
			if !slices.Contains(days, d) {
				days = append(days, d)
			}
		}
		sort.Ints(days)
		if len(days) == 7 {
			days = nil
		}
		t.Weekdays, t.At, t.EveryMinutes = days, time.Time{}, 0
	case repeatInterval:
		if t.EveryMinutes < minEvery || t.EveryMinutes > 7*24*60 {
			return fmt.Errorf("间隔要在 %d 分钟到 7 天之间", minEvery)
		}
		if t.At.IsZero() {
			t.At = now.Add(time.Duration(t.EveryMinutes) * time.Minute)
		}
		t.Time, t.Weekdays = "", nil
	case repeatOnce:
		if t.At.IsZero() {
			return errors.New("要选一个运行时间")
		}
		if t.Enabled && t.At.Before(now.Add(-time.Minute)) {
			return errors.New("这个时间已经过了，选一个以后的时间")
		}
		t.Time, t.Weekdays, t.EveryMinutes = "", nil, 0
	default:
		return fmt.Errorf("unknown repeat %q (once / daily / interval)", t.Repeat)
	}
	if t.Launcher != "" && !launcherName.MatchString(t.Launcher) {
		return fmt.Errorf("启动器 %q 的名字不对", t.Launcher)
	}
	if t.PermissionMode != "" && !slices.Contains(proto.PermissionModes, t.PermissionMode) {
		return fmt.Errorf("不认识的权限模式 %q", t.PermissionMode)
	}
	if t.Effort != "" && !slices.Contains(proto.EffortLevels, t.Effort) {
		return fmt.Errorf("不认识的思考强度 %q", t.Effort)
	}
	return nil
}

func weekdayText(ds []int) string {
	if len(ds) == 0 || len(ds) == 7 {
		return "每天"
	}
	if len(ds) == 5 && !slices.Contains(ds, 6) && !slices.Contains(ds, 7) {
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

// Run runs what the assistant does on its own until ctx ends: the scheduled tasks, closing finished tasks' agents, the
// knowledge base.
func (s *Service) Run(ctx context.Context) {
	go s.taskCloseLoop(ctx) // the agents of finished tasks (tasks_close.go)
	go s.workLoop(ctx)      // due wakes and retries, cards past their day, missed turns (agents_work.go)
	go s.channelLoop(ctx)   // the channels' runs that lost their watcher (channels.go)
	go s.kbLoop(ctx)        // the knowledge base (kb.go)
	s.scheduleLoop(ctx)
}

// scheduleLoop starts due tasks. It sleeps until the next one is due (at most scheduleIdle, so a time zone change is
// picked up) and wakes early when the list changes; the old 20-second poll ran follow-ups up to 20 seconds late
// (measured: 21 s on 2026-10-30).
func (s *Service) scheduleLoop(ctx context.Context) {
	wake := s.sched.wakeup()
	for {
		now := time.Now()
		if !s.hello(ctx).Assistant.Standby { // a mesh member's tasks wait while the center runs the group's (docs/mesh.md)
			s.runDue(ctx, now)
		}
		wait := scheduleIdle
		if next := s.nextDue(ctx, now); !next.IsZero() && next.Sub(now) < wait {
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

func (sc *schedule) wakeup() chan struct{} {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.wake == nil {
		sc.wake = make(chan struct{}, 1)
	}
	return sc.wake
}

// nextDue is the earliest moment an enabled task is due; zero when nothing is scheduled.
func (s *Service) nextDue(ctx context.Context, now time.Time) time.Time {
	loc := s.location(ctx)
	sc, err := s.lockedSchedule(ctx)
	if err != nil {
		return time.Time{} // read again on the next idle wake-up
	}
	defer sc.mu.Unlock()
	var next time.Time
	for _, t := range sc.tasks {
		if n := t.next(now, loc); !n.IsZero() && (next.IsZero() || n.Before(next)) {
			next = n
		}
	}
	return next
}

type dueTask struct {
	t   Task
	due time.Time
}

func (s *Service) runDue(ctx context.Context, now time.Time) {
	loc := s.location(ctx)
	sc, err := s.lockedSchedule(ctx)
	if err != nil {
		return // this turn is skipped: a task run now could not be marked as run
	}
	var due []dueTask
	var skipped []ScheduleRun
	changed := false
	for i := range sc.tasks {
		t := &sc.tasks[i]
		if !t.Enabled {
			continue
		}
		switch t.Repeat {
		case repeatDaily:
			last := t.LastRun
			if last.IsZero() || now.Sub(last) > 36*time.Hour {
				last = now.Add(-2 * time.Minute) // never ran, or the hub was down: only catch up the last couple of minutes
			}
			next := nextRun(*t, last, loc)
			if next.IsZero() || next.After(now) {
				continue
			}
			t.LastRun, changed = now.UTC(), true // saved before it starts: a restart in between loses the run rather than repeating it
			if now.Sub(next) < 30*time.Minute {
				due = append(due, dueTask{*t, next})
			} else { // missed by a long hub outage: skip rather than send a stale 早报 at noon
				t.LastStatus, t.LastError = runSkipped, "Persona 停机，错过了时间"
				skipped = append(skipped, ScheduleRun{Kind: t.kind(), RefID: t.ID, Title: t.label(), Due: next.UTC(), At: now.UTC(),
					Status: runSkipped, Error: t.LastError, of: now.UTC()})
			}
		case repeatOnce, repeatInterval:
			if t.At.After(now) {
				continue
			}
			due = append(due, dueTask{*t, t.At})
			t.LastRun, changed = now.UTC(), true
			if t.Repeat == repeatOnce {
				t.Enabled = false
			} else if t.At = t.At.Add(time.Duration(t.EveryMinutes) * time.Minute); !t.At.After(now) {
				t.At = now.Add(time.Duration(t.EveryMinutes) * time.Minute).UTC() // missed several: once now, then on from here
			}
		}
	}
	// A one-off that ran a day ago is only history now.
	kept := sc.tasks[:0:0]
	for _, t := range sc.tasks {
		if t.done() && now.Sub(t.LastRun) > doneKeep {
			changed = true
			continue
		}
		kept = append(kept, t)
	}
	sc.tasks = kept
	if changed {
		s.saveLocked(sc)
	}
	sc.mu.Unlock()
	if !changed {
		return
	}
	for _, r := range skipped {
		s.recordRun(r)
	}
	s.broadcastSchedule()
	for _, d := range due {
		sc.inflight.Add(1)
		go func() {
			defer sc.inflight.Done()
			_, _ = s.runTask(d.t, d.due, now, false, false)
		}()
	}
}

// recordRun appends to the history, and sets the task's last status unless a newer run has started since this one
// (r.of before the task's LastRun): runs record when they end, and a slow one — an assistant that takes a minute to
// give up — must not overwrite the status of the run or skip that came after it (#556).
func (s *Service) recordRun(r ScheduleRun) string {
	r.ID = "run_" + randomHex(4)
	sc, err := s.lockedSchedule(context.Background())
	if err != nil {
		s.logf("schedule run %s %s not recorded: %v", r.Kind, r.RefID, err)
		return ""
	}
	sc.runs = append(sc.runs, r)
	if len(sc.runs) > maxRuns {
		sc.runs = append([]ScheduleRun(nil), sc.runs[len(sc.runs)-maxRuns:]...)
	}
	if i := sc.indexLocked(r.RefID); i >= 0 && (r.of.IsZero() || !sc.tasks[i].LastRun.After(r.of)) {
		t := &sc.tasks[i]
		t.LastStatus, t.LastError = r.Status, r.Error
		if r.SessionID != "" {
			t.LastSessionID = r.SessionID
		}
	}
	s.saveLocked(sc)
	if st := s.store(); st != nil { // under the lock: two runs finishing together must not save their lists out of order
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := st.PutSetting(ctx, runsKey, sc.runs); err != nil {
			s.logf("save schedule runs: %v", err)
		}
		cancel()
	}
	sc.mu.Unlock()
	s.broadcastSchedule()
	return r.ID
}

// updateRun rewrites a recorded run's outcome (a run that was sent but whose turn ended in an API error) and the
// task's last status when this run is still the newest.
func (s *Service) updateRun(r ScheduleRun, status, errText string) {
	sc, err := s.lockedSchedule(context.Background())
	if err != nil {
		return
	}
	i := slices.IndexFunc(sc.runs, func(x ScheduleRun) bool { return x.ID == r.ID })
	if i < 0 {
		sc.mu.Unlock()
		return
	}
	sc.runs[i].Status, sc.runs[i].Error = status, errText
	if j := sc.indexLocked(r.RefID); j >= 0 && (r.of.IsZero() || !sc.tasks[j].LastRun.After(r.of)) {
		sc.tasks[j].LastStatus, sc.tasks[j].LastError = status, errText
	}
	s.saveLocked(sc)
	if st := s.store(); st != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := st.PutSetting(ctx, runsKey, sc.runs); err != nil {
			s.logf("save schedule runs: %v", err)
		}
		cancel()
	}
	sc.mu.Unlock()
	s.broadcastSchedule()
}

// errTaskBusy: the task's last run is still going; this one is skipped (and recorded) rather than queued behind it.
var errTaskBusy = errors.New("上一轮还在运行，这次跳过")

// runTask sends the task's message into the conversation it came from (on that conversation's launcher, as the phone's
// messages are), else into its own conversation, else (none yet, or that one is gone, busy elsewhere, or on another
// launcher / machine) a new one that becomes its own; those two on 模型配置 › 主对话 (#1544), with the task's
// permission mode. due: when it was meant to run; now: when the scheduler noticed (a run that
// waited for the hub to come back is told how late it is); manual is 立即运行 in the app.
//
// A repeating task whose own conversation is still on its last run (working, or waiting on the user) is skipped,
// not queued: two runs stacked in one conversation answer twice in a row, and the second one only sees the first
// half done. The skip is in the history like a missed one. A one-off still goes in (it would be lost otherwise).
func (s *Service) runTask(t Task, due, now time.Time, manual, retried bool) (proto.Session, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	loc, kb := s.location(ctx), s.kbReady(ctx)
	run := ScheduleRun{Kind: t.kind(), RefID: t.ID, Title: t.label(), Due: due.UTC(), At: time.Now().UTC(), Manual: manual, Status: runOK,
		Retried: retried, of: now.UTC()}
	if !s.sched.begin(t.ID) { // the same task is being sent right now (立即运行 as it came due)
		return proto.Session{}, s.skipRun(run, errTaskBusy)
	}
	defer s.sched.end(t.ID)
	if t.SessionID != "" {
		if m, ok := s.device(ctx, t.MachineID); ok {
			// A follow-up the assistant scheduled for itself is a turn of that conversation: it keeps the
			// conversation's launcher like a message from the phone does. On the task's category launcher (Agents
			// then) it moved the main conversation onto another one (plain claude, not logged in there) for good (#1518).
			params := map[string]any{"session_id": t.SessionID, "text": taskPrompt(t, due, now, loc, manual, kb, false), "permission_mode": t.PermissionMode}
			err := s.sendModel(ctx, t.MachineID, t.SessionID, params)
			if err == nil {
				err = s.call(ctx, m.ID, DeviceAssistantSend, params, nil)
			}
			if err == nil {
				run.MachineID, run.SessionID = t.MachineID, t.SessionID
				s.logf("task %s ran %s after its time", t.ID, lateness(due, time.Now()))
				s.startScheduledRun(run, t, &m)
				return proto.Session{MachineID: t.MachineID, SessionID: t.SessionID}, nil
			}
			s.logf("task %s into %s: %v; starting a new conversation", t.ID, t.SessionID, err)
		}
	}
	here := s.settings(ctx).MachineID
	binding, err := s.resolveModel(ctx, "task."+t.ID)
	if err != nil {
		run.Status, run.Error = runFailed, err.Error()
		s.recordRun(run)
		return proto.Session{}, err
	}
	launcher := strings.TrimPrefix(binding.Provider, "launcher.")
	if t.RunSessionID != "" {
		m, ok := s.device(ctx, t.RunMachineID)
		switch {
		case t.RunLauncher != launcher:
			s.logf("task %s: launcher %s → %s, its conversation %s can't go on; starting a new one", t.ID, t.RunLauncher, launcher, short(t.RunSessionID))
		case t.RunMachineID != here || !ok:
			s.logf("task %s: the assistant moved off %s, its conversation %s stays there; starting a new one", t.ID, t.RunMachineID, short(t.RunSessionID))
		case t.Repeat != repeatOnce && s.sessionBusy(ctx, m.ID, t.RunSessionID):
			s.logf("task %s: its conversation %s is still on the last run; skipped", t.ID, short(t.RunSessionID))
			run.MachineID, run.SessionID = t.RunMachineID, t.RunSessionID
			return proto.Session{}, s.skipRun(run, errTaskBusy)
		default:
			params := map[string]any{"session_id": t.RunSessionID, "text": taskPrompt(t, due, now, loc, manual, kb, true), "permission_mode": t.PermissionMode, "resume_only": true}
			err := s.applyModel(ctx, "task."+t.ID, params)
			if err == nil {
				err = s.call(ctx, m.ID, DeviceAssistantSend, params, nil)
			}
			if err == nil {
				run.MachineID, run.SessionID = t.RunMachineID, t.RunSessionID
				s.logf("task %s ran %s after its time (its conversation %s)", t.ID, lateness(due, time.Now()), short(t.RunSessionID))
				s.startScheduledRun(run, t, &m)
				return proto.Session{MachineID: t.RunMachineID, SessionID: t.RunSessionID}, nil
			}
			s.logf("task %s into its conversation %s: %v; starting a new one", t.ID, short(t.RunSessionID), err)
		}
	}
	sess, err := s.startConversation(ctx, startParams{Text: taskPrompt(t, due, now, loc, manual, kb, false), PermissionMode: t.PermissionMode,
		Purpose: "task." + t.ID})
	if err != nil {
		s.logf("task %s: %v", t.ID, err)
		run.Status, run.Error = runFailed, err.Error()
		s.recordRun(run)
		return proto.Session{}, err
	}
	run.MachineID, run.SessionID = sess.MachineID, sess.SessionID
	s.logf("task %s ran %s after its time (new conversation %s, kept for the next runs)", t.ID, lateness(due, time.Now()), short(sess.SessionID))
	s.adoptConversation(t.ID, sess.MachineID, sess.SessionID, sess.Launcher)
	s.titleNew(sess.MachineID, sess.SessionID, time.Now(), t.label()) // 「MMDD | 定时 | 任务名」, the same for every run in it
	var m *Device
	if d, ok := s.device(ctx, sess.MachineID); ok {
		m = &d
	}
	s.startScheduledRun(run, t, m)
	return sess, nil
}

// sessionBusy: the conversation is on a turn or waiting for the user (an approval, a question).
func (s *Service) sessionBusy(ctx context.Context, machine, id string) bool {
	c, ok := s.session(ctx, machine, id)
	return ok && (c.Status == proto.StatusWorking || c.State == proto.StateWorking || c.State == proto.StateNeedsYou)
}

func (s *Service) skipRun(run ScheduleRun, why error) error {
	run.Status, run.Error = runSkipped, why.Error()
	s.recordRun(run)
	return why
}

const runWatchTimeout = 30 * time.Minute

// runRetryDelay is how long after a failed run the automatic retry goes (a var: tests shorten it).
var runRetryDelay = 3 * time.Minute

// startRun records a sent run and follows its turn. The node accepts `assistant.send` before the model answers, so a
// run whose turn ends in an API error (quota, no such model, upstream 503) used to stay "ok" and the user was never
// told; the watcher records it as failed, pushes it, and retries once. m is the machine the run went to (nil: none).
func (s *Service) startScheduledRun(run ScheduleRun, t Task, m *Device) {
	run.ID = s.recordRun(run)
	if m == nil || run.ID == "" {
		return
	}
	go s.watchScheduledRun(run, t, *m)
}

func (s *Service) watchScheduledRun(run ScheduleRun, t Task, m Device) {
	ctx, cancel := context.WithTimeout(context.Background(), runWatchTimeout)
	defer cancel()
	if !s.waitTurnEnd(ctx, m.ID, run.SessionID) {
		return
	}
	text := s.lastTurnAPIError(ctx, m.ID, run.SessionID)
	if text == "" {
		return
	}
	text = clip(oneLine(text), 300)
	s.logf("task %s run %s failed: %s", t.ID, run.ID, text)
	s.updateRun(run, runFailed, text)
	s.push(ctx, Notification{Subtitle: m.Name, DeviceID: m.ID, SessionID: run.SessionID, Title: "定时任务失败：" + t.label(), Body: text})
	if run.Retried {
		return
	}
	select {
	case <-ctx.Done():
		return
	case <-time.After(runRetryDelay):
	}
	if _, err := s.runTask(t, time.Now(), time.Now(), false, true); err != nil {
		s.logf("task %s retry: %v", t.ID, err)
	}
}

// waitTurnEnd waits until the sent turn is over; false when ctx ended. An immediate failure (a bad model) is idle
// before the first poll sees it working, so a changed last item also means it ended.
func (s *Service) waitTurnEnd(ctx context.Context, machine, sid string) bool {
	before := s.lastItemID(ctx, machine, sid)
	ran := false
	idle := 0
	for {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(2 * time.Second):
		}
		c, ok := s.session(ctx, machine, sid)
		if ok && (c.Status == proto.StatusWorking || c.State == proto.StateWorking || c.State == proto.StateNeedsYou) {
			ran, idle = true, 0
			if c.State == proto.StateNeedsYou {
				return true // waiting on the user counts as ended
			}
			continue
		}
		if s.lastItemID(ctx, machine, sid) != before {
			return true
		}
		if ran {
			if idle++; idle >= 2 {
				return true
			}
		}
	}
}

// lastItemID is the newest item's id, "" when there is none.
func (s *Service) lastItemID(ctx context.Context, machine, sid string) string {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var res struct {
		Items []proto.Item `json:"items"`
	}
	if err := s.call(cctx, machine, DeviceSessionItems, map[string]any{"session_id": sid, "limit": 1, "lazy_images": true}, &res); err != nil || len(res.Items) == 0 {
		return ""
	}
	return res.Items[len(res.Items)-1].ID
}

// lastTurnAPIError is the text of the turn's last answer when it is a model API error; "" otherwise.
func (s *Service) lastTurnAPIError(ctx context.Context, machine, sid string) string {
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var res struct {
		Items []proto.Item `json:"items"`
	}
	if err := s.call(cctx, machine, DeviceSessionItems, map[string]any{"session_id": sid, "limit": 8, "lazy_images": true}, &res); err != nil {
		return ""
	}
	for i := len(res.Items) - 1; i >= 0; i-- {
		it := res.Items[i]
		if it.Role != "assistant" && it.Kind != "" && it.Kind != "text" {
			continue
		}
		text := strings.TrimSpace(it.Text)
		if text == "" {
			continue
		}
		if apiErrorText(text) {
			return text
		}
		return ""
	}
	return ""
}

// apiErrorMarkers start what Claude Code answers when the model call itself failed (an error card in the app).
var apiErrorMarkers = assistantwire.APIErrorMarkers

// cutOff: an agent's answer is the model API error that ended its turn.
func cutOff(answer string) bool { return assistantwire.CutOff(answer) }

// apiErrorText: what Claude Code answers when the model call itself failed (an error card in the app).
func apiErrorText(text string) bool {
	for _, marker := range apiErrorMarkers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// begin marks task id as being sent; false when it already is. end clears the mark.
func (sc *schedule) begin(id string) bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.sending[id] {
		return false
	}
	if sc.sending == nil {
		sc.sending = map[string]bool{}
	}
	sc.sending[id] = true
	return true
}

func (sc *schedule) end(id string) {
	sc.mu.Lock()
	delete(sc.sending, id)
	sc.mu.Unlock()
}

// adoptConversation makes a new conversation task id's own (saved with the next recordRun), and keeps it listed on
// its machine however quiet it gets (keepListed), so the app can open it and the next run can see it is busy.
func (s *Service) adoptConversation(id, machine, session, launcher string) {
	if sc, err := s.lockedSchedule(context.Background()); err == nil {
		if i := sc.indexLocked(id); i >= 0 {
			t := &sc.tasks[i]
			t.RunMachineID, t.RunSessionID, t.RunLauncher = machine, session, launcher
		}
		sc.mu.Unlock()
	}
	go s.keepListed(context.Background())
}

// keepListed tells the hub the conversations their machines are to list however long they are quiet have changed
// (the snapshot's profile: keptSessions); it configures the nodes.
func (s *Service) keepListed(ctx context.Context) { s.publish(ctx) }

// forgetConversation is 下次开新对话: the next run starts a new conversation (the old one stays in 对话).
func (s *Service) forgetConversation(ctx context.Context, id string) (Task, bool, error) {
	sc, err := s.lockedSchedule(ctx)
	if err != nil {
		return Task{}, false, err
	}
	i := sc.indexLocked(id)
	if i >= 0 {
		t := &sc.tasks[i]
		t.RunMachineID, t.RunSessionID, t.RunLauncher = "", "", ""
		s.saveLocked(sc)
	}
	sc.mu.Unlock()
	if i < 0 {
		return Task{}, false, nil
	}
	s.broadcastSchedule()
	go s.keepListed(context.WithoutCancel(ctx))
	t, ok := s.taskView(ctx, id)
	return t, ok, nil
}

// taskPrompt is what the conversation gets: a ⏰ line saying what this is, how late when the hub was down, and for a
// repeating one that it repeats by itself (so the run doesn't schedule the next one too). continued: it goes into the
// task's own conversation, which has had all that (and the knowledge-base hint) already; it is told the earlier
// runs are above instead. The task's prompt is always there: it may have been edited, or compacted away.
func taskPrompt(t Task, due, now time.Time, loc *time.Location, manual, kb, continued bool) string {
	r := prompts.TaskRun{Label: t.label(), Once: t.Repeat == repeatOnce, Interval: t.Repeat == repeatInterval, Due: due.In(loc),
		Prompt: t.Prompt, KBHint: kb && t.Repeat == repeatDaily, Continued: continued}
	if r.Interval {
		r.Every = everyText(t.EveryMinutes)
	}
	if late := now.Sub(due); !manual && t.Repeat != repeatDaily && late > 2*time.Minute {
		r.Late = humanDuration(late)
	}
	return prompts.RenderTaskRun(r)
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

// broadcastSchedule: the list changed. The scheduler re-reads it, the apps are told (event assistant_schedule) and
// the hub gets the tasks in the snapshot.
func (s *Service) broadcastSchedule() {
	select { // the scheduler re-reads the list
	case s.sched.wakeup() <- struct{}{}:
	default:
	}
	ctx := context.Background()
	s.publish(ctx)
	s.event(ctx, map[string]any{"type": "assistant_schedule"})
}

// taskViews are the tasks as the snapshot reports them; nil while the schedule can't be read.
func (s *Service) taskViews(ctx context.Context) []TaskView {
	sc, err := s.lockedSchedule(ctx)
	if err != nil {
		return nil
	}
	defer sc.mu.Unlock()
	out := make([]TaskView, 0, len(sc.tasks))
	for _, t := range sc.tasks {
		out = append(out, TaskView{ID: t.ID, Label: t.label(), Repeat: t.Repeat, Enabled: t.Enabled, At: t.At, EveryMinutes: t.EveryMinutes,
			MachineID: t.MachineID, RunMachineID: t.RunMachineID, RunSessionID: t.RunSessionID})
	}
	return out
}

// tasksView is the list as the app sees it: next runs filled in, one-offs that already ran left out. Empty while the
// schedule can't be read: the routes check scheduleReadable first.
func (s *Service) tasksView(ctx context.Context, keep func(Task) bool) []Task {
	sc := s.loadSchedule(ctx)
	loc := s.location(ctx)
	machine := s.settings(ctx).MachineID
	now := time.Now()
	sc.mu.Lock()
	defer sc.mu.Unlock()
	out := []Task{}
	for _, t := range sc.tasks {
		if t.done() || (keep != nil && !keep(t)) {
			continue
		}
		t.NextRun = t.next(now, loc)
		if t.MachineID == "" {
			t.MachineID = machine
		}
		t.Launcher, t.Model, t.Effort = "", "", ""
		out = append(out, t)
	}
	return out
}

func (s *Service) taskView(ctx context.Context, id string) (Task, bool) {
	for _, t := range s.tasksView(ctx, nil) {
		if t.ID == id {
			return t, true
		}
	}
	return Task{}, false
}

// editTask applies what the app changed (the hub's own fields and where runs go are kept; SessionID can only be
// cleared, to run in a new conversation each time).
func editTask(old, in Task, now time.Time) Task {
	t := old
	t.Title, t.Prompt, t.Repeat, t.Enabled = in.Title, in.Prompt, in.Repeat, in.Enabled
	t.Time, t.Weekdays, t.EveryMinutes = in.Time, in.Weekdays, in.EveryMinutes
	t.PermissionMode = in.PermissionMode
	t.Launcher, t.Model, t.Effort = "", "", ""
	if in.SessionID == "" {
		t.SessionID = ""
	}
	switch t.Repeat {
	case repeatOnce:
		t.At = in.At
	case repeatInterval:
		// The next run stays unless the rule changed (or the app picked a start); then one interval from now.
		every := time.Duration(t.EveryMinutes) * time.Minute
		switch {
		case !in.At.IsZero() && in.At.After(now) && !in.At.Equal(old.At) && in.At.Before(now.Add(every)):
			t.At = in.At
		case old.Repeat != repeatInterval || old.EveryMinutes != t.EveryMinutes || !old.At.After(now) || (!old.Enabled && t.Enabled):
			t.At = now.Add(every)
		}
	}
	return t
}

func (s *Service) scheduleRoutes() {
	s.handle("GET /api/v1/assistant/tasks", func(w http.ResponseWriter, r *http.Request) {
		if err := s.scheduleReadable(r.Context()); err != nil {
			apierr.Write(w, http.StatusServiceUnavailable, err)
			return
		}
		apierr.WriteJSON(w, map[string]any{"tasks": s.tasksView(r.Context(), nil)})
	})
	s.handle("POST /api/v1/assistant/tasks", func(w http.ResponseWriter, r *http.Request) {
		var in Task
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			apierr.Write(w, http.StatusBadRequest, err)
			return
		}
		now := time.Now()
		t := editTask(Task{ID: "task_" + randomHex(4), CreatedAt: now.UTC()}, in, now)
		t.SessionID = ""
		if err := checkTask(&t, now); err != nil {
			apierr.Write(w, http.StatusBadRequest, err)
			return
		}
		sc, err := s.lockedSchedule(r.Context())
		if err != nil {
			apierr.Write(w, http.StatusServiceUnavailable, err)
			return
		}
		sc.tasks = append(sc.tasks, t)
		s.saveLocked(sc)
		sc.mu.Unlock()
		s.broadcastSchedule()
		v, _ := s.taskView(r.Context(), t.ID)
		apierr.WriteJSON(w, map[string]any{"task": v})
	})
	s.handle("PUT /api/v1/assistant/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		var raw map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			apierr.Write(w, http.StatusBadRequest, err)
			return
		}
		sc, err := s.lockedSchedule(r.Context())
		if err != nil {
			apierr.Write(w, http.StatusServiceUnavailable, err)
			return
		}
		i := sc.indexLocked(r.PathValue("id"))
		if i < 0 {
			sc.mu.Unlock()
			apierr.Write(w, http.StatusNotFound, errors.New("这个定时任务已经不在了"))
			return
		}
		var in Task
		encoded, _ := json.Marshal(raw)
		if err := json.Unmarshal(encoded, &in); err != nil {
			sc.mu.Unlock()
			apierr.Write(w, 400, err)
			return
		}
		t := editTask(sc.tasks[i], in, time.Now())
		if err := checkTask(&t, time.Now()); err != nil {
			sc.mu.Unlock()
			apierr.Write(w, http.StatusBadRequest, err)
			return
		}
		sc.tasks[i] = t
		s.saveLocked(sc)
		sc.mu.Unlock()
		s.broadcastSchedule()
		v, _ := s.taskView(r.Context(), t.ID)
		apierr.WriteJSON(w, map[string]any{"task": v})
	})
	s.handle("DELETE /api/v1/assistant/tasks/{id}", s.deleteTaskRoute)
	s.handle("POST /api/v1/assistant/tasks/{id}/run", s.runTaskRoute)
	s.handle("POST /api/v1/assistant/tasks/{id}/fresh", func(w http.ResponseWriter, r *http.Request) {
		t, ok, err := s.forgetConversation(r.Context(), r.PathValue("id"))
		if err != nil {
			apierr.Write(w, http.StatusServiceUnavailable, err)
			return
		}
		if !ok {
			apierr.Write(w, http.StatusNotFound, errors.New("这个定时任务已经不在了"))
			return
		}
		apierr.WriteJSON(w, map[string]any{"task": t})
	})
	s.handle("GET /api/v1/assistant/runs", func(w http.ResponseWriter, r *http.Request) {
		sc, err := s.lockedSchedule(r.Context())
		if err != nil {
			apierr.Write(w, http.StatusServiceUnavailable, err)
			return
		}
		runs := make([]ScheduleRun, 0, len(sc.runs))
		for i := len(sc.runs) - 1; i >= 0; i-- { // newest first
			if ref := r.URL.Query().Get("ref"); ref == "" || sc.runs[i].RefID == ref {
				runs = append(runs, sc.runs[i])
			}
		}
		sc.mu.Unlock()
		apierr.WriteJSON(w, map[string]any{"runs": runs})
	})

	// The two lists app builds from before 2026-10-31 read and write (a task's JSON is a superset of both):
	// routines are the daily tasks; follow-ups the once and interval ones, by their next run.
	s.handle("GET /api/v1/assistant/routines", func(w http.ResponseWriter, r *http.Request) {
		if err := s.scheduleReadable(r.Context()); err != nil {
			apierr.Write(w, http.StatusServiceUnavailable, err)
			return
		}
		apierr.WriteJSON(w, map[string]any{"routines": s.tasksView(r.Context(), func(t Task) bool { return t.Repeat == repeatDaily })})
	})
	s.handle("PUT /api/v1/assistant/routines", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Routines []Task `json:"routines"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			apierr.Write(w, http.StatusBadRequest, err)
			return
		}
		now := time.Now()
		sc, err := s.lockedSchedule(r.Context())
		if err != nil {
			apierr.Write(w, http.StatusServiceUnavailable, err)
			return
		}
		var out []Task
		old := map[string]Task{}
		for _, t := range sc.tasks {
			if t.Repeat == repeatDaily {
				old[t.ID] = t
			} else {
				out = append(out, t)
			}
		}
		for _, rt := range in.Routines {
			prev, ok := old[rt.ID]
			if !ok {
				prev = Task{ID: "rt_" + randomHex(4), CreatedAt: now.UTC()}
			}
			rt.Repeat, rt.Launcher, rt.Model, rt.PermissionMode, rt.Effort, rt.SessionID = repeatDaily, prev.Launcher, prev.Model, prev.PermissionMode, prev.Effort, prev.SessionID
			t := editTask(prev, rt, now)
			if err := checkTask(&t, now); err != nil {
				sc.mu.Unlock()
				apierr.Write(w, http.StatusBadRequest, err)
				return
			}
			out = append(out, t)
		}
		sc.tasks = out
		s.saveLocked(sc)
		sc.mu.Unlock()
		s.broadcastSchedule()
		apierr.WriteJSON(w, map[string]any{"routines": s.tasksView(r.Context(), func(t Task) bool { return t.Repeat == repeatDaily })})
	})
	s.handle("POST /api/v1/assistant/routines/{id}/run", s.runTaskRoute)
	s.handle("GET /api/v1/assistant/followups", func(w http.ResponseWriter, r *http.Request) {
		if err := s.scheduleReadable(r.Context()); err != nil {
			apierr.Write(w, http.StatusServiceUnavailable, err)
			return
		}
		list := s.tasksView(r.Context(), func(t Task) bool { return t.Repeat != repeatDaily && t.Enabled })
		for i := range list {
			list[i].At = list[i].NextRun
		}
		sort.Slice(list, func(i, j int) bool { return list[i].At.Before(list[j].At) })
		apierr.WriteJSON(w, map[string]any{"followups": list})
	})
	s.handle("POST /api/v1/assistant/followups/{id}/run", s.runTaskRoute)
	s.handle("DELETE /api/v1/assistant/followups/{id}", s.deleteTaskRoute)
}

func (s *Service) deleteTaskRoute(w http.ResponseWriter, r *http.Request) {
	ok, err := s.deleteTask(r.Context(), r.PathValue("id"))
	if err != nil {
		apierr.Write(w, http.StatusServiceUnavailable, err)
		return
	}
	if !ok {
		apierr.Write(w, http.StatusNotFound, errors.New("这个定时任务已经不在了"))
		return
	}
	apierr.WriteJSON(w, map[string]any{"ok": true})
}

// runTaskRoute is 立即运行: the task runs now; a one-off is then done, a repeating one keeps its schedule.
func (s *Service) runTaskRoute(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	sc, err := s.lockedSchedule(r.Context())
	if err != nil {
		apierr.Write(w, http.StatusServiceUnavailable, err)
		return
	}
	i := sc.indexLocked(r.PathValue("id"))
	var t Task
	if i >= 0 {
		t = sc.tasks[i]
		if t.Repeat == repeatOnce {
			sc.tasks[i].Enabled, sc.tasks[i].LastRun = false, now.UTC()
			s.saveLocked(sc)
		}
	}
	sc.mu.Unlock()
	if i < 0 {
		apierr.Write(w, http.StatusNotFound, errors.New("这个定时任务已经不在了"))
		return
	}
	s.broadcastSchedule()
	sess, err := s.runTask(t, now, now, true, false)
	if errors.Is(err, errTaskBusy) {
		apierr.Write(w, http.StatusConflict, errors.New("上一轮还在运行，等它跑完再试"))
		return
	}
	if err != nil {
		apierr.Call(w, err)
		return
	}
	apierr.WriteJSON(w, map[string]any{"session_id": sess.SessionID, "machine_id": sess.MachineID})
}
