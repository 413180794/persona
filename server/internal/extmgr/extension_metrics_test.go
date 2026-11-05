package extmgr

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/extension/host"
	"github.com/nwverse/persona/server/internal/extwire"
)

// clock is a settable now for the counts.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func testMetrics(start time.Time) (*extensionMetrics, *clock) {
	c := &clock{t: start}
	m := newExtensionMetrics()
	m.now = c.now
	return m, c
}

func TestExtensionMetricsCountsAndQuantiles(t *testing.T) {
	m, _ := testMetrics(time.Date(2026, 11, 5, 10, 2, 0, 0, time.UTC))
	s := m.of("demo")
	for i := 0; i < 90; i++ {
		s.Call("connector.status", 3*time.Millisecond, "") // in (2, 5]
	}
	for i := 0; i < 9; i++ {
		s.Call("connector.status", 150*time.Millisecond, "") // in (100, 200]
	}
	s.Call("connector.status", 90*time.Second, extension.CodeTimeout) // slower than the last bound
	s.Call("ui.render", 10*time.Millisecond, host.CodeCanceled)       // the caller gave up: not the extension's error
	s.Call("HTTP /x", 20*time.Millisecond, "http_4xx")                // the caller's fault
	s.Call("HTTP /x", 20*time.Millisecond, "http_5xx")
	s.Call("HTTP /ws", -1, "") // a WebSocket: no latency

	r := m.metrics(context.Background(), nil, "demo", "1h")
	if r.Total.Calls != 104 || r.Total.Errors != 2 {
		t.Fatalf("total: %+v", r.Total)
	}
	if r.Total.Codes[extension.CodeTimeout] != 1 || r.Total.Codes[host.CodeCanceled] != 1 || r.Total.Codes["http_4xx"] != 1 {
		t.Fatalf("codes: %v", r.Total.Codes)
	}
	var status MethodMetrics
	for _, mm := range r.Methods {
		if mm.Method == "connector.status" {
			status = mm
		}
	}
	if status.Calls != 100 || status.Errors != 1 || status.ErrorRate != 0.01 {
		t.Fatalf("status: %+v", status)
	}
	if status.P50 == nil || *status.P50 <= 2 || *status.P50 > 5 {
		t.Fatalf("p50 %v", status.P50)
	}
	if status.P95 == nil || *status.P95 <= 100 || *status.P95 > 200 {
		t.Fatalf("p95 %v", status.P95)
	}
	if status.P99 == nil || *status.P99 <= 100 {
		t.Fatalf("p99 %v", status.P99)
	}
	if r.Methods[0].Method != "connector.status" {
		t.Fatalf("most called first: %v", r.Methods[0].Method)
	}
	var ws MethodMetrics
	for _, mm := range r.Methods {
		if mm.Method == "HTTP /ws" {
			ws = mm
		}
	}
	if ws.Calls != 1 || ws.P50 != nil {
		t.Fatalf("websocket: %+v", ws)
	}
	if got := quantile([]int64{0, 0}, 0.5); got != nil {
		t.Fatalf("empty: %v", *got)
	}
}

func TestExtensionMetricsWindows(t *testing.T) {
	start := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	m, c := testMetrics(start)
	s := m.of("demo")
	// One call every 10 minutes for 30 hours, an error each hour.
	for i := 0; i < 180; i++ {
		code := ""
		if i%6 == 0 {
			code = "boom"
		}
		s.Call("m", time.Millisecond, code)
		if i%60 == 0 {
			s.Run(host.RunStart, "")
			s.Run(host.RunExit, "exited: exit status 3")
		}
		c.add(10 * time.Minute)
	}
	c.add(-10 * time.Minute) // now is the last call's
	if n := len(s.buckets); n > int(metricsInMemory/metricsBucket) {
		t.Fatalf("memory keeps %d buckets", n)
	}
	h1 := m.metrics(context.Background(), nil, "demo", "1h")
	if len(h1.Series) != 12 || h1.Step != 300 {
		t.Fatalf("1h: %d points of %ds", len(h1.Series), h1.Step)
	}
	if h1.Total.Calls != 6 && h1.Total.Calls != 7 {
		t.Fatalf("1h calls %d", h1.Total.Calls)
	}
	d := m.metrics(context.Background(), nil, "demo", "24h")
	if len(d.Series) != 24 || d.Total.Calls < 140 || d.Total.Calls > 150 {
		t.Fatalf("24h: %d points, %d calls", len(d.Series), d.Total.Calls)
	}
	var sum int64
	for _, p := range d.Series {
		sum += p.Calls
	}
	if sum != d.Total.Calls {
		t.Fatalf("series %d != total %d", sum, d.Total.Calls)
	}
	if d.Exits == 0 || d.Reasons["exited: exit status 3"] != d.Exits || len(d.RecentExits) == 0 {
		t.Fatalf("exits: %d %v %v", d.Exits, d.Reasons, d.RecentExits)
	}
	if bad := m.metrics(context.Background(), nil, "demo", "nonsense"); bad.Window != "24h" {
		t.Fatalf("unknown window: %s", bad.Window)
	}
}

