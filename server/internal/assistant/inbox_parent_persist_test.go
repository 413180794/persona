package assistant

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// Setting a parent that cannot be stored answers 500 and leaves the old parent; clearing one is symmetric. After
// recovery the same requests move it and the move is stored.
func TestSetInboxParentWriteFailure(t *testing.T) {
	h, st := newBulkFailHub(t)
	mux := h.routes()
	seedFailItems(h, "a", "b")
	put := func(body string) int {
		code, _ := inboxCall(t, mux, "PUT", "/api/v1/inbox/b/parent", body)
		return code
	}
	st.failAll = true
	if code := put(`{"kind":"inbox","id":"a"}`); code != http.StatusInternalServerError {
		t.Fatalf("link with the store down: %d, want 500", code)
	}
	if p := h.inbox.items["b"].Parent; p != nil || st.hasRow("b") {
		t.Fatalf("the failed link moved the item in memory or wrote a row: %+v", p)
	}
	st.failAll = false
	if code := put(`{"kind":"inbox","id":"a"}`); code != 200 || h.inbox.items["b"].Parent == nil || !st.saved("b") {
		t.Fatalf("link retry: code=%d parent=%v saved=%v", code, h.inbox.items["b"].Parent, st.saved("b"))
	}
	st.failAll = true
	if code := put(`null`); code != http.StatusInternalServerError {
		t.Fatalf("clear with the store down: %d, want 500", code)
	}
	if h.inbox.items["b"].Parent == nil {
		t.Fatal("the failed clear took the item off in memory")
	}
	st.failAll = false
	if code := put(`null`); code != 200 || h.inbox.items["b"].Parent != nil {
		t.Fatalf("clear retry: code=%d parent=%+v", code, h.inbox.items["b"].Parent)
	}
}

