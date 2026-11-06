package extmgr

import (
	"errors"
	"net/http"
	"strings"

	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/i18n"
)

type removedMember struct {
	ID     string `json:"id"`
	Error  string `json:"error,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type uninstallOptions struct {
	keepData, members, cascade, owner bool
	failureStatus                     int
	actor                             string
}

// Uninstall members first. A partial failure retains the pack and names exactly what a retry still needs to remove.
func (h *Manager) uninstallPack(w http.ResponseWriter, r *http.Request, id string, o uninstallOptions) {
	ps := h.extensions
	p := ps.reg.Get(id)
	if p == nil {
		writeErrCode(w, http.StatusNotFound, "not_found", i18n.Err("hub-extension_uninstall.not_found"))
		return
	}
	if !o.members || !p.Full.IsPack() {
		if !h.takeDependents(w, r, id, o.cascade) {
			return
		}
		warnings, err := leftover(ps.reg.Uninstall(r.Context(), id, o.keepData, o.actor), nil)
		if err != nil {
			writeExtensionResponseError(w, r, o.failureStatus, "uninstall_failed", err)
			return
		}
		writeJSON(w, uninstalled(map[string]any{"ok": true, "removed_members": []string{}}, warnings))
		return
	}
	ids := ps.packMembers(p)
	// A market pack does not grant a device the CLI/admin right to remove a locally installed package.
	for _, mid := range ids {
		cur := ps.reg.Get(mid)
		if cur != nil && !o.owner && extension.FromMarket(cur.Record.Origin) == "" {
			writeErrCode(w, http.StatusForbidden, "this_machine_only", i18n.Err("hub-extension_uninstall.member_not_from_market", i18n.Args{"name": cur.Full.Name}))
			return
		}
	}
	if !h.takeDependentsOf(w, r, append([]string{id}, ids...), o.cascade) {
		return
	}
	removed := []string{}
	failed := []removedMember{}
	var warnings []string
	for _, mid := range ids {
		var err error
		if warnings, err = leftover(ps.reg.Uninstall(r.Context(), mid, o.keepData, o.actor), warnings); err != nil {
			failed = append(failed, removedMember{ID: mid, Error: err.Error()})
		} else {
			removed = append(removed, mid)
		}
	}
	if len(failed) == 0 {
		var err error
		if warnings, err = leftover(ps.reg.Uninstall(r.Context(), id, o.keepData, o.actor), warnings); err != nil {
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
		l := h.requestLang(r)
		if i18n.FromRequest(r) != "" {
			for i := range failed {
				failed[i].Error, failed[i].Detail = i18n.ExtensionFailureText(l, failed[i].Error, "extension-status.failed")
			}
		}
		sep := i18n.T(l, "hub-extension_uninstall.list_separator")
		removedText := i18n.T(l, "hub-extension_uninstall.none")
		if len(removed) > 0 {
			removedText = strings.Join(removed, sep)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		writeJSON(w, map[string]any{"ok": false, "code": "partial_uninstall", "error": i18n.T(l, "hub-extension_uninstall.partial", i18n.Args{"removed": removedText, "failed": strings.Join(names, sep)}), "removed_members": removed, "failed_members": failed, "remaining_members": remaining})
		return
	}
	writeJSON(w, uninstalled(map[string]any{"ok": true, "removed_members": removed}, warnings))
}

// leftover splits an uninstall's error: one that happened but left files (extension.LeftoverError) is a warning
// added to warnings, so the answer says it is uninstalled (a retry would only find no such extension) and names
// what to delete by hand; any other error is returned.
func leftover(err error, warnings []string) ([]string, error) {
	var left *extension.LeftoverError
	if errors.As(err, &left) {
		return append(warnings, left.Error()), nil
	}
	return warnings, err
}

// uninstalled is a successful uninstall's answer, with the warnings when there are any.
func uninstalled(out map[string]any, warnings []string) map[string]any {
	if len(warnings) > 0 {
		out["warnings"] = warnings
	}
	return out
}
