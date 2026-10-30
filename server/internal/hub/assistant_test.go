package hub

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/db"
	"github.com/chyroc/jarvis/server/internal/lark"
	"github.com/chyroc/jarvis/server/internal/proto"
)

func TestNextRun(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	weekdays := Routine{Time: "09:00", Weekdays: []int{1, 2, 3, 4, 5}, Enabled: true}
	fri := time.Date(2026, 10, 30, 10, 0, 0, 0, loc) // Friday after the run
	if got := nextRun(weekdays, fri, loc); !got.Equal(time.Date(2026, 11, 2, 9, 0, 0, 0, loc)) {
		t.Fatalf("after Friday 10:00 the next weekday run is Monday 09:00, got %v", got)
	}
	before := time.Date(2026, 10, 26, 8, 59, 0, 0, loc)
	if got := nextRun(weekdays, before, loc); !got.Equal(time.Date(2026, 10, 26, 9, 0, 0, 0, loc)) {
		t.Fatalf("same day, got %v", got)
	}
	every := Routine{Time: "21:30"}
	if got := nextRun(every, time.Date(2026, 10, 31, 22, 0, 0, 0, loc), loc); !got.Equal(time.Date(2026, 11, 1, 21, 30, 0, 0, loc)) {
		t.Fatalf("every day, got %v", got)
	}
	if got := nextRun(Routine{Time: "9am"}, before, loc); !got.IsZero() {
		t.Fatalf("bad time should never run, got %v", got)
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

func TestRoutineRunsOnceAndSkipsStale(t *testing.T) {
	h := newTestHub(t)
	h.userTZ = "Asia/Shanghai"
	loc, _ := time.LoadLocation("Asia/Shanghai")
	s := h.loadSchedule(context.Background())
	s.mu.Lock()
	s.routines = []Routine{{ID: "r", Title: "早报", Prompt: "x", Time: "09:00", Enabled: true,
		LastRun: time.Date(2026, 10, 25, 9, 0, 0, 0, loc)}}
	s.followups = []Followup{{ID: "f1", At: time.Date(2026, 10, 26, 9, 0, 30, 0, loc), Prompt: "y"},
		{ID: "f2", At: time.Date(2026, 10, 26, 12, 0, 0, 0, loc), Prompt: "z"}}
	s.mu.Unlock()

	// 09:01: the routine and the first follow-up are due (no machine, so the runs just log and give up).
	h.runDue(context.Background(), time.Date(2026, 10, 26, 9, 1, 0, 0, loc))
	s.mu.Lock()
	if last := s.routines[0].LastRun; !last.Equal(time.Date(2026, 10, 26, 9, 1, 0, 0, loc)) {
		t.Fatalf("routine should have run at 09:01, last = %v", last)
	}
	if len(s.followups) != 1 || s.followups[0].ID != "f2" {
		t.Fatalf("only f2 should be left, got %+v", s.followups)
	}
	// A hub that was down all morning does not send the 早报 at noon.
	s.routines[0].LastRun = time.Date(2026, 10, 25, 9, 0, 0, 0, loc)
	s.mu.Unlock()
	h.runDue(context.Background(), time.Date(2026, 10, 26, 11, 0, 0, 0, loc))
	s.mu.Lock()
	defer s.mu.Unlock()
	if last := s.routines[0].LastRun; !last.Equal(time.Date(2026, 10, 26, 11, 0, 0, 0, loc)) {
		t.Fatalf("a stale run is skipped but recorded, last = %v", last)
	}
}

func TestAskUserReturnsAnswers(t *testing.T) {
	h := newTestHub(t)
	m := newMachine(h, MachineConfig{ID: "box", Name: "Box", Kind: "local"})
	h.machines, h.byID["box"] = []*Machine{m}, m
	args := json.RawMessage(`{"questions":[{"question":"发吗？","options":[{"label":"发"},{"label":"不发"}]}]}`)
	done := make(chan proto.ToolResult, 1)
	go func() { done <- h.callAssistantTool(context.Background(), m, "conv-1", "ask_user", args) }()
	for len(h.pendingInteractions()) == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	ix := h.pendingInteractions()[0]
	if ix.Kind != proto.IxQuestion || ix.SessionID != "conv-1" || ix.MachineID != "box" || len(ix.Questions) != 1 {
		t.Fatalf("unexpected interaction %+v", ix)
	}
	if err := h.resolve(ix.ID, proto.Resolution{Answers: map[string]string{"发吗？": "发"}}); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.IsError || !strings.Contains(textOf(r), "发吗？ → 发") {
		t.Fatalf("unexpected result %+v", r)
	}
}

func TestAgentsListAndFind(t *testing.T) {
	h := newTestHub(t)
	m := newMachine(h, MachineConfig{ID: "box", Name: "Box", Kind: "local"})
	h.machines, h.byID["box"] = []*Machine{m}, m
	now := time.Now().UTC()
	m.sessions = []proto.Session{
		{SessionID: "aaaaaaaa-1", Title: "修登录", Cwd: "/code/app", Status: proto.StatusWorking, UpdatedAt: now, LastText: "正在跑测试"},
		{SessionID: "bbbbbbbb-2", Title: "旧的", Cwd: "/code/old", Status: proto.StatusIdle, UpdatedAt: now.Add(-time.Hour)},
	}
	out := h.describeAgents("", "", "")
	if !strings.Contains(out, "修登录") || strings.Contains(out, "旧的") || !strings.Contains(out, "正在跑测试") {
		t.Fatalf("active list:\n%s", out)
	}
	if all := h.describeAgents("all", "", "old"); !strings.Contains(all, "旧的") || strings.Contains(all, "修登录") {
		t.Fatalf("query filter:\n%s", all)
	}
	if _, s, err := h.findAgent("", "aaaaaaaa"); err != nil || s.SessionID != "aaaaaaaa-1" {
		t.Fatalf("prefix lookup: %v %+v", err, s)
	}
	if _, _, err := h.findAgent("box", "cccccccc"); err == nil {
		t.Fatal("unknown id should fail")
	}
}

func TestAssistantToolsOnlyForAssistant(t *testing.T) {
	h := newTestHub(t)
	h.lark = lark.New(lark.Config{})
	for _, tool := range h.mcpTools(false) {
		if isAssistantTool(tool.Name) {
			t.Fatalf("%s leaked to ordinary sessions", tool.Name)
		}
	}
	names := map[string]bool{}
	for _, tool := range h.mcpTools(true) {
		names[tool.Name] = true
		var v any
		if err := json.Unmarshal(tool.InputSchema, &v); err != nil {
			t.Fatalf("%s schema: %v", tool.Name, err)
		}
	}
	for _, want := range []string{"ask_user", "notify_user", "schedule_followup", "agents_list", "agent_read", "lark_chats", "lark_search"} {
		if !names[want] {
			t.Fatalf("assistant is missing %s", want)
		}
	}
}

func TestCompactJSONPrunes(t *testing.T) {
	out := compactJSON(json.RawMessage(`{"a":{"avatarUrl":"x","name":"n","empty":"","no":false},"list":[{"thumbnail":"t","v":1}]}`), 1000)
	if strings.Contains(out, "avatar") || strings.Contains(out, "thumbnail") || strings.Contains(out, "empty") || !strings.Contains(out, `"name":"n"`) {
		t.Fatalf("got %s", out)
	}
}

func scheduleStore(t *testing.T) *db.DB {
	t.Helper()
	url := os.Getenv("JARVIS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("JARVIS_TEST_DATABASE_URL not set (scripts/test-server.sh runs this on the hub machine)")
	}
	database, err := db.Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	for _, k := range []string{routinesKey, followupsKey, runsKey} {
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
	s.routines = []Routine{{ID: "r9", Title: "早报", Prompt: "x", Time: "09:00", Enabled: true, LastRun: at(9, 0).Add(-24 * time.Hour)},
		{ID: "r11", Title: "午报", Prompt: "x", Time: "11:00", Enabled: true, LastRun: at(11, 0).Add(-24 * time.Hour)}}
	s.followups = []Followup{{ID: "f1", At: at(9, 0), Prompt: "y"}, {ID: "f2", At: at(10, 0), Title: "看部署", Prompt: "z"}}
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
	if p := followupPrompt(Followup{At: at(10, 0), Title: "看部署", Prompt: "z"}, at(11, 45), loc, false); !strings.Contains(p, "晚了 1 小时") ||
		!strings.Contains(p, "10-26 10:00") {
		t.Fatalf("a late follow-up is told so: %q", p)
	}
	if p := followupPrompt(Followup{At: at(10, 0), Prompt: "z"}, at(10, 0).Add(30*time.Second), loc, false); strings.Contains(p, "晚了") {
		t.Fatalf("an on-time follow-up is not: %q", p)
	}

	// A third boot sees the same history and the routines' last status.
	h3 := boot()
	s3 := h3.loadSchedule(context.Background())
	if len(s3.runs) != 4 || len(s3.followups) != 0 {
		t.Fatalf("reloaded %d runs, %d follow-ups", len(s3.runs), len(s3.followups))
	}
	for _, r := range s3.routines {
		if r.LastStatus == "" {
			t.Fatalf("routine %s has no last status", r.ID)
		}
	}
	if next := h3.nextDue(context.Background(), at(11, 46)); !next.Equal(at(9, 0).Add(24 * time.Hour)) {
		t.Fatalf("next due %v, want tomorrow 09:00", next)
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
	r := s.routines[len(s.routines)-1]
	if r.CreatedBy != "assistant" || !r.Enabled || r.Title != "周报" {
		t.Fatalf("routine %+v", r)
	}
	if _, err := h.toolScheduleRoutine(context.Background(), json.RawMessage(`{"id":"`+r.ID+`","enabled":false,"weekdays":[1,2,3,4,5,6,7]}`)); err != nil {
		t.Fatal(err)
	}
	if r2 := s.routines[len(s.routines)-1]; r2.Enabled || r2.Weekdays != nil || r2.Prompt != "起草周报" {
		t.Fatalf("an update changes only the fields given: %+v", r2)
	}
	if _, err := h.toolScheduleRoutine(context.Background(), json.RawMessage(`{"title":"x","prompt":"y","time":"9am"}`)); err == nil {
		t.Fatal("a bad time is refused")
	}
	if _, err := h.toolScheduleRoutine(context.Background(), json.RawMessage(`{"title":"x","prompt":"y","time":"09:00","weekdays":[0]}`)); err == nil {
		t.Fatal("weekday 0 is refused")
	}
	if !strings.Contains(h.describeSchedule(context.Background()), r.ID) {
		t.Fatal("list_followups shows routine ids")
	}
	if !h.deleteRoutine(context.Background(), r.ID) || h.deleteRoutine(context.Background(), r.ID) {
		t.Fatal("delete once")
	}
}
