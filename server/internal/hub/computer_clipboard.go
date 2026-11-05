package hub

// The Mac's clipboard for the app's VNC (#1343): GET reads it, POST {"text": …} puts text on it, through its node
// (computer.clipboard: pbcopy, pbpaste). For the text RFB's own clipboard cannot carry (anything outside Latin-1, and
// what a Chinese input method types, pasted with ⌘V). Only for a Mac whose screen the app may show (the plugin cua or
// ego on for it), and never logged beyond its length.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// clipboardBodyMost caps a POST body: the node takes at most 1 MiB of text (node.clipboardMost), with room for the
// JSON escapes around it.
const clipboardBodyMost = 4 << 20

func (h *Hub) computerClipboard(w http.ResponseWriter, r *http.Request, m *Machine) {
	if !h.portAllowed(m, screenPort) {
		writeErr(w, http.StatusNotFound, errors.New("这台 Mac 没有打开操控电脑或浏览器扩展"))
		return
	}
	if !m.isOnline() {
		writeErr(w, http.StatusConflict, errors.New("机器离线"))
		return
	}
	params := map[string]any{}
	if r.Method == http.MethodPost {
		var in struct {
			Text string `json:"text"`
		}
		if err := decodeJSONBody(w, r, &in, clipboardBodyMost); err != nil {
			writeBodyErr(w, err, fmt.Errorf("bad body: %w", err))
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
			err = errors.New("这台电脑上的 personad 太旧，还不会读写剪贴板：重新部署它")
		}
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, map[string]any{"text": out.Text})
}
