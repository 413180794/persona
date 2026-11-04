package hub

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/rpc"
)

// The apps' requests on the assistant's routes (assistantwire.Routes, assistant_link.go) are its to answer: the hub
// passes each on as the bus request http.request and writes back what the assistant answers, unchanged; what the
// assistant sends the apps over their WebSocket (ws.broadcast) goes to them unchanged.
//
//	http.request {method, path, query, header, body} → {status, header, body}
//	ws.broadcast {message}   (an event: message is a JSON object with a string "type": assistant_schedule…)
//
// header maps a name to its values; body is base64 (JSON []byte). X-Persona-Role carries the role of the device that
// asked, as the hub's auth found it (any the app sent is replaced). forwardHTTP is the codec of the assistant's
// Handler.ServeHTTP over the bus, relayBroadcast the hub's side of its Broadcast.

func init() { handleBusEvent(assistantwire.EventBroadcast, (*Hub).busBroadcast) }

// busHTTPLimit bounds a forwarded request's body (a message on the bus is at most 64 MiB, base64 included).
const busHTTPLimit = 32 << 20

// hopHeaders are not passed on either way: they are about one connection, not the request.
var hopHeaders = map[string]bool{"Connection": true, "Keep-Alive": true, "Te": true, "Trailer": true, "Transfer-Encoding": true,
	"Upgrade": true, "Proxy-Authorization": true, "Proxy-Connection": true, "Content-Length": true}

// forwardHTTP passes r on to manager id as http.request and writes its answer.
func (h *Hub) forwardHTTP(w http.ResponseWriter, r *http.Request, id string) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, busHTTPLimit))
	if err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, err)
		return
	}
	req := assistantwire.HTTPRequest{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Header: map[string][]string{}, Body: body}
	for k, v := range r.Header {
		if !hopHeaders[k] && k != "Authorization" && k != "Cookie" { // the hub's credentials stay with the hub
			req.Header[k] = v
		}
	}
	req.Header[assistantwire.RoleHeader] = []string{principalOf(r.Context()).Role}
	ctx := r.Context()
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
	}
	var res assistantwire.HTTPResponse
	if err := h.callBus(ctx, id, assistantwire.MethodHTTP, req, &res); err != nil {
		var coded *rpc.Error
		switch {
		case errors.Is(err, errBusOffline):
			writeErrCode(w, http.StatusServiceUnavailable, assistantwire.CodeAssistantOffline, errAssistantOffline)
		case errors.As(err, &coded):
			writeErrCode(w, http.StatusBadGateway, coded.Code, err)
		default:
			writeErr(w, http.StatusBadGateway, err)
		}
		return
	}
	for k, v := range res.Header {
		if !hopHeaders[http.CanonicalHeaderKey(k)] {
			w.Header()[http.CanonicalHeaderKey(k)] = v
		}
	}
	if res.Status == 0 {
		res.Status = http.StatusOK
	}
	w.WriteHeader(res.Status)
	_, _ = w.Write(res.Body)
}

// busBroadcast is ws.broadcast from a manager.
func (h *Hub) busBroadcast(c *busConn, params json.RawMessage) {
	var p struct {
		Message json.RawMessage `json:"message"`
	}
	err := json.Unmarshal(params, &p)
	if err == nil {
		err = h.relayBroadcast(p.Message)
	}
	if err != nil {
		h.logf("bus: [%s] ws.broadcast: %v", c.ident.ID, err)
	}
}

// relayBroadcast sends message, a manager's, to every app unchanged.
func (h *Hub) relayBroadcast(message json.RawMessage) error {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(message, &head); err != nil || head.Type == "" {
		return errors.New("a message is a JSON object with a type")
	}
	h.broadcastRaw(head.Type, message)
	return nil
}
