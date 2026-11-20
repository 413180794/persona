package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

type briefDigestArgs struct {
	Day  string `json:"day"`
	Slot string `json:"slot"`
	FYI  bool   `json:"include_fyi_items"`
}

type briefDigestExtension struct {
	ExtensionTools
	args []briefDigestArgs
	read func(briefDigestArgs) string
}

func (e *briefDigestExtension) ToolAccess(name string) (bool, bool) {
	return name == larkBriefDigestTool, name == larkBriefDigestTool
}

func (e *briefDigestExtension) ReadLarkBriefDigest(_ context.Context, raw json.RawMessage) (string, error) {
	var args briefDigestArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	e.args = append(e.args, args)
	return e.read(args), nil
}

func readyBriefDigest(args briefDigestArgs) string {
	id := args.Day + ":" + args.Slot
	b, _ := json.Marshal(map[string]any{"day": args.Day, "requested_slot": id, "digest_status": "ready",
		"latest": map[string]string{"id": id, "title": "飞书简报", "content": "飞书简报\n\n本槽摘要 " + id}})
	return string(b)
}

type briefDeliveryWorld struct {
	s       *Service
	hub     *fakeHub
	store   Store
	ext     *briefDigestExtension
	status  FeishuStatus
	legacy  *briefFallbackPosts
	posts   []assistantwire.FeishuBriefPostParams
	deliver func(assistantwire.FeishuBriefPostParams) (any, error)
}

func newBriefDeliveryWorld(t *testing.T) *briefDeliveryWorld {
	t.Helper()
	w := &briefDeliveryWorld{hub: readyHub(), store: &memStore{}, ext: &briefDigestExtension{read: readyBriefDigest},
		status: FeishuStatus{Ready: true, Enabled: true, Owner: "ou_owner", PairedAt: time.Unix(100, 0), Features: FeishuFeatures{Sync: true}}}
	w.hub.settings = &Settings{Timezone: "Asia/Shanghai", MachineID: "server", Main: "main"}
	w.legacy = briefFallbackCapture(w.hub)
	w.hub.answerApp(AppFeishu, FeishuStatusGet, func(json.RawMessage) (any, error) { return w.status, nil })
	w.hub.answerApp(AppFeishu, assistantwire.FeishuBriefPost, func(raw json.RawMessage) (any, error) {
		var p assistantwire.FeishuBriefPostParams
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		w.posts = append(w.posts, p)
		return w.deliver(p)
	})
	w.deliver = func(p assistantwire.FeishuBriefPostParams) (any, error) {
		return map[string]string{"message_id": "om_" + p.UUID}, nil
	}
	w.restart(t)
	return w
}

func (w *briefDeliveryWorld) restart(t *testing.T) {
	w.s = newServiceOn(t, w.hub, w.store)
	w.s.SetExtensions(w.ext)
	initBriefFallbackState(w.s)
}

func briefTestTasks() []Task {
	return []Task{
		{ID: "morning", Title: "早报", Enabled: true, Facts: true, Repeat: repeatDaily, Time: "09:00"},
		{ID: "evening", Title: "晚报", Enabled: true, Facts: true, Repeat: repeatDaily, Time: "20:30"},
		{ID: "other", Title: "早报", Enabled: true, Facts: true, Repeat: repeatDaily, Time: "09:00"},
	}
}

func (w *briefDeliveryWorld) saveTasks(t *testing.T, tasks ...Task) {
	t.Helper()
	if err := w.store.PutSetting(t.Context(), tasksKey, tasks); err != nil {
		t.Fatal(err)
	}
	setTasks(w.s, tasks...)
}

func (w *briefDeliveryWorld) receipts(t *testing.T) map[string]briefDelivery {
	t.Helper()
	var rows map[string]briefDelivery
	if _, err := w.store.Setting(t.Context(), briefDeliveriesKey, &rows); err != nil {
		t.Fatal(err)
	}
	return rows
}

func briefTestDue(day, hour, minute int) time.Time {
	return time.Date(2030, 11, day, hour, minute, 0, 0, beijingBriefZone)
}

