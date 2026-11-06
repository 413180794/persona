package link

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
)

// A pair link the hub refuses with a localized 5xx says why: the cause is in detail, the summary only says to look
// there (#5728).
func TestPairSaysWhyTheHubRefused(t *testing.T) {
	const cause = "database: connection refused"
	hub := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":"the request did not complete; see the details","code":"internal","detail":%q}`, cause)
	}))
	defer hub.Close()
	u, _ := netip.ParseAddrPort(strings.TrimPrefix(hub.URL, "https://"))
	link := fmt.Sprintf("persona://link?url=http%%3A%%2F%%2F127.0.0.1%%3A1&tls=%d&token=jp_x&hub=h1&fp=%s&machine=laptop", u.Port(), pinOf(hub))
	_, err := Pair(context.Background(), link, "", "", filepath.Join(t.TempDir(), "link.json"))
	if err == nil || !strings.Contains(err.Error(), cause) {
		t.Fatalf("%v, want the cause %q", err, cause)
	}
}
