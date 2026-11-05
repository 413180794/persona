package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// run is what runDue does for one due task: read it as it is now, then run it.
func runNow(t *testing.T, h *Service, id string) (proto.Session, error) {
	t.Helper()
	task, ok := taskByID(h, id)
	if !ok {
		t.Fatalf("no task %s", id)
	}
	now := time.Date(2026, 11, 2, 1, 0, 0, 0, time.UTC)
	return h.runTask(task, now, now, false, false)
}

// The user's ask (2026-11-01): 「同一个定时任务，用同一个 session 跑」. The first run starts the task's own
// conversation; later runs go into it, told the earlier runs are above instead of being given the background again.
func TestATaskRunsInItsOwnConversation(t *testing.T) {
	n := newFakeHub().withMachine()
	h := newStorelessService(t, n)
	setTasks(h, Task{ID: "r", Title: "早报", Prompt: "给我今天的早报", Repeat: repeatDaily, Time: "09:00", Enabled: true})

	first, err := runNow(t, h, "r")
	if err != nil {
		t.Fatal(err)
	}
	if p := n.lastSend(t); p["session_id"] != nil && p["session_id"] != "" || strings.Contains(p["text"].(string), "还是这个任务自己的对话") {
		t.Fatalf("the first run starts a conversation with the whole message: %v", p)
	}
	task, _ := taskByID(h, "r")
	if task.RunSessionID != first.SessionID || task.RunMachineID != "server" || task.RunLauncher != "claude" {
		t.Fatalf("the new conversation is the task's own: %+v", task)
	}

	second, err := runNow(t, h, "r")
	if err != nil {
		t.Fatal(err)
	}
	p := n.lastSend(t)
	if second.MachineID != "server" || second.SessionID != first.SessionID || p["session_id"] != first.SessionID || p["resume_only"] != true {
		t.Fatalf("the second run goes into the same conversation: %+v %v", second, p)
	}
	if text := p["text"].(string); !strings.Contains(text, "还是这个任务自己的对话") || !strings.Contains(text, "给我今天的早报") {
		t.Fatalf("a continued run says so and still has the task's prompt: %q", text)
	}
	runs := waitRuns(t, h, 2)
	if runs[0].SessionID != first.SessionID || runs[1].SessionID != first.SessionID || runs[1].Status != runOK {
		t.Fatalf("both runs are recorded in that conversation: %+v", runs)
	}
	// The hub has its node keep listing the task's conversation however quiet (the profile's keep).
	deadline := time.Now().Add(5 * time.Second)
	for p := n.lastSnapshot().Profile; p == nil || !slices.Contains(p.Keep, SessionRef{MachineID: "server", SessionID: first.SessionID}); p = n.lastSnapshot().Profile {
		if time.Now().After(deadline) {
			t.Fatalf("the hub is told to keep the task's conversation listed: %+v", p)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if v := n.lastSnapshot().Tasks; len(v) != 1 || v[0].RunSessionID != first.SessionID {
		t.Fatalf("and the hub is told the task: %+v", v)
	}
}

// A conversation that is gone (Claude Code cleans transcripts up after 30 days), one on another launcher, or one the
// user reset (下次开新对话) can't be continued: the run starts a new one and that becomes the task's.
func TestATaskStartsANewConversationWhenItsOwnCannotGoOn(t *testing.T) {
	n := newFakeHub().withMachine()
	h := newTestService(t, n)
	setTasks(h, Task{ID: "p", Title: "巡检", Prompt: "巡检", Repeat: repeatInterval, EveryMinutes: 30, Enabled: true})
	first, _ := runNow(t, h, "p")

	n.forget(first.SessionID)
	gone, err := runNow(t, h, "p")
	if err != nil {
		t.Fatal(err)
	}
	if gone.SessionID == first.SessionID || gone.SessionID == "" {
		t.Fatalf("a gone conversation is replaced: %+v", gone)
	}
	if task, _ := taskByID(h, "p"); task.RunSessionID != gone.SessionID {
		t.Fatalf("and the new one is kept: %+v", task)
	}
	if text := n.lastSend(t)["text"].(string); !strings.Contains(text, "不用再用 schedule_followup") {
		t.Fatalf("a new conversation gets the whole message again: %q", text)
	}

	// 模型配置 › 主对话 now runs on another launcher (a task's conversations follow it, #1570).
	setModel(t, h, "assistant", ModelBinding{Provider: "launcher.codex"})
	switched, _ := runNow(t, h, "p")
	if switched.SessionID == gone.SessionID {
		t.Fatal("a conversation started with claude isn't continued with codex")
	}
	if p := n.lastSend(t); p["session_id"] != nil && p["session_id"] != "" || p["launcher"] != "codex" {
		t.Fatalf("a new conversation with the new launcher: %v", p)
	}
	if task, _ := taskByID(h, "p"); task.RunSessionID != switched.SessionID || task.RunLauncher != "codex" {
		t.Fatalf("%+v", task)
	}

	// 下次开新对话 in the app.
	srv := httptest.NewServer(scheduleMux(h))
	defer srv.Close()
	res, err := http.Post(srv.URL+"/api/v1/assistant/tasks/p/fresh", "application/json", nil)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("fresh: %v %v", res, err)
	}
	var body struct{ Task Task }
	_ = json.NewDecoder(res.Body).Decode(&body)
	res.Body.Close()
	if body.Task.RunSessionID != "" {
		t.Fatalf("the task forgets its conversation: %+v", body.Task)
	}
	fresh, _ := runNow(t, h, "p")
	if fresh.SessionID == switched.SessionID {
		t.Fatal("the next run starts a new conversation")
	}
	if res, _ := http.Post(srv.URL+"/api/v1/assistant/tasks/nope/fresh", "application/json", nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("an unknown task: %d", res.StatusCode)
	}
}

// A repeating task due while its last run is still going (a patrol stuck on an approval) is skipped and recorded, not
// stacked into the same conversation; a one-off still goes in, and 立即运行 says why it didn't.
func TestATaskDueWhileItsLastRunIsGoingIsSkipped(t *testing.T) {
	n := newFakeHub().withMachine()
	h := newStorelessService(t, n)
	setTasks(h, Task{ID: "p", Title: "巡检", Prompt: "巡检", Repeat: repeatInterval, EveryMinutes: 30, Enabled: true},
		Task{ID: "f", Prompt: "看看部署", Repeat: repeatOnce, Enabled: true})
	first, _ := runNow(t, h, "p")
	n.setSessions("server", proto.Session{SessionID: first.SessionID, Status: proto.StatusWorking, State: proto.StateNeedsYou})
	sends := n.sent()

	if _, err := runNow(t, h, "p"); !errors.Is(err, errTaskBusy) {
		t.Fatalf("err = %v", err)
	}
	if n.sent() != sends {
		t.Fatal("nothing is sent into a busy conversation")
	}
	runs := waitRuns(t, h, 2)
	if r := runs[1]; r.Status != runSkipped || r.Error != errTaskBusy.Error() || r.SessionID != first.SessionID {
		t.Fatalf("the skip is in the history: %+v", r)
	}
	if task, _ := taskByID(h, "p"); task.RunSessionID != first.SessionID || task.LastStatus != runSkipped {
		t.Fatalf("the task keeps its conversation: %+v", task)
	}

	srv := httptest.NewServer(scheduleMux(h))
	defer srv.Close()
	res, _ := http.Post(srv.URL+"/api/v1/assistant/tasks/p/run", "application/json", nil)
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("立即运行 on a busy task: %d", res.StatusCode)
	}

	// A one-off that shares the conversation (rescheduled by its own run) still goes in, after the turn.
	sc := h.loadSchedule(context.Background())
	sc.mu.Lock()
	f := &sc.tasks[sc.indexLocked("f")]
	f.RunMachineID, f.RunSessionID, f.RunLauncher = "server", first.SessionID, "claude"
	sc.mu.Unlock()
	if sess, err := runNow(t, h, "f"); err != nil || sess.SessionID != first.SessionID {
		t.Fatalf("a one-off is not skipped: %+v %v", sess, err)
	}

	// Two runs of one task at the same moment: the second one waits for nothing and is skipped.
	if !h.sched.begin("p") {
		t.Fatal("begin")
	}
	n.setSessions("server")
	if _, err := runNow(t, h, "p"); !errors.Is(err, errTaskBusy) {
		t.Fatalf("a run while the same task is being sent: %v", err)
	}
	h.sched.end("p")
	if _, err := runNow(t, h, "p"); err != nil {
		t.Fatal(err)
	}
}

