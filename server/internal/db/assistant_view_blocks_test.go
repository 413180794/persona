package db

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nwverse/persona/sdk/go/wire"
)

func TestAssistantInlineBlockProvenanceAndReplay(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	at := time.Now().UTC()
	conversation := "inline-db"
	if _, err := d.pool.Exec(ctx, `TRUNCATE assistant_view_blocks,assistant_view_errors,assistant_view_actions,assistant_views,assistant_feed`); err != nil {
		t.Fatal(err)
	}
	b := AssistantViewBlockRow{Conversation: conversation, MachineID: "example-machine", ItemID: "item#0", Index: 1, Key: wire.AssistantBlockKey("item#0", 1), View: json.RawMessage(`{"fallback":"Original","sections":[]}`), CreatedAt: at}
	if _, err := d.RegisterAssistantViewBlock(ctx, b); err != nil {
		t.Fatal(err)
	}
	b.View = json.RawMessage(`{"fallback":"Changed","sections":[]}`)
	if _, err := d.RegisterAssistantViewBlock(ctx, b); err != nil {
		t.Fatal(err)
	}
	got, err := d.AssistantCard(ctx, conversation, b.Key)
	if err != nil || got.Version != 1 || !json.Valid(got.View) || string(got.View) == string(b.View) {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := d.AssistantCard(ctx, "other", b.Key); !errors.Is(err, ErrAssistantViewNotFound) {
		t.Fatalf("other card %v", err)
	}
	if _, err := d.AssistantView(ctx, conversation, b.Key); !errors.Is(err, ErrAssistantViewNotFound) {
		t.Fatalf("inline card confused with a live card: %v", err)
	}
	if _, err := d.UpdateAssistantView(ctx, conversation, b.Key, 1, b.View, at); !errors.Is(err, ErrAssistantViewNotFound) {
		t.Fatalf("inline card updated as live: %v", err)
	}
	keys, err := d.AssistantViewBlockKeys(ctx, conversation)
	if err != nil || len(keys) != 1 || keys[0] != b.Key {
		t.Fatalf("%v %v", keys, err)
	}
	b.Key = "client-forged"
	if _, err := d.RegisterAssistantViewBlock(ctx, b); err == nil {
		t.Fatal("forged key accepted")
	}
}

func TestAssistantInlineActionAndErrorFeedback(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	at := time.Now().UTC()
	conversation := "inline-action-db"
	if _, err := d.pool.Exec(ctx, `TRUNCATE assistant_view_blocks,assistant_view_errors,assistant_view_actions,assistant_views,assistant_feed`); err != nil {
		t.Fatal(err)
	}
	b := AssistantViewBlockRow{Conversation: conversation, MachineID: "example-machine", ItemID: "item#0", Index: 0, Key: "item#0/0", View: json.RawMessage(`{"fallback":"Original","sections":[]}`), CreatedAt: at}
	if _, err := d.RegisterAssistantViewBlock(ctx, b); err != nil {
		t.Fatal(err)
	}
	a := AssistantViewActionRow{Conversation: conversation, Key: b.Key, Action: "yes", ClientMsgID: "client", MessageID: "message", MachineID: b.MachineID, Text: "Yes", At: at}
	if _, first, err := d.BeginAssistantViewAction(ctx, a, 1); err != nil || !first {
		t.Fatalf("inline selection %v %v", first, err)
	}
	a.MachineID = "other-machine"
	if _, _, err := d.BeginAssistantViewAction(ctx, a, 1); !errors.Is(err, ErrAssistantViewNotFound) {
		t.Fatalf("foreign machine selection %v", err)
	}
	problems := []string{"persona-view block 0: fallback missing"}
	if err := d.RecordAssistantViewErrors(ctx, conversation, b.MachineID, b.ItemID, problems, at); err != nil {
		t.Fatal(err)
	}
	if got, err := d.TakeAssistantViewErrors(ctx, "other", b.MachineID); err != nil || len(got) != 0 {
		t.Fatalf("foreign feedback %v %v", got, err)
	}
	got, err := d.TakeAssistantViewErrors(ctx, conversation, b.MachineID)
	if err != nil || len(got) != 1 {
		t.Fatalf("feedback %v %v", got, err)
	}
	if err := d.RecordAssistantViewErrors(ctx, conversation, b.MachineID, b.ItemID, problems, at); err != nil {
		t.Fatal(err)
	}
	if got, err := d.TakeAssistantViewErrors(ctx, conversation, b.MachineID); err != nil || len(got) != 0 {
		t.Fatalf("replayed feedback %v %v", got, err)
	}
	if err := d.RecordAssistantViewErrors(ctx, conversation, b.MachineID, "corrected", problems, at); err != nil {
		t.Fatal(err)
	}
	if err := d.RecordAssistantViewErrors(ctx, conversation, b.MachineID, "corrected", nil, at); err != nil {
		t.Fatal(err)
	}
	if got, err := d.TakeAssistantViewErrors(ctx, conversation, b.MachineID); err != nil || len(got) != 0 {
		t.Fatalf("corrected feedback %v %v", got, err)
	}
}
