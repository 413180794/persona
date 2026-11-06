package extmgr

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/extwire"
	"github.com/nwverse/persona/server/internal/proto"
)

// What the hub's tests read of the hub, read here where it went: what the hub shows of Cua and ego on a machine is
// the manager's report (extwire.Report.Machines), the tools agents get from the connectors, Cua and ego are the
// manager's answer to the hub (hubConnectorTools), and the admin UI's requests come relayed with the hub's
// admin header.

// shownComputer is machine m's Cua state as the hub shows it (the report), nil when not on.
func shownComputer(t *testing.T, h *Manager, m *Machine) *proto.Computer {
	t.Helper()
	x, ok := h.report().Machines[m.cfg.ID]
	if !ok || len(x.Computer) == 0 {
		return nil
	}
	var c proto.Computer
	if err := json.Unmarshal(x.Computer, &c); err != nil {
		t.Fatal(err)
	}
	return &c
}

// shownBrowser is machine m's ego state as the hub shows it (the report), nil when not on.
func shownBrowser(t *testing.T, h *Manager, m *Machine) *proto.Browser {
	t.Helper()
	x, ok := h.report().Machines[m.cfg.ID]
	if !ok || len(x.Browser) == 0 {
		return nil
	}
	var b proto.Browser
	if err := json.Unmarshal(x.Browser, &b); err != nil {
		t.Fatal(err)
	}
	return &b
}

// toolsForAgents are the tools the hub lists for an agent (the assistant's when assistant) of the manager's: the
// connectors', Cua's and ego's.
func toolsForAgents(t *testing.T, h *Manager, assistant bool) []proto.Tool {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"assistant": assistant})
	out, err := testHubCall(context.Background(), h, hubConnectorTools, raw)
	if err != nil {
		t.Fatal(err)
	}
	return managerTools(out)
}

// managerTools are a HubConnectorTools answer's tools: every agent's, then the assistant's only.
func managerTools(out any) []proto.Tool {
	m := out.(map[string]any)
	return append(m["tools"].([]proto.Tool), m["assistant_tools"].([]proto.Tool)...)
}

// adminCall is a request of the admin UI's (a signed-in admin session, as the hub relays it).
func adminCall(t *testing.T, h http.Handler, method, path, body string) (int, map[string]any) {
	t.Helper()
	return callWith(t, h, method, path, body, map[string]string{extwire.HeaderAdmin: "1"}, "application/json")
}

// adminRoutes are h's routes as the hub relays the admin UI's: no token, the admin header (routes() keeps it).
func adminRoutes(h *Manager) http.Handler {
	routes := h.routes()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(extwire.HeaderAdmin) == "1" && r.Header.Get("Authorization") == "" {
			r.Header.Set("Authorization", "Bearer "+h.cfg.Token) // the hub's auth let the admin session in
			r.RemoteAddr = "[fd00::20]:1"                        // from the LAN, not the hub's own machine
		}
		routes.ServeHTTP(w, r)
	})
}

func callWith(t *testing.T, h http.Handler, method, path, body string, header map[string]string, contentType string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, "http://manager"+path, strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Content-Type", contentType)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	w := &recorder{header: http.Header{}, code: 200}
	h.ServeHTTP(w, req)
	out := map[string]any{}
	_ = json.Unmarshal(w.body, &out)
	return w.code, out
}

type recorder struct {
	header http.Header
	code   int
	body   []byte
	wrote  bool
}

func (r *recorder) Header() http.Header { return r.header }
func (r *recorder) WriteHeader(c int) {
	if !r.wrote {
		r.code, r.wrote = c, true
	}
}
func (r *recorder) Write(p []byte) (int, error) {
	r.wrote = true
	r.body = append(r.body, p...)
	return len(p), nil
}

// marketDevices is a manager whose store keeps the extensions and markets (marketTestStore's role), and a phone with
// full rights and a watch that only reads and answers (device tokens are the hub's: routes plays its auth).
func marketDevices(t *testing.T) (h *Manager, phone, reader string) {
	t.Helper()
	h = newTestManager(t)
	h.store = newExtensionStore()
	startTestExtensions(t, h)
	return h, deviceToken("dev_phone", roleFull), deviceToken("dev_watch", roleAnswer)
}

// cardDesk holds the cards the manager asks the hub to show (interactions.wait) until the test answers them, as the
// hub's queue does: a card nobody takes within deskWait, or leaves unanswered, ends unanswered.
type cardDesk struct{ ch chan deskCard }

type deskCard struct {
	ix     proto.Interaction
	answer chan proto.Resolution
}

const deskWait = 2 * time.Second

var desks sync.Map // *Manager → *cardDesk

// deskOf puts h's cards on a desk (from now on).
func deskOf(h *Manager) *cardDesk {
	if v, ok := desks.Load(h); ok {
		return v.(*cardDesk)
	}
	d := &cardDesk{ch: make(chan deskCard)}
	desks.Store(h, d)
	th := hubOf(h)
	th.mu.Lock()
	th.answer = func(ix proto.Interaction) proto.Resolution {
		c := deskCard{ix: ix, answer: make(chan proto.Resolution, 1)}
		select {
		case d.ch <- c:
		case <-time.After(deskWait):
			return proto.Resolution{}
		}
		select {
		case r := <-c.answer:
			return r
		case <-time.After(15 * time.Second):
			return proto.Resolution{}
		}
	}
	th.mu.Unlock()
	return d
}

// answerCard waits for the phone card the call raises and answers it.
func answerCard(t *testing.T, h *Manager, allow, always bool) proto.Interaction {
	t.Helper()
	select {
	case c := <-deskOf(h).ch:
		c.answer <- proto.Resolution{Allow: &allow, Always: always}
		return c.ix
	case <-time.After(5 * time.Second):
		t.Fatal("no card")
		return proto.Interaction{}
	}
}

// logTo sends h's log (the manager's, as the hub's tests read the hub's) to w.
func logTo(h *Manager, w io.Writer) {
	h.logger = log.New(w, "", 0)
	h.opts.Logf = h.logger.Printf
}
