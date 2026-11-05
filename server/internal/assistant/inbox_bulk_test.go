package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func bulkBody(done bool, targets ...inboxBulkTarget) string {
	var parts []string
	for _, t := range targets {
		parts = append(parts, fmt.Sprintf(`{"id":%q,"version":%q}`, t.ID, t.Version))
	}
	return fmt.Sprintf(`{"done":%t,"items":[%s]}`, done, strings.Join(parts, ","))
}

func listedVersion(h *inboxHub, id string) string {
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

	_, out := inboxCall(t, mux, "GET", "/api/v1/inbox", "")
	if !strings.Contains(fmt.Sprint(out["items"]), "version:"+listedVersion(h, "a")) {
		t.Fatalf("listed items carry no version: %v", out["items"])
	}
	stale := listedVersion(h, "b")
	h.inbox.items["b"].Last = "2" // a new mail came into b after the list was drawn

	body := bulkBody(true, inboxBulkTarget{"a", listedVersion(h, "a")}, inboxBulkTarget{"b", stale},
		inboxBulkTarget{"d", listedVersion(h, "d")}, inboxBulkTarget{"x", "v"}, inboxBulkTarget{"a", ""})
	code, out := inboxCall(t, mux, "POST", "/api/v1/inbox/done", body)
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
	_, out = inboxCall(t, mux, "POST", "/api/v1/inbox/done", body)
	if len(out["changed"].([]any)) != 0 || fmt.Sprint(bulkIDs(out["unchanged"].([]any))) != "[a d]" || !h.inbox.items["a"].DoneAt.Equal(at) {
		t.Fatalf("a repeat changed something: %v", out)
	}

	// 撤销: what was filed comes back.
	_, out = inboxCall(t, mux, "POST", "/api/v1/inbox/done", bulkBody(false, inboxBulkTarget{"a", ""}))
	if len(out["changed"].([]any)) != 1 || !h.inbox.items["a"].open() || h.inbox.items["a"].DoneBy != "" {
		t.Fatalf("undo: %v", out)
	}
}

