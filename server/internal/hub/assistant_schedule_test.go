package hub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
)

func TestNextRun(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	weekdays := Task{Repeat: repeatDaily, Time: "09:00", Weekdays: []int{1, 2, 3, 4, 5}, Enabled: true}
	fri := time.Date(2026, 10, 30, 10, 0, 0, 0, loc) // Friday after the run
	if got := nextRun(weekdays, fri, loc); !got.Equal(time.Date(2026, 11, 2, 9, 0, 0, 0, loc)) {
		t.Fatalf("after Friday 10:00 the next weekday run is Monday 09:00, got %v", got)
	}
	before := time.Date(2026, 10, 26, 8, 59, 0, 0, loc)
	if got := nextRun(weekdays, before, loc); !got.Equal(time.Date(2026, 10, 26, 9, 0, 0, 0, loc)) {
		t.Fatalf("same day, got %v", got)
	}
	every := Task{Repeat: repeatDaily, Time: "21:30"}
	if got := nextRun(every, time.Date(2026, 10, 31, 22, 0, 0, 0, loc), loc); !got.Equal(time.Date(2026, 11, 1, 21, 30, 0, 0, loc)) {
		t.Fatalf("every day, got %v", got)
	}
	if got := nextRun(Task{Time: "9am"}, before, loc); !got.IsZero() {
		t.Fatalf("bad time should never run, got %v", got)
	}
	at := before.Add(time.Hour)
	if got := (Task{Repeat: repeatInterval, At: at, Enabled: true}).next(before, loc); !got.Equal(at) {
		t.Fatalf("an interval task's next run is its At, got %v", got)
	}
	if got := (Task{Repeat: repeatOnce, At: at}).next(before, loc); !got.IsZero() {
		t.Fatalf("a task that is off has no next run, got %v", got)
	}
}

