package hub

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
)

// localizedFailingHub is a loopback hub whose every route fails the way the real one writes a 502 with an arbitrary
// cause: in the request's (or the user's) language, the generic summary in error and the cause in detail (#5514).
func localizedFailingHub(t *testing.T, cause string) *Config {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeErr(routeLanguageWriter{ResponseWriter: w, language: i18n.FromRequest(r).Or(i18n.Default)}, http.StatusBadGateway, errors.New(cause))
	}))
	t.Cleanup(srv.Close)
	return &Config{Listen: strings.TrimPrefix(srv.URL, "http://"), Token: "t"}
}

// The hub's own command line says why the hub refused, not only the localized summary of a 502 (#5514 moved the cause
// to detail; #5579 taught the center's readers, not the CLI's).
func TestCLIReadsTheHubsReason(t *testing.T) {
	const cause = "dial tcp 192.0.2.7:18660: connect: connection refused"
	c := localizedFailingHub(t, cause)
	if err := c.adminCall(context.Background(), http.MethodPost, "/api/v1/admin/cli/peers", map[string]string{"code": "x"}, nil); err == nil || !strings.Contains(err.Error(), cause) {
		t.Errorf("peer CLI: %v, want the cause %q", err, cause)
	}
	if _, err := c.requestPairCodeWith(context.Background(), map[string]any{"name": "mac"}); err == nil || !strings.Contains(err.Error(), cause) {
		t.Errorf("pair code: %v, want the cause %q", err, cause)
	}
}
