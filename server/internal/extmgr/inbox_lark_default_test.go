package extmgr

import (
	"context"
	"testing"

	"github.com/nwverse/persona/server/internal/lark"
)

// The inbox's default Feishu account is the Feishu extension's, not hub.json's lark.account (#5098); while the
// extension can't answer it stays the one it answered last, and only before it ever answered is it hub.json's.
func TestInboxLarkAccount(t *testing.T) {
	type step struct {
		ext  int // the extension's default; 0: it can't answer
		want int
	}
	cases := []struct {
		name   string
		config int // hub.json's lark.account
		steps  []step
	}{
		{"same on both sides", 2, []step{{2, 2}}},
		{"the extension's default differs from hub.json", 1, []step{{2, 2}}},
		{"the extension's default differs from hub.json left out", 0, []step{{3, 3}}},
		{"the extension's default changes", 1, []step{{2, 2}, {3, 3}}},
		{"no extension: hub.json", 2, []step{{0, 2}}},
		{"no extension, hub.json left out: zero (account 1)", 0, []step{{0, 0}}},
		{"the extension stops answering: the one it answered last, not hub.json", 1, []step{{2, 2}, {0, 2}, {0, 2}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := &Manager{cfg: &Config{Lark: lark.Config{Account: c.config}}}
			var ext int
			h.larkDefault.read = func(context.Context) (int, bool) { return ext, ext > 0 }
			for i, s := range c.steps {
				ext = s.ext
				if got := h.inboxConfigView(context.Background()).LarkAccount; got != s.want {
					t.Fatalf("step %d (extension %d, hub.json %d): lark_account %d, want %d", i, s.ext, c.config, got, s.want)
				}
			}
		})
	}
}

// Without the Feishu extension running (no messaging source "lark"), the real read gives up and hub.json's is used.
func TestInboxLarkAccountWithoutExtension(t *testing.T) {
	h := &Manager{cfg: &Config{Lark: lark.Config{Account: 2}}}
	if got := h.inboxConfigView(context.Background()).LarkAccount; got != 2 {
		t.Fatalf("lark_account %d, want hub.json's 2", got)
	}
}

func TestLarkDefaultOf(t *testing.T) {
	cases := []struct {
		body string
		want int
		ok   bool
	}{
		{`{"accounts": [{"id": "1", "signed_in": true}, {"id": "2", "signed_in": true}], "default": "2"}`, 2, true},
		{`{"accounts": [{"id": "1", "signed_in": true}], "default": "1"}`, 1, true},
		{`{"accounts": [], "default": ""}`, 0, false},
		{`{"accounts": []}`, 0, false},
		{`{"accounts": [], "default": "work"}`, 0, false},
		{`{"accounts": [], "default": "0"}`, 0, false},
		{`{"accounts": [], "default": "-1"}`, 0, false},
		{`{"accounts": [], "default": "1000000000"}`, 0, false},
		{`not json`, 0, false},
	}
	for _, c := range cases {
		if got, ok := larkDefaultOf([]byte(c.body)); got != c.want || ok != c.ok {
			t.Errorf("%s: %d %v, want %d %v", c.body, got, ok, c.want, c.ok)
		}
	}
}
