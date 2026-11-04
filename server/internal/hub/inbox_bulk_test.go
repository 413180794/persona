package hub

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/lark"
)

func bulkBody(done bool, targets ...inboxBulkTarget) string {
	var parts []string
	for _, t := range targets {
		parts = append(parts, fmt.Sprintf(`{"id":%q,"version":%q}`, t.ID, t.Version))
	}
	return fmt.Sprintf(`{"done":%t,"items":[%s]}`, done, strings.Join(parts, ","))
}

func listedVersion(h *Hub, id string) string {
	h.inbox.mu.Lock()
	defer h.inbox.mu.Unlock()
	return h.inbox.items[id].listed().Version
}

func bulkIDs(list []any) []string {
	var out []string
	for _, v := range list {
		switch x := v.(type) {
		case string:
			out = append(out, x)
		case map[string]any:
			out = append(out, fmt.Sprint(x["id"], ":", x["reason"]))
		}
	}
	return out
}

// Filing what the list shows: only the items named, each as it was listed; a repeat changes nothing; 撤销 brings
// back what was filed.
func TestInboxBulkDone(t *testing.T) {
	h := muteHub(t)
	mux := h.routes()
	for _, id := range []string{"a", "b", "c", "d"} {
		h.inbox.items[id] = mailItem(id, "shop@example.com", "示例商城")
		h.inbox.items[id].Last = "1"
	}
	earlier := time.Now().Add(-time.Hour)
	h.inbox.items["d"].done(earlier, "read")

	_, out := call(t, mux, "GET", "/api/v1/inbox", h.cfg.Token, "")
	if !strings.Contains(fmt.Sprint(out["items"]), "version:"+listedVersion(h, "a")) {
		t.Fatalf("listed items carry no version: %v", out["items"])
	}
	stale := listedVersion(h, "b")
	h.inbox.items["b"].Last = "2" // a new mail came into b after the list was drawn

	body := bulkBody(true, inboxBulkTarget{"a", listedVersion(h, "a")}, inboxBulkTarget{"b", stale},
		inboxBulkTarget{"d", listedVersion(h, "d")}, inboxBulkTarget{"x", "v"}, inboxBulkTarget{"a", ""})
	code, out := call(t, mux, "POST", "/api/v1/inbox/done", h.cfg.Token, body)
	if code != 200 {
		t.Fatalf("bulk done: %d %v", code, out)
	}
	changed, _ := out["changed"].([]any)
	if len(changed) != 1 || changed[0].(map[string]any)["id"] != "a" || changed[0].(map[string]any)["done_by"] != "user" {
		t.Fatalf("changed = %v", out["changed"])
	}
	if got := fmt.Sprint(bulkIDs(out["unchanged"].([]any))); got != "[d]" {
		t.Fatalf("unchanged = %s", got)
	}
	if got := fmt.Sprint(bulkIDs(out["skipped"].([]any))); got != "[b:changed x:missing]" {
		t.Fatalf("skipped = %s", got)
	}
	if h.inbox.items["b"].DoneAt != nil || h.inbox.items["c"].DoneAt != nil {
		t.Fatal("an item outside the request, or with new mail, was filed")
	}
	if d := h.inbox.items["d"]; !d.DoneAt.Equal(earlier) || d.DoneBy != "read" {
		t.Fatalf("an item already done was filed again: %v %s", d.DoneAt, d.DoneBy)
	}

	// The same request again (a retry, another device): nothing moves, a keeps when it was filed.
	at := *h.inbox.items["a"].DoneAt
	_, out = call(t, mux, "POST", "/api/v1/inbox/done", h.cfg.Token, body)
	if len(out["changed"].([]any)) != 0 || fmt.Sprint(bulkIDs(out["unchanged"].([]any))) != "[a d]" || !h.inbox.items["a"].DoneAt.Equal(at) {
		t.Fatalf("a repeat changed something: %v", out)
	}

	// 撤销: what was filed comes back.
	_, out = call(t, mux, "POST", "/api/v1/inbox/done", h.cfg.Token, bulkBody(false, inboxBulkTarget{"a", ""}))
	if len(out["changed"].([]any)) != 1 || !h.inbox.items["a"].open() || h.inbox.items["a"].DoneBy != "" {
		t.Fatalf("undo: %v", out)
	}
}

