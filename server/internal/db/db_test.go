package db

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
)

// open connects to JARVIS_TEST_DATABASE_URL (a throwaway database; scripts/test-server.sh creates it on example-machine)
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
	if _, err := d.pool.Exec(context.Background(), `TRUNCATE devices, live_activities, phone_tools, node_sessions, mail_accounts, interactions, settings, inbox_items`); err != nil {
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

func TestDevices(t *testing.T) {
	d, ctx := open(t), context.Background()
	if err := d.UpsertDevice(ctx, Device{DeviceID: "a", Name: "iPhone", APNSToken: "t1"}); err != nil {
		t.Fatal(err)
	}
	if err := d.UpsertDevice(ctx, Device{DeviceID: "a", Name: "iPhone", APNSToken: "t2"}); err != nil {
		t.Fatal(err)
	}
	list, err := d.Devices(ctx)
	if err != nil || len(list) != 1 || list[0].APNSToken != "t2" {
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
	if err := d.SetDevicePrefs(ctx, "a", json.RawMessage(`{"done":"off"}`)); err != nil {
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
	_ = d.ReplacePhoneTools(ctx, []proto.Tool{{Name: "b"}, {Name: "a", InputSchema: json.RawMessage(`{"type":"object"}`)}})
	_ = d.ReplacePhoneTools(ctx, []proto.Tool{{Name: "z"}, {Name: "a"}})
	tools, err := d.PhoneTools(ctx)
	if err != nil || len(tools) != 2 || tools[0].Name != "z" || tools[1].Name != "a" {
		t.Fatalf("tools = %+v, %v", tools, err)
	}
}

func TestNodeSessions(t *testing.T) {
	d, ctx := open(t), context.Background()
	start := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	_ = d.PutNodeSession(ctx, "example-machine", "s1", NodeSession{Launcher: "cc_ds", Cwd: "/x", StartedAt: start})
	_ = d.PutNodeSession(ctx, "example-machine", "s1", NodeSession{Pane: "w1:p2", Launcher: "cc_ds", Cwd: "/x", StartedAt: start})
	_ = d.PutNodeSession(ctx, "mac", "s2", NodeSession{Launcher: "ccc"})
	got, err := d.NodeSessions(ctx, "example-machine")
	if err != nil || len(got) != 1 || got["s1"].Pane != "w1:p2" || !got["s1"].StartedAt.Equal(start) {
		t.Fatalf("sessions = %+v, %v", got, err)
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
	ix := proto.Interaction{ID: "ix1", MachineID: "example-machine", SessionID: "s", Kind: proto.IxQuestion, CreatedAt: time.Now()}
	_ = d.InsertInteraction(ctx, ix)
	_ = d.InsertInteraction(ctx, proto.Interaction{ID: "ix2", MachineID: "example-machine", SessionID: "s", Kind: proto.IxPlan, CreatedAt: time.Now()})
	yes := true
	if err := d.FinishInteraction(ctx, "ix1", "resolved", &proto.Resolution{Approve: &yes}); err != nil {
		t.Fatal(err)
	}
	_ = d.FinishInteraction(ctx, "ix1", "expired", nil) // first outcome wins
	// A hook may still claim a resumable one after a restart, unless it expired.
	_ = d.InsertInteraction(ctx, proto.Interaction{ID: "ix3", MachineID: "example-machine", SessionID: "s", Kind: proto.IxPermission,
		CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour), Resumable: true})
	_ = d.InsertInteraction(ctx, proto.Interaction{ID: "ix4", MachineID: "example-machine", SessionID: "s", Kind: proto.IxPermission,
		CreatedAt: time.Now(), ExpiresAt: time.Now().Add(-time.Minute), Resumable: true})
	kept, err := d.ExpireStaleInteractions(ctx)
	if err != nil || len(kept) != 1 || kept[0].ID != "ix3" || !kept[0].Resumable {
		t.Fatalf("kept %+v, %v", kept, err)
	}
	recs, err := d.Interactions(ctx, "example-machine", "s", 10)
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
	_ = d.MarkSeen(ctx, "example-machine", "s", t1)
	_ = d.MarkSeen(ctx, "example-machine", "s", t1.Add(-time.Hour)) // older view never moves it back
	got, err := d.SeenTimes(ctx)
	if err != nil || !got[[2]string{"example-machine", "s"}].Equal(t1) {
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
}
