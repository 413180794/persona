//go:build go1.25

package hub

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestRequestClientSlowLog(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newTestHub(t)
		var logs bytes.Buffer
		h.logger = log.New(&logs, "", 0)
		handler := logRequests(h, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(3 * time.Second); w.WriteHeader(200) }))
		r := httptest.NewRequest("GET", "/api/v1/machines", nil)
		r.Header.Set("X-Persona-Client", "ios/1.2")
		handler.ServeHTTP(httptest.NewRecorder(), r)
		if !strings.Contains(logs.String(), `client="ios/1.2"`) {
			t.Fatalf("slow log: %q", logs.String())
		}
	})
}
