package hub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// A provider check's failure reads in the request's language, with the gateway's own words and no key (#2455).
func TestProviderTestSpeaksTheRequestLanguage(t *testing.T) {
	h, mux := launcherTestHub(t)
	handler := logRequests(h, mux)
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid key ` + r.Header.Get("x-api-key") + `"}}`))
	}))
	defer gw.Close()
	for lang, want := range map[string]string{"": "认证失败（401）", "en": "Authentication failed (401)", "ja": "認証に失敗しました（401）"} {
		req := httptest.NewRequest("POST", "/api/v1/launchers/test", strings.NewReader(`{"base_url":"`+gw.URL+`","api_key":"`+fakeKey+`","model":"m"}`))
		if lang != "" {
			req.Header.Set("Accept-Language", lang)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		msg, _ := out["error"].(string)
		if !strings.HasPrefix(msg, want) || !strings.Contains(msg, "invalid key ***") || strings.Contains(msg, fakeKey) {
			t.Fatalf("%q: %q", lang, msg)
		}
	}
}

// A launcher's field errors read in each language and keep the values they name.
func TestLauncherProviderErrorsInEachLanguage(t *testing.T) {
	neg, effort := -1, "turbo"
	models := []proto.ProviderModel{{ID: "m1", Efforts: []string{"high"}, DefaultEffort: "max"}}
	for _, c := range []struct {
		in       launcherInput
		zh, en   string
		contains string
	}{
		{launcherInput{TimeoutMS: &neg}, "超时不能是负数", "Timeout can’t be negative", ""},
		{launcherInput{Effort: &effort}, "默认的思考档位 \"turbo\" 不支持", "Default thinking effort \"turbo\" isn’t supported", "low"},
		{launcherInput{Models: &models}, "模型 m1 的默认思考档位不在它支持的档位里", "The default thinking effort of model m1 isn’t one it supports", ""},
	} {
		err := c.in.checkProvider()
		if err == nil {
			t.Fatalf("%+v: no error", c.in)
		}
		zh, en := err.Error(), i18n.Localize(err, i18n.En)
		if !strings.HasPrefix(zh, c.zh) || !strings.HasPrefix(en, c.en) || !strings.Contains(en, c.contains) {
			t.Fatalf("zh %q / en %q", zh, en)
		}
	}
	if err := checkHeaders("X-Ok: 1\nbroken"); i18n.Localize(err, i18n.En) != "Header line 2 must be “Name: value”" {
		t.Fatalf("headers: %v", err)
	}
}

// The node's reasons for running an Agent SDK conversation on the CLI read in the app's language, the cause as it was;
// a reason the hub does not know, and Chinese, stay as they are.
func TestRuntimeFallbackInEachLanguage(t *testing.T) {
	for _, c := range []struct{ node, en string }{
		{fmt.Sprintf(proto.FallbackShellLauncher, "kcc"), "Launcher kcc is a shell command on this computer; the Agent SDK can’t use its settings"},
		{proto.FallbackNoNode + "exec: \"node\": not found", "No Node.js 22 or later: exec: \"node\": not found"},
		{proto.FallbackNoClaude + "nope", "Can’t find claude: nope"},
		{proto.FallbackNoBridge, "This personad doesn’t include the Agent SDK bridge"},
		{proto.FallbackNoBridgeScript, "The Agent SDK bridge package has no bridge.mjs"},
		{proto.FallbackReadBridge + "x", "Couldn’t read the Agent SDK bridge: x"},
		{proto.FallbackWriteBridge + "y", "Couldn’t write the Agent SDK bridge: y"},
		{proto.FallbackNoBridge + "，还有别的", proto.FallbackNoBridge + "，还有别的"},
		{"别的原因", "别的原因"},
		{"", ""},
	} {
		s := proto.Session{RuntimeFallback: c.node}
		localizeSession(i18n.En, &s)
		if s.RuntimeFallback != c.en {
			t.Errorf("%q: %q", c.node, s.RuntimeFallback)
		}
		s = proto.Session{RuntimeFallback: c.node}
		localizeSession(i18n.ZhHans, &s)
		if s.RuntimeFallback != c.node {
			t.Errorf("zh %q: %q", c.node, s.RuntimeFallback)
		}
	}
}
