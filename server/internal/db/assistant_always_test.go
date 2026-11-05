package db

import (
	"context"
	"strings"
	"testing"
)

// 本会话总是允许 on an assistant conversation: a card is allowed only when every key it names was granted there.
func TestAssistantAlways(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	long := "codex-command:" + strings.Repeat("printf x; ", 2000) // whole commands are keys: longer than an index entry
	if err := d.AllowAlways(ctx, "s1", "Bash", []string{"tool:Bash", long}); err != nil {
		t.Fatal(err)
	}
	if err := d.AllowAlways(ctx, "s1", "Bash", []string{"tool:Bash"}); err != nil { // again: no error
		t.Fatal(err)
	}
	for _, c := range []struct {
		session string
		keys    []string
		want    bool
	}{
		{"s1", []string{"tool:Bash"}, true},
		{"s1", []string{long, "tool:Bash", "tool:Bash"}, true},
		{"s1", []string{"tool:Bash", "tool:Edit"}, false}, // one not granted
		{"s2", []string{"tool:Bash"}, false},              // another conversation
		{"s1", nil, false},
		{"", []string{"tool:Bash"}, false},
	} {
		got, err := d.AlwaysAllowed(ctx, c.session, c.keys)
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("AlwaysAllowed(%q, %d keys) = %v, want %v", c.session, len(c.keys), got, c.want)
		}
	}
}
