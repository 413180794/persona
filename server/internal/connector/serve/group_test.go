package serve

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/rpc"
)

// grouper is an inbox that relates new messages to events, on made-up data.
type grouper struct{}

func (grouper) ID() string   { return "examplechat" }
func (grouper) Name() string { return "Example Chat" }
func (grouper) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapInbox, connector.CapGroup, connector.CapMeta},
		MetaDescription: "messages: the event's messages, oldest first"}
}
func (grouper) Collect(context.Context) ([]connector.InboxThing, error) {
	return []connector.InboxThing{{Thread: "chat:c1", Group: &connector.ThingGroup{Batch: "chat:c1@3", New: []string{"m3"}},
		Context: "示例：第三条 (m3)", Last: "3"}}, nil
}
func (grouper) Reply(context.Context, connector.InboxReply) error { return nil }
func (grouper) GroupTool(_ context.Context, c connector.GroupToolCall) (json.RawMessage, error) {
	return json.RawMessage(`{"thread":"` + c.Thread + `","tool":"` + c.Name + `","args":` + string(c.Args) + `}`), nil
}
func (grouper) GroupAppend(_ context.Context, a connector.GroupAppend) (connector.InboxThing, error) {
	id := a.Event
	if id == "" {
		id = "examplechat:c1:" + strings.TrimPrefix(a.Messages[0], "m")
	}
	meta, _ := json.Marshal(map[string]any{"before": a.Meta, "added": a.Messages, "continues": a.Continues})
	return connector.InboxThing{ID: id, Thread: a.Thread, Title: "示例", Last: "3", Meta: meta}, nil
}
func (grouper) GroupFallback(_ context.Context, f connector.GroupFallback) (connector.GroupFallbackResult, error) {
	return connector.GroupFallbackResult{Assign: []connector.GroupAssign{{Message: "m3", Event: f.Events[0].ID}},
		New: []connector.GroupNew{{Key: f.Batch, Messages: []string{"m4"}, RuleTier: connector.TierFYI, Title: "示例"}}}, nil
}

// The group.* methods reach a connector that declares group, in the protocol's words; its manifest says what its meta
// is, and a batch crosses the wire with its group.
func TestGroupMethods(t *testing.T) {
	progR, hubW := io.Pipe()
	hubR, progW := io.Pipe()
	go func() {
		_ = Main(context.Background(), progR, progW, func(json.RawMessage, connector.Secrets) (connector.Connector, error) {
			return grouper{}, nil
		})
		_ = progW.Close()
	}()
	peer := rpc.New("h", hubW)
	go func() { _ = peer.Serve(context.Background(), hubR) }()
	defer func() { _ = hubW.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var man Manifest
	if err := peer.Call(ctx, MInit, InitParams{Protocol: Protocol}, &man); err != nil {
		t.Fatal(err)
	}
	if m := man.Of(); !m.Has(connector.CapGroup) || m.MetaDescription != "messages: the event's messages, oldest first" {
		t.Fatalf("manifest %+v", man)
	}
	var things CollectResult
	if err := peer.Call(ctx, MCollect, struct{}{}, &things); err != nil || len(things.Things) != 1 ||
		things.Things[0].Group == nil || things.Things[0].Group.Batch != "chat:c1@3" || things.Things[0].Group.New[0] != "m3" {
		t.Fatalf("collect %+v %v", things, err)
	}

	var tool json.RawMessage
	if err := peer.Call(ctx, MGroupTool, connector.GroupToolCall{Thread: "chat:c1", Name: connector.GroupMessagesBefore,
		Args: json.RawMessage(`{"message":"m3","n":5}`)}, &tool); err != nil ||
		string(tool) != `{"thread":"chat:c1","tool":"messages_before","args":{"message":"m3","n":5}}` {
		t.Fatalf("tool %s %v", tool, err)
	}
	var ev connector.InboxThing
	if err := peer.Call(ctx, MGroupAppend, connector.GroupAppend{Thread: "chat:c1", Messages: []string{"m3"}, Continues: "examplechat:c1:1"}, &ev); err != nil ||
		ev.ID != "examplechat:c1:3" || string(ev.Meta) != `{"added":["m3"],"before":null,"continues":"examplechat:c1:1"}` {
		t.Fatalf("append %+v %s %v", ev, ev.Meta, err)
	}
	var fb connector.GroupFallbackResult
	if err := peer.Call(ctx, MGroupFallback, connector.GroupFallback{Thread: "chat:c1", Batch: "chat:c1@3",
		Events: []connector.GroupEvent{{ID: "examplechat:c1:1", Meta: json.RawMessage(`{"a":1}`)}}}, &fb); err != nil ||
		len(fb.Assign) != 1 || fb.Assign[0].Event != "examplechat:c1:1" || len(fb.New) != 1 || fb.New[0].RuleTier != connector.TierFYI {
		t.Fatalf("fallback %+v %v", fb, err)
	}
}

// A connector without group answers no_capability.
func TestGroupMethodsNeedTheCapability(t *testing.T) {
	progR, hubW := io.Pipe()
	hubR, progW := io.Pipe()
	go func() {
		_ = Main(context.Background(), progR, progW, func(json.RawMessage, connector.Secrets) (connector.Connector, error) {
			return notes{}, nil
		})
		_ = progW.Close()
	}()
	peer := rpc.New("h", hubW)
	go func() { _ = peer.Serve(context.Background(), hubR) }()
	defer func() { _ = hubW.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var man Manifest
	if err := peer.Call(ctx, MInit, InitParams{Protocol: Protocol}, &man); err != nil {
		t.Fatal(err)
	}
	var out json.RawMessage
	err := peer.Call(ctx, MGroupAppend, connector.GroupAppend{Thread: "x"}, &out)
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != CodeNoCapability {
		t.Fatalf("got %v", err)
	}
}
