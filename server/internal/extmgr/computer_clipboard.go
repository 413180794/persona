package extmgr

// The Mac's clipboard for the app's VNC (#1343): GET reads it, POST {"text": …} puts text on it, through its node
// (computer.clipboard: pbcopy, pbpaste). For the text RFB's own clipboard cannot carry (anything outside Latin-1, and
// what a Chinese input method types, pasted with ⌘V). Only for a Mac whose screen the app may show (the plugin cua or
// ego on for it), and never logged beyond its length.

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
)

// clipboardBodyMost caps a POST body: the node takes at most 1 MiB of text (node.clipboardMost), with room for the
// JSON escapes around it.
const clipboardBodyMost = 4 << 20

func (h *Manager) computerClipboard(w http.ResponseWriter, r *http.Request, m *Machine) {
	if !h.portAllowed(m, screenPort) {
		writeErr(w, http.StatusNotFound, i18n.Err("hub-computer.err_clipboard_off"))
		return
	}
	if !m.isOnline() {
		writeErr(w, http.StatusConflict, i18n.Err("hub-computer.err_offline"))
		return
	}
	params := map[string]any{}
	if r.Method == http.MethodPost {
		var in struct {
			Text string `json:"text"`
		}
		if err := decodeJSONBody(w, r, &in, clipboardBodyMost); err != nil {
			h.writeComputerBodyErr(w, r, err)
			return
		}
		params["set"] = in.Text
		h.logf("[%s] clipboard set from the app (%d bytes)", m.cfg.ID, len(in.Text))
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	var out struct {
		Text string `json:"text"`
	}
	if err := m.callBuiltin(ctx, "computer.clipboard", params, &out); err != nil {
		if strings.Contains(err.Error(), "unknown method") {
			err = i18n.Err("hub-computer.err_clipboard_outdated")
		}
		h.writeComputerErr(w, r, http.StatusConflict, err)
		return
	}
	writeJSON(w, map[string]any{"text": out.Text})
}
