package extmgr

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/extwire"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/quota"
	"github.com/nwverse/persona/server/internal/rpc"
)

// The tests of the code moved here from the hub, on a Manager as the hub's tests had a Hub: a fake hub on the bus
// (testHub: what the manager tells it, the cards it shows, the pushes), the computers set by hand (h.machines,
// h.byID), the assistant faked where a test needs one (Options.Assistant), and the hub's auth in front of the routes
// (routes: the master token over loopback, a device token, an extension's), relaying the request as the hub does.

// newTestManager is a Manager on a short temporary data directory with a fake hub.
func newTestManager(t *testing.T) *Manager {
	t.Helper()
	dir := shortTempDir(t)
	cfg := &Config{Listen: ":18660", Token: "test-master-token-" + randomHex(4), PublicURL: "http://hub.example:18660", dir: dir}
	th := &testHub{}
	// The manager's goroutines (computer.nolock, computer.cursor) can log after the test ends, when t.Logf panics.
	var logMu sync.Mutex
	over := false
	logf := func(format string, args ...any) {
		logMu.Lock()
		defer logMu.Unlock()
		if !over {
			t.Logf(format, args...)
		}
	}
	t.Cleanup(func() {
		logMu.Lock()
		over = true
		logMu.Unlock()
	})
	h := New(Options{Dir: dir, Config: cfg, Hub: th, Logf: logf})
	h.logger = log.New(io.Discard, "", 0)
	h.version = "test"
	th.h = h
	testHubs.Store(h, th)
	// Cards go to the assistant's queue, in the manager's process: here the fake hub's desk answers them.
	h.opts.Ask = func(ctx context.Context, ix proto.Interaction) (proto.Resolution, error) {
		raw, _ := json.Marshal(map[string]any{"interaction": ix})
		res, err := th.appCall(ctx, "interactions.wait", raw)
		if err != nil {
			return proto.Resolution{}, err
		}
		r, _ := res.(proto.Resolution)
		return r, nil
	}
	h.apiMux = http.NewServeMux()
	close(h.started)
	tm := &testModels{launchers: map[string][2]string{}}
	tm.usable.Store(true)
	testModelsOf.Store(h, tm)
	h.opts.Assistant = tm.answer
	t.Cleanup(func() { testHubs.Delete(h); testModelsOf.Delete(h) })
	return h
}

// testModels is the assistant's model configuration as the extensions ask it in-process (models.usable,
// models.launcher_http, models.launchers): whether inbox.triage can be called, the launchers added in Persona with
// their gateway and key. Anything else the assistant would answer is offline (no assistant), unless a test gave one
// (withAssistant).
type testModels struct {
	usable    atomic.Bool
	mu        sync.Mutex
	launchers map[string][2]string // name → base, key
}

var testModelsOf sync.Map // *Manager → *testModels

func modelsOf(h *Manager) *testModels {
	v, _ := testModelsOf.Load(h)
	return v.(*testModels)
}

// launcher puts a launcher added in Persona into the model configuration: its gateway at base, sent with key.
func (tm *testModels) launcher(name, base, key string) {
	tm.mu.Lock()
	tm.launchers[name] = [2]string{base, key}
	tm.mu.Unlock()
}

func (tm *testModels) answer(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	var out any
	switch method {
	case assistantwire.MethodModelsUsable:
		out = map[string]any{"usable": tm.usable.Load()}
	case assistantwire.MethodModelsLauncherHTTP:
		var a struct {
			Name string          `json:"name"`
			HTTP json.RawMessage `json:"http"`
		}
		_ = json.Unmarshal(params, &a)
		tm.mu.Lock()
		l, ok := tm.launchers[a.Name]
		tm.mu.Unlock()
		if !ok {
			return nil, &rpc.Error{Code: assistantwire.CodeNoLauncher, Message: "no such launcher"}
		}
		if len(a.HTTP) == 0 {
			out = map[string]any{"base_url": l[0], "machine": ""}
			break
		}
		var r quota.Request
		if err := json.Unmarshal(a.HTTP, &r); err != nil {
			return nil, err
		}
		resp, err := quota.Do(ctx, nil, l[0], l[1], r)
		if err != nil {
			return nil, err
		}
		out = resp
	case assistantwire.MethodModelsLaunchers:
		var list []assistantwire.GatewayLauncher
		tm.mu.Lock()
		for name, l := range tm.launchers {
			list = append(list, assistantwire.GatewayLauncher{Name: name, BaseURL: l[0]})
		}
		tm.mu.Unlock()
		out = map[string]any{"launchers": list}
	default:
		return nil, errAssistantOffline
	}
	return json.Marshal(out)
}