func (w *briefDeliveryWorld) due(t *testing.T, task Task, due time.Time, id string) ScheduleRun {
	t.Helper()
	run := ScheduleRun{ID: id, RefID: task.ID, Kind: task.kind(), Due: due.UTC(), Status: runOK}
	task.targetBrief = w.s.targetsLarkBrief(task)
	w.s.enqueueBriefDelivery(task, run)
	return run
}

func TestLarkBriefDeliveryRunsBothSlotsWithPushesOff(t *testing.T) {
	w := newBriefDeliveryWorld(t)
	tasks := briefTestTasks()[:2]
	for i := range tasks {
		tasks[i].Prompt = "Read larkcontext_digest"
	}
	w.saveTasks(t, tasks...)
	w.hub.setSessions("server", proto.Session{SessionID: "s1", Assistant: true}, proto.Session{SessionID: "s2", Assistant: true})
	w.hub.setItems(proto.Item{Role: "user", Kind: proto.KindText, Text: "scheduled run"},
		proto.Item{Role: "assistant", Kind: proto.KindText, Text: "reports are ready"})

	for i, due := range []time.Time{briefTestDue(20, 9, 0), briefTestDue(20, 20, 30)} {
		// Use the scheduler's normal scan, then drive the outbox as its next tick would.
		now := due.Add(time.Second)
		w.s.runDue(t.Context(), now)
		w.s.sched.inflight.Wait()
		w.s.briefDeliveryTick(t.Context(), now)
		if len(w.posts) != i+1 || len(w.ext.args) != i+1 {
			t.Fatalf("%s did not deliver exactly once: %d sends, tool calls %+v, receipts %+v", tasks[i].ID, len(w.posts), w.ext.args, w.receipts(t))
		}
		p := w.posts[i]
		args := w.ext.args[i]
		if p.Owner != w.status.Owner || !p.PairedAt.Equal(w.status.PairedAt) || p.UUID == "" ||
			args != (briefDigestArgs{Day: "2030-11-20", Slot: tasks[i].ID, FYI: true}) || !strings.Contains(p.Card, args.Day+":"+args.Slot) {
			t.Fatalf("unexpected owner, card or slot: %+v, %+v", p, args)
		}
		row := w.receipts(t)[briefFallbackKey(ScheduleRun{RefID: tasks[i].ID, Due: due})]
		if row.Status != "sent" || row.RunID == "" || !row.Due.Equal(due) || row.Slot != args.Day+":"+args.Slot ||
			row.MessageID != "om_"+p.UUID || row.ConfirmedAt.IsZero() {
			t.Fatalf("incomplete receipt: %+v", row)
		}
		t.Logf("%s pushes=false slot=%s run=%s receipt=%s", row.Routine, row.Slot, row.RunID, row.Status)
	}
	if w.posts[0].UUID == w.posts[1].UUID || len(w.legacy.list()) != 0 {
		t.Fatal("occurrences shared a UUID or leaked into ordinary Feishu pushes")
	}
}

