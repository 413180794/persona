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