// A task from a conversation (SessionID) whose conversation fails keeps the new one as its own: the next failure
// continues there instead of starting yet another one.
func TestATaskFromAConversationFallsBackToItsOwn(t *testing.T) {
	n := newFakeHub().withMachine()
	h := newStorelessService(t, n)
	setTasks(h, Task{ID: "f", Prompt: "接着看", Repeat: repeatInterval, EveryMinutes: 60, Enabled: true, MachineID: "gone", SessionID: "origin"})
	first, _ := runNow(t, h, "f")
	if first.SessionID == "" || first.SessionID == "origin" {
		t.Fatalf("%+v", first)
	}
	again, _ := runNow(t, h, "f")
	if again.SessionID != first.SessionID || n.lastSend(t)["resume_only"] != true {
		t.Fatalf("the fallback conversation is continued: %+v", again)
	}
	if task, _ := taskByID(h, "f"); task.SessionID != "origin" {
		t.Fatalf("the conversation it came from is still tried first: %+v", task)
	}
}

// scheduleMux is the routes as the hub relays them.
func scheduleMux(h *Service) http.Handler { return h }

// A run the node accepts and whose turn then ends in an API error (503 / no such model) is failed, pushed and retried
// once — the node reports the send as accepted, so it used to stay "ok" and the user was never told.
func TestTaskTurnEndingInAPIErrorFailsAndRetriesOnce(t *testing.T) {
	n := newFakeHub().withMachine()
	h := newStorelessService(t, n)
	h.runRetry = 50 * time.Millisecond
	n.items = []proto.Item{{Role: "assistant", Kind: "text", Text: "API Error: 503 upstream attempt budget is exhausted"}}
	setTasks(h, Task{ID: "p", Title: "值守", Prompt: "go", Repeat: repeatInterval, EveryMinutes: 30, At: time.Now().Add(-time.Minute), Enabled: true, MachineID: "server", CreatedBy: "assistant"})

	// The turn runs, then ends: the watcher sees working and then idle.
	n.setSessions("server", proto.Session{SessionID: "s1", Status: proto.StatusWorking, State: proto.StateWorking, Assistant: true,
		Cwd: "/home/me/.jarvis/assistant"})
	go func() {
		time.Sleep(3 * time.Second)
		n.mu.Lock()
		for i := range n.sessions["server"] {
			n.sessions["server"][i].Status, n.sessions["server"][i].State = proto.StatusOffline, proto.StateIdle
		}
		n.mu.Unlock()
	}()

	if _, err := runNow(t, h, "p"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		sc := h.loadSchedule(context.Background())
		sc.mu.Lock()
		failed := len(sc.runs) > 0 && slices.ContainsFunc(sc.runs, func(r ScheduleRun) bool {
			return r.RefID == "p" && r.Status == runFailed && strings.Contains(r.Error, "503")
		})
		sc.mu.Unlock()
		if failed {
			if p := n.pushed(); len(p) == 0 || p[0].Title != "定时任务失败：值守" || p[0].SessionID != "s1" || p[0].DeviceID != "server" {
				t.Fatalf("the failure is pushed: %+v", p)
			}
			// and the retry went, marked as such
			for time.Now().Before(deadline) {
				if n.sent() >= 2 {
					sc.mu.Lock()
					retried := slices.ContainsFunc(sc.runs, func(r ScheduleRun) bool { return r.RefID == "p" && r.Retried })
					sc.mu.Unlock()
					if retried {
						return
					}
				}
				time.Sleep(50 * time.Millisecond)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the failed run was not recorded")
}

// A run where the turn ends and the session carries a TurnError (e.g. a Codex turn failure on the model service)
// is failed, pushed and retried once (#2571).
func TestTaskTurnEndingInSessionTurnErrorFailsAndRetriesOnce(t *testing.T) {
	n := newFakeHub().withMachine()
	h := newStorelessService(t, n)
	h.runRetry = 50 * time.Millisecond
	setTasks(h, Task{ID: "p", Title: "值守", Prompt: "go", Repeat: repeatInterval, EveryMinutes: 30, At: time.Now().Add(-time.Minute), Enabled: true, MachineID: "server", CreatedBy: "assistant"})

	// The turn runs, then ends: the session carries TurnError and no assistant text item.
	n.setSessions("server", proto.Session{SessionID: "s1", Status: proto.StatusWorking, State: proto.StateWorking, Assistant: true,
		Cwd: "/home/me/.jarvis/assistant"})
	go func() {
		time.Sleep(3 * time.Second)
		n.mu.Lock()
		for i := range n.sessions["server"] {
			n.sessions["server"][i].Status, n.sessions["server"][i].State = proto.StatusOffline, proto.StateIdle
			n.sessions["server"][i].TurnError = "unexpected status 503 upstream attempt budget is exhausted"
			n.sessions["server"][i].TurnErrorKind = proto.TurnErrorTransient
		}
		n.mu.Unlock()
	}()

	if _, err := runNow(t, h, "p"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		sc := h.loadSchedule(context.Background())
		sc.mu.Lock()
		failed := len(sc.runs) > 0 && slices.ContainsFunc(sc.runs, func(r ScheduleRun) bool {
			return r.RefID == "p" && r.Status == runFailed && strings.Contains(r.Error, "503")
		})
		sc.mu.Unlock()
		if failed {
			if p := n.pushed(); len(p) == 0 || p[0].Title != "定时任务失败：值守" || p[0].SessionID != "s1" || p[0].DeviceID != "server" {
				t.Fatalf("the failure is pushed: %+v", p)
			}
			// and the retry went, marked as such
			for time.Now().Before(deadline) {
				if n.sent() >= 2 {
					sc.mu.Lock()
					retried := slices.ContainsFunc(sc.runs, func(r ScheduleRun) bool { return r.RefID == "p" && r.Retried })
					sc.mu.Unlock()
					if retried {
						return
					}
				}
				time.Sleep(50 * time.Millisecond)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the failed run was not recorded")
}

// The failure push sends the user to the task's page. A task switched off or deleted while the retry waited must not
// run again: the retry used the copy the run started with, so it went anyway, with the old prompt, and silently (a
// retry that fails is not pushed).
func TestTaskSwitchedOffOrDeletedAfterFailureIsNotRetried(t *testing.T) {
	for _, tc := range []struct {
		name string
		undo func(t *testing.T, h *Service)
	}{
		{"switched off", func(t *testing.T, h *Service) {
			sc := h.loadSchedule(context.Background())
			sc.mu.Lock()
			defer sc.mu.Unlock()
			i := sc.indexLocked("p")
			if i < 0 {
				t.Fatal("no task p")
			}
			sc.tasks[i].Enabled = false
		}},
		{"deleted", func(t *testing.T, h *Service) {
			if ok, err := h.deleteTask(context.Background(), "p"); err != nil || !ok {
				t.Fatalf("delete: %v %v", ok, err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := newFakeHub().withMachine()
			h := newStorelessService(t, n)
			h.runRetry = 50 * time.Millisecond
			n.items = []proto.Item{{Role: "assistant", Kind: "text", Text: "API Error: 503 upstream attempt budget is exhausted"}}
			setTasks(h, Task{ID: "p", Title: "值守", Prompt: "go", Repeat: repeatInterval, EveryMinutes: 30, At: time.Now().Add(-time.Minute), Enabled: true, MachineID: "server", CreatedBy: "assistant"})
			n.setSessions("server", proto.Session{SessionID: "s1", Status: proto.StatusWorking, State: proto.StateWorking, Assistant: true,
				Cwd: "/home/me/.jarvis/assistant"})
			if _, err := runNow(t, h, "p"); err != nil {
				t.Fatal(err)
			}
			tc.undo(t, h) // while the turn still runs: before the watcher can retry
			// The turn ends: the watcher sees working, then idle.
			time.Sleep(3 * time.Second)
			n.mu.Lock()
			for i := range n.sessions["server"] {
				n.sessions["server"][i].Status, n.sessions["server"][i].State = proto.StatusOffline, proto.StateIdle
			}
			n.mu.Unlock()

			// The failure is still recorded and pushed ...
			deadline := time.Now().Add(15 * time.Second)
			for len(n.pushed()) == 0 {
				if !time.Now().Before(deadline) {
					t.Fatal("the failed run was not pushed")
				}
				time.Sleep(50 * time.Millisecond)
			}
			if p := n.pushed(); p[0].Title != "定时任务失败：值守" {
				t.Fatalf("the failure is pushed: %+v", p)
			}
			// ... and the retry does not go.
			time.Sleep(20 * h.runRetry)
			if got := n.sent(); got != 1 {
				t.Fatalf("sent %d times, want the first run only", got)
			}
			sc := h.loadSchedule(context.Background())
			sc.mu.Lock()
			retried := slices.ContainsFunc(sc.runs, func(r ScheduleRun) bool { return r.RefID == "p" && r.Retried })
			sc.mu.Unlock()
			if retried {
				t.Fatal("a retry was recorded")
			}
		})
	}
}

// A model error can end the turn before the first poll sees it working (a vanished model fails immediately): the
// changed last item alone must still end the watch, or the run stays "ok" for the whole timeout.
func TestTaskFailingBeforeTheWatcherSeesItWorking(t *testing.T) {
	n := newFakeHub().withMachine()
	h := newStorelessService(t, n)
	h.runRetry = 50 * time.Millisecond
	setTasks(h, Task{ID: "p", Title: "值守", Prompt: "go", Repeat: repeatInterval, EveryMinutes: 30, At: time.Now().Add(-time.Minute), Enabled: true, MachineID: "server", CreatedBy: "assistant"})
	n.items = []proto.Item{{ID: "before", Role: "assistant", Kind: "text", Text: "old"}}
	n.setSessions("server", proto.Session{SessionID: "s1", Status: proto.StatusOffline, State: proto.StateIdle, Assistant: true,
		Cwd: "/home/me/.jarvis/assistant"})
	go func() {
		time.Sleep(1500 * time.Millisecond)
		n.mu.Lock()
		n.items = append(n.items, proto.Item{ID: "err", Role: "assistant", Kind: "text", Text: "There's an issue with the selected model (gone)."})
		n.mu.Unlock()
	}()
	if _, err := runNow(t, h, "p"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		sc := h.loadSchedule(context.Background())
		sc.mu.Lock()
		failed := slices.ContainsFunc(sc.runs, func(r ScheduleRun) bool { return r.RefID == "p" && r.Status == runFailed })
		sc.mu.Unlock()
		if failed {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("the failure before the first working poll was not recorded")
}