func TestLarkBriefDeliveryPreservesOrdinaryPushAndMirror(t *testing.T) {
	w := newBriefDeliveryWorld(t)
	tasks := briefTestTasks()
	main := proto.Session{SessionID: "main", Assistant: true}
	other := proto.Session{SessionID: "patrol", Assistant: true}
	tasks[2].RunMachineID, tasks[2].RunSessionID = "server", other.SessionID
	w.saveTasks(t, tasks...)
	w.hub.setSessions("server", main, other)
	w.hub.setItems(proto.Item{Role: "user", Kind: proto.KindText, Text: "⏰ 早报"}, proto.Item{Role: "assistant", Kind: proto.KindText, Text: "other task"})
	due := briefTestDue(20, 9, 0)
	w.due(t, tasks[2], due, "other")
	w.s.briefDeliveryTick(t.Context(), due)
	ctx := t.Context()
	w.s.feishuMirror(ctx, "server", "patrol")
	w.s.feishuMirror(ctx, "server", "main")
	_ = w.s.feishuPushKeyed(ctx, "server", "", "ordinary notify_user", "text", "key-off")
	if got := len(w.legacy.list()); got != 1 || len(w.posts) != 0 {
		t.Fatalf("pushes=false: sync should mirror only the main turn: legacy=%d targeted=%d", got, len(w.posts))
	}
	w.status.Features.Pushes = true
	w.s.feishuMirror(ctx, "server", "patrol")
	_ = w.s.feishuPushKeyed(ctx, "server", "", "ordinary notify_user", "text", "key-on")
	if got := len(w.legacy.list()); got != 3 {
		t.Fatalf("pushes=true: want ordinary Routine and notify_user, total %d", got)
	}
	run := w.due(t, tasks[0], due, "run-morning")
	tasks[0].targetBrief = true
	w.hub.setItems(proto.Item{Role: "user", Kind: proto.KindText, Text: taskPrompt(tasks[0], due, due, time.UTC, false, false, false)},
		proto.Item{Role: "assistant", Kind: proto.KindText, Text: "briefing commentary"})
	// A source-conversation briefing may go into Main. Its repeated completion still owns only the targeted card.
	for range 2 {
		w.s.feishuMirror(ctx, "server", "main")
	}
	attachBriefFallback(w.s, "main", "早报", false)
	failed := run
	failed.SessionID = "main"
	w.s.briefFallback(ctx, failed, tasks[0], Device{ID: "server"})
	w.s.briefDeliveryTick(ctx, due)
	if len(w.legacy.list()) != 3 || len(w.posts) != 1 {
		t.Fatalf("target created a duplicate ordinary mirror/fallback: legacy=%d targeted=%d", len(w.legacy.list()), len(w.posts))
	}
	waitFor(t, "existing phone fallback", func() bool { return len(w.hub.pushed()) == 1 })
}

func TestLarkBriefDeliveryDeduplicatesAfterRestartAndKeepsNextOccurrence(t *testing.T) {
	w := newBriefDeliveryWorld(t)
	task := briefTestTasks()[0]
	w.saveTasks(t, task)
	due := briefTestDue(20, 9, 0)
	first := w.due(t, task, due, "first")
	w.due(t, task, due, "duplicate-before-send")
	w.s.briefDeliveryTick(t.Context(), due)
	w.restart(t)
	w.due(t, task, due, "native-retry-after-restart")
	w.s.briefDeliveryTick(t.Context(), due)
	second := w.due(t, task, due.Add(24*time.Hour), "next-day")
	w.s.briefDeliveryTick(t.Context(), second.Due)
	rows := w.receipts(t)
	if len(w.posts) != 2 || len(rows) != 2 || w.posts[0].UUID == w.posts[1].UUID ||
		rows[briefFallbackKey(first)].RunID != first.ID || rows[briefFallbackKey(second)].Status != "sent" {
		t.Fatalf("dedupe/next occurrence failed: sends=%d, receipts=%+v", len(w.posts), rows)
	}
}

