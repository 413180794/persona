package apierr

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// langWriter is a response that knows its request's language, as the hub's request logger's does (i18n.Carrier).
type langWriter struct {
	*httptest.ResponseRecorder
	lang i18n.Lang
}

func (w langWriter) Lang() i18n.Lang { return w.lang }

func call(t *testing.T, w http.ResponseWriter, rec *httptest.ResponseRecorder, err error) map[string]any {
	t.Helper()
	Call(w, err)
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

// The refusals that say why the main conversation cannot start are 409 with a sentence for people in the request's
// language, the caller's words as the detail and what to do (docs/onboarding.md 5.2), wrapped or not.
func TestCallExplainsCodedRefusals(t *testing.T) {
	raw := "claude 没有启动起来（0s 后退出），消息没有送达：zsh:1: command not found: claude"
	refusal := &rpc.Error{Code: proto.CodeClaudeMissing, Message: raw, Data: json.RawMessage(`{"delivered":"none","launcher":"claude"}`)}
	for _, tc := range []struct {
		name string
		lang i18n.Lang
		err  error
		want string
	}{
		{"no language", "", refusal, "这台电脑上还没装 Claude Code。"},
		{"English", i18n.En, refusal, "Claude Code isn't installed on this computer yet."},
		{"named", "", OnMachine(fmt.Errorf("send: %w", refusal), "小林的 MacBook"), "小林的 MacBook 上还没装 Claude Code。"},
		{"Japanese", i18n.Ja, OnMachine(refusal, "小林的 MacBook"), "小林的 MacBook にはまだ Claude Code がインストールされていません。"},
	} {
		rec := httptest.NewRecorder()
		body := call(t, langWriter{rec, tc.lang}, rec, tc.err)
		if rec.Code != http.StatusConflict || body["code"] != proto.CodeClaudeMissing || body["error"] != tc.want ||
			body["detail"] != raw || body["action"] != "install_claude" || body["delivered"] != "none" {
			t.Errorf("%s: %d %v", tc.name, rec.Code, body)
		}
	}

	rec := httptest.NewRecorder()
	body := call(t, rec, rec, &rpc.Error{Code: proto.CodeLauncherMissing, Message: "cc_gone 没有启动起来",
		Data: json.RawMessage(`{"launcher":"cc_gone","machine_name":"mbp","action":"edit_provider"}`)})
	if body["error"] != "在 mbp 上找不到「cc_gone」这个模型入口，可能被删了。" || body["action"] != "edit_provider" {
		t.Errorf("launcher_missing: %v", body)
	}

	rec = httptest.NewRecorder()
	body = call(t, rec, rec, &rpc.Error{Code: proto.CodeNoMachine, Message: "assistant machine is not configured"})
	if rec.Code != http.StatusConflict || body["error"] != "Persona 要在你的一台电脑上干活，现在还没接上电脑。" || body["action"] != "add_computer" {
		t.Errorf("no_machine: %d %v", rec.Code, body)
	}
}

// Other coded refusals keep the words the node wrote; OnMachine leaves them and plain errors alone.
func TestCallKeepsOtherRefusals(t *testing.T) {
	rec := httptest.NewRecorder()
	refusal := &rpc.Error{Code: "input_draft", Message: "请先处理示例终端状态。", Data: json.RawMessage(`{"delivered":"none"}`)}
	body := call(t, rec, rec, OnMachine(refusal, "mbp"))
	if rec.Code != http.StatusConflict || body["error"] != "请先处理示例终端状态。" || body["detail"] != nil || body["action"] != nil || body["machine_name"] != nil {
		t.Errorf("input_draft: %d %v", rec.Code, body)
	}
	plain := errors.New("boom")
	if OnMachine(plain, "mbp") != plain {
		t.Error("a plain error is changed")
	}
}

// Status codes and retryability stay stable; only exact standard status text is translated for an explicit locale.
func TestStandardStatusTextLocalization(t *testing.T) {
	for status, key := range standardStatusKeys {
		for _, lang := range []i18n.Lang{i18n.En, i18n.ZhHans, i18n.Ja, ""} {
			rec := httptest.NewRecorder()
			original := errors.New(http.StatusText(status))
			Write(langWriter{rec, lang}, status, original)
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			want := original.Error()
			if lang != "" {
				want = i18n.T(lang, key)
			}
			if body["error"] != want || body["code"] != Body(status, "", original)["code"] || rec.Code != status {
				t.Fatalf("%d %s: %v, want %q", status, lang, body, want)
			}
		}
	}
	rec := httptest.NewRecorder()
	Write(langWriter{rec, i18n.Ja}, 404, errors.New("custom diagnostic"))
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "custom diagnostic" {
		t.Fatalf("custom text changed: %v", body)
	}
}