var testHubs sync.Map // *Manager → *testHub

// hubOf is h's fake hub.
func hubOf(h *Manager) *testHub {
	v, _ := testHubs.Load(h)
	th, _ := v.(*testHub)
	return th
}

// shortTempDir is a temporary directory short enough for the Unix sockets made under it (<dir>/extensions/<id>.sock).
func shortTempDir(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return t.TempDir()
	}
	dir, err := os.MkdirTemp("/tmp", "pm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// testHub is the hub on the other end of the bus.
type testHub struct {
	h  *Manager
	mu sync.Mutex
	// broadcasts are the messages to the apps (ws.broadcast), events every other event, calls every call.
	broadcasts []map[string]any
	events     []string
	calls      []string
	pushes     []map[string]any
	reports    []extwire.Report
	// answer answers a card (interactions.wait); nil: allowed. pending are the cards asked so far.
	answer  func(proto.Interaction) proto.Resolution
	pending []proto.Interaction
	// withdrawn are the cards whose caller stopped waiting before they were answered.
	withdrawn []string
	// resolved are the cards answered from a channel (interactions.resolve_owner).
	resolved map[string]proto.Resolution
	// app answers the hub's other app methods a test needs (nil: {}).
	app func(method string, params json.RawMessage) (any, error)
}

func (th *testHub) Call(ctx context.Context, method string, params, out any) error {
	raw, _ := json.Marshal(params)
	th.mu.Lock()
	th.calls = append(th.calls, method)
	th.mu.Unlock()
	if method != assistantwire.MethodApp {
		return errors.New("test hub: no method " + method)
	}
	var p assistantwire.AppCall
	_ = json.Unmarshal(raw, &p)
	res, err := th.appCall(ctx, p.Method, p.Params)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	b, _ := json.Marshal(res)
	switch o := out.(type) {
	case *json.RawMessage:
		*o = b
		return nil
	}
	return json.Unmarshal(b, out)
}

func (th *testHub) appCall(ctx context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case "interactions.wait": // (the assistant's queue, Options.Ask in tests: testHub.ask)
		var a struct {
			Interaction proto.Interaction `json:"interaction"`
		}
		_ = json.Unmarshal(params, &a)
		th.mu.Lock()
		th.pending = append(th.pending, a.Interaction)
		answer := th.answer
		th.mu.Unlock()
		if answer == nil {
			yes := true
			return proto.Resolution{Allow: &yes}, nil
		}
		got := make(chan proto.Resolution, 1)
		go func() { got <- answer(a.Interaction) }()
		select {
		case r := <-got:
			return r, nil
		case <-ctx.Done(): // its caller went away: the hub withdraws the card
			th.mu.Lock()
			th.withdrawn = append(th.withdrawn, a.Interaction.ID)
			th.mu.Unlock()
			return nil, ctx.Err()
		}
	case "interactions.resolve_owner":
		var a struct {
			ID         string           `json:"interaction_id"`
			Resolution proto.Resolution `json:"resolution"`
		}
		_ = json.Unmarshal(params, &a)
		th.mu.Lock()
		if th.resolved == nil {
			th.resolved = map[string]proto.Resolution{}
		}
		th.resolved[a.ID] = a.Resolution
		th.mu.Unlock()
		return map[string]any{"ok": true}, nil
	case extwire.HubPushLocalized, extwire.HubPushBackground:
		var p map[string]any
		_ = json.Unmarshal(params, &p)
		th.mu.Lock()
		th.pushes = append(th.pushes, p)
		th.mu.Unlock()
		return map[string]any{}, nil
	}
	th.mu.Lock()
	app := th.app
	th.mu.Unlock()
	if app != nil {
		return app(method, params)
	}
	return map[string]any{}, nil
}