func TestLarkBriefDeliveryWaitsForExactSlotAndRecordsTimeout(t *testing.T) {
	w := newBriefDeliveryWorld(t)
	task := briefTestTasks()[1]
	w.saveTasks(t, task)
	due := briefTestDue(20, 20, 30)
	run := w.due(t, task, due, "evening-unready")
	slot := "2030-11-20:evening"
	for _, check := range []struct{ result, reason string }{
		{`{"day":"2030-11-20","requested_slot":"2030-11-20:evening","digest_status":"pending","digest_issue":{"code":"slot_not_recorded"}}`, "slot_not_recorded"},
		{`{"day":"2030-11-20","requested_slot":"2030-11-20:morning","digest_status":"ready","latest":{"id":"2030-11-20:morning","content":"different"}}`, "response did not match"},
		{`{"day":"2030-11-20","requested_slot":"2030-11-20:evening","digest_status":"ready","latest":{"id":"2030-11-19:evening","content":"stale"}}`, "no usable digest"},
	} {
		w.ext.read = func(briefDigestArgs) string { return check.result }
		w.s.briefDeliveryTick(t.Context(), due.Add(time.Duration(len(w.ext.args))*briefDeliveryPoll))
		row := w.receipts(t)[briefFallbackKey(run)]
		if len(w.posts) != 0 || row.Status != "pending" || !strings.Contains(row.Error, check.reason) {
			t.Fatalf("unexpected readiness behavior: %+v, posts=%d", row, len(w.posts))
		}
	}
	w.s.briefDeliveryTick(t.Context(), due.Add(briefDeliveryWindow))
	row := w.receipts(t)[briefFallbackKey(run)]
	if row.Status != "failed" || row.Slot != slot || row.MessageID != "" || row.ConfirmedAt.IsZero() ||
		!strings.Contains(row.Error, "delivery window expired: Lark Context has no usable digest") || len(w.posts) != 0 {
		t.Fatalf("bad timeout receipt: %+v", row)
	}
	for _, args := range w.ext.args {
		if args != (briefDigestArgs{Day: "2030-11-20", Slot: task.ID, FYI: true}) {
			t.Fatalf("polled a different slot: %+v", w.ext.args)
		}
	}
	next := w.due(t, task, due.Add(24*time.Hour), "next-evening")
	w.ext.read = func(briefDigestArgs) string {
		return `{"day":"2030-11-21","requested_slot":"2030-11-21:evening","digest_status":"pending"}`
	}
	w.s.briefDeliveryTick(t.Context(), next.Due)
	w.ext.read = readyBriefDigest
	w.s.briefDeliveryTick(t.Context(), next.Due.Add(briefDeliveryPoll))
	if row := w.receipts(t)[briefFallbackKey(next)]; row.Status != "sent" || len(w.posts) != 1 {
		t.Fatalf("same-slot readiness retry did not deliver: %+v", row)
	}
}

func TestLarkBriefDeliveryRetriesLostReplyWithFrozenPayload(t *testing.T) {
	w := newBriefDeliveryWorld(t)
	w.store = todoTestDB(t)
	w.restart(t)
	task := briefTestTasks()[0]
	w.saveTasks(t, task)
	due := briefTestDue(20, 9, 0)
	run := w.due(t, task, due, "lost-reply")
	landed := make(map[string]string)
	w.deliver = func(p assistantwire.FeishuBriefPostParams) (any, error) {
		row := w.receipts(t)[briefFallbackKey(run)]
		if row.Card != p.Card || row.UUID != p.UUID {
			t.Fatalf("post began before durable UUID/payload: %+v", row)
		}
		if id := landed[p.UUID]; id != "" {
			return map[string]string{"message_id": id}, nil
		}
		landed[p.UUID] = "om_gateway_receipt"
		return nil, errors.New("lost reply after acceptance")
	}
	w.s.briefDeliveryTick(t.Context(), due)
	if row := w.receipts(t)[briefFallbackKey(run)]; row.Status != "pending" || !strings.Contains(row.Error, "lost reply") {
		t.Fatalf("missing failure receipt: %+v", row)
	}
	w.restart(t)
	w.s.briefDeliveryTick(t.Context(), due.Add(briefDeliveryPoll))
	row := w.receipts(t)[briefFallbackKey(run)]
	if len(w.posts) != 2 || w.posts[0] != w.posts[1] || len(landed) != 1 || len(w.ext.args) != 1 ||
		row.Status != "sent" || row.MessageID != "om_gateway_receipt" || row.ConfirmedAt.IsZero() || row.Card != "" {
		t.Fatalf("lost response recovery: sends=%d landed=%v digest reads=%d receipt=%+v", len(w.posts), landed, len(w.ext.args), row)
	}
}

