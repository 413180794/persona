package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAssistantViewActionSelectedOnce(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	if _, err := d.pool.Exec(ctx, `TRUNCATE assistant_view_actions,assistant_views,assistant_feed`); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	v, f := viewRows("view_actions", "action-conversation", at)
	if err := d.CreateAssistantView(ctx, v, f); err != nil {
		t.Fatal(err)
	}
	var first atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			a := AssistantViewActionRow{Conversation: v.Conversation, Key: v.ID, Action: fmt.Sprintf("option-%d", i), ClientMsgID: fmt.Sprintf("client-%d", i), MessageID: fmt.Sprintf("message-%d", i), MachineID: v.MachineID, Text: "Selected", Values: json.RawMessage(`{"choice":"yes"}`), At: at}
			stored, made, err := d.BeginAssistantViewAction(ctx, a, 1)
			if err != nil {
				t.Errorf("begin: %v", err)
				return
			}
			if stored == nil {
				t.Error("no stored selection")
			}
			if made {
				first.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if first.Load() != 1 {
		t.Fatalf("selections committed: %d", first.Load())
	}
	all, err := d.AssistantViewActions(ctx, v.Conversation)
	if err != nil || len(all) != 1 || all[0].Text != "Selected" || all[0].Status != "pending" {
		t.Fatalf("%+v %v", all, err)
	}
	other, err := d.AssistantViewAction(ctx, "other", v.ID)
	if err != nil || other != nil {
		t.Fatalf("cross conversation: %+v %v", other, err)
	}
	a := all[0]
	a.Conversation = "other"
	if _, _, err := d.BeginAssistantViewAction(ctx, a, 1); !errors.Is(err, ErrAssistantViewNotFound) {
		t.Fatalf("foreign begin: %v", err)
	}
	leased, err := d.ClaimAssistantViewDelivery(ctx, v.Conversation, v.ID, at.Add(time.Minute))
	if err != nil || leased == nil {
		t.Fatalf("lease %+v %v", leased, err)
	}
	if again, err := d.ClaimAssistantViewDelivery(ctx, v.Conversation, v.ID, at.Add(time.Minute)); err != nil || again != nil {
		t.Fatalf("duplicate delivery lease %+v %v", again, err)
	}
	if err := d.SettleAssistantViewDelivery(ctx, v.Conversation, v.ID, leased.LeaseUntil, "sent", ""); err != nil {
		t.Fatal(err)
	}
	if again, err := d.ClaimAssistantViewDelivery(ctx, v.Conversation, v.ID, at.Add(time.Minute)); err != nil || again != nil {
		t.Fatalf("sent claimed again %+v %v", again, err)
	}
}

func TestAssistantViewActionVersionAndPendingRecovery(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	if _, err := d.pool.Exec(ctx, `TRUNCATE assistant_view_actions,assistant_views,assistant_feed`); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	v, f := viewRows("view_version", "action-conversation", at)
	if err := d.CreateAssistantView(ctx, v, f); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpdateAssistantView(ctx, v.Conversation, v.ID, 1, v.View, at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	a := AssistantViewActionRow{Conversation: v.Conversation, Key: v.ID, Action: "yes", ClientMsgID: "client", MessageID: "message", MachineID: v.MachineID, Text: "Yes", At: at}
	if _, _, err := d.BeginAssistantViewAction(ctx, a, 1); !errors.Is(err, ErrAssistantViewVersion) {
		t.Fatalf("stale view: %v", err)
	}
	if old, err := d.AssistantViewAction(ctx, v.Conversation, v.ID); err != nil || old != nil {
		t.Fatalf("stale choice persisted: %+v %v", old, err)
	}
	if _, made, err := d.BeginAssistantViewAction(ctx, a, 2); err != nil || !made {
		t.Fatalf("begin %v %v", made, err)
	}
	pending, err := d.PendingAssistantViewActions(ctx, 20)
	if err != nil || len(pending) != 1 || pending[0].Text != "Yes" || pending[0].MessageID != "message" {
		t.Fatalf("pending %+v %v", pending, err)
	}
	oldLease, err := d.ClaimAssistantViewDelivery(ctx, v.Conversation, v.ID, time.Now().Add(-time.Second))
	if err != nil || oldLease == nil {
		t.Fatalf("old lease: %+v %v", oldLease, err)
	}
	lease, err := d.ClaimAssistantViewDelivery(ctx, v.Conversation, v.ID, time.Now().Add(time.Minute))
	if err != nil || lease == nil {
		t.Fatalf("new lease: %+v %v", lease, err)
	}
	if err := d.SettleAssistantViewDelivery(ctx, v.Conversation, v.ID, oldLease.LeaseUntil, "sent", ""); err == nil {
		t.Fatal("stale worker settled a reclaimed delivery")
	}
	if err := d.SettleAssistantViewDelivery(ctx, v.Conversation, v.ID, lease.LeaseUntil, "failed", "uncertain delivery"); err != nil {
		t.Fatal(err)
	}
	pending, err = d.PendingAssistantViewActions(ctx, 20)
	if err != nil || len(pending) != 0 {
		t.Fatalf("unsafe failed retry %+v %v", pending, err)
	}
}

func TestAssistantViewActionRecordsVersionAndHTMLKind(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	if _, err := d.pool.Exec(ctx, `TRUNCATE assistant_view_actions,assistant_views,assistant_feed`); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	v, f := viewRows("view_html", "html-conversation", at)
	if err := d.CreateAssistantView(ctx, v, f); err != nil {
		t.Fatal(err)
	}
	a := AssistantViewActionRow{Conversation: v.Conversation, Key: v.ID, Action: "chart", ClientMsgID: "client", MessageID: "message-html", MachineID: v.MachineID, Text: "Go on", At: at}
	for _, bad := range []func(*AssistantViewActionRow) int64{
		func(a *AssistantViewActionRow) int64 { a.Kind = "html"; return 1 },                     // html without its reply
		func(a *AssistantViewActionRow) int64 { a.Kind, a.Reply = "action", "Go on"; return 1 }, // a reply only for html
		func(a *AssistantViewActionRow) int64 { a.Kind = "other"; return 1 },
		func(a *AssistantViewActionRow) int64 { return 0 },
	} {
		row := a
		if _, _, err := d.BeginAssistantViewAction(ctx, row, bad(&row)); err == nil {
			t.Fatalf("invalid action accepted: %+v", row)
		}
	}
	a.Kind, a.Reply = "html", "Go on"
	stored, made, err := d.BeginAssistantViewAction(ctx, a, 1)
	if err != nil || !made || stored.Version != 1 || stored.Kind != "html" {
		t.Fatalf("begin %+v %v %v", stored, made, err)
	}
	read, err := d.AssistantViewAction(ctx, v.Conversation, v.ID)
	if err != nil || read == nil || read.Version != 1 || read.Kind != "html" || read.Reply != "Go on" || read.Text != "Go on" {
		t.Fatalf("read %+v %v", read, err)
	}
	// A declared reply after the HTML reply gets the HTML reply back: one selection per card.
	other := AssistantViewActionRow{Conversation: v.Conversation, Key: v.ID, Action: "yes", ClientMsgID: "other", MessageID: "message-other", MachineID: v.MachineID, Text: "Yes", At: at}
	if old, made, err := d.BeginAssistantViewAction(ctx, other, 1); err != nil || made || old.Kind != "html" {
		t.Fatalf("second selection %+v %v %v", old, made, err)
	}
}

// An update and a commit racing on the same card: the commit holds the row, so a committed choice is always bound to
// the version stored at its commit, never to one replaced in between.
func TestAssistantViewActionRacesUpdate(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	if _, err := d.pool.Exec(ctx, `TRUNCATE assistant_view_actions,assistant_views,assistant_feed`); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	for i := 0; i < 20; i++ {
		v, f := viewRows(fmt.Sprintf("view_race_%d", i), "race-conversation", at)
		if err := d.CreateAssistantView(ctx, v, f); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		var beginErr, updateErr error
		var stored *AssistantViewActionRow
		wg.Add(2)
		go func() {
			defer wg.Done()
			a := AssistantViewActionRow{Conversation: v.Conversation, Key: v.ID, Action: "yes", ClientMsgID: "client", MessageID: fmt.Sprintf("race-%d", i), MachineID: v.MachineID, Text: "Yes", At: at}
			stored, _, beginErr = d.BeginAssistantViewAction(ctx, a, 1)
		}()
		go func() {
			defer wg.Done()
			_, updateErr = d.UpdateAssistantView(ctx, v.Conversation, v.ID, 1, v.View, at.Add(time.Second))
		}()
		wg.Wait()
		if updateErr != nil {
			t.Fatal(updateErr)
		}
		switch {
		case beginErr == nil:
			if stored.Version != 1 {
				t.Fatalf("committed %+v", stored)
			}
		case errors.Is(beginErr, ErrAssistantViewVersion):
			if old, err := d.AssistantViewAction(ctx, v.Conversation, v.ID); err != nil || old != nil {
				t.Fatalf("lost commit persisted %+v %v", old, err)
			}
		default:
			t.Fatal(beginErr)
		}
	}
}