// waitForItem polls until landed holds; landed nil waits for generation 2, which reveals two same-valued writes.
func waitForItem(t *testing.T, h *inboxHub, id string, landed func(*InboxItem) bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		h.inbox.mu.Lock()
		it := h.inbox.items[id]
		good := (landed == nil && h.inbox.mark[id] >= 2) || (landed != nil && it != nil && landed(it))
		h.inbox.mu.Unlock()
		if good {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the concurrent request never landed on the item")
}

// storedParent reads the parent id a row holds in the store.
func storedParent(st *inboxMemStore, id string) string {
	st.imu.Lock()
	defer st.imu.Unlock()
	var row struct {
		Parent *struct{ ID string } `json:"parent"`
	}
	if err := json.Unmarshal(st.rows[id], &row); err != nil || row.Parent == nil {
		return ""
	}
	return row.Parent.ID
}

// A rollback must not touch a newer item that took the id while the write was out; the loser pointer our request
// moved is left holding the failed write.
func TestInboxRollbackSkipsReplacedItem(t *testing.T) {
	for _, tc := range []struct {
		name, id, method, path, body string
		moved                        func(*InboxItem) bool
	}{
		{"parent", "b", "PUT", "/api/v1/inbox/b/parent", `{"kind":"inbox","id":"a"}`, func(it *InboxItem) bool { return it.Parent != nil }},
		{"done", "s", "POST", "/api/v1/inbox/s/done", `{"done":true}`, func(it *InboxItem) bool { return it.DoneAt != nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, st := newBulkFailHub(t)
			mux := h.routes()
			seedFailItems(h, "a", "b", "s")
			old := h.inbox.items[tc.id]
			st.failAll = true
			st.failHook = func(id string) {
				h.inbox.mu.Lock()
				h.inbox.items[id] = mailItem(id, "shop@example.com", "Shop")
				h.inbox.mu.Unlock()
			}
			if code, _ := inboxCall(t, mux, tc.method, tc.path, tc.body); code != 500 {
				t.Fatalf("the failed request: %d, want 500", code)
			}
			if newer := h.inbox.items[tc.id]; !newer.open() || tc.moved(newer) {
				t.Fatalf("the rollback wrote onto the replacement item: %+v", newer)
			}
			if !tc.moved(old) {
				t.Fatal("the rollback rewound the loser pointer instead of leaving the failed write on it")
			}
		})
	}
}

// In a bulk request the failed ids that were replaced or forgotten while writes were out are reported failed and the
// newer item (or the id's absence) is left untouched; their loser pointers are not rewound and the other id is filed.
func TestInboxBulkRollbackSkipsMovedItems(t *testing.T) {
	h, st := newBulkFailHub(t)
	mux := h.routes()
	seedFailItems(h, "a", "b", "c")
	oldB, oldC := h.inbox.items["b"], h.inbox.items["c"]
	st.fail = map[string]bool{"b": true, "c": true}
	st.failHook = func(id string) {
		h.inbox.mu.Lock()
		if id == "b" {
			h.inbox.items["b"] = mailItem(id, "shop@example.com", "Shop") // replaced
		} else {
			delete(h.inbox.items, id) // forgotten
		}
		h.inbox.mu.Unlock()
	}
	_, out := inboxCall(t, mux, "POST", "/api/v1/inbox/bulk", `{"action":"done","ids":["a","b","c"]}`)
	if got := jsonIDs(out["failed"]); len(got) != 2 || got[0] != "b" || got[1] != "c" {
		t.Fatalf("failed = %v, want [b c]", got)
	}
	if got := jsonIDs(out["ids"]); len(got) != 1 || got[0] != "a" {
		t.Fatalf("ids = %v, want [a]", got)
	}
	if newer := h.inbox.items["b"]; newer == nil || !newer.open() || newer == oldB {
		t.Fatalf("the replacement for b was filed, lost or never installed: %+v", newer)
	}
	if h.inbox.items["c"] != nil || h.inbox.items["a"].open() {
		t.Fatalf("c was recreated or a stayed open: c=%+v a-open=%v", h.inbox.items["c"], h.inbox.items["a"].open())
	}
	if oldB.DoneAt == nil || oldC.DoneAt == nil {
		t.Fatal("the rollback rewound the loser pointers instead of leaving the failed marks on them")
	}
}

// A failed write must not rewind a newer state another request wrote on the same pointer while that request was
// queued on b.saving — including two same-valued writes (two unlinks, two reopens), told apart by the generation.
func TestInboxFailedWriteKeepsConcurrentRequest(t *testing.T) {
	dismissed := func(it *InboxItem) bool { return it.DoneBy == "dismissed" }
	for _, tc := range []struct {
		name, failID, firstMethod, firstPath, firstBody, secondMethod, secondPath, secondBody string
		ids                                                                                   []string
		seed                                                                                  func(h *inboxHub)
		landed                                                                                func(*InboxItem) bool // nil: wait for generation 2 (same value twice)
		bulk                                                                                  bool
		check                                                                                 func(t *testing.T, h *inboxHub)
	}{
		{"parent", "b", "PUT", "/api/v1/inbox/b/parent", `{"kind":"inbox","id":"a"}`,
			"PUT", "/api/v1/inbox/b/parent", `{"kind":"inbox","id":"c"}`, []string{"a", "b", "c"}, nil,
			func(it *InboxItem) bool { return it.Parent != nil && it.Parent.ID == "c" }, false,
			func(t *testing.T, h *inboxHub) {
				if p := h.inbox.items["b"].Parent; p == nil || p.ID != "c" || storedParent(h.st, "b") != "c" {
					t.Fatalf("parent c did not survive: memory=%+v row=%q", p, storedParent(h.st, "b"))
				}
			}},
		{"done", "s", "POST", "/api/v1/inbox/s/done", `{"done":true}`,
			"POST", "/api/v1/inbox/s/done", `{"done":true,"dismiss":true}`, []string{"s"}, nil, dismissed, false,
			func(t *testing.T, h *inboxHub) {
				if it := h.inbox.items["s"]; it.open() || it.DoneBy != "dismissed" || h.st.done["s"] == nil {
					t.Fatalf("the dismissed file did not survive: %+v", it)
				}
			}},
		{"bulk", "b", "POST", "/api/v1/inbox/bulk", `{"action":"done","ids":["a","b"]}`,
			"POST", "/api/v1/inbox/b/done", `{"done":true,"dismiss":true}`, []string{"a", "b"}, nil, dismissed, true,
			func(t *testing.T, h *inboxHub) {
				for _, id := range []string{"a", "b"} {
					if h.inbox.items[id].open() || h.st.done[id] == nil {
						t.Fatalf("%s did not stay filed", id)
					}
				}
				if h.inbox.items["b"].DoneBy != "dismissed" {
					t.Fatalf("b holds %q, want dismissed", h.inbox.items["b"].DoneBy)
				}
			}},
		{"unlink", "b", "PUT", "/api/v1/inbox/b/parent", `null`,
			"PUT", "/api/v1/inbox/b/parent", `null`, []string{"a", "b"},
			func(h *inboxHub) {
				h.inbox.items["b"].Parent = &InboxParent{Kind: ParentInbox, ID: "a", By: ParentByUser}
			},
			nil, false, func(t *testing.T, h *inboxHub) {
				if h.inbox.items["b"].Parent != nil || storedParent(h.st, "b") != "" {
					t.Fatalf("the unlink was rewound: memory=%+v row=%q", h.inbox.items["b"].Parent, storedParent(h.st, "b"))
				}
			}},
		{"reopen", "s", "POST", "/api/v1/inbox/s/done", `{"done":false}`,
			"POST", "/api/v1/inbox/s/done", `{"done":false}`, []string{"s"},
			func(h *inboxHub) {
				t0 := time.Time{}
				h.inbox.items["s"].DoneAt, h.inbox.items["s"].DoneBy = &t0, "user"
			}, nil, false, func(t *testing.T, h *inboxHub) {
				if !h.inbox.items["s"].open() || h.st.done["s"] != nil {
					t.Fatalf("the reopen was rewound: %+v", h.inbox.items["s"])
				}
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, st := newBulkFailHub(t)
			mux := h.routes()
			seedFailItems(h, tc.ids...)
			if tc.seed != nil {
				tc.seed(h)
			}
			second := make(chan int, 1)
			st.failOnce = map[string]bool{tc.failID: true}
			st.failHook = func(id string) {
				go func() {
					code, _ := inboxCall(t, mux, tc.secondMethod, tc.secondPath, tc.secondBody)
					second <- code
				}()
				if tc.landed != nil {
					waitForItem(t, h, id, tc.landed)
				} else {
					waitForItem(t, h, id, nil)
				}
			}
			code, out := inboxCall(t, mux, tc.firstMethod, tc.firstPath, tc.firstBody)
			if tc.bulk {
				if got := jsonIDs(out["failed"]); len(got) != 1 || got[0] != "b" || jsonIDs(out["ids"])[0] != "a" {
					t.Fatalf("bulk answer = %v, want failed [b] and changed [a]", out)
				}
			} else if code != 500 {
				t.Fatalf("the failed request: %d, want 500", code)
			}
			if code := <-second; code != 200 {
				t.Fatalf("the concurrent request: %d, want 200", code)
			}
			tc.check(t, h)
		})
	}
}