func TestExtensionLogRing(t *testing.T) {
	m, _ := testMetrics(time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC))
	s := m.of("demo")
	for i := 0; i < logLinesKept+100; i++ {
		s.Log(host.LogProgram, fmt.Sprintf("line %d", i))
	}
	lines, next := s.Lines(-1, "", 0)
	if len(lines) != logLinesKept || lines[0].Seq != 100 || lines[0].Text != "line 100" || next != int64(logLinesKept+100) {
		t.Fatalf("%d lines from %d (%q), next %d", len(lines), lines[0].Seq, lines[0].Text, next)
	}
	if lines, _ := s.Lines(next-3, "", 0); len(lines) != 2 {
		t.Fatalf("since: %v", lines)
	}
	if lines, _ := s.Lines(-1, "LINE 2099", 0); len(lines) != 1 {
		t.Fatalf("q: %v", lines)
	}
	if lines, _ := s.Lines(-1, "", 5); len(lines) != 5 || lines[4].Text != fmt.Sprintf("line %d", logLinesKept+99) {
		t.Fatalf("limit keeps the latest: %v", lines)
	}
	s.Log(host.LogProgram, strings.Repeat("长", 3000))
	lines, _ = s.Lines(next-1, "", 0)
	if l := lines[len(lines)-1].Text; len(l) > logLineMost+len("…") || !strings.HasSuffix(l, "…") || !strings.HasPrefix(l, "长") {
		t.Fatalf("long line: %d bytes", len(l))
	}
}

func TestExtensionLogRedacts(t *testing.T) {
	m, _ := testMetrics(time.Now())
	m.redact = func(l string) string { return strings.ReplaceAll(l, "hubtoken123456", "***") }
	s := m.of("demo")
	for _, c := range []struct{ in, out string }{
		{"Authorization: Bearer abcdefghijklmnop", "Authorization: ***"},
		{"sent with bearer abcdefghijklmnop", "sent with bearer ***"},
		{`token=abcdef0123 rest`, "token=*** rest"},
		{`{"api_key": "sk-123456789"}`, `{"api_key": "***"}`},
		{"GET /x?key=abc&y=1", "GET /x?key=***&y=1"},
		{"the hub's hubtoken123456 leaked", "the hub's *** leaked"},
		{"nothing to hide", "nothing to hide"},
	} {
		s.Log(host.LogProgram, c.in)
		lines, _ := s.Lines(-1, "", 1)
		if lines[0].Text != c.out {
			t.Errorf("%q → %q, want %q", c.in, lines[0].Text, c.out)
		}
	}
	s.Run(host.RunExit, "dial: password=hunter2xyz refused")
	if r := m.metrics(context.Background(), nil, "demo", "1h"); !strings.Contains(r.RecentExits[0].Reason, "password=***") {
		t.Fatalf("exit reason: %q", r.RecentExits[0].Reason)
	}
}

// metricsMemStore is the database's extension_metrics.
type metricsMemStore struct {
	mu      sync.Mutex
	rows    map[string]map[time.Time]json.RawMessage
	puts    int
	deleted time.Time
}

func (s *metricsMemStore) PutExtensionMetrics(_ context.Context, id string, start time.Time, data json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rows == nil {
		s.rows = map[string]map[time.Time]json.RawMessage{}
	}
	if s.rows[id] == nil {
		s.rows[id] = map[time.Time]json.RawMessage{}
	}
	s.rows[id][start] = data
	s.puts++
	return nil
}

func (s *metricsMemStore) ExtensionMetrics(_ context.Context, id string, since time.Time) ([]db.ExtensionMetricsRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []db.ExtensionMetricsRow
	for start, data := range s.rows[id] {
		if !start.Before(since) {
			out = append(out, db.ExtensionMetricsRow{Extension: id, Start: start, Data: data})
		}
	}
	return out, nil
}

func (s *metricsMemStore) DeleteExtensionMetricsBefore(_ context.Context, before time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleted = before
	for _, rows := range s.rows {
		for start := range rows {
			if start.Before(before) {
				delete(rows, start)
			}
		}
	}
	return nil
}