func TestInboxBulkDoneBadRequests(t *testing.T) {
	h := muteHub(t)
	mux := h.routes()
	if code, _ := inboxCall(t, mux, "POST", "/api/v1/inbox/done", `{"items":[{"id":"a"}]}`); code != 400 {
		t.Fatalf("no done: %d", code)
	}
	var many []inboxBulkTarget
	for i := 0; i <= inboxBulkMax; i++ {
		many = append(many, inboxBulkTarget{ID: fmt.Sprint(i)})
	}
	if code, _ := inboxCall(t, mux, "POST", "/api/v1/inbox/done", bulkBody(true, many...)); code != 413 {
		t.Fatalf("too many: %d", code)
	}
	if code, out := inboxCall(t, mux, "POST", "/api/v1/inbox/done", `{"done":true,"items":[]}`); code != 200 || len(out["changed"].([]any)) != 0 {
		t.Fatalf("nothing to do: %d %v", code, out)
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

// POST /api/v1/inbox/bulk with a filter files what the list as of as_of showed under it: an item that arrived after
// (a new one, or new mail into a listed one) stays open; 忽略 is done_by=dismissed; the undo it answers brings back
// exactly what it filed.
func TestInboxBulkFilterAsOf(t *testing.T) {
	h := muteHub(t)
	mux := h.routes()
	for _, id := range []string{"a", "b", "c"} {
		h.inbox.items[id] = mailItem(id, "shop@example.com", "示例商城")
		h.inbox.items[id].Last, h.inbox.items[id].Tier = "1", TierFYI
	}
	h.inbox.items["c"].Tier = TierAction // another section
	h.inbox.items["l"] = &InboxItem{ID: "l", Source: "lark", Kind: "dm", Account: "feishu", Last: "9", Tier: TierFYI, UpdatedAt: time.Now()}

	_, list := inboxCall(t, mux, "GET", "/api/v1/inbox", "")
	asOf, _ := list["as_of"].(string)
	if asOf == "" {
		t.Fatalf("the list says no as_of: %v", list)
	}
	time.Sleep(2 * time.Millisecond)
	h.inbox.mu.Lock()
	h.inbox.items["b"].Last = "2" // new mail into b after the list
	h.inbox.items["n"] = mailItem("n", "new@example.com", "新的")
	h.inbox.items["n"].Last, h.inbox.items["n"].Tier = "1", TierFYI // a new item, not listed yet
	h.inbox.mu.Unlock()
	before := len(h.in.changes())

	body := fmt.Sprintf(`{"action":"dismiss","filter":{"source":"mail","tier":"fyi"},"as_of":%q}`, asOf)
	code, out := inboxCall(t, mux, "POST", "/api/v1/inbox/bulk", body)
	if code != 200 || out["action"] != "dismiss" {
		t.Fatalf("bulk dismiss: %d %v", code, out)
	}
	if got := fmt.Sprint(out["ids"]); got != "[a]" {
		t.Fatalf("filed %s, want only a", got)
	}
	if got := fmt.Sprint(bulkIDs(out["skipped"].([]any))); got != "[b:changed n:changed]" {
		t.Fatalf("skipped = %s", got)
	}
	if a := h.inbox.items["a"]; a.open() || a.DoneBy != "dismissed" {
		t.Fatalf("a: %v %s", a.DoneAt, a.DoneBy)
	}
	for _, id := range []string{"b", "c", "l", "n"} {
		if !h.inbox.items[id].open() {
			t.Fatalf("%s was filed", id)
		}
	}
	if !h.st.saved("a") || len(h.in.changes()) != before+1 {
		t.Fatalf("filed but not saved and announced once: saved %v, %d announcements", h.st.ids, len(h.in.changes())-before)
	}

	undo, _ := out["undo"].(map[string]any)
	raw, _ := json.Marshal(undo)
	if code, out := inboxCall(t, mux, "POST", "/api/v1/inbox/bulk", string(raw)); code != 200 || fmt.Sprint(out["ids"]) != "[a]" || out["undo"] != nil {
		t.Fatalf("undo %s: %d %v", raw, code, out)
	}
	if a := h.inbox.items["a"]; !a.open() || a.DoneBy != "" {
		t.Fatalf("a after the undo: %v %s", a.DoneAt, a.DoneBy)
	}

	// The same list again, everything 已处理: b and n still did not arrive by then.
	body = fmt.Sprintf(`{"action":"done","filter":{},"as_of":%q}`, asOf)
	_, out = inboxCall(t, mux, "POST", "/api/v1/inbox/bulk", body)
	if got := fmt.Sprint(out["ids"]); got != "[a c l]" || h.inbox.items["a"].DoneBy != "user" {
		t.Fatalf("all done: %s", got)
	}
}

// ids file what they name (no version check), dismissed items list as such, and the single item's 忽略 matches.
func TestInboxBulkIDsAndSingleDismiss(t *testing.T) {
	h := muteHub(t)
	mux := h.routes()
	for _, id := range []string{"a", "b"} {
		h.inbox.items[id] = mailItem(id, "shop@example.com", "示例商城")
	}
	if code, out := inboxCall(t, mux, "POST", "/api/v1/inbox/bulk", `{"action":"dismiss","ids":["a","x"]}`); code != 200 ||
		fmt.Sprint(out["ids"]) != "[a]" || fmt.Sprint(bulkIDs(out["skipped"].([]any))) != "[x:missing]" {
		t.Fatalf("ids: %d %v", code, out)
	}
	if code, out := inboxCall(t, mux, "POST", "/api/v1/inbox/b/done", `{"done":true,"dismiss":true}`); code != 200 || out["done_by"] != "dismissed" {
		t.Fatalf("one item dismissed: %d %v", code, out)
	}
	if code, out := inboxCall(t, mux, "POST", "/api/v1/inbox/bulk", `{"action":"done","ids":["a","b"]}`); code != 200 ||
		fmt.Sprint(out["unchanged"]) != "[a b]" || h.inbox.items["a"].DoneBy != "dismissed" {
		t.Fatalf("filing dismissed items again: %v", out)
	}
}

func TestInboxBulkBadRequests(t *testing.T) {
	h := muteHub(t)
	mux := h.routes()
	for _, body := range []string{
		`{"ids":["a"]}`, // no action
		`{"action":"archive","ids":["a"]}`,
		`{"action":"done"}`, // neither ids nor a filter
		`{"action":"done","ids":["a"],"filter":{}}`,
		`{"action":"done","filter":{}}`,                                  // no as_of
		`{"action":"reopen","filter":{},"as_of":"2026-11-05T10:00:00Z"}`, // reopen takes ids
		`{"action":"done","filter":{"tier":"urgent"},"as_of":"2026-11-05T10:00:00Z"}`,
		`not json`,
	} {
		if code, _ := inboxCall(t, mux, "POST", "/api/v1/inbox/bulk", body); code != 400 {
			t.Errorf("%s: %d, want 400", body, code)
		}
	}
	ids := make([]string, inboxBulkMax+1)
	for i := range ids {
		ids[i] = fmt.Sprint(i)
	}
	raw, _ := json.Marshal(map[string]any{"action": "done", "ids": ids})
	if code, _ := inboxCall(t, mux, "POST", "/api/v1/inbox/bulk", string(raw)); code != 413 {
		t.Fatalf("too many: %d", code)
	}
}

// Rows stored before arrivals were stamped count as arrived when they were last updated.
func TestInboxArrivedOfLegacyRows(t *testing.T) {
	it := &InboxItem{ID: "a", Last: "1"}
	if !it.arrivedAfter(time.Now()) {
		t.Fatal("an unstamped item counts as arrived")
	}
	now := time.Now()
	it.arrived(now)
	if it.arrivedAfter(now) || !it.arrivedAfter(now.Add(-time.Second)) {
		t.Fatal("stamped arrival")
	}
	it.arrived(now.Add(time.Hour)) // the same Last: kept
	if !it.ArrivedAt.Equal(now) {
		t.Fatal("re-stamped without anything new")
	}
	if l := it.listed(); l.ArrivedOf != "" || !l.ArrivedAt.IsZero() {
		t.Fatal("bookkeeping listed")
	}
}
