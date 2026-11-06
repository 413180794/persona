package transcript

import (
	"strings"
	"testing"
)

// A Claude turn is open from its prompt until its turn_duration line or an interrupt (#2069: the node shows a Claude
// at work from this when herdr says done); a typed-ahead (queued) prompt does not start one, and a Codex rollout's
// turns are its own.
func TestClaudeTurnOpenAndClosed(t *testing.T) {
	var m Meta
	feed := func(lines ...string) {
		for _, l := range lines {
			ParseLine(0, []byte(l), &m)
		}
	}
	check := func(when string, open, closed bool) {
		t.Helper()
		if m.TurnOpen != open || m.TurnClosed != closed {
			t.Fatalf("%s: open %v closed %v, want %v %v", when, m.TurnOpen, m.TurnClosed, open, closed)
		}
	}
	check("nothing read", false, false)
	feed(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"mid-turn tail"}]},"uuid":"a0","timestamp":"2026-01-01T00:00:00Z"}`)
	check("a tail from mid-turn", false, false)
	feed(`{"type":"user","message":{"role":"user","content":"look for the bug"},"uuid":"u1","timestamp":"2026-01-01T00:00:01Z"}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Grep","input":{"pattern":"x"}}]},"uuid":"a1","parentUuid":"u1","timestamp":"2026-01-01T00:00:02Z"}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"x.go:1"}]},"uuid":"r1","parentUuid":"a1","timestamp":"2026-01-01T00:00:03Z"}`)
	check("a prompt, a tool call and its result", true, false)
	feed(`{"type":"system","subtype":"turn_duration","uuid":"d1","parentUuid":"a1","durationMs":2000,"timestamp":"2026-01-01T00:00:04Z"}`)
	check("turn_duration", false, true)
	feed(`{"type":"user","message":{"role":"user","content":"and the other one"},"uuid":"u2","timestamp":"2026-01-01T00:00:05Z"}`)
	check("the next prompt", true, false)
	feed(`{"type":"user","message":{"role":"user","content":"[Request interrupted by user]"},"uuid":"u3","timestamp":"2026-01-01T00:00:06Z"}`)
	check("an interrupt", false, true)

	var c Meta
	ParseLine(0, []byte(`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"x","cwd":"/work/project"}}`), &c)
	ParseLine(0, []byte(`{"timestamp":"2026-01-01T00:00:01Z","type":"event_msg","payload":{"type":"task_complete"}}`), &c)
	ParseLine(0, []byte(`{"timestamp":"2026-01-01T00:00:02Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}}`), &c)
	if c.TurnOpen || !c.TurnClosed {
		t.Fatalf("Codex: open %v closed %v, want its own turn (closed)", c.TurnOpen, c.TurnClosed)
	}
}