func TestLarkBriefDeliveryReceiptsForRejectionAndOwnerGuards(t *testing.T) {
	for _, tc := range []struct{ name, reason string }{
		{"no_binding", "no bound Feishu owner"},
		{"repaired", "owner binding changed"},
		{"disabled_bot", "bot is disabled"},
		{"health_session", "conversation is private"},
		{"rejected", "Feishu send rejected"},
		{"no_message_id", "Feishu returned no message ID"},
		{"expired_slot", "delivery window expired"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newBriefDeliveryWorld(t)
			task := briefTestTasks()[0]
			task.targetBrief = true
			due := briefTestDue(20, 9, 0)
			run := ScheduleRun{ID: "first", RefID: task.ID, Kind: "routine", Due: due.UTC(), Status: runOK}
			now := due
			var posts int
			switch tc.name {
			case "no_binding":
				w.status.Ready, w.status.Owner = false, ""
			case "health_session":
				run.MachineID, run.SessionID = "server", "health"
				w.hub.setSessions("server", proto.Session{SessionID: "health", Space: SpaceHealth})
			case "rejected":
				posts = 1
				w.deliver = func(assistantwire.FeishuBriefPostParams) (any, error) {
					return nil, errors.New("feishu send: 230099 rejected")
				}
			case "no_message_id":
				posts = 1
				w.deliver = func(assistantwire.FeishuBriefPostParams) (any, error) { return map[string]string{}, nil }
			case "expired_slot":
				now = due.Add(24 * time.Hour)
			}
			w.saveTasks(t, task)
			w.s.enqueueBriefDelivery(task, run)
			if tc.name == "repaired" {
				w.status.PairedAt = w.status.PairedAt.Add(time.Minute)
			}
			if tc.name == "disabled_bot" {
				w.status.Enabled, w.status.Ready = false, false
			}
			w.s.briefDeliveryTick(t.Context(), now)
			w.s.briefDeliveryTick(t.Context(), now.Add(briefDeliveryWindow))
			row := w.receipts(t)[briefFallbackKey(run)]
			if len(w.posts) != posts || row.Status != "failed" || row.MessageID != "" || row.ConfirmedAt.IsZero() || !strings.Contains(row.Error, tc.reason) {
				t.Fatalf("expected failure %q: %+v, posts=%d", tc.reason, row, len(w.posts))
			}
		})
	}
}

func TestLarkBriefDeliveryUsesLegacyConfigAndExcludesManualRuns(t *testing.T) {
	w := newBriefDeliveryWorld(t)
	former := prompts.FormerEveningRoutines()[0]
	// The first world has already loaded the defaults. Start a service on the actual pre-migration settings.
	w.store = &memStore{}
	if err := w.store.PutSetting(t.Context(), routinesKey, []Routine{{ID: "evening", Time: "21:30", Enabled: true, Prompt: former}}); err != nil {
		t.Fatal(err)
	}
	w.restart(t)
	task, ok := taskByID(w.s, "evening")
	if !ok || !task.Facts || task.Time != "21:30" || !w.s.targetsLarkBrief(task) {
		t.Fatalf("legacy configuration was not kept and routed: %+v", task)
	}
	due := briefTestDue(20, 21, 30)
	manual := ScheduleRun{ID: "manual", RefID: task.ID, Due: due, Manual: true}
	task.targetBrief = true // even if misclassified upstream, the receipt rejects a manual run
	w.s.enqueueBriefDelivery(task, manual)
	w.s.briefDeliveryTick(t.Context(), due)
	if len(w.receipts(t)) != 0 || len(w.ext.args) != 0 {
		t.Fatal("manual run obtained an automatic receipt")
	}
	// The native start failure also owns this same scheduled occurrence.
	_, _ = w.s.failTaskRun(t.Context(), task, ScheduleRun{RefID: task.ID, Kind: "routine", Due: due},
		errors.New("model rejected start"), true, nil)
	w.s.briefDeliveryTick(t.Context(), due)
	if row := w.receipts(t)[briefFallbackKey(manual)]; len(w.posts) != 1 || row.Status != "sent" || row.Slot != "2030-11-20:evening" {
		t.Fatalf("legacy evening schedule / fixed fallback: %+v, posts=%d", row, len(w.posts))
	}
}