func (th *testHub) Notify(event string, params any) error {
	raw, _ := json.Marshal(params)
	th.mu.Lock()
	defer th.mu.Unlock()
	switch event {
	case assistantwire.EventBroadcast:
		var b assistantwire.Broadcast
		_ = json.Unmarshal(raw, &b)
		var m map[string]any
		_ = json.Unmarshal(b.Message, &m)
		th.broadcasts = append(th.broadcasts, m)
	case extwire.EventReport:
		var r extwire.Report
		_ = json.Unmarshal(raw, &r)
		th.reports = append(th.reports, r)
	default:
		th.events = append(th.events, event)
	}
	return nil
}

// broadcastsOf are the messages of type typ the apps were sent.
func (th *testHub) broadcastsOf(typ string) []map[string]any {
	th.mu.Lock()
	defer th.mu.Unlock()
	var out []map[string]any
	for _, b := range th.broadcasts {
		if b["type"] == typ {
			out = append(out, b)
		}
	}
	return out
}

// asked are the cards shown so far.
func (th *testHub) asked() []proto.Interaction {
	th.mu.Lock()
	defer th.mu.Unlock()
	return slices.Clone(th.pending)
}

// routes is h's routes behind the hub's auth: the master token (owner over loopback), and the principal a test gave
// with asDevice / asExtension.
func (h *Manager) routes() http.Handler {
	h.apiMux = http.NewServeMux()
	h.registerRoutes(h.apiMux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if tok == "" {
			tok = r.URL.Query().Get("token")
		}
		for k := range r.Header {
			if strings.HasPrefix(strings.ToLower(k), "x-persona-") && !strings.EqualFold(k, extwire.HeaderAdmin) {
				r.Header.Del(k)
			}
		}
		set := func(k, v string) {
			if v != "" {
				r.Header.Set(k, v)
			}
		}
		switch {
		case tok != "" && tok == h.cfg.Token:
			set(extwire.HeaderRole, roleFull)
			if remoteIP(r).IsLoopback() {
				set(extwire.HeaderOwner, "1")
			}
		case strings.HasPrefix(tok, "pext_"):
			e, ok := h.extTokens.lookupHash(hashToken(tok))
			if !ok {
				writeErr(w, http.StatusUnauthorized, errors.New("unauthorized"))
				return
			}
			p := e.principal()
			pattern := ""
			if _, pt := h.apiMux.Handler(r); pt != "" {
				pattern = pt
			}
			if ok, scope := e.opens(pattern, r.URL.EscapedPath()); !ok {
				body := map[string]any{"error": "this extension was not granted this part of the hub's API", "code": "permission_denied"}
				if scope != "" {
					body["scope"] = scope
				}
				writeErrorBody(w, http.StatusForbidden, body)
				return
			}
			set(extwire.HeaderRole, roleExtension)
			set(extwire.HeaderDevice, p.ID)
			set(extwire.HeaderExtension, p.Extension)
			set(extwire.HeaderScopes, strings.Join(p.Scopes, ","))
		default:
			d, ok := testDevices.Load(tok)
			if !ok {
				writeErr(w, http.StatusUnauthorized, errors.New("unauthorized"))
				return
			}
			p := d.(principal)
			set(extwire.HeaderRole, p.Role)
			set(extwire.HeaderDevice, p.ID)
			set(extwire.HeaderDeviceName, p.Name)
			if p.Paired {
				set(extwire.HeaderPaired, "1")
			}
		}
		set(extwire.HeaderLang, string(i18n.FromRequest(r).Or(h.userLang())))
		set(extwire.HeaderRemote, remoteIP(r).String())
		r.Header.Del("Authorization") // the hub's credentials stay with the hub (relayToManager)
		r.Header.Del("Cookie")
		q := r.URL.Query()
		if q.Has("token") {
			q.Del("token")
			r.URL.RawQuery = q.Encode()
		}
		h.ServeHTTP(w, r)
	})
}

var testDevices sync.Map // token → principal

// deviceToken is a device token of role for routes (a phone: full, a watch: answer).
func deviceToken(id, role string) string {
	tok := "jd_" + id + "_" + randomHex(4)
	testDevices.Store(tok, principal{ID: id, Name: id, Role: role})
	return tok
}

