package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/mail"
	"github.com/nwverse/persona/server/internal/rpc"
)

func errorResponse(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("non-JSON error: %v", w.Header())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	var legacy struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &legacy); err != nil || legacy.Error == "" {
		t.Fatalf("legacy error: %+v (%v)", legacy, err)
	}
	if code, ok := body["code"].(string); !ok || code == "" {
		t.Fatalf("missing code: %v", body)
	}
	if v, ok := body["retryable"]; ok {
		if _, valid := v.(bool); !valid {
			t.Fatalf("invalid retryable: %v", body)
		}
	}
	if v, ok := body["delivered"]; ok && v != "none" && v != "unknown" && v != "yes" {
		t.Fatalf("invalid delivered: %v", body)
	}
	return body
}

func TestErrorCodes(t *testing.T) {
	for _, tt := range []struct {
		status int
		code   string
	}{
		{400, "bad_request"}, {401, "invalid_token"}, {403, "forbidden"}, {404, "not_found"}, {405, "method_not_allowed"},
		{408, "timeout"}, {409, "conflict"}, {410, "gone"}, {413, "too_large"}, {429, "rate_limited"},
		{500, "internal"}, {502, "internal"}, {503, "internal"}, {504, "timeout"},
	} {
		t.Run(tt.code+fmt.Sprint(tt.status), func(t *testing.T) {
			w := httptest.NewRecorder()
			writeErr(w, tt.status, errors.New("示例错误"))
			body := errorResponse(t, w)
			if w.Code != tt.status || body["code"] != tt.code || body["error"] != "示例错误" {
				t.Fatalf("%d %v", w.Code, body)
			}
			if tt.status == 500 && (body["retryable"] != nil || body["delivered"] != nil) {
				t.Fatalf("invented delivery or retryability: %v", body)
			}
		})
	}
	w := httptest.NewRecorder()
	writeErrCode(w, 403, "tls_required", errors.New("需要加密连接"))
	if body := errorResponse(t, w); body["code"] != "tls_required" {
		t.Fatal(body)
	}
}

func TestCallErrorDelivery(t *testing.T) {
	for _, tt := range []struct {
		name            string
		err             error
		status          int
		code, delivered string
		retryable       any
	}{
		{"offline", fmt.Errorf("node: %w", errOffline), 503, "node_unreachable", "none", true},
		{"closed", fmt.Errorf("send: %w", rpc.ErrClosed), 502, "node_unreachable", "", nil},
		{"eof", io.EOF, 502, "node_unreachable", "", nil},
		{"timeout", fmt.Errorf("send: %w", context.DeadlineExceeded), 502, "timeout", "", nil},
		{"network timeout", &net.DNSError{IsTimeout: true}, 502, "timeout", "", nil},
		{"cancelled", context.Canceled, 502, "internal", "", nil},
		{"plain", errors.New("node failed"), 502, "internal", "", nil},
		{"remote timeout", &rpc.Error{Message: context.DeadlineExceeded.Error()}, 502, "timeout", "", nil},
		{"coded timeout", &rpc.Error{Code: "timeout", Message: "node deadline"}, 502, "timeout", "", nil},
		{"invalid optional fields", &rpc.Error{Message: "failed", Data: json.RawMessage(`{"delivered":{},"retryable":"yes"}`)}, 502, "internal", "", nil},
		{"remote unknown", &rpc.Error{Message: "partly typed", Data: json.RawMessage(`{"delivered":"unknown"}`)}, 502, "internal", "unknown", nil},
		{"remote yes", &rpc.Error{Message: "acknowledgement failed", Data: json.RawMessage(`{"delivered":"yes","retryable":false}`)}, 502, "internal", "yes", false},
		{"null data", &rpc.Error{Message: "failed", Data: json.RawMessage(`null`)}, 502, "internal", "", nil},
		{"malformed data", &rpc.Error{Message: "failed", Data: json.RawMessage(`[]`)}, 502, "internal", "", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			callErr(w, tt.err)
			body := errorResponse(t, w)
			if delivered, _ := body["delivered"].(string); w.Code != tt.status || body["code"] != tt.code || delivered != tt.delivered || body["retryable"] != tt.retryable {
				t.Fatalf("%d %v", w.Code, body)
			}
		})
	}
}

