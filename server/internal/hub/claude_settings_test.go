package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
)

func TestClaudeSettingsRoutesForwardResponseLocale(t *testing.T) {
	h := newTestHub(t)
	h.userLocale.Store(i18n.Ja)
	modelsNode(t, h, MachineConfig{ID: "box"}, func(method string, params map[string]any) (any, error) {
		return map[string]any{"method": method, "params": params}, nil
	})
	routes := h.routes()
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		for _, tc := range []struct{ header, want string }{
			{"en-US", "en"}, {"zh-TW", "zh-Hans"}, {"ja-JP", "ja"}, {"", "ja"},
		} {
			t.Run(method+"/"+tc.header, func(t *testing.T) {
				body := ""
				wantMethod := "claude.settings"
				if method == http.MethodPut {
					// The response language comes from the request, not an arbitrary body field.
					body = `{"key":"effortLevel","value":"auto","locale":"de"}`
					wantMethod += ".set"
				}
				r := httptest.NewRequest(method, "https://example.com/api/v1/machines/box/claude-settings", strings.NewReader(body))
				r.RemoteAddr = netip.AddrFrom4([4]byte{127, 0, 0, 1}).String() + ":1234"
				r.Header.Set("Authorization", "Bearer "+h.cfg.Token)
				r.Header.Set("Accept-Language", tc.header)
				w := httptest.NewRecorder()
				routes.ServeHTTP(w, r)
				if w.Code != http.StatusOK {
					t.Fatalf("%d %s", w.Code, w.Body)
				}
				var response struct {
					Method string         `json:"method"`
					Params map[string]any `json:"params"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if response.Method != wantMethod || response.Params["locale"] != tc.want {
					t.Fatalf("node received %+v, want %s in %s", response, wantMethod, tc.want)
				}
				if method == http.MethodPut && (response.Params["key"] != "effortLevel" || response.Params["value"] != "auto") {
					t.Fatalf("setting change lost: %+v", response.Params)
				}
			})
		}
	}
}
