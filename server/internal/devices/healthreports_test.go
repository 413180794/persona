package devices

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/nodeproto"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestHealthReportsReachTheNode(t *testing.T) {
	d := newTestDevices(t)
	n := &testNode{}
	connectedMachine(t, d, "box", n)
	mux := d.Handler(http.NotFoundHandler())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/machines/box/health/reports", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "health.reports.list") {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("DELETE", "/api/v1/machines/box/health/reports/2026%20%E4%BD%93%E6%A3%80.pdf", nil))
	if w.Code != 200 {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if !slices.ContainsFunc(n.calls, func(c string) bool {
		return strings.HasPrefix(c, "health.reports.delete") && strings.Contains(c, "2026 体检.pdf")
	}) {
		t.Fatalf("calls: %v", n.calls)
	}
}

func TestHealthReportsOldNodeSaysSo(t *testing.T) {
	d := newTestDevices(t)
	n := &testNode{refuse: errors.New("unknown method health.reports.list")}
	connectedMachine(t, d, "box", n)
	w := httptest.NewRecorder()
	d.Handler(http.NotFoundHandler()).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/machines/box/health/reports", nil))
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), proto.CodeNodeOutdated) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestHealthReportsAreRelayed(t *testing.T) {
	for _, want := range []string{"GET /api/v1/machines/{mid}/health/reports", "DELETE /api/v1/machines/{mid}/health/reports/{name}"} {
		if !slices.Contains(devicewire.Routes, want) {
			t.Errorf("%s is not relayed by the hub", want)
		}
	}
}

// Another manager than the built-in one has its own health space on a computer (health-<id>): its reports are listed
// and deleted there, on a node that knows other managers' spaces; an older node would answer about the built-in's,
// so it is told to update instead, and nothing is asked of it.
func TestHealthReportsOfAnotherManagersSpace(t *testing.T) {
	d := newTestDevices(t)
	d.opts.ManagerID = "m-0b0b0b"
	n := &testNode{info: &proto.NodeInfo{Hostname: "box", OS: "linux", Arch: "amd64", Home: "/home/me", Version: "test",
		Protocol: nodeproto.Version, ProtocolMin: nodeproto.MinManagerVersion, Capabilities: nodeproto.Announced}, echoHealth: true}
	connectedMachine(t, d, "box", n)
	old := &testNode{}
	connectedMachine(t, d, "old", old)
	mux := d.Handler(http.NotFoundHandler())
	for _, req := range []*http.Request{httptest.NewRequest("GET", "/api/v1/machines/box/health/reports", nil),
		httptest.NewRequest("DELETE", "/api/v1/machines/box/health/reports/a.pdf", nil)} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("%s %s: %d %s", req.Method, req.URL, w.Code, w.Body)
		}
	}
	// (the delete is asked after a list of the same space: the node said it reads it)
	if got := n.seen("health.reports.list"); len(got) != 2 || !strings.Contains(got[0], `"health_dir":"health-m-0b0b0b"`) ||
		!strings.Contains(got[1], `"health_dir":"health-m-0b0b0b"`) {
		t.Fatalf("lists: %v", got)
	}
	if got := n.seen("health.reports.delete"); len(got) != 1 || !strings.Contains(got[0], `"health_dir":"health-m-0b0b0b"`) {
		t.Fatalf("delete: %v", got)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("DELETE", "/api/v1/machines/old/health/reports/a.pdf", nil))
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), proto.CodeNodeOutdated) || len(old.seen("health.reports")) != 0 {
		t.Fatalf("an older node: %d %s %v", w.Code, w.Body, old.seen("health.reports"))
	}
}

// A node that does not say it answered about the manager's own space (no health_dir echo: a node with no version
// yet, an old dev build) used the built-in's: its list is not shown, and a delete is never asked of it.
func TestHealthReportsNeedTheNodesEcho(t *testing.T) {
	d := newTestDevices(t)
	d.opts.ManagerID = "m-0b0b0b"
	n := &testNode{info: &proto.NodeInfo{Hostname: "box", OS: "linux", Arch: "amd64", Home: "/home/me"}} // no version: taken as capable
	connectedMachine(t, d, "box", n)
	mux := d.Handler(http.NotFoundHandler())
	for _, req := range []*http.Request{httptest.NewRequest("GET", "/api/v1/machines/box/health/reports", nil),
		httptest.NewRequest("DELETE", "/api/v1/machines/box/health/reports/a.pdf", nil)} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), proto.CodeNodeOutdated) {
			t.Fatalf("%s %s: %d %s", req.Method, req.URL, w.Code, w.Body)
		}
	}
	if got := n.seen("health.reports.delete"); len(got) != 0 {
		t.Fatalf("a delete reached a node that reads the built-in's space: %v", got)
	}
}
