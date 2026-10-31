package node

import "testing"

func TestTabLabel(t *testing.T) {
	taken := map[string]bool{"duo-sidebar": true, "duo-sidebar-2": true, "agent-1": true}
	cases := []struct{ name, want string }{
		{"inbox-agent", "inbox-agent"},
		{"duo-sidebar", "duo-sidebar-3"},
		{"", "agent-2"},
		{"  leader \n kiro ", "leader kiro"},
		{"一个非常非常非常非常非常非常非常非常非常非常长的标签名字超过了三十二个字", "一个非常非常非常非常非常非常非常非常非常非常长的标签名字超过了三"}, // 32 characters
	}
	for _, c := range cases {
		if got := tabLabel(c.name, taken); got != c.want {
			t.Errorf("tabLabel(%q) = %q, want %q", c.name, got, c.want)
		}
	}
	if got := tabLabel("", nil); got != "agent-1" {
		t.Errorf("tabLabel in an empty workspace = %q", got)
	}
}