// A local command typed in a Claude that finished (/effort, /model, "! cmd"; the phone's thinking switch types /effort
// too) is no turn: its local output ends what its line started, and the session stays done (#4661). The lines are
// Claude Code's own (2.1.318; /model as 2.1.287's system line), with ids and paths replaced.
func TestClaudeLocalCommandIsNoTurn(t *testing.T) {
	const done = `{"type":"user","message":{"role":"user","content":"ping"},"uuid":"u1","timestamp":"2026-11-04T09:20:00Z"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"pong"}]},"uuid":"a1","parentUuid":"u1","timestamp":"2026-11-04T09:20:01Z"}
{"type":"system","subtype":"turn_duration","uuid":"d1","parentUuid":"a1","durationMs":1000,"timestamp":"2026-11-04T09:20:02Z"}`
	effort := `{"parentUuid":"d1","isSidechain":false,"promptId":"p2","type":"user","message":{"role":"user","content":"<local-command-caveat>The command below was run directly in Claude Code, not sent to you as a request, and its output goes straight to the user. It's recorded here as context for later messages.</local-command-caveat>"},"isMeta":true,"uuid":"c2","timestamp":"2026-11-04T09:21:48.375Z","userType":"external","entrypoint":"cli","cwd":"/work/project","version":"2.1.318"}
{"parentUuid":"c2","isSidechain":false,"promptId":"p2","type":"user","message":{"role":"user","content":"<command-name>/effort</command-name>\n            <command-message>effort</command-message>\n            <command-args>medium</command-args>"},"uuid":"u2","timestamp":"2026-11-04T09:21:48.375Z","userType":"external","entrypoint":"cli","cwd":"/work/project","version":"2.1.318"}
{"parentUuid":"u2","isSidechain":false,"promptId":"p2","type":"user","message":{"role":"user","content":"<local-command-stdout>Set effort level to medium: Balanced reasoning (on supported models)</local-command-stdout>"},"uuid":"o2","timestamp":"2026-11-04T09:21:48.375Z","userType":"external","entrypoint":"cli","cwd":"/work/project","version":"2.1.318"}`
	model := `{"parentUuid":"d1","isSidechain":false,"type":"system","subtype":"local_command","content":"<command-name>/model</command-name>\n            <command-message>model</command-message>\n            <command-args></command-args>","level":"info","timestamp":"2026-11-04T09:21:50Z","uuid":"u3","isMeta":false,"userType":"external","entrypoint":"cli","cwd":"/work/project","version":"2.1.287"}
{"parentUuid":"u3","isSidechain":false,"type":"system","subtype":"local_command","content":"<local-command-stdout>Kept model as ` + "`Model 5`" + `</local-command-stdout>","level":"info","timestamp":"2026-11-04T09:21:50Z","uuid":"o3","isMeta":false,"commandRun":{"command":"model","args":""},"userType":"external","entrypoint":"cli","cwd":"/work/project","version":"2.1.287"}`
	bang := `{"parentUuid":"d1","isSidechain":false,"promptId":"p4","type":"user","message":{"role":"user","content":"<bash-input> ls</bash-input>"},"uuid":"u4","timestamp":"2026-11-04T09:21:52.186Z","userType":"external","entrypoint":"cli","cwd":"/work/project","version":"2.1.318"}
{"parentUuid":"u4","isSidechain":false,"promptId":"p4","type":"user","message":{"role":"user","content":"<bash-stdout>README.md</bash-stdout><bash-stderr></bash-stderr>"},"uuid":"o4","timestamp":"2026-11-04T09:21:52.237Z","turnOrigin":"human","userType":"external","entrypoint":"cli","cwd":"/work/project","version":"2.1.318"}`
	prompt := `{"parentUuid":"d1","type":"user","message":{"role":"user","content":"now fix it"},"uuid":"u5","timestamp":"2026-11-04T09:22:00Z"}`
	feed := func(m *Meta, lines ...string) {
		for _, block := range lines {
			for _, l := range strings.Split(block, "\n") {
				ParseLine(0, []byte(l), m)
			}
		}
	}
	for _, c := range []struct {
		name         string
		lines        []string
		open, closed bool
	}{
		{"/effort", []string{done, effort}, false, true},
		{"/model", []string{done, model}, false, true},
		{"! command", []string{done, bang}, false, true},
		{"all three", []string{done, effort, model, bang}, false, true},
		{"a message to the agent", []string{done, prompt}, true, false},
		{"a message after a local command", []string{done, effort, prompt}, true, false},
		{"a local command inside the agent's turn", []string{done, prompt, bang}, true, false},
		{"a tail starting at the command", []string{effort}, false, false},
	} {
		var m Meta
		feed(&m, c.lines...)
		if m.TurnOpen != c.open || m.TurnClosed != c.closed {
			t.Errorf("%s: open %v closed %v, want %v %v", c.name, m.TurnOpen, m.TurnClosed, c.open, c.closed)
		}
	}

	// A skill or a custom command expands into a prompt the agent answers: no local output, a turn as before.
	var m Meta
	feed(&m, done, `{"type":"user","message":{"role":"user","content":"<command-message>review is running…</command-message>\n<command-name>/review</command-name>"},"uuid":"u6","parentUuid":"d1","timestamp":"2026-11-04T09:23:00Z"}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"Review the current diff."}]},"isMeta":true,"uuid":"u7","parentUuid":"u6","timestamp":"2026-11-04T09:23:00Z"}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Looking at the diff."}]},"uuid":"a6","parentUuid":"u7","timestamp":"2026-11-04T09:23:01Z"}`)
	if !m.TurnOpen || m.TurnClosed {
		t.Errorf("a skill: open %v closed %v, want an open turn", m.TurnOpen, m.TurnClosed)
	}
}
