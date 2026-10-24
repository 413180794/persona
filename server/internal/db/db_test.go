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
	if _, err := d.pool.Exec(context.Background(), `TRUNCATE devices, phone_tools, node_sessions, mail_accounts, interactions, settings`); err != nil {
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
	if err := d.ExpireStaleInteractions(ctx); err != nil {
		t.Fatal(err)
	}
	recs, err := d.Interactions(ctx, "example-machine", "s", 10)
	if err != nil || len(recs) != 2 {
		t.Fatalf("records = %+v, %v", recs, err)
	}
	status := map[string]string{}
	for _, r := range recs {
		status[r.Interaction.ID] = r.Status
	}
	if status["ix1"] != "resolved" || status["ix2"] != "cancelled" {
		t.Fatalf("status = %v", status)
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
