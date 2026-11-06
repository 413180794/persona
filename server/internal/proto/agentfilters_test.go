package proto

import "testing"

func TestAgentFilterMatches(t *testing.T) {
	f := AgentFilter{Agents: []string{"claude"}, AddedDirs: []string{"~/.bot/bundles"}, FirstMessagePrefixes: []string{"You are a bot of the platform"}}
	codex := AgentFilter{Agents: []string{"codex"}, Originators: []string{"codex-bot"}}
	cases := []struct {
		name  string
		f     AgentFilter
		agent string
		m     SessionMarks
		want  bool
	}{
		{"the directory", f, "claude", SessionMarks{AddedDirs: []string{"/home/me/.bot/bundles"}}, true},
		{"one under it", f, "claude", SessionMarks{AddedDirs: []string{"/home/me/x", "/home/me/.bot/bundles/v5/abc"}}, true},
		{"a sibling with the same start", f, "claude", SessionMarks{AddedDirs: []string{"/home/me/.bot/bundles-old"}}, false},
		{"the prefix, after blanks", f, "claude", SessionMarks{First: "\n  You are a bot of the platform. Go."}, true},
		{"the words later in the prompt", f, "claude", SessionMarks{First: "Why does it say You are a bot of the platform?"}, false},
		{"another agent", f, "codex", SessionMarks{AddedDirs: []string{"/home/me/.bot/bundles"}}, false},
		{"the originator", codex, "codex", SessionMarks{Originator: "codex-bot"}, true},
		{"another originator", codex, "codex", SessionMarks{Originator: "persona"}, false},
		{"no marks", codex, "codex", SessionMarks{}, false},
	}
	for _, c := range cases {
		if got := c.f.Matches(c.agent, c.m, "/home/me"); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	if AgentFiltered(nil, "codex", SessionMarks{Originator: "codex-bot"}, "") {
		t.Error("no filters take nothing")
	}
}
