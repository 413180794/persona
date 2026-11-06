package i18n

import (
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestComputerTextLanguages(t *testing.T) {
	for _, c := range []struct {
		lang               Lang
		read, action, note string
	}{
		{En, "Could not read the clipboard", "unknown action", "ego lite first-time setup is not complete"},
		{ZhHans, "读不了剪贴板", "未知操作", "ego lite 还没做完首次设置"},
		{Ja, "クリップボードを読み取れませんでした", "不明な操作です", "ego liteの初期設定が完了していません"},
	} {
		if got := ComputerText(c.lang, "读不了剪贴板"); got != c.read {
			t.Fatalf("%s: %q", c.lang, got)
		}
		if got := Localize(ErrEnglish("hub-computer.err_unknown_action"), c.lang); got != c.action {
			t.Fatalf("%s: %q", c.lang, got)
		}
		if got := ComputerText(c.lang, "ego lite 还没做完首次设置"); got != c.note {
			t.Fatalf("%s: %q", c.lang, got)
		}
	}
	for _, lang := range []Lang{En, ZhHans, Ja} {
		if got := ComputerText(lang, "没放进剪贴板："); got != T(lang, "hub-computer.rpc_clipboard_write", Args{"detail": ""}) {
			t.Fatalf("empty diagnostic: %q", got)
		}
	}
	if got := Localize(ErrEnglish("hub-computer.err_unknown_action"), ""); got != "unknown action" {
		t.Fatalf("legacy English: %q", got)
	}
	for _, raw := range []string{"Original runtime message", `{"error":"读不了剪贴板"}`, "computer.call: args must be an object", "cua-driver run_script: original detail"} {
		for _, lang := range []Lang{En, ZhHans, Ja} {
			if got := ComputerText(lang, raw); got != raw {
				t.Fatalf("unknown/runtime data changed: %q", got)
			}
		}
	}
}

func TestComputerStatusCopiesAcrossLanguages(t *testing.T) {
	job := &proto.ComputerJob{Kind: "install", State: "failed", Step: "下载安装器", Error: "安装器失败（exit status 1）：original detail"}
	computer := &proto.Computer{Note: "这台 Mac 上没有装 Cua Driver", Job: job}
	browser := &proto.Browser{Note: "ego lite 还没做完首次设置", Job: job}
	for _, lang := range []Lang{En, ZhHans, Ja, En} {
		c, b := ComputerStatus(lang, computer), BrowserStatus(lang, browser)
		if c == computer || b == browser || c.Job == job || b.Job == job {
			t.Fatal("shared pointers escaped")
		}
		if c.Note != T(lang, "hub-computer.rpc_missing_cua") || b.Note != T(lang, "hub-browser.rpc_setup") || c.Job.Step != T(lang, "hub-computer.rpc_download_installer") || c.Job.Error != T(lang, "hub-computer.rpc_installer_failed", Args{"reason": "exit status 1", "detail": "original detail"}) {
			t.Fatalf("%s: %+v %+v", lang, c, b)
		}
		c.Job.Step = "modified presentation"
	}
	if job.Step != "下载安装器" || computer.Note != "这台 Mac 上没有装 Cua Driver" {
		t.Fatal("canonical state mutated")
	}
	if ComputerStatus(En, nil) != nil || BrowserStatus(En, nil) != nil {
		t.Fatal("nil capability is not nil")
	}
}