func TestParseWhen(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Date(2026, 10, 26, 16, 0, 0, 0, loc)
	cases := map[string]time.Time{
		"30m":                       now.Add(30 * time.Minute),
		"2h":                        now.Add(2 * time.Hour),
		"1d":                        now.Add(24 * time.Hour),
		"2026-10-27T09:50:00+08:00": time.Date(2026, 10, 27, 9, 50, 0, 0, loc),
		"2026-10-27 09:50":          time.Date(2026, 10, 27, 9, 50, 0, 0, loc),
	}
	for in, want := range cases {
		got, err := parseWhen(in, now, loc)
		if err != nil || !got.Equal(want) {
			t.Fatalf("%q: got %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := parseWhen("tomorrow", now, loc); err == nil {
		t.Fatal("expected an error")
	}
}

func setTasks(h *Hub, tasks ...Task) *schedule {
	s := h.loadSchedule(context.Background())
	s.mu.Lock()
	s.tasks = tasks
	s.mu.Unlock()
	return s
}

func taskByID(h *Hub, id string) (Task, bool) {
	s := h.loadSchedule(context.Background())
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.indexLocked(id); i >= 0 {
		return s.tasks[i], true
	}
	return Task{}, false
}

func TestDueTasksRunOnceAndStaleDailyOnesAreSkipped(t *testing.T) {
	h := newTestHub(t)
	h.userTZ = "Asia/Shanghai"
	loc, _ := time.LoadLocation("Asia/Shanghai")
	at := func(hh, mm int) time.Time { return time.Date(2026, 10, 26, hh, mm, 0, 0, loc) }
	s := setTasks(h,
		Task{ID: "r", Title: "早报", Prompt: "x", Repeat: repeatDaily, Time: "09:00", Enabled: true, LastRun: at(9, 0).Add(-24 * time.Hour)},
		Task{ID: "f1", Repeat: repeatOnce, At: at(9, 0).Add(30 * time.Second), Prompt: "y", Enabled: true},
		Task{ID: "f2", Repeat: repeatOnce, At: at(12, 0), Prompt: "z", Enabled: true},
		Task{ID: "p", Title: "巡检", Repeat: repeatInterval, EveryMinutes: 30, At: at(8, 50), Prompt: "w", Enabled: true})

	// 09:01: the daily task, the first one-off and the patrol are due (no machine, so the runs just log and give up).
	h.runDue(context.Background(), at(9, 1))
	h.sched.inflight.Wait() // the runs it started give up (no machine) before we look
	if r, _ := taskByID(h, "r"); !r.LastRun.Equal(at(9, 1)) {
		t.Fatalf("the daily task should have run at 09:01, last = %v", r.LastRun)
	}
	if f, _ := taskByID(h, "f1"); f.Enabled || !f.done() {
		t.Fatalf("a one-off that ran is done: %+v", f)
	}
	if f, _ := taskByID(h, "f2"); !f.Enabled {
		t.Fatal("f2 is still to come")
	}
	if p, _ := taskByID(h, "p"); !p.Enabled || !p.At.Equal(at(9, 20)) {
		t.Fatalf("a little late, the patrol runs now and stays on its half hours: %+v", p)
	}
	// Down for longer than an interval: once now, then every 30 minutes from here.
	s.mu.Lock()
	s.tasks[3].At = at(7, 0)
	s.mu.Unlock()
	h.runDue(context.Background(), at(9, 2))
	h.sched.inflight.Wait() // the runs it started give up (no machine) before we look
	if p, _ := taskByID(h, "p"); !p.At.Equal(at(9, 32)) {
		t.Fatalf("after a long outage: %+v", p)
	}
	if len(h.tasksView(context.Background(), nil)) != 3 {
		t.Fatal("the app doesn't list a one-off that already ran")
	}
	// A hub that was down all morning does not send the 早报 at noon.
	s.mu.Lock()
	s.tasks[0].LastRun = at(9, 0).Add(-24 * time.Hour)
	s.mu.Unlock()
	h.runDue(context.Background(), at(11, 0))
	h.sched.inflight.Wait() // the runs it started give up (no machine) before we look
	if r, _ := taskByID(h, "r"); !r.LastRun.Equal(at(11, 0)) || r.LastStatus != runSkipped {
		t.Fatalf("a stale run is skipped but recorded: %+v", r)
	}
	// A day later the one-off that ran is gone.
	h.runDue(context.Background(), at(9, 2).Add(25*time.Hour))
	h.sched.inflight.Wait() // the runs it started give up (no machine) before we look
	if _, ok := taskByID(h, "f1"); ok {
		t.Fatal("a one-off that ran a day ago is only history")
	}
}

// A run that ends after a newer run or skip of its task started is history only: the slow 09:01 run (an assistant
// that takes a while to give up) records its failure after the 11:00 skip, and the task still says skipped (#556).
func TestALateRunDoesNotOverwriteANewerStatus(t *testing.T) {
	h := newTestHub(t)
	h.userTZ = "Asia/Shanghai"
	loc, _ := time.LoadLocation("Asia/Shanghai")
	at := func(hh, mm int) time.Time { return time.Date(2026, 10, 26, hh, mm, 0, 0, loc) }
	setTasks(h, Task{ID: "r", Title: "早报", Prompt: "x", Repeat: repeatDaily, Time: "09:00", Enabled: true,
		LastRun: at(11, 0).UTC(), LastStatus: runSkipped, LastError: "Jarvis 停机，错过了时间"})

	h.recordRun(ScheduleRun{Kind: "routine", RefID: "r", Status: runFailed, Error: "assistant machine is not configured", of: at(9, 1).UTC()})
	r, _ := taskByID(h, "r")
	if r.LastStatus != runSkipped || r.LastError != "Jarvis 停机，错过了时间" {
		t.Fatalf("the late run overwrote the newer status: %+v", r)
	}
	s := h.loadSchedule(context.Background())
	s.mu.Lock()
	n := len(s.runs)
	s.mu.Unlock()
	if n != 1 {
		t.Fatalf("the late run is not in the history: %d runs", n)
	}
	// The run of the current LastRun, and 立即运行 (started after it), do set it.
	h.recordRun(ScheduleRun{Kind: "routine", RefID: "r", Status: runOK, SessionID: "s2", of: at(11, 0).UTC()})
	if r, _ := taskByID(h, "r"); r.LastStatus != runOK || r.LastSessionID != "s2" {
		t.Fatalf("the latest run's status: %+v", r)
	}
	h.recordRun(ScheduleRun{Kind: "routine", RefID: "r", Status: runFailed, Error: "x", Manual: true, of: at(11, 30).UTC()})
	if r, _ := taskByID(h, "r"); r.LastStatus != runFailed {
		t.Fatalf("立即运行 after it: %+v", r)
	}
}

// The 30-minute patrol from 2026-10-31: each run ended with schedule_followup(new_conversation, at=30m, the same
// prompt), so it was a new one-off every time, gone from the app while a run was stuck. Now its first re-schedule
// turns it into one task every 30 minutes, with the launcher and mode the user picked, and later ones change nothing.
func TestARunReschedulingItselfKeepsOneTask(t *testing.T) {
	h := newTestHub(t)
	h.userTZ = "Asia/Shanghai"
	m := &Machine{cfg: MachineConfig{ID: "server"}}
	prompt := "按手册巡检一次，最后用 schedule_followup（new_conversation=true，at=30m，prompt 和本条相同）安排下一次。"
	setTasks(h, Task{ID: "fu_1", Title: "30 分钟巡检", Prompt: prompt, Repeat: repeatOnce, At: time.Now().Add(-time.Minute),
		LastRun: time.Now(), MachineID: "server", PermissionMode: "acceptEdits", Launcher: "claude-fast"})
	h.recordRun(ScheduleRun{Kind: "followup", RefID: "fu_1", Status: runOK, MachineID: "server", SessionID: "run-1"})

	copied := "⏰ 30 分钟巡检（你之前安排的跟进）\n\n" + prompt // the run copies its whole message
	args, _ := json.Marshal(map[string]any{"at": "30m", "prompt": copied, "title": "30 分钟巡检", "new_conversation": true})
	out, err := h.toolScheduleFollowup(context.Background(), m, "run-1", args)
	if err != nil || !strings.Contains(out, "every 30 分钟") {
		t.Fatalf("schedule: %q, %v", out, err)
	}
	tasks := h.tasksView(context.Background(), nil)
	if len(tasks) != 1 {
		t.Fatalf("still one task, got %+v", tasks)
	}
	p := tasks[0]
	if p.ID != "fu_1" || p.Repeat != repeatInterval || p.EveryMinutes != 30 || !p.Enabled || p.Prompt != prompt ||
		p.PermissionMode != "acceptEdits" || p.Launcher != "" {
		t.Fatalf("one repeating task with the user's settings and the prompt without the ⏰ line: %+v", p)
	}
	if got := taskPrompt(p, p.At, p.At, time.UTC, false, false, false); !strings.Contains(got, "不用再用 schedule_followup") ||
		strings.Count(got, "⏰") != 1 {
		t.Fatalf("a repeating run is told it repeats by itself: %q", got)
	}

	// The next run still asks for "the next one": nothing new, the schedule stays.
	h.recordRun(ScheduleRun{Kind: "routine", RefID: "fu_1", Status: runOK, MachineID: "server", SessionID: "run-2"})
	next := p.At
	if out, err = h.toolScheduleFollowup(context.Background(), m, "run-2", args); err != nil || !strings.Contains(out, "already repeats") {
		t.Fatalf("again: %q, %v", out, err)
	}
	if tasks = h.tasksView(context.Background(), nil); len(tasks) != 1 || !tasks[0].At.Equal(next) {
		t.Fatalf("no second task, same next run: %+v", tasks)
	}
	// Turned off in the app: a run doesn't turn it back on.
	s := h.loadSchedule(context.Background())
	s.mu.Lock()
	s.tasks[0].Enabled = false
	s.mu.Unlock()
	if out, _ = h.toolScheduleFollowup(context.Background(), m, "run-2", args); !strings.Contains(out, "turned off") {
		t.Fatalf("off: %q", out)
	}
	if p, _ := taskByID(h, "fu_1"); p.Enabled {
		t.Fatal("stays off")
	}
	// Another conversation, or another follow-up from this one, is a new task.
	other, _ := json.Marshal(map[string]any{"at": "2h", "prompt": "看看 CI", "title": "看 CI"})
	if _, err := h.toolScheduleFollowup(context.Background(), m, "run-2", other); err != nil {
		t.Fatal(err)
	}
	if n := len(h.tasksView(context.Background(), nil)); n != 2 {
		t.Fatalf("a different follow-up is its own task, have %d", n)
	}
}

func TestScheduleFollowupEvery(t *testing.T) {
	h := newTestHub(t)
	m := &Machine{cfg: MachineConfig{ID: "server"}}
	setTasks(h)
	out, err := h.toolScheduleFollowup(context.Background(), m, "s1", json.RawMessage(`{"every":"2h","prompt":"看看部署","title":"部署巡检"}`))
	if err != nil || !strings.Contains(out, "every 2 小时") {
		t.Fatalf("%q, %v", out, err)
	}
	tasks := h.tasksView(context.Background(), nil)
	if len(tasks) != 1 || tasks[0].Repeat != repeatInterval || tasks[0].EveryMinutes != 120 || tasks[0].SessionID != "s1" ||
		tasks[0].CreatedBy != "assistant" || tasks[0].PermissionMode != "" {
		t.Fatalf("%+v", tasks)
	}
	if time.Until(tasks[0].At) < 119*time.Minute {
		t.Fatalf("the first run is one interval away: %v", tasks[0].At)
	}
	if _, err := h.toolScheduleFollowup(context.Background(), m, "s1", json.RawMessage(`{"every":"1m","prompt":"x"}`)); err == nil {
		t.Fatal("more often than every 5 minutes is refused")
	}
}

func TestCheckTask(t *testing.T) {
	now := time.Now()
	ok := []Task{
		{Repeat: repeatDaily, Title: "早报", Prompt: "x", Time: "09:00", Weekdays: []int{5, 1, 1}},
		{Repeat: repeatInterval, Prompt: "x", EveryMinutes: 30, PermissionMode: "bypassPermissions", Effort: "high", Launcher: "claude-fast"},
		{Repeat: repeatOnce, Prompt: "x", At: now.Add(time.Hour), Enabled: true, PermissionMode: "acceptEdits"},
		{Repeat: repeatOnce, Prompt: "x", At: now.Add(-time.Hour)}, // off: a past time is fine
	}
	for _, task := range ok {
		if err := checkTask(&task, now); err != nil {
			t.Fatalf("%+v: %v", task, err)
		}
	}
	d := ok[0]
	_ = checkTask(&d, now)
	if len(d.Weekdays) != 2 || d.Weekdays[0] != 1 {
		t.Fatalf("weekdays tidied: %v", d.Weekdays)
	}
	bad := []Task{
		{Repeat: repeatDaily, Title: "x", Prompt: "x", Time: "9am"},
		{Repeat: repeatDaily, Title: "x", Prompt: "x", Time: "09:00", Weekdays: []int{0}},
		{Repeat: repeatInterval, Prompt: "x", EveryMinutes: 2},
		{Repeat: repeatOnce, Prompt: "x", At: now.Add(-time.Hour), Enabled: true},
		{Repeat: repeatOnce, Prompt: "x", At: now.Add(time.Hour), PermissionMode: "yolo"},
		{Repeat: repeatOnce, Prompt: "x", At: now.Add(time.Hour), Effort: "huge"},
		{Repeat: repeatOnce, Prompt: "x", At: now.Add(time.Hour), Launcher: "rm -rf"},
		{Repeat: "weekly", Prompt: "x"},
		{Repeat: repeatOnce, At: now.Add(time.Hour)},
	}
	for _, task := range bad {
		if err := checkTask(&task, now); err == nil {
			t.Fatalf("%+v should be refused", task)
		}
	}
}

func TestEditTask(t *testing.T) {
	now := time.Now()
	old := Task{ID: "p", Repeat: repeatInterval, EveryMinutes: 30, At: now.Add(10 * time.Minute), Enabled: true, SessionID: "s1",
		MachineID: "server", LastStatus: runOK, CreatedBy: "assistant"}
	in := old
	in.LastStatus, in.CreatedBy, in.SessionID, in.MachineID = "", "", "s-other", "elsewhere"
	in.PermissionMode = "bypassPermissions"
	got := editTask(old, in, now)
	if got.LastStatus != runOK || got.CreatedBy != "assistant" || got.SessionID != "s1" || got.MachineID != "server" ||
		got.PermissionMode != "bypassPermissions" || !got.At.Equal(old.At) {
		t.Fatalf("the hub's fields stay, the conversation can't be pointed elsewhere, the next run stays: %+v", got)
	}
	in.EveryMinutes, in.SessionID = 60, ""
	got = editTask(old, in, now)
	if got.SessionID != "" || !got.At.Equal(now.Add(time.Hour)) {
		t.Fatalf("a new interval starts from now; clearing the conversation means a new one each run: %+v", got)
	}
}

func TestTaskRoutes(t *testing.T) {
	h := newTestHub(t)
	setTasks(h, Task{ID: "morning", Title: "早报", Prompt: "x", Repeat: repeatDaily, Time: "09:00", Enabled: true},
		Task{ID: "fu_1", Title: "巡检", Prompt: "y", Repeat: repeatInterval, EveryMinutes: 30, At: time.Now().Add(time.Minute), Enabled: true,
			MachineID: "server"})
	mux := http.NewServeMux()
	h.scheduleRoutes(mux)
	do := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rec
	}
	rec := do("GET", "/api/v1/assistant/tasks", "")
	if !strings.Contains(rec.Body.String(), `"repeat":"interval"`) || !strings.Contains(rec.Body.String(), `"next_run"`) {
		t.Fatalf("list: %s", rec.Body)
	}
	rec = do("PUT", "/api/v1/assistant/tasks/fu_1", `{"title":"巡检","prompt":"y","repeat":"interval","every_minutes":30,"enabled":true,"permission_mode":"acceptEdits","effort":"low","launcher":"claude-fast"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"permission_mode":"acceptEdits"`) {
		t.Fatalf("put: %d %s", rec.Code, rec.Body)
	}
	if rec = do("PUT", "/api/v1/assistant/tasks/fu_1", `{"title":"巡检","prompt":"y","repeat":"interval","every_minutes":30,"permission_mode":"yolo"}`); rec.Code != 400 {
		t.Fatalf("an unknown mode is refused: %d", rec.Code)
	}
	rec = do("POST", "/api/v1/assistant/tasks", `{"title":"喝水","prompt":"提醒我喝水","repeat":"once","at":"2099-01-01T09:00:00Z","enabled":true,"session_id":"s9"}`)
	var created struct{ Task Task }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if rec.Code != 200 || created.Task.ID == "" || created.Task.SessionID != "" || created.Task.MachineID == "" && h.assistantConfig(context.Background()).MachineID != "" {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	// Builds from before keep working: routines are the daily tasks, follow-ups the rest.
	rec = do("GET", "/api/v1/assistant/routines", "")
	if strings.Contains(rec.Body.String(), "fu_1") || !strings.Contains(rec.Body.String(), "morning") {
		t.Fatalf("routines: %s", rec.Body)
	}
	rec = do("GET", "/api/v1/assistant/followups", "")
	if !strings.Contains(rec.Body.String(), "fu_1") || !strings.Contains(rec.Body.String(), created.Task.ID) || strings.Contains(rec.Body.String(), "morning") {
		t.Fatalf("follow-ups: %s", rec.Body)
	}
	rec = do("PUT", "/api/v1/assistant/routines", `{"routines":[{"id":"morning","title":"早报","prompt":"x2","time":"08:30","enabled":true}]}`)
	if rec.Code != 200 {
		t.Fatalf("put routines: %d %s", rec.Code, rec.Body)
	}
	if n := len(h.tasksView(context.Background(), nil)); n != 3 {
		t.Fatalf("saving the routines leaves the other tasks alone, have %d", n)
	}
	if p, _ := taskByID(h, "fu_1"); p.PermissionMode != "acceptEdits" {
		t.Fatal("and their settings")
	}
	if rec = do("DELETE", "/api/v1/assistant/followups/fu_1", ""); rec.Code != 200 {
		t.Fatalf("delete: %d", rec.Code)
	}
	if rec = do("DELETE", "/api/v1/assistant/tasks/fu_1", ""); rec.Code != 404 {
		t.Fatalf("delete twice: %d", rec.Code)
	}
}

func TestScheduleRoutineTool(t *testing.T) {
	h := newTestHub(t)
	h.userTZ = "Asia/Shanghai"
	out, err := h.toolScheduleRoutine(context.Background(), json.RawMessage(`{"title":"周报","prompt":"起草周报","time":"17:00","weekdays":[5]}`))
	if err != nil || !strings.Contains(out, "周五 17:00") {
		t.Fatalf("create: %q, %v", out, err)
	}
	s := h.loadSchedule(context.Background())
	r := s.tasks[len(s.tasks)-1]
	if r.CreatedBy != "assistant" || !r.Enabled || r.Title != "周报" || r.Repeat != repeatDaily {
		t.Fatalf("routine %+v", r)
	}
	if _, err := h.toolScheduleRoutine(context.Background(), json.RawMessage(`{"id":"`+r.ID+`","enabled":false,"weekdays":[1,2,3,4,5,6,7]}`)); err != nil {
		t.Fatal(err)
	}
	if r2 := s.tasks[len(s.tasks)-1]; r2.Enabled || r2.Weekdays != nil || r2.Prompt != "起草周报" {
		t.Fatalf("an update changes only the fields given: %+v", r2)
	}
	if _, err := h.toolScheduleRoutine(context.Background(), json.RawMessage(`{"title":"x","prompt":"y","time":"9am"}`)); err == nil {
		t.Fatal("a bad time is refused")
	}
	if _, err := h.toolScheduleRoutine(context.Background(), json.RawMessage(`{"title":"x","prompt":"y","time":"09:00","weekdays":[0]}`)); err == nil {
		t.Fatal("weekday 0 is refused")
	}
	if !strings.Contains(h.describeSchedule(context.Background()), r.ID) {
		t.Fatal("list_followups shows task ids")
	}
	if ok, err := h.deleteTask(context.Background(), r.ID); !ok || err != nil {
		t.Fatalf("delete: %v, %v", ok, err)
	}
	if ok, _ := h.deleteTask(context.Background(), r.ID); ok {
		t.Fatal("delete once")
	}
}

func TestStripScheduleHeader(t *testing.T) {
	in := "⏰ 30 分钟巡检（你之前安排的跟进）\n（本该 10-31 11:00 运行，Jarvis 当时没在运行，晚了 25 分钟；看看还有没有必要按原计划做。）\n\n" +
		"⏰ 30 分钟巡检（定时任务，每 30 分钟）\n（Jarvis 会按时再运行它，不用再用 schedule_followup 安排下一次。）\n\n跑 detect.sh"
	if got := stripScheduleHeader(in); got != "跑 detect.sh" {
		t.Fatalf("%q", got)
	}
	if got := stripScheduleHeader("⏰ 提醒我喝水"); got != "⏰ 提醒我喝水" {
		t.Fatalf("a prompt of the user's own is kept: %q", got)
	}
}

func scheduleStore(t *testing.T) *db.DB {
	t.Helper()
	url := os.Getenv("JARVIS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("JARVIS_TEST_DATABASE_URL not set (scripts/test/test-server.sh runs this on the hub machine)")
	}
	database, err := db.Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	for _, k := range []string{tasksKey, routinesKey, followupsKey, runsKey} {
		if err := database.PutSetting(context.Background(), k, nil); err != nil {
			t.Fatal(err)
		}
	}
	return database
}

func waitRuns(t *testing.T, h *Hub, n int) []ScheduleRun {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s := h.loadSchedule(context.Background())
		s.mu.Lock()
		runs := append([]ScheduleRun(nil), s.runs...)
		s.mu.Unlock()
		if len(runs) >= n {
			return runs
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d runs, have %+v", n, runs)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The first hub with one task list moves the routines and follow-ups over: ids, times and last runs kept, the
// follow-up still runs once, the old list is emptied, and a second boot doesn't move anything again.
func TestTasksMovedFromTheOldLists(t *testing.T) {
	store := scheduleStore(t)
	ctx := context.Background()
	loc, _ := time.LoadLocation("Asia/Shanghai")
	due := time.Now().Add(-time.Minute).Truncate(time.Second)
	if err := store.PutSetting(ctx, routinesKey, []Routine{{ID: "morning", Title: "早报", Prompt: "x", Time: "09:00", Enabled: true,
		LastStatus: runOK}}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSetting(ctx, followupsKey, []Followup{{ID: "fu_p", At: due, Title: "30 分钟巡检",
		Prompt: "⏰ 30 分钟巡检（你之前安排的跟进）\n\n巡检", MachineID: "server"}}); err != nil {
		t.Fatal(err)
	}
	boot := func() *Hub {
		h := newTestHub(t)
		h.store = store
		h.userTZ = loc.String()
		return h
	}
	h1 := boot()
	tasks := h1.tasksView(ctx, nil)
	if len(tasks) != 2 || tasks[0].ID != "morning" || tasks[0].Repeat != repeatDaily || tasks[0].LastStatus != runOK ||
		tasks[1].ID != "fu_p" || tasks[1].Repeat != repeatOnce || !tasks[1].At.Equal(due) || tasks[1].Prompt != "巡检" || !tasks[1].Enabled {
		t.Fatalf("moved: %+v", tasks)
	}
	var old []Followup
	if _, err := store.Setting(ctx, followupsKey, &old); err != nil || len(old) != 0 {
		t.Fatalf("the old follow-ups are emptied: %+v, %v", old, err)
	}
	h1.runDue(ctx, time.Now())
	waitRuns(t, h1, 1)

	h2 := boot()
	h2.runDue(ctx, time.Now())
	if runs := h2.loadSchedule(ctx).runs; len(runs) != 1 || runs[0].RefID != "fu_p" {
		t.Fatalf("after a restart the patrol doesn't run again: %+v", runs)
	}
	if n := len(h2.tasksView(ctx, nil)); n != 1 {
		t.Fatalf("nothing moved twice, the one-off that ran is hidden: %d", n)
	}
}

// The schedule lives in Postgres: after a restart nothing runs twice, a follow-up missed while the hub was down runs
// once (late), a routine missed by long is skipped, and the history keeps all of it.
func TestScheduleAcrossHubRestart(t *testing.T) {
	store := scheduleStore(t)
	loc, _ := time.LoadLocation("Asia/Shanghai")
	at := func(h, m int) time.Time { return time.Date(2026, 10, 26, h, m, 0, 0, loc) }
	boot := func() *Hub {
		h := newTestHub(t)
		h.store = store
		h.userTZ = "Asia/Shanghai"
		return h
	}
	h1 := boot()
	s := h1.loadSchedule(context.Background())
	s.mu.Lock()
	s.tasks = []Task{{ID: "r9", Title: "早报", Prompt: "x", Repeat: repeatDaily, Time: "09:00", Enabled: true, LastRun: at(9, 0).Add(-24 * time.Hour)},
		{ID: "r11", Title: "午报", Prompt: "x", Repeat: repeatDaily, Time: "11:00", Enabled: true, LastRun: at(11, 0).Add(-24 * time.Hour)},
		{ID: "f1", Repeat: repeatOnce, At: at(9, 0), Prompt: "y", Enabled: true},
		{ID: "f2", Repeat: repeatOnce, At: at(10, 0), Title: "看部署", Prompt: "z", Enabled: true}}
	h1.saveLocked(s)
	s.mu.Unlock()

	h1.runDue(context.Background(), at(9, 0).Add(2*time.Second))
	runs := waitRuns(t, h1, 2) // no assistant machine in tests: both runs fail to start, and say so
	for _, r := range runs {
		if r.Status != runFailed || r.Error == "" || !r.Due.Equal(at(9, 0)) {
			t.Fatalf("run %+v", r)
		}
	}

	// The hub restarts at 09:01 and stays down until 11:45.
	h2 := boot()
	h2.runDue(context.Background(), at(9, 1))
	if n := len(h2.loadSchedule(context.Background()).runs); n != 2 {
		t.Fatalf("after the restart nothing is due again, have %d runs", n)
	}
	h2.runDue(context.Background(), at(11, 45))
	runs = waitRuns(t, h2, 4)
	byRef := map[string]ScheduleRun{}
	for _, r := range runs[2:] {
		byRef[r.RefID] = r
	}
	if r := byRef["r11"]; r.Status != runSkipped || !r.Due.Equal(at(11, 0)) {
		t.Fatalf("the 11:00 routine missed by 45 minutes is skipped: %+v", r)
	}
	if r := byRef["f2"]; r.Kind != "followup" || !r.Due.Equal(at(10, 0)) {
		t.Fatalf("the missed follow-up still runs: %+v", r)
	}
	late := Task{Repeat: repeatOnce, Title: "看部署", Prompt: "z"}
	if p := taskPrompt(late, at(10, 0), at(11, 45), loc, false, false, false); !strings.Contains(p, "晚了 1 小时") || !strings.Contains(p, "10-26 10:00") {
		t.Fatalf("a late follow-up is told so: %q", p)
	}
	if p := taskPrompt(late, at(10, 0), at(10, 0).Add(30*time.Second), loc, false, false, false); strings.Contains(p, "晚了") {
		t.Fatalf("an on-time follow-up is not: %q", p)
	}

	// A third boot sees the same history and the tasks' last status.
	h3 := boot()
	s3 := h3.loadSchedule(context.Background())
	if len(s3.runs) != 4 {
		t.Fatalf("reloaded %d runs", len(s3.runs))
	}
	for _, r := range s3.tasks {
		if r.LastStatus == "" {
			t.Fatalf("task %s has no last status", r.ID)
		}
	}
	if next := h3.nextDue(context.Background(), at(11, 46)); !next.Equal(at(9, 0).Add(24 * time.Hour)) {
		t.Fatalf("next due %v, want tomorrow 09:00", next)
	}
}

// flakyScheduleStore keeps settings in memory; reads fail while failRead is set.
type flakyScheduleStore struct {
	Store
	mu       sync.Mutex
	failRead bool
	data     map[string][]byte
	puts     int
}

func (s *flakyScheduleStore) Setting(_ context.Context, key string, out any) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failRead {
		return false, errors.New("database unavailable")
	}
	b, ok := s.data[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(b, out)
}

func (s *flakyScheduleStore) PutSetting(_ context.Context, key string, v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.data[key] = b
	s.puts++
	return nil
}

// A failed read used to mark the schedule loaded with the two default routines, and the next save wrote them over
// the stored routines and follow-ups (found in review, 2026-11-02). Now nothing is written, and nothing is changed in
// memory either, until a read succeeds: the tools and the app get an error, the scheduler skips its turn.
func TestScheduleReadFailureNeverOverwrites(t *testing.T) {
	ctx := context.Background()
	store := &flakyScheduleStore{data: map[string][]byte{}}
	// Lists from before tasks were one: the first successful read moves them over (and writes).
	_ = store.PutSetting(ctx, routinesKey, []Routine{{ID: "rt_mine", Title: "我的", Prompt: "p", Time: "08:00", Enabled: true}})
	_ = store.PutSetting(ctx, followupsKey, []Followup{{ID: "fu_mine", At: time.Now().Add(time.Hour).UTC(), Prompt: "q"}})
	before := map[string]string{}
	for k, v := range store.data {
		before[k] = string(v)
	}
	store.puts, store.failRead = 0, true
	h := newTestHub(t)
	h.store = store
	m := &Machine{cfg: MachineConfig{ID: "server"}}

	if _, err := h.toolScheduleRoutine(ctx, json.RawMessage(`{"title":"x","prompt":"y","time":"09:00"}`)); !errors.Is(err, errScheduleUnavailable) {
		t.Fatalf("schedule_routine while unreadable: %v", err)
	}
	if _, err := h.toolScheduleFollowup(ctx, m, "s1", json.RawMessage(`{"at":"30m","prompt":"y"}`)); !errors.Is(err, errScheduleUnavailable) {
		t.Fatalf("schedule_followup while unreadable: %v", err)
	}
	if _, err := h.deleteTask(ctx, "rt_mine"); !errors.Is(err, errScheduleUnavailable) {
		t.Fatalf("delete while unreadable: %v", err)
	}
	if got := h.describeSchedule(ctx); got != errScheduleUnavailable.Error() {
		t.Fatalf("list_followups while unreadable: %q", got)
	}
	h.recordRun(ScheduleRun{Kind: "routine", RefID: "rt_mine", Status: "ok", At: time.Now().UTC()})
	h.runDue(ctx, time.Now())
	if next := h.nextDue(ctx, time.Now()); !next.IsZero() {
		t.Fatalf("next due while unreadable: %v", next)
	}
	mux := http.NewServeMux()
	h.scheduleRoutes(mux)
	for _, req := range []struct{ method, path, body string }{
		{"GET", "/api/v1/assistant/tasks", ""},
		{"GET", "/api/v1/assistant/routines", ""},
		{"GET", "/api/v1/assistant/followups", ""},
		{"GET", "/api/v1/assistant/runs", ""},
		{"POST", "/api/v1/assistant/tasks", `{"title":"喝水","prompt":"提醒我喝水","repeat":"once","at":"2099-01-01T09:00:00Z","enabled":true}`},
		{"PUT", "/api/v1/assistant/tasks/rt_mine", `{"title":"我的","prompt":"p","repeat":"daily","time":"08:00","enabled":false}`},
		{"PUT", "/api/v1/assistant/routines", `{"routines":[]}`},
		{"POST", "/api/v1/assistant/tasks/rt_mine/run", ""},
		{"POST", "/api/v1/assistant/tasks/rt_mine/fresh", ""},
		{"DELETE", "/api/v1/assistant/tasks/fu_mine", ""},
		{"DELETE", "/api/v1/assistant/followups/fu_mine", ""},
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(req.method, req.path, strings.NewReader(req.body)))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s while unreadable: %d %s", req.method, req.path, rec.Code, rec.Body)
		}
	}
	if store.puts != 0 {
		t.Fatalf("%d writes while the schedule could not be read", store.puts)
	}
	for k, v := range store.data {
		if before[k] != string(v) {
			t.Fatalf("%s changed while unreadable: %s", k, v)
		}
	}

	// Once the database answers, the stored lists come back (not the defaults) and changes are saved again.
	store.mu.Lock()
	store.failRead = false
	store.mu.Unlock()
	tasks := h.tasksView(ctx, nil)
	if len(tasks) != 2 || tasks[0].ID != "rt_mine" || tasks[1].ID != "fu_mine" {
		t.Fatalf("tasks after recovery: %+v", tasks)
	}
	puts := store.puts
	if ok, err := h.deleteTask(ctx, "fu_mine"); !ok || err != nil {
		t.Fatalf("delete after recovery: %v, %v", ok, err)
	}
	if store.puts == puts {
		t.Fatal("a change after recovery is saved")
	}
}

// With nothing stored yet (a new hub), the defaults are the schedule and saving them is fine.
func TestScheduleDefaultsWhenNothingStored(t *testing.T) {
	ctx := context.Background()
	h := newTestHub(t)
	h.store = &flakyScheduleStore{data: map[string][]byte{}}
	if err := h.scheduleReadable(ctx); err != nil {
		t.Fatal(err)
	}
	if tasks := h.tasksView(ctx, nil); len(tasks) != len(defaultRoutines()) {
		t.Fatalf("defaults: %+v", tasks)
	}
}