func TestCallErrorRefusalFields(t *testing.T) {
	for _, code := range []string{"input_draft", "dialog_open", "prompt_changed", "attach_busy", "node_outdated"} {
		t.Run(code, func(t *testing.T) {
			w := httptest.NewRecorder()
			callErr(w, fmt.Errorf("send: %w", &rpc.Error{Code: code, Message: "请先处理当前输入", Data: json.RawMessage(`{"delivered":"none","draft":"示例草稿","clearable":"示例草稿","code":"ignored","error":"ignored"}`)}))
			body := errorResponse(t, w)
			if w.Code != 409 || body["code"] != code || body["error"] != "请先处理当前输入" || body["draft"] != "示例草稿" || body["clearable"] != "示例草稿" || body["delivered"] != "none" {
				t.Fatalf("%d %v", w.Code, body)
			}
		})
	}
}

// Only the API answers with the error body: everything outside /api is the web app (webApp), which serves its own 404.
func TestRouteErrorsJSON(t *testing.T) {
	h := newTestHub(t)
	mux := h.routes()
	for _, tt := range []struct {
		method, path string
		status       int
		code, allow  string
	}{
		{"GET", "/api/v1/missing", 404, "not_found", ""},
		{"POST", "/api/v1/machines", 405, "method_not_allowed", "GET, HEAD"},
		{"DELETE", "/api/v1/sessions", 405, "method_not_allowed", "GET, HEAD, POST"},
		{"GET", "/api/v1/machines/example/launchers", 404, "not_found", ""},
	} {
		t.Run(tt.method+tt.path, func(t *testing.T) {
			r := localRequest(tt.method, tt.path)
			r.Header.Set("Authorization", "Bearer "+h.cfg.Token)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			body := errorResponse(t, w)
			if w.Code != tt.status || body["code"] != tt.code || w.Header().Get("Allow") != tt.allow {
				t.Fatalf("%d %v %v", w.Code, body, w.Header())
			}
		})
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, localRequest("GET", "/api/v1/machines"))
	if body := errorResponse(t, w); w.Code != 401 || body["code"] != "invalid_token" || body["delivered"] != "none" {
		t.Fatalf("%d %v", w.Code, body)
	}
	for _, method := range []string{"GET", "HEAD"} {
		r := localRequest(method, "/api/v1/machines")
		r.Header.Set("Authorization", "Bearer "+h.cfg.Token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("matched route changed: %d", w.Code)
		}
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, localRequest("GET", "/api/v1/health"))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"error_codes"`) {
		t.Fatalf("health: %d %s", w.Code, w.Body.String())
	}
}

func TestMailRoutingErrorsUnchanged(t *testing.T) {
	h := newTestHub(t)
	h.mail = &mail.Service{}
	for _, tt := range []struct{ method, path string }{{"GET", "/api/v1/mail/missing"}, {"DELETE", "/api/v1/mail/accounts"}} {
		r := localRequest(tt.method, tt.path)
		r.Header.Set("Authorization", "Bearer "+h.cfg.Token)
		want := httptest.NewRecorder()
		h.mail.Handler().ServeHTTP(want, r)
		got := httptest.NewRecorder()
		h.routes().ServeHTTP(got, r)
		if got.Code != want.Code || got.Body.String() != want.Body.String() || got.Header().Get("Allow") != want.Header().Get("Allow") {
			t.Fatalf("mail error changed: %d %s", got.Code, got.Body.String())
		}
	}
}

func TestCallErrorKeepsWrappedLegacyMessage(t *testing.T) {
	w := httptest.NewRecorder()
	err := fmt.Errorf("send: %w", &rpc.Error{Message: "failed"})
	callErr(w, err)
	if body := errorResponse(t, w); body["error"] != err.Error() {
		t.Fatalf("legacy error text changed: %v", body)
	}
}

// localRequest comes from the LAN: httptest's default remote address is a public documentation one, which the
// gate would refuse as plaintext from the internet (gate.go).
func localRequest(method, path string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	r.RemoteAddr = "127.0.0.1:1234"
	return r
}
