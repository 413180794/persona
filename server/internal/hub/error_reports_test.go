package hub

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/errreport"
)

// A handler's panic: a 500, logged once with its stack and the reported mark (so the log's tee does not report it
// again), and queued as a report while an extension takes them; http.ErrAbortHandler passes on.
func TestRecoverPanics(t *testing.T) {
	var buf bytes.Buffer
	h := &Hub{logger: log.New(&buf, "", 0)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/things/{id}", func(http.ResponseWriter, *http.Request) { panic("boom") })
	mux.HandleFunc("GET /abort", func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) })
	srv := recoverPanics(h, mux)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/things/secret-id", nil))
	if rec.Code != 500 || !strings.Contains(buf.String(), errreport.Reported+" handler GET /api/v1/things/{id}: boom") ||
		!strings.Contains(buf.String(), "TestRecoverPanics") || strings.Contains(buf.String(), "secret-id") {
		t.Fatalf("%d %s", rec.Code, buf.String())
	}

	h.errs.on.Store(true)
	h.errs.once.Do(func() { h.errs.queue = make(chan wire.ErrorReport, 1) }) // no sender: the test reads the queue
	srv.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/things/x", nil))
	select {
	case r := <-h.errs.queue:
		if r.Kind != "panic" || r.Source != "hub" || r.Tags["where"] != "GET /api/v1/things/{id}" || len(r.Exceptions[0].Frames) == 0 {
			t.Fatalf("%+v", r)
		}
	case <-time.After(time.Second):
		t.Fatal("not reported")
	}

	defer func() {
		if v := recover(); v != http.ErrAbortHandler {
			t.Fatalf("an abort passes on: %v", v)
		}
	}()
	srv.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/abort", nil))
}
