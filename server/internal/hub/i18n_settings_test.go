package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
)

// The model config, its errors and the other texts for people from these settings read in the request's language
// (Accept-Language), else the user's; with neither they are the Chinese they were before i18n.
func TestModelConfigSpeaksTheRequestLanguage(t *testing.T) {
	h, mux := modelTestHub(t)
	handler := logRequests(h, mux)
	get := func(path, lang string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		if lang != "" {
			req.Header.Set("Accept-Language", lang)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	for _, c := range []struct{ lang, name, group, use, child string }{
		{"", "主对话", "默认模型", "定时任务", "信息处理"},
		{"en-US", "Main chat", "Default models", "Scheduled tasks", "Processing"},
		{"ja", "メインチャット", "デフォルトのモデル", "定期タスク", "情報処理"},
	} {
		w := get("/api/v1/model-config", c.lang)
		if w.Code != 200 {
			t.Fatalf("%q: %d %s", c.lang, w.Code, w.Body.String())
		}
		var out struct {
			Purposes []ModelPurpose `json:"purposes"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		p := out.Purposes[0]
		if p.ID != "assistant" || p.Name != c.name || p.Group != c.group || len(p.Uses) != 2 || p.Uses[1].Name != c.use {
			t.Fatalf("%q: %+v", c.lang, p)
		}
		w = get("/api/v1/model-config/purposes/translate", c.lang)
		var detail struct {
			Purpose ModelPurpose `json:"purpose"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
			t.Fatal(err)
		}
		if detail.Purpose.InheritedFrom != c.child {
			t.Fatalf("%q: inherited from %q", c.lang, detail.Purpose.InheritedFrom)
		}
	}
	for lang, want := range map[string]string{"": "没有这个模型用途", "en": "No such model use", "ja": "このモデルの用途はありません"} {
		w := get("/api/v1/model-config/purposes/missing", lang)
		var body struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if w.Code != http.StatusNotFound || body.Error != want {
			t.Fatalf("%q: %d %q", lang, w.Code, body.Error)
		}
	}
}

func TestSettingsTextsInEachLanguage(t *testing.T) {
	st := FleetState{NeedsYou: 1, Working: 2, Unread: 3}
	for l, want := range map[i18n.Lang]string{
		i18n.ZhHans: "1 个需要你 · 2 个在工作 · 3 个做完了",
		i18n.En:     "1 needs you · 2 working · 3 done",
		i18n.Ja:     "1件が要対応 · 2件が作業中 · 3件が完了",
	} {
		if got := fleetSentence(l, st); got != want {
			t.Errorf("%s fleet: %q", l, got)
		}
	}
	if got := fleetSentence(i18n.En, FleetState{NeedsYou: 2}); got != "2 need you" {
		t.Errorf("en plural: %q", got)
	}
	// The live activity's start alert, built without a language, is the Chinese it was.
	aps := startPayload(st, time.Now(), false)["aps"].(map[string]any)
	if body := aps["alert"].(map[string]any)["body"]; body != "1 个需要你 · 2 个在工作 · 3 个做完了" {
		t.Errorf("default alert: %q", body)
	}

	if got := computerSummary(i18n.En, "Studio", "run_script", "open -a Notes\nsecond", nil); got != "Run a script on “Studio”: open -a Notes" {
		t.Errorf("computer en: %q", got)
	}
	if got := computerSummary(i18n.ZhHans, "Studio", "run_script", "open -a Notes", nil); got != "在「Studio」上运行脚本：open -a Notes" {
		t.Errorf("computer zh: %q", got)
	}

	err := launcherInput{Name: "x", Kind: "nope"}.check()
	if err.Error() != "接入类型不支持" || i18n.Localize(err, i18n.En) != "Unsupported connection type" || i18n.Localize(err, i18n.Ja) != "対応していない接続の種類です" {
		t.Errorf("launcher check: %q / %q", err, i18n.Localize(err, i18n.En))
	}
	err = launcherInput{Name: "ok", BaseURL: "https://example.com", Model: "m", Args: []string{"x"}}.check()
	if err.Error() != `额外参数 "x" 要以 - 开头` || i18n.Localize(err, i18n.En) != `Extra argument "x" must start with -` {
		t.Errorf("launcher args: %q / %q", err, i18n.Localize(err, i18n.En))
	}

	if got := unknownDelivery(i18n.Ja, "hub-send.lost_during").Error(); got != "送信中にコンピューターとの接続が切れました：すでにコンピューターに届いている可能性があります。チャットを確認してから再送するか決めてください" {
		t.Errorf("send ja: %q", got)
	}
	if got := clipped(i18n.En, " "); got != "(none)" {
		t.Errorf("none en: %q", got)
	}
	if got := changeSummary(i18n.ZhHans, "规则", "", "新"); got != "改规则：（无） → 新" {
		t.Errorf("change zh: %q", got)
	}
	_, summary, err := mailAccountRequest(i18n.En, json.RawMessage(`{"email":"me@example.com"}`))
	if err != nil || summary != "Add mail account me@example.com (servers detected automatically). After you enter the password it connects once before saving" {
		t.Errorf("mail en: %q %v", summary, err)
	}
}

func TestSelfUpdateWhyNotInEachLanguage(t *testing.T) {
	h := newTestHub(t)
	u := &updater{h: h}
	if err := u.whyNot(); err.Error() != "这个 hub 不能自己更新：开发版（"+h.version+"）" {
		t.Errorf("zh: %q", err)
	}
	if got := u.whyNotReason(i18n.En); got != "Development build ("+h.version+")" {
		t.Errorf("en: %q", got)
	}
	if got := i18n.Localize(u.whyNot(), i18n.Ja); got != "このハブは自動で更新できません：開発版（"+h.version+"）" {
		t.Errorf("ja: %q", got)
	}
}
