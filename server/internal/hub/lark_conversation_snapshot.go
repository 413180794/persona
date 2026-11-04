package hub

import (
	"net/http"

	"github.com/nwverse/persona/server/internal/lark"
)

// Account-bound reads are opt-in so existing clients keep their wire contract. The
// selected user, not another primary account, owns both the pin shelf and its pages.
// Check again after upstream I/O; never publish a page from a changed login.
func (h *Hub) larkConversationSnapshot(w http.ResponseWriter, r *http.Request, pinned bool) {
	w.Header().Set("Cache-Control", "no-store")
	account, err := h.larkOf(r).SelectedUserID(r.Context())
	if err != nil {
		writeLarkErr(w, err)
		return
	}
	changed := func() {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusConflict)
		writeJSON(w, map[string]any{"code": "lark_account_changed", "error": "飞书账号已切换，正在重新加载"})
	}
	q := r.URL.Query()
	if expected := q.Get("account_id"); expected != "" && expected != account {
		changed()
		return
	}
	var rows []lark.Conversation
	var next string
	if pinned {
		rows, err = h.larkOf(r).Pinned(r.Context())
	} else {
		rows, next, err = h.larkOf(r).Conversations(r.Context(), q.Get("scope"), q.Get("cursor"), atoi(q.Get("limit")))
	}
	if err != nil {
		writeLarkErr(w, err)
		return
	}
	after, err := h.larkOf(r).SelectedUserID(r.Context())
	if err != nil {
		writeLarkErr(w, err)
		return
	}
	if after != account {
		changed()
		return
	}
	if rows == nil {
		rows = []lark.Conversation{}
	}
	writeJSON(w, map[string]any{"account_id": account, "conversations": rows, "next_cursor": next})
}
