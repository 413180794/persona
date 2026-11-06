package devices

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/devicewire"
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