func TestExtensionMetricsFlushAndLoad(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	store := &metricsMemStore{}
	// A week-old bucket in the database: dropped at the first flush.
	_ = store.PutExtensionMetrics(ctx, "demo", start.Add(-8*24*time.Hour), json.RawMessage(`{}`))
	// Three days ago: only the 7-day window reads it.
	_ = store.PutExtensionMetrics(ctx, "demo", start.Add(-3*24*time.Hour), json.RawMessage(`{"methods":{"m":{"calls":5,"errors":1}}}`))
	m, c := testMetrics(start)
	s := m.of("demo")
	s.Call("m", time.Millisecond, "")
	c.add(6 * time.Minute)
	s.Call("m", time.Millisecond, "") // the open bucket
	store.puts = 0
	if err := m.flush(ctx, store, false); err != nil {
		t.Fatal(err)
	}
	if store.puts != 1 || !store.deleted.Equal(c.now().Add(-metricsKept)) {
		t.Fatalf("closed buckets only: %d puts, deleted before %v", store.puts, store.deleted)
	}
	if err := m.flush(ctx, store, false); err != nil || store.puts != 1 {
		t.Fatalf("a flushed bucket is not written again: %d", store.puts)
	}
	if err := m.flush(ctx, store, true); err != nil || store.puts != 2 {
		t.Fatalf("all: %d", store.puts)
	}
	s.Call("m", time.Millisecond, "") // changes the open bucket again
	if err := m.flush(ctx, store, true); err != nil || store.puts != 3 {
		t.Fatalf("a changed bucket is written again: %d", store.puts)
	}
	if n := len(store.rows["demo"]); n != 3 {
		t.Fatalf("rows: %d", n)
	}

	// A restart: the last day comes back from the database, the 7 days read it too.
	m2, _ := testMetrics(c.now())
	m2.load(ctx, store, []string{"demo"})
	if r := m2.metrics(ctx, store, "demo", "24h"); r.Total.Calls != 3 {
		t.Fatalf("after a restart: %d", r.Total.Calls)
	}
	if r := m2.metrics(ctx, store, "demo", "7d"); r.Total.Calls != 8 || r.Total.Errors != 1 || len(r.Series) != 28 {
		t.Fatalf("7d: %d calls, %d errors, %d points", r.Total.Calls, r.Total.Errors, len(r.Series))
	}
	store.puts = 0
	if err := m2.flush(ctx, store, true); err != nil || store.puts != 0 {
		t.Fatalf("loaded buckets are not written back: %d", store.puts)
	}
}

func TestExtensionMetricsHubLines(t *testing.T) {
	m, _ := testMetrics(time.Now())
	m.of("far")
	m.hubLine("plugin far: the new version does not come up")
	m.hubLine("extension far: once more")
	m.hubLine("plugin nobody: no stats are made for it")
	m.hubLine("plugins: not about one")
	lines, _ := m.of("far").Lines(-1, "", 0)
	if len(lines) != 2 || lines[0].Source != host.LogHub || lines[0].Text != "the new version does not come up" {
		t.Fatalf("%+v", lines)
	}
	if ids := m.known(); len(ids) != 1 {
		t.Fatalf("known: %v", ids)
	}
}

// The relayed HTTP is counted by route; the hub's lines about the extension land in its log.
func TestExtensionHTTPCounted(t *testing.T) {
	dir, err := os.MkdirTemp("", "jm")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "far.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/boom"):
			w.WriteHeader(http.StatusBadGateway)
		case r.URL.Path == "/missing":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	ps := newExtensions(dir, []ExtensionConfig{{ID: "far", Kind: "voice", Socket: sock}}, func(string, ...any) {})
	hp := ps.byID["far"]
	for _, p := range []string{"/api/v1/extensions/far/models", "/api/v1/plugins/far/models?x=1", "/api/v1/extensions/far/boom/7", "/api/v1/extensions/far/missing"} {
		hp.serveExtensionHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", p, nil))
	}
	r := ps.stats.metrics(context.Background(), nil, "far", "1h")
	got := map[string]MethodMetrics{}
	for _, mm := range r.Methods {
		got[mm.Method] = mm
	}
	if got["HTTP /models"].Calls != 2 || got["HTTP /boom"].Errors != 1 || got["HTTP /missing"].Errors != 0 ||
		got["HTTP /missing"].Codes["http_4xx"] != 1 || r.Total.Errors != 1 {
		t.Fatalf("%+v", got)
	}
	if httpRoute("lark", "/api/v1/lark/chats/oc_1/messages") != "HTTP /api/v1/lark" {
		t.Fatal("an api prefix's route")
	}
	ps.logf("plugin far: %s", "restarted")
	if lines, _ := ps.stats.of("far").Lines(-1, "", 0); len(lines) != 1 || lines[0].Text != "restarted" {
		t.Fatalf("%+v", lines)
	}
}

