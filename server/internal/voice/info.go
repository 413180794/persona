package voice

import "net/http"

// Codes of the error frames (docs/voice.md「扩展 → App」): a client shows its own words for them, the message is the
// detail.
const (
	codeNoUpstream       = "no_upstream"       // fatal: no realtime voice service is set up yet
	codeModelUnavailable = "model_unavailable" // not fatal: the app's model is gone, the default one is used instead
)

// infoProtocol is the voice wire this plugin speaks (docs/voice.md「版本协商」): 1 is the frames of
// docs/extensions.md「语音」.
const infoProtocol = 1

// serveInfo is GET /info: the wire it speaks, what is missing before a conversation can start and the settings it did
// not take. Never a key, nor the upstreams.
func (pl *Plugin) serveInfo(w http.ResponseWriter, _ *http.Request) {
	out := map[string]any{"protocol": infoProtocol, "features": []string{}}
	if pl.cfg.Setup != "" {
		out["setup"] = pl.cfg.Setup
	}
	if len(pl.cfg.Warnings) > 0 {
		out["warnings"] = pl.cfg.Warnings
	}
	writeHTTPJSON(w, http.StatusOK, out)
}
