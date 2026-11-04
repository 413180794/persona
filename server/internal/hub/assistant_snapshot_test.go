package hub

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/prompts"
)

// The snapshot goes with every message now: it says the time of the message, and a slow source doesn't hold the
// message up once there is an earlier snapshot to go on.
func TestSnapshotIsNowAndDoesNotWait(t *testing.T) {
	h := snapshotHub(t, false)
	if err := h.addConnector(slowSnapshot{}); err != nil {
		t.Fatal(err)
	}
	loc := h.userLocation()
	old := time.Now().Add(-10 * time.Minute).In(loc)
	h.snap = &prompts.Snapshot{Now: old, Zone: loc.String(), Chats: &prompts.SnapshotChats{AtMe: []prompts.SnapshotChat{{Name: "发布群", Unread: 2}}}}
	h.snapAt = time.Now().Add(-10 * time.Minute)

	start := time.Now()
	got := h.assistantSnapshot(context.Background())
	if d := time.Since(start); d > snapshotWait+time.Second {
		t.Fatalf("waited %v for a slow source", d)
	}
	now := time.Now().In(loc)
	if !strings.Contains(got, "- 现在："+now.Format("2006-01-02")+" ") || !strings.Contains(got, now.Format("15:04")+"（"+loc.String()+"）") {
		t.Fatalf("not the time now (%s):\n%s", now.Format("15:04"), got)
	}
	if !strings.Contains(got, "发布群（2 条）") {
		t.Fatalf("the earlier snapshot's chats are gone:\n%s", got)
	}
}

// slowSnapshot is a connector whose live state takes longer than the snapshot waits.
type slowSnapshot struct{}

func (slowSnapshot) ID() string   { return "slow" }
func (slowSnapshot) Name() string { return "Slow" }
func (slowSnapshot) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapSnapshot}}
}
func (slowSnapshot) Snapshot(ctx context.Context, _ string) (connector.SnapshotData, error) {
	select {
	case <-ctx.Done():
		return connector.SnapshotData{}, ctx.Err()
	case <-time.After(4 * time.Second):
	}
	return connector.SnapshotData{Chats: &connector.SnapshotChats{}}, nil
}
