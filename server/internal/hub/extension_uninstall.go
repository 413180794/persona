package hub

import (
	"errors"
	"net/http"
	"strings"

	"github.com/nwverse/persona/server/internal/extension"
)

type removedMember struct {
	ID    string `json:"id"`
	Error string `json:"error,omitempty"`
}

type uninstallOptions struct {
	keepData, members, cascade, owner bool
	failureStatus                     int
	actor                             string
}

// Uninstall members first. A partial failure retains the pack and names exactly what a retry still needs to remove.
func (h *Hub) uninstallPack(w http.ResponseWriter, r *http.Request, id string, o uninstallOptions) {
	ps := h.extensions
	p := ps.reg.Get(id)
	if p == nil {
		writeErrCode(w, http.StatusNotFound, "not_found", errors.New("没有这个扩展"))
		return
	}
	if !o.members || !p.Full.IsPack() {
		if !h.takeDependents(w, r, id, o.cascade) {
			return
		}
		if err := ps.reg.Uninstall(r.Context(), id, o.keepData, o.actor); err != nil {
			writeErrCode(w, o.failureStatus, "uninstall_failed", err)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "removed_members": []string{}})
		return
	}
	ids := ps.packMembers(p)
	// A market pack does not grant a device the CLI/admin right to remove a locally installed package.
	for _, mid := range ids {
		cur := ps.reg.Get(mid)
		if cur != nil && !o.owner && extension.FromMarket(cur.Record.Origin) == "" {
			writeErrCode(w, http.StatusForbidden, "this_machine_only", errors.New("成员「"+cur.Full.Name+"」不是从市场安装的，请在 hub 机器或管理界面卸载"))
			return
		}
	}
	if !h.takeDependentsOf(w, r, append([]string{id}, ids...), o.cascade) {
		return
	}
	removed := []string{}
	failed := []removedMember{}
	for _, mid := range ids {
		if err := ps.reg.Uninstall(r.Context(), mid, o.keepData, o.actor); err != nil {
			failed = append(failed, removedMember{ID: mid, Error: err.Error()})
		} else {
			removed = append(removed, mid)
		}
	}
	if len(failed) == 0 {
		if err := ps.reg.Uninstall(r.Context(), id, o.keepData, o.actor); err != nil {
			failed = append(failed, removedMember{ID: id, Error: err.Error()})
		}
	}
	if len(failed) > 0 {
		var names []string
		for _, f := range failed {
			names = append(names, f.ID)
		}
		remaining := ps.packMembers(p)
		if remaining == nil {
			remaining = []string{}
		}
		removedText := "无"
		if len(removed) > 0 {
			removedText = strings.Join(removed, "、")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		writeJSON(w, map[string]any{"ok": false, "code": "partial_uninstall", "error": "卸载未完成。已卸载成员：" + removedText + "；未卸载：" + strings.Join(names, "、") + "。合集仍保留，可以重试。", "removed_members": removed, "failed_members": failed, "remaining_members": remaining})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "removed_members": removed})
}
