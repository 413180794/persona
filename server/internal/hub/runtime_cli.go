package hub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// The runtime CLI over the hub (docs/operations/runtime-cli.md "Reaching a real device through the hub"): a DEBUG build of the app, started with the CLI
// over the hub turned on, lists phone_runtime_cli with its tools in register_phone. It is not one of the phone's
// tools: the app's screen holds mail, messages and health data, so it is never offered to an agent (the MCP tool
// list, callTool), only to POST /api/v1/phone/runtime-cli from a full device credential (scripts/app/personactl.py --hub),
// and every call is logged without its arguments or its answer.
const runtimeCLITool = "phone_runtime_cli"

// runtimeCLIMaxTimeout caps how long one command may run on the phone (a settle or wait asks for its own timeout).
const runtimeCLIMaxTimeout = 2 * time.Minute

// splitRuntimeCLI takes the runtime CLI out of the tools a phone registers: the rest are the phone's tools.
func splitRuntimeCLI(tools []proto.Tool) ([]proto.Tool, bool) {
	out := make([]proto.Tool, 0, len(tools))
	has := false
	for _, t := range tools {
		if t.Name == runtimeCLITool {
			has = true
			continue
		}
		out = append(out, t)
	}
	return out, has
}

// runtimeCLIClient is the connected app that offers the runtime CLI (as the phone, or with nothing else): a real
// iPhone over a simulator (a simulator only while no real iPhone offers it, as with the phone's tools), then the most
// recently registered.
func (h *Hub) runtimeCLIClient() *client {
	h.mu.Lock()
	defer h.mu.Unlock()
	var best *client
	for c := range h.clients {
		if !c.runtimeCLI {
			continue
		}
		if best == nil || (best.sim && !c.sim) || (best.sim == c.sim && c.since.After(best.since)) {
			best = c
		}
	}
	return best
}

func (h *Hub) runtimeCLIRoutes(api *http.ServeMux) {
	api.HandleFunc("POST /api/v1/phone/runtime-cli", h.runtimeCLI)
}

// runtimeCLI is POST /api/v1/phone/runtime-cli {cmd, args, timeout (seconds)}: the command run by the phone's DEBUG
// app, its answer as the app gives it ({"ok": …, …}). 403 for any credential but a full one, 503 phone_offline when no
// phone offers the runtime CLI (it is not woken: a debugging session has the app open), 504 when it does not answer
// in time, 502 when the app says the call itself failed.
func (h *Hub) runtimeCLI(w http.ResponseWriter, r *http.Request) {
	who := principalOf(r.Context())
	if who.Role != roleFull {
		writeErr(w, http.StatusForbidden, errors.New("only a full device credential can drive the app"))
		return
	}
	var req struct {
		Cmd     string          `json:"cmd"`
		Args    json.RawMessage `json:"args"`
		Timeout float64         `json:"timeout"`
	}
	if err := decodeJSONBody(w, r, &req, 1<<20); err != nil || req.Cmd == "" {
		writeBodyErr(w, err, errors.New("want {cmd, args, timeout}"))
		return
	}
	if len(req.Args) == 0 || string(req.Args) == "null" {
		req.Args = json.RawMessage("{}")
	}
	timeout := runtimeCLIMaxTimeout
	if req.Timeout > 0 && time.Duration(req.Timeout*float64(time.Second)) < timeout {
		timeout = time.Duration(req.Timeout * float64(time.Second))
	}
	caller := who.ID
	if caller == "" {
		caller = "pairing-token"
	}
	started := time.Now()
	status, body := h.callRuntimeCLI(r.Context(), req.Cmd, req.Args, timeout)
	// The audit line: when (the logger's time), what, who, how it went; never the arguments (text typed into the
	// app) or the answer (screenshots, what is on the screen).
	h.logf("runtime-cli: cmd=%s device=%s status=%d %d ms", req.Cmd, caller, status, time.Since(started).Milliseconds())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (h *Hub) callRuntimeCLI(ctx context.Context, cmd string, args json.RawMessage, timeout time.Duration) (int, []byte) {
	fail := func(status int, msg string) (int, []byte) {
		b, _ := json.Marshal(map[string]any{"ok": false, "error": msg})
		return status, b
	}
	c := h.runtimeCLIClient()
	if c == nil {
		return fail(http.StatusServiceUnavailable, "phone_offline")
	}
	input, _ := json.Marshal(map[string]any{"cmd": cmd, "args": args})
	id := randomHex(8)
	ch := make(chan proto.ToolResult, 1)
	h.mu.Lock()
	h.calls[id] = ch
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.calls, id)
		h.mu.Unlock()
	}()
	c.sendJSON(map[string]any{"type": "tool_call", "call_id": id, "name": runtimeCLITool, "args": json.RawMessage(input)})
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case res := <-ch:
		text := ""
		for _, part := range res.Content {
			if part.Type == "text" {
				text += part.Text
			}
		}
		if res.IsError {
			return fail(http.StatusBadGateway, text)
		}
		if !json.Valid([]byte(text)) {
			return fail(http.StatusBadGateway, "the app's answer is not JSON")
		}
		return http.StatusOK, []byte(text)
	case <-ctx.Done():
		return fail(http.StatusGatewayTimeout, "the app did not answer in time")
	}
}
