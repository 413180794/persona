package connector

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"
)

// recallWriter is a writer that can also take back what it wrote (Recaller).
type recallWriter struct{ writer }

func (recallWriter) CanRecall(action string, age time.Duration) bool {
	return action == "example_comment" && age < time.Hour
}

func (recallWriter) Recall(context.Context, SentMessage) error { return nil }

// A connector is a Recaller by implementing it, and only then: one without it, and no connector at all, have nothing
// to recall.
func TestRecallerOf(t *testing.T) {
	f := &fake{id: "example", status: Status{State: StateOK}}
	if r, ok := RecallerOf(recallWriter{writer{f}}); !ok || !r.CanRecall("example_comment", time.Minute) || r.CanRecall("example_note", time.Minute) {
		t.Fatalf("a connector with Recall: %v %v", r, ok)
	}
	if _, ok := RecallerOf(writer{f}); ok {
		t.Error("a connector without Recall has it")
	}
	if _, ok := RecallerOf(plain{}); ok {
		t.Error("a connector with no actions has Recall")
	}
	if _, ok := RecallerOf(nil); ok {
		t.Error("no connector has Recall")
	}
}

// Recall is not a capability: a connector that has it registers as it did, declares nothing more, and its writes go
// through the gate and run as before.
func TestRecallerIsNoCapability(t *testing.T) {
	f := &fake{id: "example", status: Status{State: StateOK}}
	r := NewRegistry([]Rule{{Connector: "*", Action: "*", Decision: Allow}})
	c := recallWriter{writer{f}}
	if err := r.Add(c); err != nil {
		t.Fatalf("a Recaller does not register: %v", err)
	}
	if got, plainCaps := Capabilities(c), Capabilities(writer{f}); !slices.Equal(got, plainCaps) {
		t.Errorf("capabilities %v, without Recall %v", got, plainCaps)
	}
	if res := r.Call(context.Background(), "example_comment", json.RawMessage(`{"text":"hi"}`), nil); res.IsError || len(f.did) != 1 {
		t.Fatalf("a write of a Recaller: %+v, ran %v", res, f.did)
	}
}