func TestInboxBulkDoneBadRequests(t *testing.T) {
	h := muteHub(t)
	mux := h.routes()
	if code, _ := call(t, mux, "POST", "/api/v1/inbox/done", h.cfg.Token, `{"items":[{"id":"a"}]}`); code != 400 {
		t.Fatalf("no done: %d", code)
	}
	var many []inboxBulkTarget
	for i := 0; i <= inboxBulkMax; i++ {
		many = append(many, inboxBulkTarget{ID: fmt.Sprint(i)})
	}
	if code, _ := call(t, mux, "POST", "/api/v1/inbox/done", h.cfg.Token, bulkBody(true, many...)); code != 413 {
		t.Fatalf("too many: %d", code)
	}
	if code, out := call(t, mux, "POST", "/api/v1/inbox/done", h.cfg.Token, `{"done":true,"items":[]}`); code != 200 || len(out["changed"].([]any)) != 0 {
		t.Fatalf("nothing to do: %d %v", code, out)
	}
}

// New Feishu messages around a bulk 已处理: merged into the item before it (its version moved), the item stays
// open; arriving while the collection reads them (the item filed meanwhile), they open a new item.
func TestInboxBulkDoneWithNewMessages(t *testing.T) {
	h := muteHub(t)
	now := time.Now()
	chat := []lark.Message{larkMsg("11", "张三", false, now.Add(-3*time.Minute)), larkMsg("12", "张三", false, now.Add(-2*time.Minute))}
	fetch := func(c lark.Conversation, n int) ([]lark.Message, error) { return chat[max(0, len(chat)-n):], nil }
	conv := lark.Conversation{ID: "f1", Name: "张三", Unread: 2, UpdatedAt: now.Add(-2 * time.Minute)}
	h.inbox.applyLark([]lark.Conversation{conv}, fetch, now, testUserZone)
	id := "lark:f1:11"
	seenVersion := listedVersion(h, id) // what the phone drew

	// A message merges in before the request: not filed.
	chat = append(chat, larkMsg("13", "张三", false, now.Add(-time.Minute)))
	conv.Unread, conv.UpdatedAt = 3, now.Add(-time.Minute)
	h.inbox.applyLark([]lark.Conversation{conv}, fetch, now, testUserZone)
	res := h.setInboxDoneBulk(context.Background(), []inboxBulkTarget{{id, seenVersion}}, true, now)
	if len(res.Changed) != 0 || len(res.Skipped) != 1 || res.Skipped[0].Reason != bulkSkipChanged || !h.inbox.items[id].open() {
		t.Fatalf("an item with a message the user had not seen was filed: %+v", res)
	}

	// Filed with what it shows now, while the next collection reads a new message: that message opens a new item.
	current := listedVersion(h, id)
	chat = append(chat, larkMsg("14", "李四", false, now))
	conv.Unread, conv.UpdatedAt = 4, now
	var once sync.Once
	filing := func(c lark.Conversation, n int) ([]lark.Message, error) {
		once.Do(func() {
			if r := h.setInboxDoneBulk(context.Background(), []inboxBulkTarget{{id, current}}, true, now); len(r.Changed) != 1 {
				t.Errorf("not filed: %+v", r)
			}
		})
		return fetch(c, n)
	}
	h.inbox.applyLark([]lark.Conversation{conv}, filing, now, testUserZone)
	old := h.inbox.items[id]
	if old.open() || old.Last != "13" {
		t.Fatalf("the filed item took the new message: open=%v last=%s", old.open(), old.Last)
	}
	fresh := h.inbox.items["lark:f1:14"]
	if fresh == nil || !fresh.open() || fresh.Lark.StartPosition != "14" || strings.Contains(fresh.Context, "hi 13") {
		t.Fatalf("the new message did not open a new item: %+v", fresh)
	}
}

// Concurrent bulk requests over the same items: each item is filed once, by exactly one of them.
func TestInboxBulkDoneConcurrent(t *testing.T) {
	h := muteHub(t)
	var targets []inboxBulkTarget
	for i := 0; i < 50; i++ {
		id := fmt.Sprint("m", i)
		h.inbox.items[id] = mailItem(id, "shop@example.com", "示例商城")
		targets = append(targets, inboxBulkTarget{id, listedVersion(h, id)})
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	filed := map[string]int{}
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := h.setInboxDoneBulk(context.Background(), targets, true, time.Now())
			mu.Lock()
			defer mu.Unlock()
			for _, it := range res.Changed {
				filed[it.ID]++
			}
		}()
	}
	wg.Wait()
	if len(filed) != 50 {
		t.Fatalf("filed %d of 50", len(filed))
	}
	for id, n := range filed {
		if n != 1 {
			t.Fatalf("%s filed %d times", id, n)
		}
	}
}
