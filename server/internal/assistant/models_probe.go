package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// 测试连接 / 获取模型 for a provider being added or edited (#2455, docs/launchers.md): the manager itself talks to the
// gateway, as cindy's provider-diagnostics / provider-model-fetch do. POST /api/v1/launchers/test sends one 1-token
// Messages request; POST /api/v1/launchers/discover lists GET <base>/v1/models, filled in from the built-in catalog.
// Neither ever puts the key or a header value in what it answers.

// probeTimeout bounds one request to the gateway (a var for the tests).
var probeTimeout = 30 * time.Second

// probeInput is the provider to talk to: a stored one by name, what the request gives overriding it.
type probeInput struct {
	Name    string  `json:"name"`
	BaseURL string  `json:"base_url"`
	Auth    string  `json:"auth"`
	APIKey  string  `json:"api_key"`
	Headers *string `json:"headers"`
	Model   string  `json:"model"`
}

// probeTarget is a resolved gateway: everything needed for one request.
type probeTarget struct {
	base, key, headers, model, proxy string
}

func (s *Service) probeTarget(ctx context.Context, in probeInput) (probeTarget, error) {
	var t probeTarget
	if in.Name != "" {
		list := s.storedLaunchers(ctx)
		i := slices.IndexFunc(list, func(l storedLauncher) bool { return l.Name == in.Name })
		if i < 0 {
			return t, i18n.Err("assistant-models_probe.err_no_provider", i18n.Args{"name": in.Name})
		}
		l := list[i]
		key, err := s.openKey(l.KeySealed)
		if err != nil {
			return t, i18n.Err("assistant-models_probe.err_open_key")
		}
		headers, err := s.openKey(l.HeadersSealed)
		if err != nil {
			return t, i18n.Err("assistant-models_probe.err_open_headers")
		}
		t = probeTarget{base: l.BaseURL, key: key, headers: headers, model: l.Model, proxy: l.Proxy}
	}
	if b := strings.TrimSpace(in.BaseURL); b != "" {
		// The stored key and headers only go to the stored gateway: another address gets just what the request brings.
		if in.Name != "" && strings.TrimRight(b, "/") != strings.TrimRight(strings.TrimSpace(t.base), "/") {
			t.key, t.headers = "", ""
		}
		t.base = b
	}
	if k := strings.TrimSpace(in.APIKey); k != "" {
		t.key = k
	}
	if in.Headers != nil {
		t.headers = *in.Headers
	}
	if m := strings.TrimSpace(in.Model); m != "" {
		t.model = m
	}
	// Both credential headers go anyway (cindy's buildProbeRequest), so Auth only tidies a pasted "Bearer …".
	t.key = strings.TrimSpace(strings.TrimPrefix(t.key, "Bearer "))
	if err := checkGateway(t.base); err != nil {
		return t, err
	}
	if err := checkHeaders(t.headers); err != nil {
		return t, err
	}
	t.base = strings.TrimRight(strings.TrimSpace(t.base), "/")
	return t, nil
}

// endpoint is <base>/v1/<path>, a base already ending in /v1 not doubled.
func (t probeTarget) endpoint(path string) string {
	if strings.HasSuffix(t.base, "/v1") {
		return t.base + "/" + path
	}
	return t.base + "/v1/" + path
}

// do sends one request with the custom headers first, then the version and both credential headers.
func (t probeTarget) do(ctx context.Context, method, endpoint string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	for _, hd := range parseHeaders(t.headers) {
		req.Header.Set(hd[0], hd[1])
	}
	if req.Header.Get("anthropic-version") == "" {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	if t.key != "" {
		req.Header.Set("x-api-key", t.key)
		req.Header.Set("authorization", "Bearer "+t.key)
	}
	// A redirect is answered as is, never followed: Go keeps x-api-key and the custom headers on a redirect to another
	// host, and the key must only go to the gateway asked.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if t.proxy != "" {
		// A gateway behind its own proxy: a one-off transport that keeps no connection behind.
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.DisableKeepAlives = true
		if u, err := url.Parse(t.proxy); err == nil {
			transport.Proxy = http.ProxyURL(u)
		}
		client.Transport = transport
	}
	return client.Do(req)
}

