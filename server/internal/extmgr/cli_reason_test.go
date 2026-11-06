package extmgr

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/i18n"
)

// The extension command says why the hub failed: a 502 the hub localized keeps its cause in detail (#5514).
func TestExtensionCLIReadsTheHubsReason(t *testing.T) {
	const cause = "market download: unexpected status 404"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apierr.Write(&localeWriter{ResponseWriter: w, lang: i18n.FromRequest(r)}, http.StatusBadGateway, errors.New(cause))
	}))
	defer srv.Close()
	c := &extensionClient{cfg: &Config{Listen: strings.TrimPrefix(srv.URL, "http://"), Token: "t"}}
	if err := c.do(context.Background(), http.MethodPost, "/api/v1/extensions/demo/install", nil, nil); err == nil || !strings.Contains(err.Error(), cause) {
		t.Fatalf("%v, want the cause %q", err, cause)
	}
}
