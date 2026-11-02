package hub

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func securityRequest(handler http.Handler, method, path, token, body, remote string, tlsOn bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = remote
	r.Header.Set("Authorization", "Bearer "+token)
	if tlsOn {
		r.TLS = &tls.ConnectionState{}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func requireCode(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != status || (code != "" && body["code"] != code) {
		t.Fatalf("response %d %s, want %d %s", w.Code, w.Body.String(), status, code)
	}
}

func TestGateAndMasterSource(t *testing.T) {
	for _, tc := range []struct {
		name, remote, token string
		tls, allow          bool
		status              int
		code                string
	}{
		{"plaintext public", "198.51.100.1:1", "invalid", false, false, 403, "tls_required"},
		{"plaintext switch", "198.51.100.1:1", "invalid", false, true, 401, ""},
		{"TLS public master", "198.51.100.1:1", "master", true, false, 403, "master_token_local_only"},
		{"switch still restricts master", "198.51.100.1:1", "master", false, true, 403, "master_token_local_only"},
		{"loopback master", "127.0.0.1:1", "master", false, false, 200, ""},
		{"trusted master", "192.0.2.1:1", "master", false, false, 200, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHub(t)
			h.cfg.Security = SecurityConfig{TrustedCIDRs: []string{"192.0.2.0/24"}, AllowPublicPlaintext: tc.allow}
			token := tc.token
			if token == "master" {
				token = h.cfg.Token
			}
			w := securityRequest(h.routes(), "GET", "/api/v1/machines", token, "", tc.remote, tc.tls)
			requireCode(t, w, tc.status, tc.code)
		})
	}
	h := newTestHub(t)
	// Health intentionally bypasses gate and authentication for public discovery.
	w := securityRequest(h.routes(), "GET", "/api/v1/health", "", "", "198.51.100.1:1", false)
	requireCode(t, w, 200, "")
}

func TestPlaintextWarning(t *testing.T) {
	h := newTestHub(t)
	h.cfg.Security.AllowPublicPlaintext = true
	var logs bytes.Buffer
	h.logger = log.New(&logs, "", 0)
	_ = h.routes()
	if !strings.Contains(logs.String(), "WARNING: security.allow_public_plaintext") {
		t.Fatal(logs.String())
	}
}