// parseHeaders reads "Name: value" lines (ANTHROPIC_CUSTOM_HEADERS); lines without a name are skipped.
func parseHeaders(raw string) [][2]string {
	var out [][2]string
	for _, line := range strings.Split(raw, "\n") {
		if name, value, ok := strings.Cut(strings.TrimSpace(line), ":"); ok && strings.TrimSpace(name) != "" {
			out = append(out, [2]string{strings.TrimSpace(name), strings.TrimSpace(value)})
		}
	}
	return out
}

// redact takes the key and every header value out of a message about the gateway.
func (t probeTarget) redact(s string) string {
	secrets := []string{t.key}
	for _, hd := range parseHeaders(t.headers) {
		secrets = append(secrets, hd[1])
	}
	for _, v := range secrets {
		if len(v) >= 4 {
			s = strings.ReplaceAll(s, v, "***")
		}
	}
	return s
}

// pt is an assistant-models_probe text in l.
func pt(l i18n.Lang, key string, args ...i18n.Args) string {
	return i18n.T(l, "assistant-models_probe."+key, args...)
}

// failure is what went wrong, said for a person (in ctx's language): the status first, then what the gateway said (redacted, clipped).
func (t probeTarget) failure(ctx context.Context, res *http.Response, err error) string {
	l := i18n.From(ctx)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			return pt(l, "timeout", i18n.Args{"seconds": int(probeTimeout / time.Second)})
		}
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err // the URL is the user's own; the cause is what helps
		}
		return clip(oneLine(t.redact(pt(l, "unreachable", i18n.Args{"error": err.Error()}))), 300)
	}
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	said := gatewayMessage(raw)
	var head string
	switch res.StatusCode {
	case http.StatusUnauthorized:
		head = pt(l, "status_401")
	case http.StatusForbidden:
		head = pt(l, "status_403")
	case http.StatusNotFound:
		head = pt(l, "status_404")
	case http.StatusTooManyRequests:
		head = pt(l, "status_429")
	default:
		head = pt(l, "status_other", i18n.Args{"status": res.StatusCode})
	}
	if said != "" {
		head = pt(l, "said", i18n.Args{"head": head, "said": said})
	}
	return clip(oneLine(t.redact(head)), 300)
}

// gatewayMessage is the error message in a gateway's answer ({"error":{"message"}}, {"error":"…"}, {"message"}), or
// the start of the body.
func gatewayMessage(raw []byte) string {
	var body struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
	}
	if json.Unmarshal(raw, &body) == nil {
		var e struct {
			Message string `json:"message"`
		}
		var s string
		switch {
		case json.Unmarshal(body.Error, &e) == nil && e.Message != "":
			return e.Message
		case json.Unmarshal(body.Error, &s) == nil && s != "":
			return s
		case body.Message != "":
			return body.Message
		}
	}
	return clip(oneLine(string(raw)), 160)
}

// providerTest: POST <base>/v1/messages, 1 token of "ping".
func (s *Service) providerTest(ctx context.Context, in probeInput) map[string]any {
	out := map[string]any{"ok": false, "ms": 0, "model": in.Model}
	t, err := s.probeTarget(ctx, in)
	if err != nil {
		out["error"] = i18n.Localize(err, i18n.From(ctx))
		return out
	}
	if out["model"] = t.model; t.model == "" {
		out["error"] = i18n.T(i18n.From(ctx), "assistant-models_launchers.err_model_required")
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	body, _ := json.Marshal(map[string]any{"model": t.model, "max_tokens": 1,
		"messages": []any{map[string]any{"role": "user", "content": "ping"}}})
	started := time.Now()
	res, err := t.do(ctx, http.MethodPost, t.endpoint("messages"), bytes.NewReader(body))
	out["ms"] = int(time.Since(started).Milliseconds())
	if err != nil || res.StatusCode/100 != 2 {
		out["error"] = t.failure(ctx, res, err)
	} else {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10)) // lets the connection be reused
		out["ok"] = true
	}
	if res != nil {
		res.Body.Close()
	}
	return out
}

