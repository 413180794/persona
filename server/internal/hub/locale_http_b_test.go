package hub

import (
	"errors"
	"testing"

	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// The work line of an agent waiting on the user reads in the language asked for, Chinese when none was said.
func TestInteractionLineLanguages(t *testing.T) {
	plan := proto.Interaction{Kind: proto.IxPlan}
	perm := proto.Interaction{Kind: proto.IxPermission, Summary: "Bash: go test"}
	for _, c := range []struct {
		l    i18n.Lang
		ix   proto.Interaction
		want string
	}{
		{"", plan, "计划等你审批"},
		{i18n.ZhHans, perm, "等你授权：Bash: go test"},
		{i18n.En, plan, "Plan waiting for approval"},
		{i18n.En, perm, "Waiting for approval: Bash: go test"},
		{i18n.Ja, proto.Interaction{}, "ターミナルが対応待ち"},
	} {
		if got := interactionLine(c.l, c.ix); got != c.want {
			t.Errorf("%q %s: %q, want %q", c.l, c.ix.Kind, got, c.want)
		}
	}
}

func TestMorePermissionsTextLanguages(t *testing.T) {
	one := []extension.PermissionLine{{Title: "访问更多网络", Detail: "api.example.com"}}
	if got := morePermissionsText("", one); got != "要多一项权限：访问更多网络（api.example.com）" {
		t.Fatalf("default: %q", got)
	}
	two := []extension.PermissionLine{{Title: "Network", Detail: "api.example.com"}, {Title: "Files"}}
	if got := morePermissionsText(i18n.En, two); got != "Asks for two more permissions: Network (api.example.com), Files" {
		t.Fatalf("en: %q", got)
	}
	if got := morePermissionsText(i18n.En, one[:1]); got != "Asks for one more permission: 访问更多网络 (api.example.com)" {
		t.Fatalf("en one: %q", got)
	}
	many := make([]extension.PermissionLine, 7)
	for i := range many {
		many[i].Title = "p"
	}
	if got := morePermissionsText(i18n.Ja, many); got != "権限が7件増えます：p、p、p、p、p、p、p" {
		t.Fatalf("ja: %q", got)
	}
}

// Errors keep their Chinese Error() (logs, older paths) and read in a client's language when written to it; the ones
// with detail still match their kind.
func TestHTTPBErrorsLocalize(t *testing.T) {
	err := adminPasswordProblem("short")
	if err == nil || err.Error() != "密码至少 8 个字符" && err.Error() != i18n.T("", "hub-admin_auth.password_too_short", i18n.Args{"min": adminPasswordMin}) {
		t.Fatalf("default: %v", err)
	}
	if got := i18n.Localize(err, i18n.En); got != i18n.T(i18n.En, "hub-admin_auth.password_too_short", i18n.Args{"min": adminPasswordMin}) || got == err.Error() {
		t.Fatalf("en: %q", got)
	}
	if got := i18n.Localize(errDeleteUnknown, i18n.Ja); got != "そのセッションはありません" {
		t.Fatalf("ja: %q", got)
	}
	if errDeleteUnknown.Error() != "没有这个会话" {
		t.Fatalf("Error() changed: %q", errDeleteUnknown.Error())
	}
	kind := localizedKind(errShortcutArg, "hub-extension_system.shortcut_missing", i18n.Args{"title": "City"})
	if !errors.Is(kind, errShortcutArg) {
		t.Fatal("kind lost")
	}
	if kind.Error() != "快捷指令的参数不对：缺 City" {
		t.Fatalf("default: %q", kind.Error())
	}
	if got := i18n.Localize(kind, i18n.En); got != "The shortcut’s parameters aren’t right: City is missing" {
		t.Fatalf("en: %q", got)
	}
}

func TestPendingRestartLanguages(t *testing.T) {
	if got := i18n.T(i18n.En, "hub-admin_hub.restart_add_computer", i18n.Args{"id": "box"}); got != "Add computer “box”" {
		t.Fatalf("en: %q", got)
	}
	if got := i18n.T("", "hub-admin_hub.restart_add_computer", i18n.Args{"id": "box"}); got != "添加电脑「box」" {
		t.Fatalf("default: %q", got)
	}
}