// call makes one request to h with token, its JSON answer read.
func call(t *testing.T, h http.Handler, method, path, token, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

// eventually waits up to d for ok.
func eventually(t *testing.T, d time.Duration, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(d); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// memStore is the settings in memory.
type memStore struct {
	mu       sync.Mutex
	settings map[string]json.RawMessage
}

func newMemStore() *memStore { return &memStore{settings: map[string]json.RawMessage{}} }

func (s *memStore) Setting(_ context.Context, key string, out any) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, ok := s.settings[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, out)
}

func (s *memStore) PutSetting(_ context.Context, key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settings == nil {
		s.settings = map[string]json.RawMessage{}
	}
	s.settings[key] = raw
	return nil
}

// fakeAssistant is the assistant in the manager's process, for the tests that need one: what it was asked (method →
// params), its answers by method (nil: {}), and the events it was told.
type fakeAssistant struct {
	mu      sync.Mutex
	asked   []string
	params  map[string][]json.RawMessage
	answers map[string]func(json.RawMessage) (any, error)
	told    []string
}

// withAssistant gives h a fake assistant.
func withAssistant(h *Manager) *fakeAssistant {
	a := &fakeAssistant{params: map[string][]json.RawMessage{}, answers: map[string]func(json.RawMessage) (any, error){}}
	models := modelsOf(h)
	h.opts.Assistant = func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
		if strings.HasPrefix(method, "models.") {
			return models.answer(ctx, method, params)
		}
		a.mu.Lock()
		a.asked = append(a.asked, method)
		a.params[method] = append(a.params[method], params)
		f := a.answers[method]
		a.mu.Unlock()
		var out any = map[string]any{}
		if f != nil {
			var err error
			if out, err = f(params); err != nil {
				return nil, err
			}
		}
		return json.Marshal(out)
	}
	h.opts.AssistantEvent = func(event string, _ json.RawMessage) {
		a.mu.Lock()
		a.told = append(a.told, event)
		a.mu.Unlock()
	}
	return a
}

// on sets what the assistant answers method.
func (a *fakeAssistant) on(method string, f func(json.RawMessage) (any, error)) {
	a.mu.Lock()
	a.answers[method] = f
	a.mu.Unlock()
}

// got are the params of every method call.
func (a *fakeAssistant) got(method string) []json.RawMessage {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.params[method])
}

// pluginsCall is the assistant's app.call plugins.<method> (its model configuration asks the extensions), as it makes
// it in this process.
func pluginsCall(t *testing.T, h *Manager, method string, params any) (json.RawMessage, error) {
	t.Helper()
	raw, _ := json.Marshal(params)
	out, handled, err := h.AppCall(context.Background(), assistantwire.AppCall{AppID: assistantwire.AppPlugins, Method: method, Params: raw})
	if !handled {
		t.Fatalf("plugins %s not answered here", method)
	}
	return out, err
}

// What the nodes' MCP calls ask of the extensions (the assistant's fleet_tools.go: AgentTools, CallTool), in the shape
// the tests read: connector_tools → {tools, assistant_tools}, connector_tool → {handled, result}.
const (
	hubConnectorTools = "connector_tools"
	hubConnectorTool  = "connector_tool"
)

func testHubCall(ctx context.Context, h *Manager, method string, raw json.RawMessage) (any, error) {
	var a struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
		Assistant bool            `json:"assistant"`
		MachineID string          `json:"machine_id"`
		SessionID string          `json:"session_id"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, err
		}
	}
	switch method {
	case hubConnectorTools:
		agents, asst := h.AgentTools(ctx, a.Assistant)
		if agents == nil {
			agents = []proto.Tool{}
		}
		if asst == nil {
			asst = []proto.Tool{}
		}
		return map[string]any{"tools": agents, "assistant_tools": asst}, nil
	case hubConnectorTool:
		res, ok := h.CallTool(ctx, a.MachineID, a.SessionID, a.Name, a.Arguments, a.Assistant)
		return map[string]any{"handled": ok, "result": res}, nil
	}
	return h.hubCall(ctx, method, raw)
}