// providerDiscover: GET <base>/v1/models, its models filled in from the catalog.
func (s *Service) providerDiscover(ctx context.Context, in probeInput) map[string]any {
	models := []proto.ProviderModel{}
	fail := func(msg string) map[string]any { return map[string]any{"models": models, "error": msg} }
	t, err := s.probeTarget(ctx, in)
	if err != nil {
		return fail(i18n.Localize(err, i18n.From(ctx)))
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	res, err := t.do(ctx, http.MethodGet, t.endpoint("models"), nil)
	if res != nil {
		defer res.Body.Close()
	}
	if err != nil || res.StatusCode/100 != 2 {
		return fail(t.failure(ctx, res, err))
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return fail(t.failure(ctx, nil, err))
	}
	listed, ok := parseModelsList(raw)
	if !ok {
		return fail(pt(i18n.From(ctx), "models_unreadable"))
	}
	for _, m := range listed {
		models = append(models, enrichModel(m))
	}
	return map[string]any{"models": models}
}

// parseModelsList reads the three shapes of a model list (cindy's parseModelsListResponse): {"data":[…]},
// {"models":[…]} and a bare array, of objects or of id strings; duplicates dropped, order kept.
func parseModelsList(raw []byte) ([]proto.ProviderModel, bool) {
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) != nil {
		var wrapped struct {
			Data   []json.RawMessage `json:"data"`
			Models []json.RawMessage `json:"models"`
		}
		if json.Unmarshal(raw, &wrapped) != nil {
			return nil, false
		}
		switch {
		case wrapped.Data != nil:
			list = wrapped.Data
		case wrapped.Models != nil:
			list = wrapped.Models
		default:
			return nil, false
		}
	}
	var out []proto.ProviderModel
	seen := map[string]bool{}
	for _, item := range list {
		var m proto.ProviderModel
		var id string
		if json.Unmarshal(item, &id) != nil {
			var fields map[string]json.RawMessage
			if json.Unmarshal(item, &fields) != nil {
				continue
			}
			str := func(k string) string {
				var s string
				_ = json.Unmarshal(fields[k], &s)
				return strings.TrimSpace(s)
			}
			num := func(k string) int {
				var f float64
				if json.Unmarshal(fields[k], &f) != nil || f <= 0 || f > 1e9 {
					return 0
				}
				return int(f)
			}
			for _, k := range []string{"id", "slug", "key", "model_name", "model_id"} {
				if id = str(k); id != "" {
					break
				}
			}
			if id == "" {
				continue
			}
			m.Name = str("display_name")
			if m.Name == "" {
				m.Name = str("name")
			}
			if m.Name == id {
				m.Name = ""
			}
			for _, k := range []string{"context_length", "context_window", "max_context_length", "max_input_tokens"} {
				if m.ContextWindow = num(k); m.ContextWindow != 0 {
					break
				}
			}
		}
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		m.ID = id
		out = append(out, m)
	}
	return out, true
}

func (s *Service) providerProbeRoutes() {
	s.handle("POST /api/v1/launchers/test", s.probeRoute(s.providerTest))
	s.handle("POST /api/v1/launchers/discover", s.probeRoute(s.providerDiscover))
}

// probeRoute is a test / discover handler: the request is a probeInput, the answer always 200 with ok / error inside.
func (s *Service) probeRoute(run func(context.Context, probeInput) map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in probeInput
		if err := decodeBody(w, r, &in, 1<<20); err != nil {
			writeBodyErr(w, err, i18n.Err("assistant-models_probe.err_bad_request"))
			return
		}
		// the answers' texts read in the request's language, else the user's (failure, providerTest)
		apierr.WriteJSON(w, run(i18n.With(r.Context(), s.requestCtxLang(r.Context())), in))
	}
}
