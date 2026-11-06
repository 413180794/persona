package lcuconfirm

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func nativeParams(t *testing.T, persist []string, risk, warning string) json.RawMessage {
	t.Helper()
	body := map[string]any{
		"mode":    "form",
		"message": "Allow Computer Use to use \"Calculator\"?",
		"requestedSchema": map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		"_meta": map[string]any{
			"codex_approval_kind": "mcp_tool_call",
			"connector_id":        "computer-use",
			"tool_params":         map[string]any{"app": "com.apple.calculator"},
			"tool_params_display": []any{map[string]any{"name": "app", "value": "Calculator"}},
			"persist":             persist,
			"riskLevel":           risk,
			"warningSubtitle":     warning,
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func allow(always bool) proto.Resolution {
	yes := true
	return proto.Resolution{Allow: &yes, Always: always}
}

func deny() proto.Resolution {
	no := false
	return proto.Resolution{Allow: &no}
}

func action(t *testing.T, out map[string]any) string {
	t.Helper()
	s, _ := out["action"].(string)
	return s
}

func persist(t *testing.T, out map[string]any) string {
	t.Helper()
	meta, _ := out["_meta"].(map[string]any)
	if meta == nil {
		return ""
	}
	s, _ := meta["persist"].(string)
	return s
}

func TestNativeAppApprovalUsesThePermissionCard(t *testing.T) {
	raw := nativeParams(t, []string{"session"}, "", "")
	ix := Card(raw)
	if ix.Kind != proto.IxPermission || ix.ToolName != "操控电脑" || !strings.Contains(ix.Summary, "「Calculator」") {
		t.Fatalf("card: %+v", ix)
	}
	if ix.AlwaysLabel != "本会话允许「Calculator」" {
		t.Fatalf("session scope is the card's always: %q", ix.AlwaysLabel)
	}
	if got := Answer(raw, allow(false)); action(t, got) != "accept" || persist(t, got) != "" {
		t.Fatalf("once: %+v", got)
	}
	if got := Answer(raw, allow(true)); action(t, got) != "accept" || persist(t, got) != "session" {
		t.Fatalf("session: %+v", got)
	}
	if got := Answer(raw, deny()); action(t, got) != "decline" {
		t.Fatalf("deny: %+v", got)
	}
	if got := Answer(raw, proto.Resolution{Dismiss: true}); action(t, got) != "cancel" {
		t.Fatalf("dismiss: %+v", got)
	}
	if got := Answer(raw, proto.Resolution{}); action(t, got) != "cancel" {
		t.Fatalf("nothing answered: %+v", got)
	}
	yes := true
	if got := Answer(raw, proto.Resolution{Allow: &yes, AnsweredBy: proto.AnsweredByAssistant}); action(t, got) != "cancel" {
		t.Fatalf("the assistant must not approve: %+v", got)
	}
	// A raw elicitation accept, the shape the runtime itself uses, is not a permission-card answer.
	if got := Answer(raw, proto.Resolution{Action: "accept"}); action(t, got) != "cancel" {
		t.Fatalf("action alone is not an approval: %+v", got)
	}
}

func TestNativeAppBothScopesAreAQuestion(t *testing.T) {
	raw := nativeParams(t, []string{"always", "session"}, "high", "Allowing ChatGPT to use this app can monitor ChatGPT")
	ix := Card(raw)
	if ix.Kind != proto.IxQuestion || len(ix.Questions) != 1 || len(ix.Questions[0].Options) != 4 {
		t.Fatalf("once, session, always, deny: %+v", ix)
	}
	if !strings.Contains(ix.Summary, "高风险") || strings.Contains(ix.Summary, "ChatGPT") || !strings.Contains(ix.Reason, "助手") {
		t.Fatalf("warning is rewritten onto the card: %q reason %q", ix.Summary, ix.Reason)
	}
	q := ix.Questions[0].Question
	for _, c := range []struct {
		label, want, scope string
	}{
		{"允许一次", "accept", ""},
		{"本会话允许", "accept", "session"},
		{"一直允许", "accept", "always"},
		{"拒绝", "decline", ""},
		{"yes please", "cancel", ""},
	} {
		got := Answer(raw, proto.Resolution{Answers: map[string]string{q: c.label}})
		if action(t, got) != c.want || persist(t, got) != c.scope {
			t.Fatalf("%s: %+v", c.label, got)
		}
	}
}

func TestAlwaysScopeWithoutSession(t *testing.T) {
	raw := nativeParams(t, []string{"always"}, "", "")
	ix := Card(raw)
	if ix.Kind != proto.IxPermission || ix.AlwaysLabel != "一直允许「Calculator」" {
		t.Fatalf("%+v", ix)
	}
	if got := Answer(raw, allow(true)); persist(t, got) != "always" {
		t.Fatalf("%+v", got)
	}
	if got := Answer(raw, allow(false)); persist(t, got) != "" || action(t, got) != "accept" {
		t.Fatalf("once stays once: %+v", got)
	}
}

func TestNoPersistDoesNotInventAGrant(t *testing.T) {
	raw := nativeParams(t, nil, "", "")
	ix := Card(raw)
	if ix.AlwaysLabel != "" {
		t.Fatalf("no longer scope was offered: %q", ix.AlwaysLabel)
	}
	// The web card still shows "Always allow". That press must not store a grant the runtime did not offer.
	if got := Answer(raw, allow(true)); action(t, got) != "accept" || persist(t, got) != "" {
		t.Fatalf("%+v", got)
	}
}

func TestBrowserOriginAndEmptyFormAndURL(t *testing.T) {
	origin := json.RawMessage(`{"mode":"form","message":"Open this site?","requestedSchema":{"type":"object","properties":{}},"_meta":{"tool_name":"access_browser_origin","origin":"https://example.com"}}`)
	ix := Card(origin)
	if ix.Kind != proto.IxPermission || !strings.Contains(ix.Summary, "https://example.com") {
		t.Fatalf("%+v", ix)
	}
	if got := Answer(origin, allow(true)); action(t, got) != "accept" || persist(t, got) != "" {
		t.Fatalf("an origin is once: %+v", got)
	}
	if got := Answer(origin, deny()); action(t, got) != "decline" {
		t.Fatalf("%+v", got)
	}
	bad := json.RawMessage(`{"mode":"form","message":"Open this site?","_meta":{"tool_name":"access_browser_origin","origin":"file:///etc/passwd"}}`)
	if Kind(bad) == KindOrigin {
		t.Fatal("only an exact http(s) origin")
	}
	if got := Answer(bad, allow(true)); action(t, got) != "cancel" {
		t.Fatalf("a bad origin is not accepted: %+v", got)
	}

	form := json.RawMessage(`{"mode":"form","message":"Allow this?","requestedSchema":{"type":"object","properties":{}}}`)
	if Kind(form) != KindForm {
		t.Fatal(Kind(form))
	}
	if action(t, Answer(form, allow(false))) != "accept" || action(t, Answer(form, deny())) != "decline" {
		t.Fatal("empty form")
	}

	url := json.RawMessage(`{"mode":"url","message":"Confirm in the browser","url":"https://example.com/confirm","elicitationId":"e1"}`)
	ix = Card(url)
	if !strings.Contains(ix.Summary, "https://example.com/confirm") {
		t.Fatalf("%+v", ix)
	}
	got := Answer(url, allow(true))
	if action(t, got) != "accept" || got["content"] != nil || persist(t, got) != "" {
		t.Fatalf("url accept carries no invented content: %+v", got)
	}
	if action(t, Answer(url, deny())) != "decline" || action(t, Answer(url, proto.Resolution{Dismiss: true})) != "cancel" {
		t.Fatal("url deny and dismiss")
	}
}

func TestEnumAndFieldsAndUnknown(t *testing.T) {
	enum := json.RawMessage(`{"mode":"form","message":"Pick one","requestedSchema":{"type":"object","properties":{"choice":{"type":"string","enum":["alpha","beta"]}},"required":["choice"]}}`)
	ix := Card(enum)
	if ix.Kind != proto.IxQuestion || len(ix.Questions[0].Options) != 3 {
		t.Fatalf("%+v", ix)
	}
	got := Answer(enum, proto.Resolution{Answers: map[string]string{ix.Questions[0].Question: "beta"}})
	content, _ := got["content"].(map[string]any)
	if action(t, got) != "accept" || content["choice"] != "beta" {
		t.Fatalf("%+v", got)
	}
	if action(t, Answer(enum, proto.Resolution{Answers: map[string]string{"Pick one": "nope"}})) != "cancel" {
		t.Fatal("unknown label")
	}

	fields := json.RawMessage(`{"mode":"form","message":"Say the title","requestedSchema":{"type":"object","properties":{"title":{"type":"string","title":"标题"}},"required":["title"]}}`)
	ix = Card(fields)
	if ix.Kind != proto.IxPermission || len(ix.Fields) != 1 || ix.Fields[0].Key != "title" || ix.Fields[0].Optional {
		t.Fatalf("%+v", ix)
	}
	got = Answer(fields, proto.Resolution{Allow: allow(false).Allow, Values: map[string]string{"title": "hello"}})
	content, _ = got["content"].(map[string]any)
	if content["title"] != "hello" {
		t.Fatalf("%+v", got)
	}
	if action(t, Answer(fields, allow(false))) != "cancel" {
		t.Fatal("required field missing is not an accept")
	}

	other := json.RawMessage(`{"mode":"form","message":"Something nested","requestedSchema":{"type":"object","properties":{"box":{"type":"object"}}}}`)
	ix = Card(other)
	if ix.Kind != proto.IxPermission || !strings.Contains(ix.Summary, "取消") {
		t.Fatalf("%+v", ix)
	}
	if action(t, Answer(other, allow(true))) != "cancel" || action(t, Answer(other, deny())) != "decline" {
		t.Fatal("an unreadable schema is never accepted")
	}
}

func TestStateAndStepsJS(t *testing.T) {
	if !strings.Contains(StateJS("", ""), "cua.getState()") || strings.Contains(StateJS("", ""), "getApp") {
		t.Fatal(StateJS("", ""))
	}
	app := StateJS("Calculator", "7")
	if !strings.Contains(app, `cua.getApp("Calculator")`) || !strings.Contains(app, `"7"`) {
		t.Fatal(app)
	}
	code, err := StepsJS("Notes", json.RawMessage(`[{"click":{"app":"Calculator","role":"button","name":"7"}},{"press_key":{"key":"return"}},{"type_text":{"text":"hi\"there"}},{"launch_app":{"name":"TextEdit"}}]`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`appNamed("Calculator")`, `.click(`, `"role":"button"`, `pressKey("return")`, `appNamed("Notes")`, `typeText("hi\"there")`, `cua.launch("TextEdit")`} {
		if !strings.Contains(code, want) {
			t.Fatalf("missing %s in %s", want, code)
		}
	}
	if _, err := StepsJS("", json.RawMessage(`[]`)); err == nil {
		t.Fatal("empty steps")
	}
	bad, err := StepsJS("", json.RawMessage(`[{"explode":{"app":"Calculator"}}]`))
	if err != nil || !strings.Contains(bad, "unsupported step explode") {
		t.Fatalf("%v %s", err, bad)
	}
}
