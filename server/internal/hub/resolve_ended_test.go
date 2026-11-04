package hub

import (
	"context"
	"net/http"
	"testing"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

// Answering a card that was answered already (its event did not reach this app) gets the answer it was given, not an
// error; one that expired or was withdrawn still fails, saying how it ended.
func TestResolveEndedInteraction(t *testing.T) {
	h := newTestHub(t)
	st := &tokenStore{memStore: *newMemStore(), rows: map[string]db.DeviceToken{}, rev: map[string]bool{}}
	h.store = st
	mux := h.routes()
	tok := h.cfg.Token

	ix := proto.Interaction{ID: "ix_q", MachineID: "server", SessionID: "s1", Kind: proto.IxQuestion,
		Questions: []proto.Question{{Question: "发吗？", Options: []proto.QuestionOption{{Label: "发"}, {Label: "不发"}}}}}
	done := make(chan proto.Resolution, 1)
	go func() { r, _ := h.waitInteraction(context.Background(), ix); done <- r }()
	waitPending(t, h, 1)

	if code, out := call(t, mux, "POST", "/api/v1/interactions/ix_q/resolve", tok, `{"answers":{"发吗？":"发"}}`); code != 200 || out["already"] != nil {
		t.Fatalf("first answer: %d %v", code, out)
	}
	if r := <-done; r.Answers["发吗？"] != "发" {
		t.Fatalf("the waiting tool got %+v", r)
	}

	// Sent again (with another pick even): the answer that took effect comes back.
	code, out := call(t, mux, "POST", "/api/v1/interactions/ix_q/resolve", tok, `{"answers":{"发吗？":"不发"}}`)
	if code != 200 || out["ok"] != true || out["already"] != true || out["status"] != "resolved" {
		t.Fatalf("second answer: %d %v", code, out)
	}
	res, _ := out["resolution"].(map[string]any)
	if ans, _ := res["answers"].(map[string]any); ans["发吗？"] != "发" {
		t.Fatalf("second answer should carry the first one: %v", out)
	}

	// Handing back a card that was answered is answered too; one that expired is gone, and says so.
	if code, out := call(t, mux, "POST", "/api/v1/interactions/ix_q/resolve", tok, `{"dismiss":true}`); code != 200 || out["status"] != "resolved" {
		t.Fatalf("dismissing an answered card: %d %v", code, out)
	}
	st.status["ix_old"], st.status["ix_back"] = "expired", "dismissed"
	if code, out := call(t, mux, "POST", "/api/v1/interactions/ix_old/resolve", tok, `{"answers":{"a":"b"}}`); code != http.StatusGone || out["status"] != "expired" || out["code"] != "gone" {
		t.Fatalf("expired: %d %v", code, out)
	}
	if code, out := call(t, mux, "POST", "/api/v1/interactions/ix_back/resolve", tok, `{"dismiss":true}`); code != 200 || out["already"] != true || out["status"] != "dismissed" {
		t.Fatalf("handed back twice: %d %v", code, out)
	}
	if code, out := call(t, mux, "POST", "/api/v1/interactions/ix_back/resolve", tok, `{"answers":{"a":"b"}}`); code != http.StatusGone || out["status"] != "dismissed" {
		t.Fatalf("answering a card handed back: %d %v", code, out)
	}
	if code, out := call(t, mux, "POST", "/api/v1/interactions/ix_none/resolve", tok, `{"allow":true}`); code != http.StatusGone || out["status"] != nil {
		t.Fatalf("unknown: %d %v", code, out)
	}
}