// Only a signed-in admin session reads the counts and the log; the log streams as server-sent events.
func TestAdminExtensionMetrics(t *testing.T) {
	h := newTestManager(t)
	h.store = newExtensionStore()
	startTestExtensions(t, h, ExtensionConfig{ID: "voice", Name: "Voice", Kind: "voice", Socket: "/nonexistent/admin-metrics.sock"})
	mux := h.routes()
	st := h.extensions.stats.of("voice")
	st.Call("HTTP /models", 12*time.Millisecond, "")
	st.Call("HTTP /models", 40*time.Millisecond, "http_5xx")
	st.Log(host.LogProgram, "hello from voice")
	h.legacyExtensionRoutesCount(t, mux) // an old path's call

	anon := &b1Admin{t: t, h: h, remote: b1AdminLAN}
	for _, p := range []string{"/api/v1/admin/extensions/-/metrics", "/api/v1/admin/extensions/voice/metrics",
		"/api/v1/admin/extensions/voice/logs", "/api/v1/admin/extensions/voice/logs/stream", "/api/v1/admin/plugins/voice/logs"} {
		if code, out, _ := anon.do("GET", p, ""); code != 401 || out["code"] != "login_required" {
			t.Errorf("%s signed out: %d %v", p, code, out)
		}
	}
	if code, _ := call(t, mux, "GET", "/api/v1/admin/extensions/voice/logs", h.cfg.Token, ""); code != 401 {
		t.Errorf("the master token is not a session: %d", code)
	}

	c := &b1Admin{t: t, h: h, remote: b1AdminLAN, signedIn: true}
	code, out, _ := c.do("GET", "/api/v1/admin/extensions/voice/metrics?window=1h", "")
	total, _ := out["total"].(map[string]any)
	if code != 200 || total["calls"] != float64(2) || total["errors"] != float64(1) || total["error_rate"] != 0.5 || out["window"] != "1h" {
		t.Fatalf("metrics: %d %v", code, out)
	}
	code, out, _ = c.do("GET", "/api/v1/admin/extensions/-/metrics", "")
	exts, _ := out["extensions"].([]any)
	legacy, _ := out["legacy_calls"].([]any)
	if code != 200 || len(exts) == 0 || len(legacy) != 1 {
		t.Fatalf("overview: %d %v", code, out)
	}
	if l := legacy[0].(map[string]any); l["calls"] != float64(1) || l["since_start"] != float64(1) {
		t.Fatalf("legacy: %v", l)
	}
	code, out, _ = c.do("GET", "/api/v1/admin/extensions/voice/logs?q=HELLO", "")
	lines, _ := out["lines"].([]any)
	if code != 200 || len(lines) != 1 || out["kept"] != float64(logLinesKept) {
		t.Fatalf("logs: %d %v", code, out)
	}
	if code, _, _ := c.do("GET", "/api/v1/admin/extensions/nobody/logs", ""); code != 404 {
		t.Fatalf("no such extension: %d", code)
	}
	if code, _, _ := c.do("GET", "/api/v1/admin/extensions/voice/logs?since=x", ""); code != 400 {
		t.Fatalf("bad since: %d", code)
	}

	// The stream: the line kept so far, then one logged while it is open.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.RemoteAddr = b1AdminLAN // relayed from the hub's admin mux, signed in
		r.Header.Set(extwire.HeaderRole, roleFull)
		r.Header.Set(extwire.HeaderAdmin, "1")
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/v1/admin/extensions/voice/logs/stream", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("stream: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	rd := bufio.NewReader(res.Body)
	next := func() LogLine {
		t.Helper()
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
				t.Fatalf("stream ended: %v", err)
			}
			if data, ok := strings.CutPrefix(strings.TrimSpace(line), "data: "); ok {
				var l LogLine
				if err := json.Unmarshal([]byte(data), &l); err != nil {
					t.Fatal(err)
				}
				return l
			}
		}
	}
	if l := next(); l.Text != "hello from voice" {
		t.Fatalf("backlog: %+v", l)
	}
	st.Log(host.LogHub, "a new line")
	if l := next(); l.Text != "a new line" || l.Source != host.LogHub {
		t.Fatalf("live: %+v", l)
	}
}

// legacyExtensionRoutesCount calls an old path once, as an app out there still does.
func (h *Manager) legacyExtensionRoutesCount(t *testing.T, mux http.Handler) {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/plugins", nil)
	req.RemoteAddr = "127.0.0.1:1"
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("old path: %d %s", w.Code, w.Body.String())
	}
}
