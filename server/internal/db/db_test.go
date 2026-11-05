package db

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nwverse/persona/server/internal/db/dbtest"
	"github.com/nwverse/persona/server/internal/proto"
)

// TestMain runs the tests on a database of their own (dbtest, #583): other runs on the same machine do not touch it.
func TestMain(m *testing.M) { os.Exit(dbtest.Main(m)) }

// open connects to JARVIS_TEST_DATABASE_URL (a throwaway database; scripts/test/test-server.sh creates it on the hub machine)
// and wipes every table so each test starts clean.
func open(t *testing.T) *DB {
	t.Helper()
	url := os.Getenv("JARVIS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("JARVIS_TEST_DATABASE_URL not set")
	}
	d, err := Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	// Union of the integration branch's list and master's TruncateForTest: TruncateForTest alone
	// does not clear lock_tests, client_ops, pair_tokens or device_sessions.
	if _, err := d.pool.Exec(context.Background(), `TRUNCATE lock_tests, client_ops, devices, device_tokens, pair_tokens, device_sessions, live_activities, phone_tools, node_sessions, mail_accounts, interactions, settings, inbox_items, kb_entities, connector_secrets, machine_systems, machine_metrics, sent_messages, todos, todo_agents, assistant_always CASCADE`); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestMigrateIsIdempotent(t *testing.T) {
	d := open(t)
	if err := d.migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDeviceSessions(t *testing.T) {
	d, ctx := open(t), context.Background()
	base := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	insert := func(device string, start, end time.Duration, reason string) int64 {
		t.Helper()
		id, err := d.InsertDeviceSession(ctx, DeviceSession{DeviceID: device, ConnectedAt: base.Add(start),
			Source: "192.0.2.0/24", HeartbeatInterval: 20})
		if err != nil {
			t.Fatal(err)
		}
		if reason != "" {
			if err := d.FinishDeviceSession(ctx, id, base.Add(end), reason, 20); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	insert("device-a", -2*time.Hour, -30*time.Minute, "closed")
	a := insert("device-a", 10*time.Minute, 20*time.Minute, "pong_timeout")
	insert("device-a", 25*time.Minute, 30*time.Minute, "hub_stopped")
	insert("device-a", 50*time.Minute, 55*time.Minute, "closed")
	insert("device-a", 52*time.Minute, 58*time.Minute, "error")
	insert("device-b", 0, 0, "")
	history, err := d.DeviceSessions(ctx, "device-a", base, base.Add(time.Hour))
	if err != nil || len(history.Sessions) != 4 || history.Summary.Disconnects != 3 || history.Summary.LongestGapSeconds != 600 {
		t.Fatalf("history = %+v, %v", history, err)
	}
	if s := history.Sessions[0]; s.ID != a || s.Source != "192.0.2.0/24" || s.HeartbeatInterval != 20 || s.Reason != "pong_timeout" {
		t.Fatalf("session = %+v", s)
	}
	// The restart gap is excluded, overlapping sessions stay online, and the trailing gap is included.
	history, err = d.DeviceSessions(ctx, "device-a", base.Add(26*time.Minute), base.Add(time.Hour))
	if err != nil || len(history.Sessions) != 3 || history.Summary.Disconnects != 2 || history.Summary.LongestGapSeconds != 120 {
		t.Fatalf("window history = %+v, %v", history, err)
	}
	live := insert("device-a", 59*time.Minute, 0, "")
	if err := d.UpdateDeviceSessionHeartbeat(ctx, live, 240); err != nil {
		t.Fatal(err)
	}
	history, err = d.DeviceSessions(ctx, "device-a", base.Add(58*time.Minute), base.Add(time.Hour))
	if err != nil || len(history.Sessions) != 2 || history.Summary.Disconnects != 1 || history.Summary.LongestGapSeconds != 60 {
		t.Fatalf("live history = %+v, %v", history, err)
	}
	if s := history.Sessions[1]; s.DisconnectedAt != nil || s.HeartbeatInterval != 240 {
		t.Fatalf("live session = %+v", s)
	}
	if err := d.StopDeviceSessions(ctx, base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := d.FinishDeviceSession(ctx, live, base.Add(2*time.Hour), "error", 20); err != nil {
		t.Fatal(err)
	}
	history, err = d.DeviceSessions(ctx, "device-a", base.Add(59*time.Minute), base.Add(2*time.Hour))
	if err != nil || len(history.Sessions) != 1 || history.Sessions[0].Reason != "hub_stopped" ||
		history.Sessions[0].HeartbeatInterval != 240 || history.Summary.Disconnects != 0 || history.Summary.LongestGapSeconds != 0 {
		t.Fatalf("recovered history = %+v, %v", history, err)
	}
	old := insert("device-a", -16*24*time.Hour, -15*24*time.Hour, "replaced")
	if err := d.PruneDeviceSessions(ctx, base.Add(-14*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := d.pool.QueryRow(ctx, `SELECT count(*) FROM device_sessions WHERE id = $1`, old).Scan(&count); err != nil || count != 0 {
		t.Fatalf("pruned count = %d, %v", count, err)
	}
	history, err = d.DeviceSessions(ctx, "missing", base, base.Add(time.Hour))
	if err != nil || history.Sessions == nil || len(history.Sessions) != 0 || history.Summary != (DeviceSessionSummary{}) {
		t.Fatalf("empty history = %+v, %v", history, err)
	}
}

func TestClientOps(t *testing.T) {
	d, ctx := open(t), context.Background()
	op := ClientOp{ID: "00000000-0000-4000-8000-000000000001", DeviceID: "device", Kind: "message",
		Target: json.RawMessage(`{"path":"/api/v1/sessions/m/s/messages"}`), Digest: "digest"}
	var wg sync.WaitGroup
	claims := make(chan bool, 8)
	for i := 0; i < cap(claims); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stored, claimed, err := d.BeginClientOp(ctx, op)
			if err != nil || stored == nil || stored.State != "pending" || stored.Digest != op.Digest {
				t.Errorf("claim = %+v, %v", stored, err)
			}
			claims <- claimed
		}()
	}
	wg.Wait()
	close(claims)
	count := 0
	for claimed := range claims {
		if claimed {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("executors = %d", count)
	}
	result := json.RawMessage(`{"status":201,"body":"eyAib2siOiB0cnVlIH0K"}`)
	if err := d.FinishClientOp(ctx, op.ID, "done", result); err != nil {
		t.Fatal(err)
	}
	if err := d.FinishClientOp(ctx, op.ID, "unknown", nil); err != nil {
		t.Fatal(err)
	}
	op.Digest = "changed"
	stored, claimed, err := d.BeginClientOp(ctx, op)
	if err != nil || claimed || stored.State != "done" || stored.Digest != "digest" {
		t.Fatalf("existing = %+v, claimed = %v, err = %v", stored, claimed, err)
	}
	var saved struct {
		Status int
		Body   []byte
	}
	if err := json.Unmarshal(stored.Result, &saved); err != nil || saved.Status != 201 || string(saved.Body) != "{ \"ok\": true }\n" {
		t.Fatalf("saved result = %s, %v", stored.Result, err)
	}
	op.ID = "00000000-0000-4000-8000-000000000002"
	if _, _, err := d.BeginClientOp(ctx, op); err != nil {
		t.Fatal(err)
	}
	if err := d.RecoverClientOps(ctx); err != nil {
		t.Fatal(err)
	}
	if stored, err := d.ClientOp(ctx, op.ID); err != nil || stored.State != "unknown" {
		t.Fatalf("recovered = %+v, %v", stored, err)
	}
	if _, err := d.pool.Exec(ctx, `UPDATE client_ops SET created_at = now() - interval '31 days' WHERE id = $1`, op.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.CleanupClientOps(ctx, time.Now().AddDate(0, 0, -30)); err != nil {
		t.Fatal(err)
	}
	if stored, err := d.ClientOp(ctx, op.ID); err != nil || stored != nil {
		t.Fatalf("expired = %+v, %v", stored, err)
	}
	if stored, err := d.ClientOp(ctx, "00000000-0000-4000-8000-000000000001"); err != nil || stored == nil {
		t.Fatalf("recent receipt removed: %+v, %v", stored, err)
	}
}

func TestDevices(t *testing.T) {
	d, ctx := open(t), context.Background()
	if err := d.UpsertDevice(ctx, Device{DeviceID: "a", Name: "iPhone", APNSToken: "t1"}); err != nil {
		t.Fatal(err)
	}
	if err := d.UpsertDevice(ctx, Device{DeviceID: "a", Name: "iPhone", APNSToken: "t2", PushKey: "pk", RelayURL: "https://relay.example/relay", RelayGrant: "g", Credential: "dev_1"}); err != nil {
		t.Fatal(err)
	}
	list, err := d.Devices(ctx)
	if err != nil || len(list) != 1 || list[0].APNSToken != "t2" || list[0].PushKey != "pk" || list[0].RelayURL != "https://relay.example/relay" || list[0].RelayGrant != "g" || list[0].Credential != "dev_1" {
		t.Fatalf("devices = %+v, %v", list, err)
	}
	if err := d.DeleteDevice(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if list, _ := d.Devices(ctx); len(list) != 0 {
		t.Fatalf("device not deleted: %+v", list)
	}
}

// Prefs survive re-registration (the app posts its token on every launch) and can be set before the device has a token.
func TestDevicePrefs(t *testing.T) {
	d, ctx := open(t), context.Background()
	if err := d.SetDevicePrefs(ctx, "a", "", json.RawMessage(`{"done":"off"}`)); err != nil {
		t.Fatal(err)
	}
	if err := d.UpsertDevice(ctx, Device{DeviceID: "a", Name: "iPhone", APNSToken: "t1"}); err != nil {
		t.Fatal(err)
	}
	list, err := d.Devices(ctx)
	if err != nil || len(list) != 1 || list[0].APNSToken != "t1" || string(list[0].Prefs) != `{"done": "off"}` {
		t.Fatalf("devices = %+v (%s), %v", list, list[0].Prefs, err)
	}
	if err := d.UpsertDevice(ctx, Device{DeviceID: "b", Name: "iPad"}); err != nil {
		t.Fatal(err)
	}
	for _, v := range mustDevices(t, d) {
		if v.DeviceID == "b" && v.Prefs != nil {
			t.Fatalf("b has prefs %s", v.Prefs)
		}
	}
}

// A row only prefs made takes the credential that wrote them (revoking it removes the row); a registered one keeps its own.
func TestDevicePrefsCredential(t *testing.T) {
	d, ctx := open(t), context.Background()
	if err := d.SetDevicePrefs(ctx, "web-1", "dev_web", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err := d.UpsertDevice(ctx, Device{DeviceID: "phone", APNSToken: "t", Credential: "dev_phone"}); err != nil {
		t.Fatal(err)
	}
	if err := d.SetDevicePrefs(ctx, "phone", "dev_web", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, v := range mustDevices(t, d) {
		got[v.DeviceID] = v.Credential
	}
	if got["web-1"] != "dev_web" || got["phone"] != "dev_phone" {
		t.Fatalf("credentials = %v", got)
	}
}

func TestLiveActivities(t *testing.T) {
	d, ctx := open(t), context.Background()
	if err := d.SaveLiveActivity(ctx, LiveActivity{DeviceID: "a", PushToStartToken: "s1"}); err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	if err := d.SaveLiveActivity(ctx, LiveActivity{DeviceID: "a", ActivityID: "act", PushToken: "p1", PushToStartToken: "s1", StartedAt: started}); err != nil {
		t.Fatal(err)
	}
	list, err := d.LiveActivities(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("%+v %v", list, err)
	}
	if v := list[0]; v.ActivityID != "act" || v.PushToken != "p1" || v.PushToStartToken != "s1" || !v.StartedAt.Equal(started) {
		t.Fatalf("%+v", v)
	}
}

func mustDevices(t *testing.T, d *DB) []Device {
	t.Helper()
	list, err := d.Devices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestPhoneToolsReplace(t *testing.T) {
	d, ctx := open(t), context.Background()
	_ = d.ReplacePhoneTools(ctx, PhoneToolRegistration{DeviceID: "legacy", Tools: []proto.Tool{{Name: "b"}, {Name: "a", InputSchema: json.RawMessage(`{"type":"object"}`)}}})
	_ = d.ReplacePhoneTools(ctx, PhoneToolRegistration{DeviceID: "legacy", Tools: []proto.Tool{{Name: "z"}, {Name: "a"}}})
	tools, err := d.PhoneTools(ctx)
	if err != nil || len(tools) != 1 || len(tools[0].Tools) != 2 || tools[0].Tools[0].Name != "z" || tools[0].Tools[1].Name != "a" {
		t.Fatalf("tools = %+v, %v", tools, err)
	}
}

func TestNodeSessions(t *testing.T) {
	d, ctx := open(t), context.Background()
	start := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	_ = d.PutNodeSession(ctx, "server", "s1", NodeSession{Launcher: "cc_ds", Cwd: "/x", StartedAt: start})
	_ = d.PutNodeSession(ctx, "server", "s1", NodeSession{Pane: "w1:p2", Launcher: "cc_ds", Cwd: "/x", StartedAt: start})
	_ = d.PutNodeSession(ctx, "mac", "s2", NodeSession{Launcher: "ccc"})
	got, err := d.NodeSessions(ctx, "server")
	if err != nil || len(got) != 1 || got["s1"].Pane != "w1:p2" || !got["s1"].StartedAt.Equal(start) || len(got["s1"].Tasks) != 0 {
		t.Fatalf("sessions = %+v, %v", got, err)
	}
	_ = d.PutNodeSession(ctx, "server", "s1", NodeSession{Launcher: "cc_ds", Cwd: "/x", StartedAt: start, Tasks: []string{"tail log", "sleep"}})
	if got, _ = d.NodeSessions(ctx, "server"); len(got["s1"].Tasks) != 2 || got["s1"].Tasks[0] != "tail log" {
		t.Fatalf("tasks = %v", got["s1"].Tasks)
	}
}

func TestMailAccountsReplace(t *testing.T) {
	d, ctx := open(t), context.Background()
	rows := []MailAccountRow{{ID: "1", Email: "a@x", Data: json.RawMessage(`{"name":"A"}`), PasswordEnc: "enc"}, {ID: "2", Email: "b@x", Data: json.RawMessage(`{}`)}}
	if err := d.ReplaceMailAccounts(ctx, rows); err != nil {
		t.Fatal(err)
	}
	if err := d.ReplaceMailAccounts(ctx, rows[1:]); err != nil {
		t.Fatal(err)
	}
	got, err := d.MailAccounts(ctx)
	if err != nil || len(got) != 1 || got[0].ID != "2" {
		t.Fatalf("accounts = %+v, %v", got, err)
	}
}

func TestInteractions(t *testing.T) {
	d, ctx := open(t), context.Background()
	ix := proto.Interaction{ID: "ix1", MachineID: "server", SessionID: "s", Kind: proto.IxQuestion, CreatedAt: time.Now()}
	_ = d.InsertInteraction(ctx, ix)
	_ = d.InsertInteraction(ctx, proto.Interaction{ID: "ix2", MachineID: "server", SessionID: "s", Kind: proto.IxPlan, CreatedAt: time.Now()})
	yes := true
	if err := d.FinishInteraction(ctx, "ix1", "resolved", &proto.Resolution{Approve: &yes}); err != nil {
		t.Fatal(err)
	}
	_ = d.FinishInteraction(ctx, "ix1", "expired", nil) // first outcome wins
	// A hook may still claim a resumable one after a restart, unless it expired.
	_ = d.InsertInteraction(ctx, proto.Interaction{ID: "ix3", MachineID: "server", SessionID: "s", Kind: proto.IxPermission,
		CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour), Resumable: true})
	_ = d.InsertInteraction(ctx, proto.Interaction{ID: "ix4", MachineID: "server", SessionID: "s", Kind: proto.IxPermission,
		CreatedAt: time.Now(), ExpiresAt: time.Now().Add(-time.Minute), Resumable: true})
	kept, err := d.ExpireStaleInteractions(ctx)
	if err != nil || len(kept) != 1 || kept[0].ID != "ix3" || !kept[0].Resumable {
		t.Fatalf("kept %+v, %v", kept, err)
	}
	recs, err := d.Interactions(ctx, "server", "s", 10)
	if err != nil || len(recs) != 4 {
		t.Fatalf("records = %+v, %v", recs, err)
	}
	status := map[string]string{}
	for _, r := range recs {
		status[r.Interaction.ID] = r.Status
	}
	if status["ix1"] != "resolved" || status["ix2"] != "cancelled" || status["ix3"] != "pending" || status["ix4"] != "cancelled" {
		t.Fatalf("status = %v", status)
	}
	if st, res, err := d.InteractionOutcome(ctx, "ix1"); err != nil || st != "resolved" || res == nil || res.Approve == nil || !*res.Approve {
		t.Fatalf("outcome ix1 %q %+v %v", st, res, err)
	}
	if st, res, err := d.InteractionOutcome(ctx, "nope"); err != nil || st != "" || res != nil {
		t.Fatalf("outcome of a missing one %q %+v %v", st, res, err)
	}
}

// The startup lookup of pending interactions can use the partial index instead of scanning every past interaction.
func TestPendingInteractionsIndexed(t *testing.T) {
	d, ctx := open(t), context.Background()
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// A test table is too small for the planner to prefer any index on its own; rule the scan out to see the choice.
	if _, err := tx.Exec(ctx, `SET LOCAL enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		sql  string
		args []any
	}{
		{resumablePendingSQL, nil},
		{cancelPendingSQL, []any{[]string{}}},
	} {
		q := c.sql
		rows, err := tx.Query(ctx, `EXPLAIN `+q, c.args...)
		if err != nil {
			t.Fatal(err)
		}
		lines, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		if plan := strings.Join(lines, "\n"); !strings.Contains(plan, "interactions_pending") {
			t.Fatalf("%s\nplan:\n%s", q, plan)
		}
	}
}

func TestSettings(t *testing.T) {
	d, ctx := open(t), context.Background()
	var v struct{ Launcher string }
	if ok, err := d.Setting(ctx, "assistant", &v); ok || err != nil {
		t.Fatalf("unset setting: %v %v", ok, err)
	}
	_ = d.PutSetting(ctx, "assistant", map[string]string{"Launcher": "cc_max"})
	if ok, err := d.Setting(ctx, "assistant", &v); !ok || err != nil || v.Launcher != "cc_max" {
		t.Fatalf("setting = %+v %v %v", v, ok, err)
	}
}

func TestSessionViews(t *testing.T) {
	d, ctx := open(t), context.Background()
	_, _ = d.pool.Exec(ctx, `TRUNCATE session_views`)
	t1 := time.Now().Add(-time.Minute).UTC().Truncate(time.Millisecond)
	_ = d.MarkSeen(ctx, "server", "s", t1)
	_ = d.MarkSeen(ctx, "server", "s", t1.Add(-time.Hour)) // older view never moves it back
	got, err := d.SeenTimes(ctx)
	if err != nil || !got[[2]string{"server", "s"}].Equal(t1) {
		t.Fatalf("seen = %v, %v", got, err)
	}
}

func TestInboxItems(t *testing.T) {
	d, ctx := open(t), context.Background()
	now := time.Now()
	old := now.Add(-10 * 24 * time.Hour)
	if err := d.PutInboxItem(ctx, "a", []byte(`{"id":"a"}`), now, nil); err != nil {
		t.Fatal(err)
	}
	if err := d.PutInboxItem(ctx, "a", []byte(`{"id":"a","count":2}`), now, &now); err != nil { // update
		t.Fatal(err)
	}
	if err := d.PutInboxItem(ctx, "old", []byte(`{"id":"old"}`), old, &old); err != nil {
		t.Fatal(err)
	}
	rows, err := d.InboxItems(ctx, now.Add(-8*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want the old done item gone", len(rows))
	}
	var it struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(rows[0], &it); err != nil || it.Count != 2 {
		t.Fatalf("row = %s", rows[0])
	}
	// Deleted for good: a source taken back.
	_ = d.PutInboxItem(ctx, "imessage:1", []byte(`{"id":"imessage:1"}`), now, nil)
	if err := d.DeleteInboxItems(ctx, []string{"imessage:1"}); err != nil {
		t.Fatal(err)
	}
	if rows, _ := d.InboxItems(ctx, now.Add(-8*24*time.Hour)); len(rows) != 1 {
		t.Fatalf("rows after delete = %d", len(rows))
	}
}

func TestDeviceTokens(t *testing.T) {
	d, ctx := open(t), context.Background()
	if _, err := d.pool.Exec(ctx, `TRUNCATE device_tokens`); err != nil {
		t.Fatal(err)
	}
	if err := d.CreateDeviceToken(ctx, DeviceToken{ID: "dt1", Name: "iPhone", Kind: "phone", Role: "full"}, "h1"); err != nil {
		t.Fatal(err)
	}
	_ = d.CreateDeviceToken(ctx, DeviceToken{ID: "dt2", Name: "Watch", Kind: "watch", Role: "answer"}, "h2")
	got, err := d.DeviceTokenByHash(ctx, "h2")
	if err != nil || got == nil || got.ID != "dt2" || got.Role != "answer" {
		t.Fatalf("%+v %v", got, err)
	}
	_ = d.TouchDeviceToken(ctx, "dt2")
	if ok, err := d.RevokeDeviceToken(ctx, "dt2"); !ok || err != nil {
		t.Fatalf("revoke %v %v", ok, err)
	}
	if ok, _ := d.RevokeDeviceToken(ctx, "dt2"); ok {
		t.Fatal("revoked twice")
	}
	if got, _ := d.DeviceTokenByHash(ctx, "h2"); got != nil {
		t.Fatal("a revoked token still works")
	}
	list, _ := d.DeviceTokens(ctx)
	if len(list) != 2 || list[0].ID != "dt1" || list[1].RevokedAt == nil || list[1].LastSeen == nil {
		t.Fatalf("%+v", list)
	}
}

func TestKBEntities(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	_ = d.PutKBEntity(ctx, "projects", "a", "A", "P2", 50, []string{"发布"}, "", now)
	_ = d.PutKBEntity(ctx, "projects", "b", "B", "P0", 10, nil, "", now)
	_ = d.PutKBEntity(ctx, "chats", "c", "C", "", 90, []string{"发布"}, "x", time.Time{})
	if err := d.PutKBEntity(ctx, "projects", "a", "A2", "P1", 60, []string{"发布", "后端"}, "y", now); err != nil {
		t.Fatal(err)
	}
	list, err := d.KBEntities(ctx, "projects", "", 10)
	if err != nil || len(list) != 2 || list[0].ID != "b" || list[1].Name != "A2" || list[1].Importance != "P1" {
		t.Fatalf("%+v %v", list, err)
	}
	if list, _ := d.KBEntities(ctx, "", "发布", 10); len(list) != 2 {
		t.Fatalf("tag filter: %+v", list)
	}
	// A source taken back: only its rows of that kind go.
	_ = d.PutKBEntity(ctx, "chats", "imessage-0123456789ab", "张三", "", 10, nil, "", now)
	_ = d.PutKBEntity(ctx, "people", "imessage-0123456789ab", "张三", "", 10, nil, "", now)
	if err := d.DeleteKBEntities(ctx, "chats", []string{"imessage-0123456789ab", "nope"}); err != nil {
		t.Fatal(err)
	}
	chats, _ := d.KBEntities(ctx, "chats", "", 10)
	people, _ := d.KBEntities(ctx, "people", "", 10)
	if len(chats) != 1 || chats[0].ID != "c" || len(people) == 0 {
		t.Fatalf("after delete: chats %+v people %+v", chats, people)
	}
	if err := d.DeleteKBEntities(ctx, "chats", nil); err != nil {
		t.Fatal(err)
	}
}

func TestMachineMetrics(t *testing.T) {
	d, ctx := open(t), context.Background()
	if err := d.PutMachineSystem(ctx, "m1", proto.System{OS: "darwin", Distro: "macos", Cores: 8}); err != nil {
		t.Fatal(err)
	}
	if err := d.PutMachineSystem(ctx, "m1", proto.System{OS: "darwin", Distro: "macos", Cores: 10}); err != nil {
		t.Fatal(err)
	}
	if sys, err := d.MachineSystems(ctx); err != nil || sys["m1"].Cores != 10 {
		t.Fatalf("systems = %+v, %v", sys, err)
	}

	// Two hours of samples every 10 s, CPU = the minute's index (so a minute's average is exact), with a 10-minute
	// hole and a sample without CPU.
	base := time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC)
	f := func(v float64) *float64 { return &v }
	for i := 0; i < 720; i++ {
		ts := base.Add(time.Duration(i) * 10 * time.Second)
		if ts.Sub(base) >= 30*time.Minute && ts.Sub(base) < 40*time.Minute {
			continue
		}
		s := proto.MetricSample{T: ts.Unix(), CPU: f(float64(i / 6)), Load: f(1), MemUsed: 100, MemTotal: 1000,
			NetRx: f(float64(10 * (i % 6))), DiskUsed: 5, DiskTotal: 50}
		if i == 6 {
			s.CPU = nil
		}
		if err := d.InsertMetric(ctx, "m1", s); err != nil {
			t.Fatal(err)
		}
	}
	_ = d.InsertMetric(ctx, "m2", proto.MetricSample{T: base.Unix(), CPU: f(99), MemTotal: 1})

	// Roll up everything before 1:00:30 → only whole minutes before 1:00 move: 60 minutes minus the hole.
	n, err := d.RollupMetrics(ctx, base.Add(time.Hour+30*time.Second))
	if err != nil || n != 51 { // 50 minutes of m1, 1 of m2
		t.Fatalf("rolled %d, %v", n, err)
	}
	if n, _ := d.RollupMetrics(ctx, base.Add(time.Hour+30*time.Second)); n != 0 {
		t.Fatalf("rolled again: %d", n)
	}
	var raw, rolled int
	_ = d.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE step = 10), count(*) FILTER (WHERE step = 60) FROM machine_metrics WHERE machine_id = 'm1'`).Scan(&raw, &rolled)
	if raw != 360 || rolled != 50 {
		t.Fatalf("raw %d rolled %d", raw, rolled)
	}

	// By minute over the whole two hours: rolled-up and raw minutes read the same.
	series, err := d.MetricSeries(ctx, []string{"m1"}, base, base.Add(2*time.Hour), 60)
	if err != nil {
		t.Fatal(err)
	}
	m1 := series["m1"]
	if len(m1) != 110 || len(series) != 1 {
		t.Fatalf("%d points, %d machines", len(m1), len(series))
	}
	if *m1[0].CPU != 0 || *m1[5].CPU != 5 || *m1[100].CPU != 110 || *m1[0].Rx != 25 || *m1[0].Mem != 100 {
		t.Fatalf("points %+v %+v %+v", m1[0], m1[5], m1[100])
	}
	if m1[1].T-m1[0].T != 60 || m1[30].T != base.Add(40*time.Minute).Unix() {
		t.Fatalf("times %d %d (the hole has no points)", m1[0].T, m1[30].T)
	}
	if *m1[1].CPU != 1 { // minute 1 had one sample without CPU: the average of the other five
		t.Fatalf("minute 1 cpu = %v", *m1[1].CPU)
	}
	// Raw over the last hour: one point a sample.
	series, _ = d.MetricSeries(ctx, nil, base.Add(time.Hour), base.Add(2*time.Hour), 10)
	if len(series["m1"]) != 360 {
		t.Fatalf("raw points %d", len(series["m1"]))
	}

	if n, err := d.PruneMetrics(ctx, base.Add(30*time.Minute)); err != nil || n != 31 {
		t.Fatalf("pruned %d, %v", n, err)
	}
}

func TestPhoneToolsDevicePersistence(t *testing.T) {
	d, ctx := open(t), context.Background()
	for _, id := range []string{"credential-ios", "credential-android"} {
		if err := d.CreateDeviceToken(ctx, DeviceToken{ID: id, Name: "Device", Kind: "phone", Role: "full"}, "hash-"+id); err != nil {
			t.Fatal(err)
		}
	}
	iphone := PhoneToolRegistration{DeviceID: "ios-device", Platform: "ios", CredentialID: "credential-ios", Tools: []proto.Tool{{Name: "phone_reminders_list"}, {Name: "phone_device_info"}}}
	android := PhoneToolRegistration{DeviceID: "android-device", Platform: "android", CredentialID: "credential-android", Tools: []proto.Tool{{Name: "phone_notifications_list"}}}
	for _, r := range []PhoneToolRegistration{iphone, android} {
		if err := d.ReplacePhoneTools(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	iphone.Tools = iphone.Tools[:1]
	if err := d.ReplacePhoneTools(ctx, iphone); err != nil {
		t.Fatal(err)
	}
	got, err := d.PhoneTools(ctx)
	if err != nil || len(got) != 2 || len(got[0].Tools) != 1 || len(got[1].Tools) != 1 || got[0].Platform != "android" || got[1].CredentialID != iphone.CredentialID {
		t.Fatalf("registrations = %+v, %v", got, err)
	}
	if err := d.UpsertDevice(ctx, Device{DeviceID: "push-ios", CredentialID: iphone.CredentialID}); err != nil {
		t.Fatal(err)
	}
	if ok, err := d.RevokeDeviceToken(ctx, iphone.CredentialID); err != nil || !ok {
		t.Fatalf("revoke = %v, %v", ok, err)
	}
	got, err = d.PhoneTools(ctx)
	if err != nil || len(got) != 1 || got[0].DeviceID != android.DeviceID {
		t.Fatalf("registrations after revoke = %+v, %v", got, err)
	}
	if devices, err := d.Devices(ctx); err != nil || len(devices) != 0 {
		t.Fatalf("push registrations after revoke = %+v, %v", devices, err)
	}
	if err := d.ReplacePhoneTools(ctx, iphone); err == nil {
		t.Fatal("revoked device registered tools again")
	}
	android.Tools = nil
	if err := d.ReplacePhoneTools(ctx, android); err != nil {
		t.Fatal(err)
	}
	if got, err := d.PhoneTools(ctx); err != nil || len(got) != 0 {
		t.Fatalf("registrations after empty update = %+v, %v", got, err)
	}
}

func TestSentMessages(t *testing.T) {
	d, ctx := open(t), context.Background()
	if st, err := d.ClaimSend(ctx, "m1", "box", "s1"); err != nil || st != "" {
		t.Fatalf("new id: %q %v", st, err)
	}
	if st, _ := d.ClaimSend(ctx, "m1", "box", "s1"); st != "sending" {
		t.Fatalf("claimed again while sending: %q", st)
	}
	if err := d.SettleSend(ctx, "m1", "delivered"); err != nil {
		t.Fatal(err)
	}
	if st, _ := d.ClaimSend(ctx, "m1", "box", "s1"); st != "delivered" {
		t.Fatalf("after delivery: %q", st)
	}
	_ = d.SettleSend(ctx, "m1", "") // refused: forgotten, the retry sends
	if st, _ := d.ClaimSend(ctx, "m1", "box", "s1"); st != "" {
		t.Fatalf("after forgetting: %q", st)
	}
	_, _ = d.pool.Exec(ctx, `UPDATE sent_messages SET created_at = now() - interval '2 days'`)
	if st, _ := d.ClaimSend(ctx, "m1", "box", "s1"); st != "sending" {
		t.Fatalf("an old unacknowledged operation must not execute again: %q", st)
	}
}

// Where a sent message landed (node/echo.go): kept with the send, a found line not replaced by a late "expected"
// record, handed back per machine for a day.
func TestSentEcho(t *testing.T) {
	d, ctx := open(t), context.Background()
	for _, id := range []string{"m1", "m2", "m3"} {
		if _, err := d.ClaimSend(ctx, id, "box", "s1"); err != nil {
			t.Fatal(err)
		}
	}
	_, _ = d.ClaimSend(ctx, "o1", "other", "s9")
	expected := json.RawMessage(`{"session_id":"s1","client_msg_id":"m1","text":"hi","after_seq":3}`)
	found := json.RawMessage(`{"session_id":"s1","client_msg_id":"m1","text":"hi","after_seq":3,"line":"u1"}`)
	for _, e := range []json.RawMessage{expected, found, expected} {
		if err := d.RecordEcho(ctx, "m1", "box", e); err != nil {
			t.Fatal(err)
		}
	}
	_ = d.RecordEcho(ctx, "m2", "box", json.RawMessage(`{"client_msg_id":"m2"}`))
	_ = d.RecordEcho(ctx, "o1", "box", json.RawMessage(`{"client_msg_id":"o1"}`)) // another machine's send
	_ = d.RecordEcho(ctx, "nope", "box", json.RawMessage(`{"client_msg_id":"nope"}`))
	got, err := d.Echoes(ctx, "box")
	if err != nil || len(got) != 2 {
		t.Fatalf("echoes %s %v", got, err)
	}
	var first struct {
		Line string `json:"line"`
	}
	_ = json.Unmarshal(got[0], &first)
	if first.Line != "u1" {
		t.Fatalf("the found line was replaced: %s", got[0])
	}
	_, _ = d.pool.Exec(ctx, `UPDATE sent_messages SET created_at = now() - interval '2 days' WHERE client_msg_id = 'm2'`)
	if got, _ := d.Echoes(ctx, "box"); len(got) != 1 {
		t.Fatalf("a send older than a day was handed back: %s", got)
	}
}

// A connector's secret: stored sealed by the hub, replaced, read back, deleted; another connector has none.
func TestConnectorSecrets(t *testing.T) {
	d, ctx := open(t), context.Background()
	if _, ok, err := d.ConnectorSecret(ctx, "example"); ok || err != nil {
		t.Fatalf("none yet: %v %v", ok, err)
	}
	for _, v := range []string{"sealed-1", "sealed-2"} {
		if err := d.PutConnectorSecret(ctx, "example", v); err != nil {
			t.Fatal(err)
		}
	}
	if v, ok, err := d.ConnectorSecret(ctx, "example"); !ok || err != nil || v != "sealed-2" {
		t.Fatalf("got %q %v %v", v, ok, err)
	}
	if _, ok, _ := d.ConnectorSecret(ctx, "other"); ok {
		t.Fatal("another connector has it")
	}
	if err := d.DeleteConnectorSecret(ctx, "example"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := d.ConnectorSecret(ctx, "example"); ok {
		t.Fatal("still there after delete")
	}
}
