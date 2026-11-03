package hub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/lark"
	"github.com/nwverse/persona/server/internal/prompts"
)

// The snapshot goes with every message now: it says the time of the message, and a slow source doesn't hold the
// message up once there is an earlier snapshot to go on.
func TestSnapshotIsNowAndDoesNotWait(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(4 * time.Second) }))
	t.Cleanup(slow.Close)
	h := snapshotHub(t, false)
	h.lark = lark.New(lark.Config{BaseURL: slow.URL, Token: "t"})
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
