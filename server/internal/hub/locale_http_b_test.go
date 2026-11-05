package hub

import (
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
)

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
}

func TestPendingRestartLanguages(t *testing.T) {
	if got := i18n.T(i18n.En, "hub-admin_hub.restart_add_computer", i18n.Args{"id": "box"}); got != "Add computer “box”" {
		t.Fatalf("en: %q", got)
	}
	if got := i18n.T("", "hub-admin_hub.restart_add_computer", i18n.Args{"id": "box"}); got != "添加电脑「box」" {
		t.Fatalf("default: %q", got)
	}
}
